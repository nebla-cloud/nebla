package servers

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// TagNebla marks every VM the panel manages.
const TagNebla = "nebla"

// ServerTag is the Proxmox tag that ties a VM to a server row.
func ServerTag(serverID int64) string { return fmt.Sprintf("nb-srv-%d", serverID) }

func vmTags(serverID int64) string { return TagNebla + ";" + ServerTag(serverID) }

// SplitTags splits a Proxmox tag list.
func SplitTags(tags string) []string {
	return strings.FieldsFunc(tags, func(r rune) bool { return r == ';' || r == ',' || r == ' ' })
}

func hasTag(tags, want string) bool { return slices.Contains(SplitTags(tags), want) }

// isoName is the cloud-init ISO file name for a server and generation.
func isoName(serverID int64, gen int) string { return fmt.Sprintf("nb-srv-%d-%d.iso", serverID, gen) }

func isoVolid(storage string, serverID int64, gen int) string {
	return storage + ":iso/" + isoName(serverID, gen)
}

// permanentError fails the job without retry; msg is shown on the action.
type permanentError struct{ msg string }

func (e *permanentError) Error() string { return e.msg }

func permanent(format string, a ...any) error { return &permanentError{fmt.Sprintf(format, a...)} }

// transientError is retried like a transient Proxmox error.
type transientError struct{ msg string }

func (e *transientError) Error() string { return e.msg }

func transient(format string, a ...any) error { return &transientError{fmt.Sprintf(format, a...)} }

// CloneProof is what the create job saved about its clone request.
type CloneProof struct{ UPID, Nonce *string }

func newNonce() string { return rand.Text() }

func cloneDescription(serverID int64, nonce string) string {
	return "nebla " + ServerTag(serverID) + " " + nonce
}

// hasNonce reports whether cfg's description is exactly the clone description we sent.
func hasNonce(cfg map[string]string, serverID int64, nonce string) bool {
	return nonce != "" && strings.TrimSpace(cfg["description"]) == cloneDescription(serverID, nonce)
}

