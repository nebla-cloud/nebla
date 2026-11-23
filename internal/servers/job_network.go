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

package servers

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// digestTries bounds the config writes retried on a digest mismatch.
const digestTries = 5

// NetworkWorker carries out attach_to_network and detach_from_network; Attach and Detach are the two registered workers.
type NetworkWorker struct{ o WorkerOptions }

func NewNetworkWorker(o WorkerOptions) *NetworkWorker {
	o.defaults()
	return &NetworkWorker{o: o}
}

func (w *NetworkWorker) Attach() *AttachNetworkWorker { return &AttachNetworkWorker{w: w} }

func (w *NetworkWorker) Detach() *DetachNetworkWorker { return &DetachNetworkWorker{w: w} }

type AttachNetworkWorker struct {
	river.WorkerDefaults[networks.AttachArgs]
	w *NetworkWorker
}

func (*AttachNetworkWorker) Timeout(*river.Job[networks.AttachArgs]) time.Duration {
	return applyTimeout
}

func (a *AttachNetworkWorker) Work(ctx context.Context, job *river.Job[networks.AttachArgs]) error {
	ids := netIDs{job.Args.ActionID, job.Args.ServerID, job.Args.NetworkID}
	return a.w.work(ctx, job.JobRow, ids, a.w.attach, a.w.failAttach)
}

type DetachNetworkWorker struct {
	river.WorkerDefaults[networks.DetachArgs]
	w *NetworkWorker
}

func (*DetachNetworkWorker) Timeout(*river.Job[networks.DetachArgs]) time.Duration {
	return applyTimeout
}

func (d *DetachNetworkWorker) Work(ctx context.Context, job *river.Job[networks.DetachArgs]) error {
	ids := netIDs{job.Args.ActionID, job.Args.ServerID, job.Args.NetworkID}
	return d.w.work(ctx, job.JobRow, ids, d.w.detach, d.w.failDetach)
}

type netIDs struct{ actionID, serverID, networkID int64 }

