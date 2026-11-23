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

package cloudinit_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/netip"
	"strings"
	"testing"

	"github.com/kdomanski/iso9660"
	"github.com/nebla-cloud/nebla/internal/cloudinit"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

const keyA = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA a@example"
const keyB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB b@example"

func input() cloudinit.Input {
	vlan := 20
	return cloudinit.Input{
		InstanceID: "nb-srv-7-1", Hostname: "web", SSHKeys: []string{keyA}, MAC: "BC:24:11:0A:1B:2C",
		IPs: []ipam.Assignment{
			{Family: "ipv4", IP: netip.MustParseAddr("10.0.0.10"), Prefix: 24, Gateway: netip.MustParseAddr("10.0.0.1"),
				DNS: []netip.Addr{netip.MustParseAddr("1.1.1.1")}, Bridge: "vmbr0", VLAN: &vlan},
			{Family: "ipv6", IP: netip.MustParseAddr("2001:db8::10"), Prefix: 64, Gateway: netip.MustParseAddr("2001:db8::1"),
				DNS: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("1.1.1.1")}, Bridge: "vmbr0"},
		},
	}
}

func readISO(t *testing.T, in cloudinit.Input) (string, map[string]string) {
	t.Helper()
	r, err := cloudinit.Build(in)
	require.NoError(t, err)
	raw, err := io.ReadAll(r)
	require.NoError(t, err)
	img, err := iso9660.OpenImage(bytes.NewReader(raw))
	require.NoError(t, err)
	label, err := img.Label()
	require.NoError(t, err)
	root, err := img.RootDir()
	require.NoError(t, err)
	kids, err := root.GetChildren()
	require.NoError(t, err)
	files := map[string]string{}
	for _, f := range kids {
		b, err := io.ReadAll(f.Reader())
		require.NoError(t, err)
		files[f.Name()] = string(b)
	}
	return label, files
}

func TestISOContents(t *testing.T) {
	label, files := readISO(t, input())
	require.Equal(t, "cidata", label)
	require.Len(t, files, 3)
	require.Contains(t, files, "meta-data")
	require.Contains(t, files, "user-data")
	require.Contains(t, files, "network-config")

	var meta map[string]string
	require.NoError(t, yaml.Unmarshal([]byte(files["meta-data"]), &meta))
	require.Equal(t, map[string]string{"instance-id": "nb-srv-7-1", "local-hostname": "web"}, meta)

	var net struct {
		Version   int `yaml:"version"`
		Ethernets map[string]struct {
			Match struct {
				MAC string `yaml:"macaddress"`
			} `yaml:"match"`
			SetName   string   `yaml:"set-name"`
			DHCP4     bool     `yaml:"dhcp4"`
			DHCP6     bool     `yaml:"dhcp6"`
			Addresses []string `yaml:"addresses"`
			Routes    []struct {
				To  string `yaml:"to"`
				Via string `yaml:"via"`
			} `yaml:"routes"`
			Nameservers struct {
				Addresses []string `yaml:"addresses"`
			} `yaml:"nameservers"`
		} `yaml:"ethernets"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(files["network-config"]), &net), files["network-config"])
	require.Equal(t, 2, net.Version)
	require.Len(t, net.Ethernets, 1)
	for _, eth := range net.Ethernets {
		require.Equal(t, "bc:24:11:0a:1b:2c", eth.Match.MAC)
		require.Equal(t, []string{"10.0.0.10/24", "2001:db8::10/64"}, eth.Addresses)
		require.Len(t, eth.Routes, 2)
		require.Equal(t, "default", eth.Routes[0].To)
		require.Equal(t, "10.0.0.1", eth.Routes[0].Via)
		require.Equal(t, "2001:db8::1", eth.Routes[1].Via)
		require.Equal(t, []string{"1.1.1.1", "2606:4700:4700::1111"}, eth.Nameservers.Addresses)
		require.False(t, eth.DHCP4)
	}

	require.True(t, strings.HasPrefix(files["user-data"], "#cloud-config\n"), files["user-data"])
	var ud map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(files["user-data"]), &ud))
	require.Equal(t, []any{keyA}, ud["ssh_authorized_keys"])
}

func userData(t *testing.T, in cloudinit.Input) string {
	t.Helper()
	_, files := readISO(t, in)
	return files["user-data"]
}

func TestUserDataMergesKeys(t *testing.T) {
	in := input()
	in.SSHKeys = []string{keyA, keyB}
	in.UserData = "#cloud-config\npackages: [htop]\nssh_authorized_keys:\n  - " + keyB + "\n"
	ud := userData(t, in)
	require.True(t, strings.HasPrefix(ud, "#cloud-config\n"), ud)
	var got struct {
		Packages []string `yaml:"packages"`
		Keys     []string `yaml:"ssh_authorized_keys"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(ud), &got), ud)
	require.Equal(t, []string{"htop"}, got.Packages)
	require.Equal(t, []string{keyB, keyA}, got.Keys, "existing keys kept, new ones appended without duplicates")

	in.UserData = "#cloud-config\n"
	require.NoError(t, yaml.Unmarshal([]byte(userData(t, in)), &got))
	require.Equal(t, []string{keyA, keyB}, got.Keys)

	in.UserData = "#cloud-config\n- not\n- a map\n"
	_, err := cloudinit.Build(in)
	require.Error(t, err)
}

