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
	"fmt"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
)

// applyPoll spaces the checks while an assign waits for the new server's apply.
const applyPoll = 2 * time.Second

// FloatingIPWorker carries out an assign, unassign or delete: release from the old server first, then apply on the new one.
type FloatingIPWorker struct {
	river.WorkerDefaults[floatingips.ChangeArgs]
	o             WorkerOptions
	beforeOutcome func()
}

func NewFloatingIPWorker(o WorkerOptions) *FloatingIPWorker {
	o.defaults()
	return &FloatingIPWorker{o: o}
}

func (*FloatingIPWorker) Timeout(*river.Job[floatingips.ChangeArgs]) time.Duration {
	return applyTimeout
}

func (w *FloatingIPWorker) Work(ctx context.Context, job *river.Job[floatingips.ChangeArgs]) error {
	a := job.Args
	act, err := w.o.Ent.Action.Get(ctx, a.ActionID)
	if ent.IsNotFound(err) {
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	log := w.o.Logger.With("floating_ip_id", a.FloatingIPID, "action_id", act.ID, "command", act.Command)
	return contract{job: job.JobRow, name: "floating ip", finished: act.Finished != nil, log: log,
		run:  func(ctx context.Context) error { return w.run(ctx, a) },
		fail: func(ctx context.Context, f failure) error { return w.fail(ctx, a, f) }}.work(ctx)
}

func (w *FloatingIPWorker) run(ctx context.Context, a floatingips.ChangeArgs) error {
	row, err := w.o.Ent.IPAddress.Get(ctx, a.FloatingIPID)
	if ent.IsNotFound(err) {
		return actions.Succeed(ctx, w.o.Ent, a.ActionID)
	}
	if err != nil {
		return err
	}
	if row.ReleasingFromID != nil {
		if err := w.release(ctx, row.ID, *row.ReleasingFromID); err != nil {
			return err
		}
		if row, err = w.o.Ent.IPAddress.Get(ctx, a.FloatingIPID); ent.IsNotFound(err) {
			return actions.Succeed(ctx, w.o.Ent, a.ActionID)
		}
		if err != nil {
			return err
		}
		if row.ReleasingFromID != nil {
			return transient("floating ip %d is still releasing from server %d", row.ID, *row.ReleasingFromID)
		}
	}
	if row.ServerID != nil && row.PendingGen == nil && !row.Deleting {
		// Deleting the old server's row ended the release without handing the address over.
		if row, err = w.handOver(ctx, row.ID); ent.IsNotFound(err) {
			return actions.Succeed(ctx, w.o.Ent, a.ActionID)
		}
		if err != nil {
			return err
		}
	}
	switch {
	case row.Deleting:
		return w.delete(ctx, a)
	case row.PendingGen != nil:
		return w.await(ctx, a, row)
	}
	return actions.Succeed(ctx, w.o.Ent, a.ActionID)
}

// handOver bumps the server a released row is assigned to and records the generation to wait for.
func (w *FloatingIPWorker) handOver(ctx context.Context, id int64) (*ent.IPAddress, error) {
	err := db.WithTx(ctx, w.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		cur, err := etx.IPAddress.Query().Where(ipaddress.ID(id)).ForUpdate().Only(ctx)
		if err != nil {
			return err
		}
		if cur.ReleasingFromID != nil || cur.ServerID == nil || cur.PendingGen != nil || cur.Deleting {
			return nil
		}
		return bumpTarget(ctx, tx, etx, w.o.jobs(), cur, etx.IPAddress.UpdateOneID(id))
	})
	if err != nil {
		return nil, err
	}
	return w.o.Ent.IPAddress.Get(ctx, id)
}

