// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package cloudinit builds NoCloud seed ISOs.
package cloudinit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/netip"
	"net/textproto"
	"slices"
	"strings"

	"github.com/kdomanski/iso9660"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"go.yaml.in/yaml/v4"
)

const cloudConfigHeader = "#cloud-config"

type Input struct {
	InstanceID, Hostname string
	SSHKeys              []string
	UserData             string
	MAC                  string
	IPs                  []ipam.Assignment
	// Floating are extra single addresses on eth0, with no routes.
	Floating []netip.Addr
}

// Build returns a NoCloud ISO (label cidata) with meta-data, network-config and user-data.
func Build(in Input) (io.Reader, error) {
	meta, err := yaml.Marshal(map[string]string{"instance-id": in.InstanceID, "local-hostname": in.Hostname})
	if err != nil {
		return nil, err
	}
	netcfg, err := networkConfig(in)
	if err != nil {
		return nil, err
	}
	ud, err := userData(in.UserData, in.SSHKeys)
	if err != nil {
		return nil, err
	}
	w, err := iso9660.NewWriter()
	if err != nil {
		return nil, err
	}
	defer w.Cleanup()
	for name, b := range map[string][]byte{"meta-data": meta, "network-config": netcfg, "user-data": ud} {
		if err := w.AddFile(bytes.NewReader(b), name); err != nil {
			return nil, err
		}
	}
	var out bytes.Buffer
	if err := w.WriteTo(&out, "cidata"); err != nil {
		return nil, err
	}
	return &out, nil
}

type route struct {
	To  string `yaml:"to"`
	Via string `yaml:"via"`
}

type ethernet struct {
	Match       map[string]string   `yaml:"match"`
	SetName     string              `yaml:"set-name"`
	DHCP4       bool                `yaml:"dhcp4"`
	DHCP6       bool                `yaml:"dhcp6"`
	Addresses   []string            `yaml:"addresses,omitempty"`
	Routes      []route             `yaml:"routes,omitempty"`
	Nameservers map[string][]string `yaml:"nameservers,omitempty"`
}

func networkConfig(in Input) ([]byte, error) {
	eth := ethernet{Match: map[string]string{"macaddress": strings.ToLower(in.MAC)}, SetName: "eth0"}
	var dns []string
	for _, a := range in.IPs {
		eth.Addresses = append(eth.Addresses, fmt.Sprintf("%s/%d", a.IP, a.Prefix))
		if a.Gateway.IsValid() {
			eth.Routes = append(eth.Routes, route{To: "default", Via: a.Gateway.String()})
		}
		for _, d := range a.DNS {
			if s := d.String(); !slices.Contains(dns, s) {
				dns = append(dns, s)
			}
		}
	}
	for _, a := range in.Floating {
		eth.Addresses = append(eth.Addresses, netip.PrefixFrom(a, a.BitLen()).String())
	}
	if len(dns) > 0 {
		eth.Nameservers = map[string][]string{"addresses": dns}
	}
	return yaml.Marshal(map[string]any{"version": 2, "ethernets": map[string]ethernet{"eth0": eth}})
}

// NetworkFingerprint is the hex sha256 of the network-config the seed ISO carries.
func NetworkFingerprint(in Input) (string, error) {
	b, err := networkConfig(in)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func isCloudConfig(ud string) bool {
	first, _, _ := strings.Cut(ud, "\n")
	return strings.TrimSpace(first) == cloudConfigHeader
}

// ValidateUserData checks user data builds; #cloud-config must be a YAML mapping.
func ValidateUserData(ud string) error {
	_, err := userData(ud, nil)
	return err
}

func userData(ud string, keys []string) ([]byte, error) {
	switch {
	case strings.TrimSpace(ud) == "":
		return keysConfig(keys)
	case isCloudConfig(ud):
		return mergeKeys(ud, keys)
	default:
		return multipartUserData(ud, keys)
	}
}

func keysConfig(keys []string) ([]byte, error) {
	b, err := yaml.Marshal(map[string][]string{"ssh_authorized_keys": append([]string{}, keys...)})
	if err != nil {
		return nil, err
	}
	return append([]byte(cloudConfigHeader+"\n"), b...), nil
}

var errNotMapping = errors.New("cloudinit: #cloud-config user data must be a YAML mapping")

// mergeKeys adds keys to ssh_authorized_keys, keeping the rest of the document as written.
func mergeKeys(ud string, keys []string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(ud), &doc); err != nil {
		return nil, fmt.Errorf("cloudinit: user data is not valid YAML: %w", err)
	}
	if doc.Kind == 0 {
		return keysConfig(keys)
	}
	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		return keysConfig(keys)
	}
	if root.Kind != yaml.MappingNode {
		return nil, errNotMapping
	}
	var list *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "ssh_authorized_keys" {
			list = root.Content[i+1]
		}
	}
	if list == nil {
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ssh_authorized_keys"}, list)
	}
	if list.Kind != yaml.SequenceNode {
		return nil, errors.New("cloudinit: ssh_authorized_keys must be a list")
	}
	list.Style = 0
	for _, k := range keys {
		if !slices.ContainsFunc(list.Content, func(n *yaml.Node) bool { return n.Value == k }) {
			list.Content = append(list.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k})
		}
	}
	b, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(cloudConfigHeader+"\n"), b...), nil
}

var partTypes = []struct{ prefix, typ string }{
	{"#!", "text/x-shellscript"},
	{"#include", "text/x-include-url"},
	{"#cloud-config-archive", "text/cloud-config-archive"},
	{"#cloud-boothook", "text/cloud-boothook"},
	{"#part-handler", "text/part-handler"},
	{"## template: jinja", "text/jinja2"},
}

func partType(ud string) string {
	for _, p := range partTypes {
		if strings.HasPrefix(ud, p.prefix) {
			return p.typ
		}
	}
	return "text/plain"
}

func multipartUserData(ud string, keys []string) ([]byte, error) {
	cfg, err := keysConfig(keys)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	parts := []struct {
		typ, name string
		data      []byte
	}{{"text/cloud-config", "nebla-keys.cfg", cfg}, {partType(ud), "user-data", []byte(ud)}}
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", p.typ+`; charset="utf-8"`)
		h.Set("MIME-Version", "1.0")
		h.Set("Content-Disposition", `attachment; filename="`+p.name+`"`)
		pw, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(p.data); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	head := fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\nMIME-Version: 1.0\n\n", mw.Boundary())
	return append([]byte(head), body.Bytes()...), nil
}