// upidNode is the node field of a UPID ("UPID:node:...").
func upidNode(upid string) string {
	parts := strings.SplitN(upid, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// stepError names the step that failed.
type stepError struct {
	step string
	err  error
}

func (e *stepError) Error() string { return e.step + ": " + e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

// errorMessage is the tenant-safe message behind err, prefixed with the step.
func errorMessage(err error) string {
	var se *stepError
	inner := err
	if errors.As(err, &se) {
		inner = se.err
	}
	msg := "unexpected error"
	var pe *pve.Error
	var te *pve.TaskError
	var perm *permanentError
	var ue *url.Error
	var ne net.Error
	switch {
	case errors.As(inner, &perm):
		msg = perm.msg
	case errors.As(inner, &te):
		msg = te.ExitStatus
	case errors.As(inner, &pe):
		msg = pe.Message
	case errors.As(inner, &ue), errors.As(inner, &ne), errors.Is(inner, context.DeadlineExceeded):
		msg = "could not reach Proxmox"
	}
	if se != nil {
		return se.step + ": " + msg
	}
	return msg
}

// findVM returns the VM with vmid from the cluster listing.
func findVM(ctx context.Context, pc pve.Client, vmid int) (pve.VM, bool, error) {
	vms, err := pc.VMs(ctx)
	if err != nil {
		return pve.VM{}, false, err
	}
	i := slices.IndexFunc(vms, func(v pve.VM) bool { return v.VMID == vmid })
	if i < 0 {
		return pve.VM{}, false, nil
	}
	return vms[i], true, nil
}

// templateNode finds the node holding template vmid, preferring prefer.
func templateNode(ctx context.Context, pc pve.Client, vmid int, prefer string) (string, error) {
	vms, err := pc.VMs(ctx)
	if err != nil {
		return "", err
	}
	node := ""
	for _, v := range vms {
		if v.VMID == vmid && v.Template && (node == "" || v.Node == prefer) {
			node = v.Node
		}
	}
	if node == "" {
		return "", templateGone(vmid)
	}
	return node, nil
}

func templateGone(vmid int) error {
	return permanent("template %d no longer exists in Proxmox", vmid)
}

// diskBytes parses the size= option of a disk config value.
func diskBytes(disk string) (int64, bool) {
	for _, p := range strings.Split(disk, ",") {
		sz, ok := strings.CutPrefix(p, "size=")
		if !ok || sz == "" {
			continue
		}
		mult := int64(1)
		switch sz[len(sz)-1] {
		case 'K':
			mult = 1 << 10
		case 'M':
			mult = 1 << 20
		case 'G':
			mult = 1 << 30
		case 'T':
			mult = 1 << 40
		}
		if mult > 1 {
			sz = sz[:len(sz)-1]
		}
		f, err := strconv.ParseFloat(sz, 64)
		if err != nil {
			return 0, false
		}
		return int64(f * float64(mult)), true
	}
	return 0, false
}

// bridgeOf returns the bridge= option of a net config value.
func bridgeOf(net string) string {
	for _, p := range strings.Split(net, ",") {
		if b, ok := strings.CutPrefix(p, "bridge="); ok {
			return b
		}
	}
	return ""
}

// ownedBy reports whether v carries both the nebla tag and serverID's tag.
func ownedBy(v pve.VM, serverID int64) bool {
	return !v.Template && hasTag(v.Tags, TagNebla) && hasTag(v.Tags, ServerTag(serverID))
}

// OwnedVM returns the VM at vmid when it carries serverID's tags, else ErrMissing.
func OwnedVM(ctx context.Context, pc pve.Client, serverID int64, vmid int) (pve.VM, error) {
	v, found, err := findVM(ctx, pc, vmid)
	if err != nil {
		return pve.VM{}, err
	}
	if !found || !ownedBy(v, serverID) {
		return pve.VM{}, ErrMissing
	}
	return v, nil
}

const cleanupTimeout = 2 * time.Minute

// errUnproven means an untagged VM sits at the vmid and nothing was saved that could prove it ours.
var errUnproven = errors.New("no clone proof")

// destroyVM removes vmid if proven serverID's; nil means no VM of ours is left there.
func destroyVM(ctx context.Context, pc pve.Client, serverID int64, vmid int, proof CloneProof) error {
	v, ok, err := findVM(ctx, pc, vmid)
	if err != nil || !ok {
		return err
	}
	if !ownedBy(v, serverID) {
		ours, err := provenOurs(ctx, pc, serverID, v, proof)
		if err != nil {
			return err
		}
		if !ours {
			return nil
		}
		if err := pc.SetConfig(ctx, v.Node, vmid, map[string]string{"tags": vmTags(serverID)}); err != nil {
			return fmt.Errorf("tag vm %d: %w", vmid, err)
		}
	}
	if v.Status == "running" {
		up, err := pc.Power(ctx, v.Node, vmid, pve.PowerStop)
		if err == nil {
			err = pc.WaitTask(ctx, v.Node, up)
		}
		if err != nil {
			return fmt.Errorf("stop vm %d: %w", vmid, err)
		}
	}
	up, err := pc.Destroy(ctx, v.Node, vmid)
	if err == nil {
		err = pc.WaitTask(ctx, v.Node, up)
	}
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("destroy vm %d: %w", vmid, err)
	}
	return nil
}

// destroyOrForget is destroyVM where an unproven untagged VM counts as someone else's.
func destroyOrForget(ctx context.Context, pc pve.Client, serverID int64, vmid int, proof CloneProof) error {
	if err := destroyVM(ctx, pc, serverID, vmid, proof); err != nil && !errors.Is(err, errUnproven) {
		return err
	}
	return nil
}

// provenOurs decides an untagged VM at our vmid; an error means it could not be decided.
func provenOurs(ctx context.Context, pc pve.Client, serverID int64, v pve.VM, proof CloneProof) (bool, error) {
	if hasTag(v.Tags, TagNebla) && !hasTag(v.Tags, ServerTag(serverID)) {
		// Another panel VM: never ours, whatever was saved.
		return false, nil
	}
	if proof.UPID != nil {
		err := pc.WaitTask(ctx, upidNode(*proof.UPID), pve.UPID(*proof.UPID))
		switch {
		case errors.Is(err, pve.ErrTaskFailed):
			// Our clone task failed, so the VM there is not ours.
			return false, nil
		case err != nil && (ctx.Err() != nil || proof.Nonce == nil):
			return false, fmt.Errorf("vm %d: clone task unresolved: %w", v.VMID, err)
		}
		// A successful task never proves the VM here now; the nonce below must.
	}
	if proof.Nonce == nil {
		return false, fmt.Errorf("vm %d lacks tags %s and %s: %w", v.VMID, TagNebla, ServerTag(serverID), errUnproven)
	}
	cfg, err := pc.Config(ctx, v.Node, v.VMID)
	if err != nil {
		return false, fmt.Errorf("vm %d: read config: %w", v.VMID, err)
	}
	if cfg["lock"] != "" {
		return false, fmt.Errorf("vm %d is locked (%s)", v.VMID, cfg["lock"])
	}
	return hasNonce(cfg, serverID, *proof.Nonce), nil
}

// deleteISO removes a cloud-init ISO; a missing volume is not an error.
func deleteISO(ctx context.Context, pc pve.Client, node, storage, volid string) error {
	err := pc.DeleteVolume(ctx, node, storage, volid)
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	return err
}

// CleanupVM best-effort removes a server's VM and cloud-init ISO, logging failures; it reports whether the VM is gone.
func CleanupVM(ctx context.Context, log *slog.Logger, pc pve.Client, serverID int64, node string, vmid *int, proof CloneProof, isoStorage, volid string) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	gone := true
	if vmid != nil {
		if err := destroyVM(ctx, pc, serverID, *vmid, proof); err != nil {
			gone = false
			log.Error("cleanup: destroy vm", "server_id", serverID, "vmid", *vmid, "err", err)
		}
	}
	if volid != "" {
		if err := deleteISO(ctx, pc, node, isoStorage, volid); err != nil {
			log.Error("cleanup: delete iso", "server_id", serverID, "volid", volid, "err", err)
		}
	}
	return gone
}
