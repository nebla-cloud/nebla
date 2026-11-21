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

package volumes

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/riverqueue/river"
)

// plug puts row into the server's VM config, records it and applies any deferred growth; progress is set once plugged.
func (o *WorkerOptions) plug(ctx context.Context, pc pve.Client, vms []pve.VM, row *ent.Volume, serverID, actionID int64, progress int) (*ent.Volume, error) {
	srv, err := o.loadServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	vm, err := serverVM(vms, srv)
	if err != nil {
		return nil, err
	}
	row, key, err := o.setKey(ctx, pc, vm, row, srv.ID)
	if err != nil {
		return nil, err
	}
	if err := actions.Progress(ctx, o.Ent, actionID, progress); err != nil {
		return nil, err
	}
	if row.AppliedGB < row.SizeGB {
		if err := pc.ResizeDisk(ctx, vm.Node, vm.VMID, key, row.SizeGB); err != nil {
			return nil, servers.StepError("grow volume", err)
		}
		if row, err = row.Update().SetAppliedGB(row.SizeGB).Save(ctx); err != nil {
			return nil, err
		}
	}
	return row, nil
}

// setKey writes row's virtioN key with the config digest, unless a key already references it.
// The row records the server and key before the write, so a write whose reply is lost is never untracked.
func (o *WorkerOptions) setKey(ctx context.Context, pc pve.Client, vm pve.VM, row *ent.Volume, serverID int64) (*ent.Volume, string, error) {
	for range digestTries {
		cfg, err := pc.Config(ctx, vm.Node, vm.VMID)
		if err != nil {
			return row, "", servers.StepError("read vm config", err)
		}
		if key := ourKey(cfg, row.Volid); key != "" {
			row, err = record(ctx, row, serverID, key)
			return row, key, err
		}
		if err := needsPowerOff(vm, cfg); err != nil {
			return row, "", err
		}
		taken, err := o.Ent.Volume.Query().Where(volume.ServerID(serverID), volume.IDNEQ(row.ID)).Select(volume.FieldDevice).Strings(ctx)
		if err != nil {
			return row, "", err
		}
		key := freeKey(cfg, taken)
		if key == "" {
			return row, "", servers.Permanent("vm %d has no free virtio slot", vm.VMID)
		}
		if row, err = record(ctx, row, serverID, key); err != nil {
			return row, "", err
		}
		err = pc.SetConfigDigest(ctx, vm.Node, vm.VMID, map[string]string{key: vref.ConfigValue(row.Volid, row.ID)}, cfg["digest"])
		if errors.Is(err, pve.ErrDigestMismatch) {
			continue
		}
		if err != nil {
			return row, "", servers.StepError("attach volume", err)
		}
		return row, key, nil
	}
	return row, "", servers.Transient("vm %d config kept changing", vm.VMID)
}

// record saves serverID and key on row unless it holds them already.
func record(ctx context.Context, row *ent.Volume, serverID int64, key string) (*ent.Volume, error) {
	if row.ServerID != nil && *row.ServerID == serverID && row.Device == key {
		return row, nil
	}
	return row.Update().SetServerID(serverID).SetDevice(key).Save(ctx)
}

// unplug deletes row's virtioN key from the VM; an unplug still pending is retried.
func unplug(ctx context.Context, pc pve.Client, vm pve.VM, row *ent.Volume) error {
	removed := false
	for range digestTries {
		cfg, err := pc.Config(ctx, vm.Node, vm.VMID)
		if err != nil {
			return servers.StepError("read vm config", err)
		}
		key := ourKey(cfg, row.Volid)
		if key == "" {
			removed = true
			break
		}
		if err := needsPowerOff(vm, cfg); err != nil {
			return err
		}
		err = pc.SetConfigDigest(ctx, vm.Node, vm.VMID, map[string]string{"delete": key}, cfg["digest"])
		if errors.Is(err, pve.ErrDigestMismatch) {
			continue
		}
		if err != nil {
			return servers.StepError("detach volume", err)
		}
		removed = true
		break
	}
	if !removed {
		return servers.Transient("vm %d config kept changing", vm.VMID)
	}
	items, err := pc.Pending(ctx, vm.Node, vm.VMID)
	if err != nil {
		return servers.StepError("read pending config", err)
	}
	for _, it := range items {
		for _, v := range []string{it.Value, it.Pending} {
			if vref.Matches(v, row.Volid) {
				return servers.Transient("vm %d still holds the volume in %s; the unplug is pending", vm.VMID, it.Key)
			}
		}
	}
	return nil
}