func (w *NetworkWorker) work(ctx context.Context, job *rivertype.JobRow, ids netIDs,
	run func(context.Context, netIDs) error, fail func(context.Context, netIDs, failure) error) error {
	act, err := w.o.Ent.Action.Get(ctx, ids.actionID)
	if ent.IsNotFound(err) {
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	log := w.o.Logger.With("server_id", ids.serverID, "network_id", ids.networkID, "action_id", act.ID, "command", act.Command)
	return contract{job: job, name: "network", finished: act.Finished != nil, log: log,
		run:  func(ctx context.Context) error { return run(ctx, ids) },
		fail: func(ctx context.Context, f failure) error { return fail(ctx, ids, f) }}.work(ctx)
}

// attachment is the server's row on the network, nil once gone.
func (w *NetworkWorker) attachment(ctx context.Context, ids netIDs) (*ent.ServerNetwork, error) {
	row, err := w.o.Ent.ServerNetwork.Query().Where(servernetwork.ServerID(ids.serverID), servernetwork.NetworkID(ids.networkID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

// vms returns the server's VM and its cloned rebuild VM; a VM missing from Proxmox is transient.
func (w *NetworkWorker) vms(ctx context.Context, srv *ent.Server) (pve.Client, []pve.VM, error) {
	pc, err := w.o.Catalog.ProxmoxFor(ctx, srv.LocationID)
	if err != nil {
		return nil, nil, &stepError{"connect to proxmox", err}
	}
	var out []pve.VM
	for i, vmid := range []*int{srv.Vmid, srv.RebuildVmid} {
		if vmid == nil || i == 1 && notCloned(srv) {
			continue
		}
		v, err := OwnedVM(ctx, pc, srv.ID, *vmid)
		if errors.Is(err, ErrMissing) {
			return nil, nil, transient("vm %d missing", *vmid)
		}
		if err != nil {
			return nil, nil, &stepError{"find vm", err}
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, nil, transient("server %d has no vm yet", srv.ID)
	}
	return pc, out, nil
}

// applyAll renders the server's firewalls and sets onto each VM and returns the last generation rendered.
func (w *NetworkWorker) applyAll(ctx context.Context, pc pve.Client, srvID int64, vms []pve.VM) (int64, error) {
	var gen int64
	for _, v := range vms {
		g, _, err := w.o.applyFirewallsNow(ctx, pc, srvID, v.Node, v.VMID)
		if errors.Is(err, pve.ErrNotFound) {
			return 0, transient("vm %d missing", v.VMID)
		}
		if err != nil {
			return 0, err
		}
		gen = g
	}
	return gen, nil
}

func (w *NetworkWorker) attach(ctx context.Context, ids netIDs) error {
	row, err := w.attachment(ctx, ids)
	if err != nil {
		return err
	}
	// A detach won, or already owns the row.
	if row == nil || row.State == servernetwork.StateDetaching {
		return actions.Succeed(ctx, w.o.Ent, ids.actionID)
	}
	srv, err := w.o.Ent.Server.Get(ctx, ids.serverID)
	if ent.IsNotFound(err) {
		return actions.Succeed(ctx, w.o.Ent, ids.actionID)
	}
	if err != nil {
		return err
	}
	if srv.Status == StatusDeleting {
		return river.JobSnooze(buildSnooze)
	}
	l, ok, err := claimLease(ctx, w.o.SQL, srv.ID)
	if err != nil {
		return err
	}
	if !ok {
		return river.JobSnooze(leaseSnooze)
	}
	defer l.release(ctx)
	pc, vms, err := w.vms(ctx, srv)
	if err != nil {
		return err
	}
	gen, err := w.applyAll(ctx, pc, srv.ID, vms)
	if err != nil {
		return err
	}
	nics, err := networks.PrivateNICs(ctx, w.o.Ent, srv.ID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(nics, func(n networks.NIC) bool { return n.Index == row.Nic })
	if i < 0 || nics[i].VNet == "" {
		return permanent("network %d has no vnet for nic %d", ids.networkID, row.Nic)
	}
	for _, v := range vms {
		if err := w.o.writeNIC(ctx, pc, v, nics[i]); err != nil {
			return err
		}
	}
	err = w.o.Ent.ServerNetwork.Update().Where(servernetwork.ID(row.ID), servernetwork.StateEQ(servernetwork.StateAttaching)).
		SetState(servernetwork.StateAttached).Exec(ctx)
	if err != nil {
		return err
	}
	if err := l.raise(ctx, gen); err != nil {
		return err
	}
	return actions.Succeed(ctx, w.o.Ent, ids.actionID)
}

func (w *NetworkWorker) detach(ctx context.Context, ids netIDs) error {
	srv, err := w.o.Ent.Server.Get(ctx, ids.serverID)
	if ent.IsNotFound(err) {
		return actions.Succeed(ctx, w.o.Ent, ids.actionID)
	}
	if err != nil {
		return err
	}
	row, err := w.attachment(ctx, ids)
	if err != nil {
		return err
	}
	// The VMs of a server being deleted are destroyed; its rows go with it.
	if srv.Status == StatusDeleting {
		if row != nil {
			if err := w.o.Ent.ServerNetwork.DeleteOneID(row.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
				return err
			}
		}
		return actions.Succeed(ctx, w.o.Ent, ids.actionID)
	}
	l, ok, err := claimLease(ctx, w.o.SQL, srv.ID)
	if err != nil {
		return err
	}
	if !ok {
		return river.JobSnooze(leaseSnooze)
	}
	defer l.release(ctx)
	pc, vms, err := w.vms(ctx, srv)
	if err != nil {
		return err
	}
	if row != nil {
		if err := w.unplugAll(ctx, pc, vms, row.Nic); err != nil {
			return err
		}
		err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.ServerNetwork.DeleteOneID(row.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	// With the row gone the apply drops its set; a retry after a failed apply lands here with no row.
	gen, err := w.applyAll(ctx, pc, srv.ID, vms)
	if err != nil {
		return err
	}
	if err := l.raise(ctx, gen); err != nil {
		return err
	}
	return actions.Succeed(ctx, w.o.Ent, ids.actionID)
}

// unplugAll deletes net<idx> from every VM, then waits until none holds it in config or pending.
func (w *NetworkWorker) unplugAll(ctx context.Context, pc pve.Client, vms []pve.VM, idx int) error {
	for _, v := range vms {
		if err := w.o.unplugNIC(ctx, pc, v.Node, v.VMID, idx); err != nil {
			return err
		}
	}
	for _, v := range vms {
		held, err := nicHeld(ctx, pc, v.Node, v.VMID, idx)
		if err != nil {
			return err
		}
		if held {
			return transient("vm %d still holds the NIC; the unplug is pending", v.VMID)
		}
	}
	return nil
}

// failAttach drops the row and what it wrote, then the set; the action fails last.
func (w *NetworkWorker) failAttach(ctx context.Context, ids netIDs, f failure) error {
	row, err := w.attachment(ctx, ids)
	if err != nil {
		return err
	}
	var srv *ent.Server
	if row != nil {
		if srv, err = w.o.Ent.Server.Get(ctx, ids.serverID); err != nil && !ent.IsNotFound(err) {
			return err
		}
	}
	if srv != nil && row.State == servernetwork.StateAttaching {
		w.removeStrayNIC(ctx, srv, row)
	}
	err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		_, err := etx.ServerNetwork.Delete().Where(servernetwork.ServerID(ids.serverID), servernetwork.NetworkID(ids.networkID),
			servernetwork.StateEQ(servernetwork.StateAttaching)).Exec(ctx)
		return err
	})
	if err != nil {
		return err
	}
	if srv != nil {
		w.dropSet(ctx, srv)
	}
	return failAction(ctx, w.o.Ent, ids.actionID, f)
}

// removeStrayNIC unplugs the NIC a half-done attach left on the server's VMs; best effort.
func (w *NetworkWorker) removeStrayNIC(ctx context.Context, srv *ent.Server, row *ent.ServerNetwork) {
	pc, vms, err := w.vms(ctx, srv)
	if err != nil {
		return
	}
	key := "net" + strconv.Itoa(row.Nic)
	for _, v := range vms {
		cfg, err := pc.Config(ctx, v.Node, v.VMID)
		if err != nil || !strings.Contains(strings.ToLower(cfg[key]), strings.ToLower(row.MAC)) {
			continue
		}
		_ = w.o.unplugNIC(ctx, pc, v.Node, v.VMID, row.Nic)
	}
}

// dropSet applies the server's firewalls so the set of a dropped row goes; best effort under the lease.
func (w *NetworkWorker) dropSet(ctx context.Context, srv *ent.Server) {
	l, ok, err := claimLease(ctx, w.o.SQL, srv.ID)
	if err != nil || !ok {
		return
	}
	defer l.release(ctx)
	pc, vms, err := w.vms(ctx, srv)
	if err != nil {
		return
	}
	_, _ = w.applyAll(ctx, pc, srv.ID, vms)
}

// failDetach leaves the row detaching for the reconciler to resume.
func (w *NetworkWorker) failDetach(ctx context.Context, ids netIDs, f failure) error {
	return failAction(ctx, w.o.Ent, ids.actionID, f)
}

// nicValue is the netN config value of n.
func nicValue(n networks.NIC) string {
	v := "virtio=" + n.MAC + ",bridge=" + n.VNet + ",firewall=1"
	if n.MTU != 1500 {
		v += ",mtu=" + strconv.Itoa(n.MTU)
	}
	return v
}

// netHotplug reports whether hotplug includes network; a missing key means Proxmox's default.
func netHotplug(cfg map[string]string) bool {
	h, ok := cfg["hotplug"]
	switch {
	case !ok || h == "1":
		return true
	case h == "0":
		return false
	}
	return slices.Contains(strings.Split(h, ","), "network")
}

// writeNIC puts n into vm's config as net<N> with the config digest, unless it is there already.
func (o *WorkerOptions) writeNIC(ctx context.Context, pc pve.Client, vm pve.VM, n networks.NIC) error {
	key, want := "net"+strconv.Itoa(n.Index), nicValue(n)
	for range digestTries {
		cfg, err := pc.Config(ctx, vm.Node, vm.VMID)
		if err != nil {
			return &stepError{"read vm config", err}
		}
		if strings.EqualFold(cfg[key], want) {
			return nil
		}
		if vm.Status == "running" && !netHotplug(cfg) {
			return &permanentError{actions.CodePowerOffRequired, "vm " + strconv.Itoa(vm.VMID) + " is running and hotplug " + strconv.Quote(cfg["hotplug"]) + " excludes network"}
		}
		err = pc.SetConfigDigest(ctx, vm.Node, vm.VMID, map[string]string{key: want}, cfg["digest"])
		if errors.Is(err, pve.ErrDigestMismatch) {
			continue
		}
		if err != nil {
			return &stepError{"attach network", err}
		}
		return nil
	}
	return transient("vm %d config kept changing", vm.VMID)
}

// unplugNIC deletes net<idx> from the VM's config with the config digest; an absent key is done.
func (o *WorkerOptions) unplugNIC(ctx context.Context, pc pve.Client, node string, vmid, idx int) error {
	key := "net" + strconv.Itoa(idx)
	for range digestTries {
		cfg, err := pc.Config(ctx, node, vmid)
		if err != nil {
			return &stepError{"read vm config", err}
		}
		if _, has := cfg[key]; !has {
			return nil
		}
		err = pc.SetConfigDigest(ctx, node, vmid, map[string]string{"delete": key}, cfg["digest"])
		if errors.Is(err, pve.ErrDigestMismatch) {
			continue
		}
		if err != nil {
			return &stepError{"detach network", err}
		}
		return nil
	}
	return transient("vm %d config kept changing", vmid)
}

// nicHeld reports whether net<idx> is in the VM's config or its pending changes.
func nicHeld(ctx context.Context, pc pve.Client, node string, vmid, idx int) (bool, error) {
	key := "net" + strconv.Itoa(idx)
	cfg, err := pc.Config(ctx, node, vmid)
	if err != nil {
		return false, &stepError{"read vm config", err}
	}
	if _, has := cfg[key]; has {
		return true, nil
	}
	items, err := pc.Pending(ctx, node, vmid)
	if err != nil {
		return false, &stepError{"read pending config", err}
	}
	return slices.ContainsFunc(items, func(it pve.PendingItem) bool { return it.Key == key }), nil
}
