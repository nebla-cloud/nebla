package servers

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
)

// volumeDigestTries bounds the volume config writes retried on a digest mismatch.
const volumeDigestTries = 5

// attachedVolumes lists the volumes recorded in serverID's VM config.
func attachedVolumes(ctx context.Context, c *ent.Client, serverID int64) ([]*ent.Volume, error) {
	return c.Volume.Query().Where(volume.ServerID(serverID), volume.DeviceNEQ("")).Order(ent.Asc(volume.FieldID)).All(ctx)
}

// present drops volumes whose disk is missing, so a gone disk never blocks a plug.
func present(vols []*ent.Volume) []*ent.Volume {
	return slices.DeleteFunc(vols, func(v *ent.Volume) bool { return v.Status == volume.StatusMissing })
}

// plugVolumes sets each volume's key on the VM; a key holding anything else is refused.
func plugVolumes(ctx context.Context, pc pve.Client, node string, vmid int, vols []*ent.Volume) error {
	if len(vols) == 0 {
		return nil
	}
	return writeVolumeKeys(ctx, pc, node, vmid, func(cfg map[string]string) (map[string]string, error) {
		kv := map[string]string{}
		for _, v := range vols {
			cur, ok := cfg[v.Device]
			if id, ours := vref.Parse(cur); ours && id == v.ID {
				continue
			}
			if ok {
				return nil, permanent("vm %d already uses %s for something else", vmid, v.Device)
			}
			kv[v.Device] = vref.ConfigValue(v.Storage, v.ID)
		}
		return kv, nil
	})
}

// unplugVolumes deletes each volume's key from the VM, only where it still references that volume.
func unplugVolumes(ctx context.Context, pc pve.Client, node string, vmid int, vols []*ent.Volume) error {
	if len(vols) == 0 {
		return nil
	}
	return writeVolumeKeys(ctx, pc, node, vmid, func(cfg map[string]string) (map[string]string, error) {
		var del []string
		for _, v := range vols {
			if id, ours := vref.Parse(cfg[v.Device]); ours && id == v.ID {
				del = append(del, v.Device)
			}
		}
		if len(del) == 0 {
			return nil, nil
		}
		return map[string]string{"delete": strings.Join(del, ",")}, nil
	})
}

// writeVolumeKeys writes what change derives from the current config, with its digest; no change means done.
func writeVolumeKeys(ctx context.Context, pc pve.Client, node string, vmid int, change func(map[string]string) (map[string]string, error)) error {
	for range volumeDigestTries {
		cfg, err := pc.Config(ctx, node, vmid)
		if err != nil {
			return err
		}
		kv, err := change(cfg)
		if err != nil || len(kv) == 0 {
			return err
		}
		err = pc.SetConfigDigest(ctx, node, vmid, kv, cfg["digest"])
		if !errors.Is(err, pve.ErrDigestMismatch) {
			return err
		}
	}
	return transient("vm %d config kept changing", vmid)
}

// unplugServerVolumes removes the server's volumes from vmid when that VM is ours, best effort: the destroy keeps foreign disks.
// Only a retryable error is returned; any other is logged and the caller goes on to the destroy.
func unplugServerVolumes(ctx context.Context, pc pve.Client, log *slog.Logger, serverID int64, vmid int, vols []*ent.Volume) error {
	if len(vols) == 0 {
		return nil
	}
	err := func() error {
		vm, err := OwnedVM(ctx, pc, serverID, vmid)
		if errors.Is(err, ErrMissing) {
			return nil
		}
		if err != nil {
			return err
		}
		return unplugVolumes(ctx, pc, vm.Node, vmid, vols)
	}()
	if err == nil || retryable(err) || ctx.Err() != nil {
		return err
	}
	keys := make([]string, len(vols))
	for i, v := range vols {
		keys[i] = v.Device
	}
	log.Warn("unplug volumes failed; destroying the vm anyway", "server_id", serverID, "vmid", vmid, "keys", strings.Join(keys, ","), "err", err)
	return nil
}

// plugNewVM puts the server's volumes into the new VM before it becomes the server's.
func (r *rebuildRun) plugNewVM(ctx context.Context) error {
	vols, err := attachedVolumes(ctx, r.w.o.Ent, r.row.ID)
	if err != nil || r.b.st.Vmid == nil {
		return err
	}
	return plugVolumes(ctx, r.b.pc, r.row.Node, *r.b.st.Vmid, present(vols))
}

// unplugOldVM takes the server's volumes out of the old VM before it is destroyed.
func (r *rebuildRun) unplugOldVM(ctx context.Context) error {
	vols, err := attachedVolumes(ctx, r.w.o.Ent, r.row.ID)
	if err != nil || r.b.st.Vmid == nil {
		return err
	}
	return unplugServerVolumes(ctx, r.b.pc, r.w.o.Logger, r.row.ID, *r.b.st.Vmid, vols)
}

// detachVolumesTx frees the server's volumes in the tx that deletes it; the server row is locked first.
func detachVolumesTx(ctx context.Context, etx *ent.Client, serverID int64) error {
	if _, err := etx.Server.Query().Where(server.ID(serverID)).ForUpdate().Only(ctx); err != nil {
		return err
	}
	if err := etx.Volume.Update().Where(volume.ServerID(serverID), volume.StatusNEQ(volume.StatusMissing)).
		ClearServerID().SetDevice("").SetStatus(volume.StatusAvailable).Exec(ctx); err != nil {
		return err
	}
	// A missing disk stays missing.
	return etx.Volume.Update().Where(volume.ServerID(serverID)).ClearServerID().SetDevice("").Exec(ctx)
}