// settledStatus is the status a failed job leaves: attached while the row holds a server, else available.
func settledStatus(row *ent.Volume) volume.Status {
	if row.ServerID != nil {
		return volume.StatusAttached
	}
	return volume.StatusAvailable
}

type AttachVolumeWorker struct {
	river.WorkerDefaults[AttachVolumeArgs]
	o WorkerOptions
}

func NewAttachVolumeWorker(o WorkerOptions) *AttachVolumeWorker {
	o.defaults()
	return &AttachVolumeWorker{o: o}
}

func (*AttachVolumeWorker) Timeout(*river.Job[AttachVolumeArgs]) time.Duration { return jobTimeout }

func (w *AttachVolumeWorker) Work(ctx context.Context, job *river.Job[AttachVolumeArgs]) error {
	a := job.Args
	act, row, ret, stop := w.o.begin(ctx, a.ActionID, a.VolumeID)
	if stop {
		return ret
	}
	run := func(ctx context.Context) error {
		pc, vms, err := w.o.connect(ctx, row.LocationID)
		if err != nil {
			return err
		}
		row, err := w.o.plug(ctx, pc, vms, row, a.ServerID, a.ActionID, 50)
		if err != nil {
			return err
		}
		return db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Volume.UpdateOneID(row.ID).SetStatus(volume.StatusAttached).Exec(ctx); err != nil {
				return err
			}
			return actions.Succeed(ctx, etx, a.ActionID)
		})
	}
	fail := func(ctx context.Context, f servers.Failure) error {
		cur, err := w.o.Ent.Volume.Get(ctx, row.ID)
		if err != nil {
			return err
		}
		if cur.Status == volume.StatusAvailable || cur.Status == volume.StatusAttached {
			if err := cur.Update().SetStatus(settledStatus(cur)).Exec(ctx); err != nil {
				return err
			}
		}
		return failAction(ctx, w.o.Ent, a.ActionID, f)
	}
	return w.o.contract(job.JobRow, "attach", act, row.ID, run, fail).Work(ctx)
}

type DetachVolumeWorker struct {
	river.WorkerDefaults[DetachVolumeArgs]
	o WorkerOptions
}

func NewDetachVolumeWorker(o WorkerOptions) *DetachVolumeWorker {
	o.defaults()
	return &DetachVolumeWorker{o: o}
}

func (*DetachVolumeWorker) Timeout(*river.Job[DetachVolumeArgs]) time.Duration { return jobTimeout }

func (w *DetachVolumeWorker) Work(ctx context.Context, job *river.Job[DetachVolumeArgs]) error {
	a := job.Args
	act, row, ret, stop := w.o.begin(ctx, a.ActionID, a.VolumeID)
	if stop {
		return ret
	}
	run := func(ctx context.Context) error {
		pc, vms, err := w.o.connect(ctx, row.LocationID)
		if err != nil {
			return err
		}
		srv, err := w.o.Ent.Server.Get(ctx, a.ServerID)
		switch {
		case ent.IsNotFound(err):
			// The server row is gone, and ON DELETE SET NULL already detached the volume.
		case err != nil:
			return err
		case srv.Vmid != nil && !slices.ContainsFunc(vms, func(v pve.VM) bool { return v.VMID == *srv.Vmid }):
			exists, err := pc.GuestExists(ctx, *srv.Vmid)
			if err != nil {
				return servers.StepError("check server vmid", err)
			}
			if exists {
				return servers.PermanentCode(actions.CodeServerMissing, "server %d has no vm in proxmox", srv.ID)
			}
		case srv.Vmid != nil:
			vm, err := serverVM(vms, srv)
			if err != nil {
				return err
			}
			if err := unplug(ctx, pc, vm, row); err != nil {
				return err
			}
		}
		st := volume.StatusAvailable
		if row.Status == volume.StatusMissing {
			st = volume.StatusMissing
		}
		return db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Volume.UpdateOneID(row.ID).ClearServerID().SetDevice("").SetStatus(st).Exec(ctx); err != nil {
				return err
			}
			return actions.Succeed(ctx, etx, a.ActionID)
		})
	}
	// The volume is still in the VM, so the row stays as it is.
	fail := func(ctx context.Context, f servers.Failure) error { return failAction(ctx, w.o.Ent, a.ActionID, f) }
	return w.o.contract(job.JobRow, "detach", act, row.ID, run, fail).Work(ctx)
}
