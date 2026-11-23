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

package pve

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// FirewallRule is one VM firewall rule as Proxmox lists it.
type FirewallRule struct {
	Pos                                                      int
	Type, Action, Iface, Proto, Dport, Source, Dest, Comment string
	Enable                                                   bool
	Digest                                                   string // of the rule list it was read from
}

// Conn holds the connection settings for one Proxmox endpoint.
type Conn struct {
	BaseURL, TokenID, Secret string
	InsecureTLS              bool
	CAFingerprint            string        // SHA-256 of the leaf cert, colon-hex
	PollInterval             time.Duration // task poll period; 500ms when zero
}

type UPID string

type PowerOp string

const (
	PowerStart    PowerOp = "start"
	PowerStop     PowerOp = "stop"
	PowerShutdown PowerOp = "shutdown"
	PowerReboot   PowerOp = "reboot"
	PowerReset    PowerOp = "reset"
)

type Node struct {
	Name, Status string
	MaxCPU       int
	MaxMem, Mem  int64
	CPU          float64
}

type Storage struct {
	ID string
	// Type is the storage plugin, such as lvmthin, dir or nfs.
	Type    string
	Content []string
	Shared  bool
	Avail   int64
}

type VM struct {
	VMID               int
	Node, Name, Status string
	Tags               string
	Template           bool
}

type CloneParams struct {
	NewID       int
	Name        string
	Target      string
	Storage     string
	Full        bool
	Description string
}

// DownloadParams describes a storage download-url request.
type DownloadParams struct{ URL, Filename, Content, Checksum, Algorithm string }

type VNCTicket struct {
	Port         int
	Ticket, User string
}

// RRDPoint is one RRD sample; a nil value is a gap (null or NaN in Proxmox).
type RRDPoint struct {
	Time                                                 int64
	CPU, Mem, MaxMem, NetIn, NetOut, DiskRead, DiskWrite *float64
}

// StorageItem is one volume in a storage's content listing.
type StorageItem struct {
	Volid string
	Size  int64 // bytes
	VMID  int
}

// BackupItem is one vzdump archive in a storage's content listing.
type BackupItem struct {
	Volid string
	VMID  int
	Size  int64 // bytes
	Ctime time.Time
	Notes string
}

// PendingItem is one row of a VM's pending config; Pending is the unapplied value.
type PendingItem struct {
	Key, Value, Pending string
	Delete              int
}

// SDNZone is one SDN zone; empty Nodes means every node.
type SDNZone struct {
	Zone, Type string
	MTU        int
	Nodes      []string
}

type SDNVNet struct {
	VNet, Zone string
	VLANAware  bool
}

// GuestRef names one guest; Kind is "qemu" or "lxc".
type GuestRef struct {
	VMID       int
	Node, Kind string
}

// IPFilterSetFor is the ipset Proxmox's ipfilter checks for the nic-th interface.
func IPFilterSetFor(nic int) string { return "ipfilter-net" + strconv.Itoa(nic) }