// bumpTarget bumps cur's server and has u record the generation the assign waits for.
func bumpTarget(ctx context.Context, tx *sql.Tx, etx *ent.Client, j *jobs.Jobs, cur *ent.IPAddress, u *ent.IPAddressUpdateOne) error {
	if _, err := firewalls.Bump(ctx, tx, etx, j, cur.ProjectID, []int64{*cur.ServerID}, nil); err != nil {
		return err
	}
	srv, err := etx.Server.Get(ctx, *cur.ServerID)
	if err != nil {
		return err
	}
	return u.SetPendingGen(srv.FirewallGenDesired).Exec(ctx)
}

// release writes the old server's allow-set onto every VM it has, waits for the reload, then lets the new server have the address.
func (w *FloatingIPWorker) release(ctx context.Context, id, oldID int64) error {
	// Reading before the lease is safe: every ipset write below is fenced by writeAllowSet.
	old, err := w.o.Ent.Server.Get(ctx, oldID)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	// A delete destroys the old server's VMs; deleting its row then ends the release.
	if old != nil && old.Status == StatusDeleting {
		return river.JobSnooze(buildSnooze)
	}
	l, ok, err := claimLease(ctx, w.o.SQL, oldID)
	if err != nil {
		return err
	}
	if !ok {
		return river.JobSnooze(leaseSnooze)
	}
	defer l.release(ctx)
	if old != nil {
		if err := w.writeVMs(ctx, old); err != nil {
			return err
		}
		if err := settleWait(ctx, FirewallSettle); err != nil {
			return err
		}
	}
	return db.WithTx(ctx, w.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		cur, err := etx.IPAddress.Query().Where(ipaddress.ID(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if cur.ReleasingFromID == nil || *cur.ReleasingFromID != oldID {
			return nil
		}
		u := etx.IPAddress.UpdateOneID(id).ClearReleasingFromID()
		if cur.ServerID != nil && !cur.Deleting {
			return bumpTarget(ctx, tx, etx, w.o.jobs(), cur, u)
		}
		return u.Exec(ctx)
	})
}

// writeVMs writes the allow-set onto the server's VM and its rebuild VM, whatever the server's status.
func (w *FloatingIPWorker) writeVMs(ctx context.Context, old *ent.Server) error {
	if old.Vmid == nil && old.RebuildVmid == nil {
		return nil
	}
	pc, err := w.o.Catalog.ProxmoxFor(ctx, old.LocationID)
	if err != nil {
		return &stepError{"connect to proxmox", err}
	}
	for i, vmid := range []*int{old.Vmid, old.RebuildVmid} {
		if vmid == nil || i == 1 && notCloned(old) {
			continue
		}
		v, err := OwnedVM(ctx, pc, old.ID, *vmid)
		if errors.Is(err, ErrMissing) {
			return transient("vm %d missing", *vmid)
		}
		if err != nil {
			return &stepError{"find vm", err}
		}
		err = w.o.writeAllowSet(ctx, pc, old.ID, v.Node, *vmid)
		if errors.Is(err, pve.ErrNotFound) {
			return transient("vm %d missing", *vmid)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// notCloned reports a rebuild that has picked its VMID but not cloned; its own later ipset write reads the allow-set under the fence.
func notCloned(row *ent.Server) bool {
	return row.Status == StatusRebuilding && row.RebuildStep >= stepVMID && row.RebuildStep < stepClone
}

// delete removes the released row and frees its address.
func (w *FloatingIPWorker) delete(ctx context.Context, a floatingips.ChangeArgs) error {
	var gone *ent.IPAddress
	err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		cur, err := etx.IPAddress.Query().Where(ipaddress.ID(a.FloatingIPID)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return actions.Succeed(ctx, etx, a.ActionID)
		}
		if err != nil {
			return err
		}
		if cur.ReleasingFromID != nil || cur.ServerID != nil {
			return transient("floating ip %d is still assigned", cur.ID)
		}
		if err := etx.IPAddress.DeleteOneID(cur.ID).Exec(ctx); err != nil {
			return err
		}
		if err := ipam.FreeAllocation(ctx, etx, cur.AllocationID); err != nil {
			return err
		}
		gone = cur
		return actions.Succeed(ctx, etx, a.ActionID)
	})
	if err != nil || gone == nil || w.o.Bus == nil {
		return err
	}
	w.o.Bus.Publish(ctx, ext.Event{Kind: ext.FloatingIPDeleted, ProjectID: gone.ProjectID, ResourceType: floatingips.ResourceType,
		ResourceID: gone.ID, At: time.Now(), Data: map[string]any{"ip": gone.IP, "family": gone.Family, "location_id": gone.LocationID}})
	return nil
}

// await waits for the new server's apply to reach pending_gen and for Proxmox to load it.
func (w *FloatingIPWorker) await(ctx context.Context, a floatingips.ChangeArgs, row *ent.IPAddress) error {
	var srv *ent.Server
	if row.ServerID != nil {
		var err error
		if srv, err = w.o.Ent.Server.Get(ctx, *row.ServerID); err != nil && !ent.IsNotFound(err) {
			return err
		}
	}
	if srv != nil && srv.Status != StatusDeleting {
		if srv.FirewallGenApplied < *row.PendingGen {
			if err := w.applyOutcome(ctx, srv, *row.PendingGen); err != nil {
				return err
			}
		}
		if err := settleWait(ctx, FirewallSettle); err != nil {
			return err
		}
	}
	err := w.o.Ent.IPAddress.Update().Where(ipaddress.ID(row.ID), ipaddress.PendingGen(*row.PendingGen)).ClearPendingGen().Exec(ctx)
	if err != nil {
		return err
	}
	return actions.Succeed(ctx, w.o.Ent, a.ActionID)
}

// applyOutcome is nil once srv reached gen; it snoozes while an apply is unfinished, else fails with the last apply's code.
func (w *FloatingIPWorker) applyOutcome(ctx context.Context, srv *ent.Server, gen int64) error {
	if w.beforeOutcome != nil {
		w.beforeOutcome()
	}
	q := w.o.Ent.Action.Query().Where(ActionsOf(srv.ProjectID, srv.ID), action.Command(firewalls.KindApply))
	running, err := q.Clone().Where(action.StatusEQ(action.StatusRunning)).Exist(ctx)
	if err != nil {
		return err
	}
	if running {
		return river.JobSnooze(applyPoll)
	}
	// An apply raises applied before it finishes its action, so read applied again after seeing none running.
	if srv, err = w.o.Ent.Server.Get(ctx, srv.ID); err != nil {
		return err
	}
	if srv.FirewallGenApplied >= gen {
		return nil
	}
	code := actions.CodeFailed
	last, err := q.Order(ent.Desc(action.FieldID)).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if last != nil && last.ErrorCode != nil {
		code = *last.ErrorCode
	}
	return &permanentError{code, fmt.Sprintf("server %d did not apply firewall generation %d", srv.ID, gen)}
}

// fail unassigns a row whose release never finished and fails with release_pending; else it stops the wait and fails with f's code.
func (w *FloatingIPWorker) fail(ctx context.Context, a floatingips.ChangeArgs, f failure) error {
	err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		cur, err := etx.IPAddress.Query().Where(ipaddress.ID(a.FloatingIPID)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if cur.ReleasingFromID == nil {
			// The address stays assigned; drift repair fills the ipset later.
			return etx.IPAddress.UpdateOneID(cur.ID).ClearPendingGen().Exec(ctx)
		}
		f.code = actions.CodeReleasePending
		return etx.IPAddress.UpdateOneID(cur.ID).ClearServerID().ClearPendingGen().Exec(ctx)
	})
	if err != nil {
		return err
	}
	return failAction(ctx, w.o.Ent, a.ActionID, f)
}

func (o *WorkerOptions) jobs() *jobs.Jobs {
	if o.Jobs == nil {
		return nil
	}
	return o.Jobs()
}