func TestUserDataScriptBecomesMultipart(t *testing.T) {
	in := input()
	in.UserData = "#!/bin/sh\necho hi\n"
	ud := userData(t, in)
	header, rest, ok := strings.Cut(ud, "\n\n")
	require.True(t, ok, ud)
	var ct string
	for _, l := range strings.Split(header, "\n") {
		if v, ok := strings.CutPrefix(l, "Content-Type: "); ok {
			ct = v
		}
	}
	mt, params, err := mime.ParseMediaType(ct)
	require.NoError(t, err)
	require.Equal(t, "multipart/mixed", mt)
	mr := multipart.NewReader(strings.NewReader(rest), params["boundary"])
	var types, bodies []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		b, err := io.ReadAll(p)
		require.NoError(t, err)
		if p.Header.Get("Content-Transfer-Encoding") == "base64" {
			b, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(string(b), "\r\n", ""))
			require.NoError(t, err)
		}
		mt, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		types = append(types, mt)
		bodies = append(bodies, string(b))
	}
	require.Equal(t, []string{"text/cloud-config", "text/x-shellscript"}, types)
	require.Contains(t, bodies[0], keyA)
	require.Equal(t, in.UserData, bodies[1])
}

func TestNetworkConfigFloating(t *testing.T) {
	in := input()
	in.Floating = []netip.Addr{netip.MustParseAddr("192.0.2.7"), netip.MustParseAddr("2001:db8:1::7")}
	_, files := readISO(t, in)
	const want = `ethernets:
    eth0:
        match:
            macaddress: bc:24:11:0a:1b:2c
        set-name: eth0
        dhcp4: false
        dhcp6: false
        addresses:
            - 10.0.0.10/24
            - 2001:db8::10/64
            - 192.0.2.7/32
            - 2001:db8:1::7/128
        routes:
            - to: default
              via: 10.0.0.1
            - to: default
              via: 2001:db8::1
        nameservers:
            addresses:
                - 1.1.1.1
                - 2606:4700:4700::1111
version: 2
`
	require.Equal(t, want, files["network-config"])
}

func TestNetworkFingerprintStable(t *testing.T) {
	fp, err := cloudinit.NetworkFingerprint(input())
	require.NoError(t, err)
	again, err := cloudinit.NetworkFingerprint(input())
	require.NoError(t, err)
	require.Equal(t, fp, again)

	in := input()
	in.Floating = []netip.Addr{netip.MustParseAddr("203.0.113.5")}
	withFIP, err := cloudinit.NetworkFingerprint(in)
	require.NoError(t, err)
	require.NotEqual(t, fp, withFIP)

	in = input()
	in.Hostname, in.UserData, in.InstanceID = "other", "#!/bin/sh\necho hi\n", "nb-srv-7-9"
	same, err := cloudinit.NetworkFingerprint(in)
	require.NoError(t, err)
	require.Equal(t, fp, same)

	in = input()
	in.Private = []cloudinit.PrivateNIC{{MAC: "BC:24:11:AA:00:01", Index: 1, Addr: netip.MustParsePrefix("10.9.0.2/24"), MTU: 1500}}
	withPriv, err := cloudinit.NetworkFingerprint(in)
	require.NoError(t, err)
	require.NotEqual(t, fp, withPriv)
}

func TestNetworkConfigPrivate(t *testing.T) {
	in := input()
	in.Private = []cloudinit.PrivateNIC{
		{MAC: "BC:24:11:AA:00:01", Index: 1, Addr: netip.MustParsePrefix("10.9.0.2/24"), MTU: 1450},
		{MAC: "BC:24:11:AA:00:02", Index: 2, Addr: netip.MustParsePrefix("172.16.0.5/16"), MTU: 1500},
	}
	_, files := readISO(t, in)
	const want = `ethernets:
    eth0:
        match:
            macaddress: bc:24:11:0a:1b:2c
        set-name: eth0
        dhcp4: false
        dhcp6: false
        addresses:
            - 10.0.0.10/24
            - 2001:db8::10/64
        routes:
            - to: default
              via: 10.0.0.1
            - to: default
              via: 2001:db8::1
        nameservers:
            addresses:
                - 1.1.1.1
                - 2606:4700:4700::1111
    eth1:
        match:
            macaddress: bc:24:11:aa:00:01
        set-name: eth1
        dhcp4: false
        dhcp6: false
        addresses:
            - 10.9.0.2/24
        mtu: 1450
    eth2:
        match:
            macaddress: bc:24:11:aa:00:02
        set-name: eth2
        dhcp4: false
        dhcp6: false
        addresses:
            - 172.16.0.5/16
version: 2
`
	require.Equal(t, want, files["network-config"])
}