type Client interface {
	Version(ctx context.Context) (string, error)
	NodeVersion(ctx context.Context, node string) (string, error)
	Nodes(ctx context.Context) ([]Node, error)
	Storages(ctx context.Context, node string) ([]Storage, error)
	VMs(ctx context.Context) ([]VM, error)
	// GuestExists reports whether any guest, QEMU or LXC, holds vmid in the cluster.
	GuestExists(ctx context.Context, vmid int) (bool, error)
	NextID(ctx context.Context) (int, error)
	Clone(ctx context.Context, node string, tmpl int, p CloneParams) (UPID, error)
	DownloadURL(ctx context.Context, node, storage string, p DownloadParams) (UPID, error)
	CreateVM(ctx context.Context, node string, vmid int, kv map[string]string) (UPID, error)
	// ConvertToTemplate returns "" when Proxmox answers without a task.
	ConvertToTemplate(ctx context.Context, node string, vmid int) (UPID, error)
	WaitTask(ctx context.Context, node string, upid UPID) error
	Config(ctx context.Context, node string, vmid int) (map[string]string, error)
	SetConfig(ctx context.Context, node string, vmid int, kv map[string]string) error
	// SetConfigDigest is SetConfig that fails with ErrDigestMismatch when digest is stale.
	SetConfigDigest(ctx context.Context, node string, vmid int, kv map[string]string, digest string) error
	Pending(ctx context.Context, node string, vmid int) ([]PendingItem, error)
	// AllocVolume creates a volume named filename owned by ownerVMID and returns its volid.
	AllocVolume(ctx context.Context, node, storage string, ownerVMID int, filename string, sizeGB int) (string, error)
	// StorageContent lists volumes; vmid <= 0 means no filter.
	StorageContent(ctx context.Context, node, storage string, vmid int) ([]StorageItem, error)
	// Vzdump starts a snapshot backup of vmid into storage; the archive is kept (remove=0).
	Vzdump(ctx context.Context, node string, vmid int, storage, notes string) (UPID, error)
	// BackupContent lists backup archives; vmid <= 0 means no filter.
	BackupContent(ctx context.Context, node, storage string, vmid int) ([]BackupItem, error)
	// RestoreVM creates vmid from archive with fresh unique properties such as MACs.
	RestoreVM(ctx context.Context, node string, vmid int, archive, storage string) (UPID, error)
	ResizeDisk(ctx context.Context, node string, vmid int, disk string, sizeGB int) error
	SetFirewall(ctx context.Context, node string, vmid int, ips []netip.Addr) error
	// FirewallIPSet lists the IPFilterSet entries; ok is false when the set does not exist.
	FirewallIPSet(ctx context.Context, node string, vmid int) (cidrs []string, ok bool, err error)
	// IPSet lists a named ipset's entries; ok is false when the set does not exist.
	IPSet(ctx context.Context, node string, vmid int, name string) (cidrs []string, ok bool, err error)
	// SetIPSet makes the named ipset hold exactly ips, creating it if missing; firewall options stay untouched.
	SetIPSet(ctx context.Context, node string, vmid int, name string, ips []netip.Addr) error
	// DeleteIPSet removes the set and its entries; a missing set is not an error.
	DeleteIPSet(ctx context.Context, node string, vmid int, name string) error
	// SDNZones and SDNVNets answer empty without SDN.Audit.
	SDNZones(ctx context.Context) ([]SDNZone, error)
	SDNVNets(ctx context.Context) ([]SDNVNet, error)
	SDNSubnets(ctx context.Context, vnet string) ([]string, error)
	// Guests lists QEMU and LXC guests, templates included.
	Guests(ctx context.Context) ([]GuestRef, error)
	// GuestBridges lists the bridge of every net key in the guest's config, pending changes and snapshots.
	GuestBridges(ctx context.Context, g GuestRef) ([]string, error)
	FirewallOptions(ctx context.Context, node string, vmid int) (map[string]string, error)
	SetFirewallOptions(ctx context.Context, node string, vmid int, kv map[string]string) error
	FirewallRules(ctx context.Context, node string, vmid int) ([]FirewallRule, error)
	// AddFirewallRule adds r; Proxmox puts it at the top, shifting the rest down.
	AddFirewallRule(ctx context.Context, node string, vmid int, r FirewallRule) error
	// DeleteFirewallRule fails if digest is set and the rule list changed since it was read.
	DeleteFirewallRule(ctx context.Context, node string, vmid, pos int, digest string) error
	ClusterFirewallEnabled(ctx context.Context) (bool, error)
	Power(ctx context.Context, node string, vmid int, op PowerOp) (UPID, error)
	Destroy(ctx context.Context, node string, vmid int) (UPID, error)
	UploadISO(ctx context.Context, node, storage, filename string, r io.Reader) error
	DeleteVolume(ctx context.Context, node, storage, volid string) error
	// Permissions returns the token's privileges by ACL path, as GET /access/permissions?path= answers.
	Permissions(ctx context.Context, path string) (map[string]map[string]int, error)
	VNCProxy(ctx context.Context, node string, vmid int) (VNCTicket, error)
	VNCWebsocketURL(node string, vmid int, t VNCTicket) string
	RRD(ctx context.Context, node string, vmid int, timeframe string) ([]RRDPoint, error)
	AuthHeader() http.Header
}

// AtLeast reports whether a major.minor[.patch] version is at least major.minor; false if unparsable.
func AtLeast(version string, major, minor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		if len(parts) == 1 {
			parts = append(parts, "0")
		} else {
			return false
		}
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}

var ErrNotFound = errors.New("pve: not found")

// Error is a non-2xx answer from Proxmox.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("pve: status %d: %s", e.Status, e.Message) }

// ErrDigestMismatch means a config write carried a stale digest.
var ErrDigestMismatch = errors.New("pve: config digest mismatch")

func (e *Error) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.notFound()
	case ErrDigestMismatch:
		return strings.Contains(e.Message, "digest")
	}
	return false
}

func (e *Error) notFound() bool {
	return e.Status == http.StatusNotFound || (e.Status >= 500 && strings.Contains(e.Message, "does not exist"))
}

// ErrTaskFailed matches a task that stopped with a non-OK exit status.
var ErrTaskFailed = errors.New("pve: task failed")

// TaskError is a task that finished unsuccessfully.
type TaskError struct {
	UPID       UPID
	ExitStatus string
}

func (e *TaskError) Error() string {
	return fmt.Sprintf("pve: task %s failed: %s", e.UPID, e.ExitStatus)
}

func (e *TaskError) Is(target error) bool { return target == ErrTaskFailed }

// ErrFingerprint means the server certificate did not match the pinned fingerprint.
var ErrFingerprint = errors.New("pve: certificate fingerprint mismatch")

var transientMessages = []string{"timeout", "can't lock file", "is locked", "connection refused", "unable to connect",
	"detected modified configuration"}

// IsTransient reports errors worth retrying: network failures, 502/503/504 and known-transient 500s.
func IsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrFingerprint) {
		return false
	}
	var cv *tls.CertificateVerificationError
	var ua x509.UnknownAuthorityError
	if errors.As(err, &cv) || errors.As(err, &ua) {
		return false
	}
	var pe *Error
	if errors.As(err, &pe) {
		switch {
		case pe.notFound():
			return false
		case pe.Status == 502 || pe.Status == 503 || pe.Status == 504:
			return true
		case pe.Status == 500:
			m := strings.ToLower(pe.Message)
			return slices.ContainsFunc(transientMessages, func(k string) bool { return strings.Contains(m, k) })
		}
		return false
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF)
}
