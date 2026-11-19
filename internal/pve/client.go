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
	"strings"
	"time"
)

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
	ID      string
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

type VNCTicket struct {
	Port         int
	Ticket, User string
}

type RRDPoint struct {
	Time                                                 int64
	CPU, Mem, MaxMem, NetIn, NetOut, DiskRead, DiskWrite float64
}

type Client interface {
	Version(ctx context.Context) (string, error)
	Nodes(ctx context.Context) ([]Node, error)
	Storages(ctx context.Context, node string) ([]Storage, error)
	VMs(ctx context.Context) ([]VM, error)
	NextID(ctx context.Context) (int, error)
	Clone(ctx context.Context, node string, tmpl int, p CloneParams) (UPID, error)
	WaitTask(ctx context.Context, node string, upid UPID) error
	Config(ctx context.Context, node string, vmid int) (map[string]string, error)
	SetConfig(ctx context.Context, node string, vmid int, kv map[string]string) error
	ResizeDisk(ctx context.Context, node string, vmid int, disk string, sizeGB int) error
	SetFirewall(ctx context.Context, node string, vmid int, ips []netip.Addr) error
	Power(ctx context.Context, node string, vmid int, op PowerOp) (UPID, error)
	Destroy(ctx context.Context, node string, vmid int) (UPID, error)
	UploadISO(ctx context.Context, node, storage, filename string, r io.Reader) error
	DeleteVolume(ctx context.Context, node, storage, volid string) error
	VNCProxy(ctx context.Context, node string, vmid int) (VNCTicket, error)
	VNCWebsocketURL(node string, vmid int, t VNCTicket) string
	RRD(ctx context.Context, node string, vmid int, timeframe string) ([]RRDPoint, error)
	AuthHeader() http.Header
}

var ErrNotFound = errors.New("pve: not found")

// Error is a non-2xx answer from Proxmox.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("pve: status %d: %s", e.Status, e.Message) }

func (e *Error) Is(target error) bool { return target == ErrNotFound && e.notFound() }

func (e *Error) notFound() bool {
	return e.Status == http.StatusNotFound || (e.Status >= 500 && strings.Contains(e.Message, "does not exist"))
}

// ErrFingerprint means the server certificate did not match the pinned fingerprint.
var ErrFingerprint = errors.New("pve: certificate fingerprint mismatch")

var transientMessages = []string{"timeout", "can't lock file", "is locked", "connection refused", "unable to connect"}

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
