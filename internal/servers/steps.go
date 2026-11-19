package servers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/pve"
)

// TagNebla marks every VM the panel manages.
const TagNebla = "nebla"

// ServerTag is the Proxmox tag that ties a VM to a server row.
func ServerTag(serverID int64) string { return fmt.Sprintf("nb-srv-%d", serverID) }

func vmTags(serverID int64) string { return TagNebla + ";" + ServerTag(serverID) }

func hasTag(tags, want string) bool {
	return slices.Contains(strings.FieldsFunc(tags, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }), want)
}

// isoName is the cloud-init ISO file name for a server and generation.
func isoName(serverID int64, gen int) string { return fmt.Sprintf("nb-srv-%d-%d.iso", serverID, gen) }

func isoVolid(storage string, serverID int64, gen int) string {
	return storage + ":iso/" + isoName(serverID, gen)
}

// permanentError fails the job without retry; msg is shown on the action.
type permanentError struct{ msg string }

func (e *permanentError) Error() string { return e.msg }

func permanent(format string, a ...any) error { return &permanentError{fmt.Sprintf(format, a...)} }

// stepError names the step that failed.
type stepError struct {
	step string
	err  error
}

func (e *stepError) Error() string { return e.step + ": " + e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

// errorMessage is the Proxmox message behind err, prefixed with the step.
func errorMessage(err error) string {
	msg := err.Error()
	var pe *pve.Error
	var perm *permanentError
	switch {
	case errors.As(err, &perm):
		msg = perm.msg
	case errors.As(err, &pe):
		msg = pe.Message
	}
	var se *stepError
	if errors.As(err, &se) {
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

// ownedBy reports whether v carries serverID's tag.
func ownedBy(v pve.VM, serverID int64) bool {
	return !v.Template && hasTag(v.Tags, ServerTag(serverID))
}

// ourClone also accepts the clone name, for a crash between clone and tagging.
func ourClone(v pve.VM, serverID int64) bool {
	return ownedBy(v, serverID) || (!v.Template && v.Tags == "" && v.Name == ServerTag(serverID))
}

const cleanupTimeout = 2 * time.Minute

// destroyVM stops and destroys vmid if it carries serverID's tag; a missing or foreign VM is left alone.
func destroyVM(ctx context.Context, pc pve.Client, serverID int64, vmid int) error {
	v, ok, err := findVM(ctx, pc, vmid)
	if err != nil || !ok {
		return err
	}
	if !ownedBy(v, serverID) {
		return nil
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
	return err
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
func CleanupVM(ctx context.Context, log *slog.Logger, pc pve.Client, serverID int64, node string, vmid *int, isoStorage, volid string) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	gone := true
	if vmid != nil {
		if err := destroyVM(ctx, pc, serverID, *vmid); err != nil {
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
