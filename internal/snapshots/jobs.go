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

package snapshots

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	// ShutdownTimeout bounds the clean shutdown of a running server before its snapshot.
	ShutdownTimeout = 3 * time.Minute
	// TagSnapshot marks every snapshot template VM.
	TagSnapshot = "nebla-snapshot"

	// jobTimeout stays under jobs.RescueStuckJobsAfter; long imports snooze instead.
	jobTimeout   = 12 * time.Minute
	powerWait    = 2 * time.Minute
	pollInterval = time.Second
	waitMargin   = 30 * time.Second
)

var (
	// waitBudget bounds one task wait; past it the job snoozes and resumes on the saved UPID.
	waitBudget = 10 * time.Minute
	snoozeFor  = 30 * time.Second
)

// Commands are the snapshot action commands; the server rescue leaves them to RescueOrphaned.
var Commands = []string{KindCreateSnapshot, KindDeleteSnapshot}

// SnapshotTag is the tag that proves a template VM belongs to snapshot id.
func SnapshotTag(id int64) string { return fmt.Sprintf("%s-%d", TagSnapshot, id) }

func vmTags(id int64) string { return TagSnapshot + ";" + SnapshotTag(id) }

func vmName(id int64) string { return fmt.Sprintf("nebla-snap-%d", id) }

func vmDescription(id int64, nonce string) string {
	return fmt.Sprintf("nebla snapshot %d %s", id, nonce)
}

func hasTag(tags, want string) bool { return slices.Contains(servers.SplitTags(tags), want) }

type WorkerOptions struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// Bus nil drops events.
	Bus    servers.Publisher
	Logger *slog.Logger
	// ShutdownTimeout bounds the clean shutdown; the ShutdownTimeout constant when zero.
	ShutdownTimeout time.Duration
	// PollInterval spaces VM state checks; 1s when zero.
	PollInterval time.Duration
	// PowerWait bounds a start or hard stop and the wait for its state; 2m when zero.
	PowerWait time.Duration
	// pveFor is Catalog.ProxmoxFor unless a test replaces it.
	pveFor func(ctx context.Context, locationID int64) (pve.Client, error)
	// afterStep runs once a create step is saved; tests use it to crash.
	afterStep func(step int)
	// afterCreatePost runs once the create POST returns, before its upid is saved.
	afterCreatePost func()
}

// RegisterWorkers adds the snapshot job workers; they follow the jobs.StartAction worker contract.
func RegisterWorkers(w *river.Workers, o WorkerOptions) {
	river.AddWorker(w, NewCreateSnapshotWorker(o))
	river.AddWorker(w, NewDeleteSnapshotWorker(o))
	river.AddWorker(w, NewUsageWorker(o))
}

func (o *WorkerOptions) defaults() {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.pveFor == nil && o.Catalog != nil {
		o.pveFor = o.Catalog.ProxmoxFor
	}
	if o.ShutdownTimeout <= 0 {
		o.ShutdownTimeout = ShutdownTimeout
	}
	if o.PollInterval <= 0 {
		o.PollInterval = pollInterval
	}
	if o.PowerWait <= 0 {
		o.PowerWait = powerWait
	}
}

// begin loads a job's action and snapshot; when stop is true the worker returns ret.
func (o *WorkerOptions) begin(ctx context.Context, actionID, snapshotID int64) (act *ent.Action, row *ent.Snapshot, ret error, stop bool) {
	act, err := o.Ent.Action.Get(ctx, actionID)
	if ent.IsNotFound(err) {
		return nil, nil, river.JobCancel(err), true
	}
	if err != nil {
		return nil, nil, err, true
	}
	row, err = o.Ent.Snapshot.Get(ctx, snapshotID)
	if ent.IsNotFound(err) {
		if act.Finished != nil {
			return act, nil, nil, true
		}
		if ferr := failAction(ctx, o.Ent, actionID, servers.Failure{Code: actions.CodeFailed, Detail: "snapshot no longer exists"}); ferr != nil {
			return act, nil, ferr, true
		}
		return act, nil, river.JobCancel(err), true
	}
	if err != nil {
		return act, nil, err, true
	}
	return act, row, nil, false
}

func (o *WorkerOptions) contract(job *rivertype.JobRow, name string, act *ent.Action, snapshotID int64,
	run func(context.Context) error, fail func(context.Context, servers.Failure) error) servers.Contract {
	return servers.Contract{Job: job, Name: name, Finished: act.Finished != nil,
		Log: o.Logger.With("snapshot_id", snapshotID, "action_id", act.ID, "command", act.Command), Run: run, Fail: fail}
}

func failAction(ctx context.Context, c *ent.Client, id int64, f servers.Failure) error {
	return actions.Fail(context.WithoutCancel(ctx), c, id, f.Code, f.Detail)
}

// publish sends a snapshot event; snapshot events leave ServerID 0.
func (o *WorkerOptions) publish(ctx context.Context, kind ext.EventKind, row *ent.Snapshot) {
	if o.Bus == nil {
		return
	}
	o.Bus.Publish(ctx, ext.Event{Kind: kind, ProjectID: row.ProjectID, ResourceType: ResourceType, ResourceID: row.ID, At: time.Now(),
		Data: map[string]any{"size_gb": row.SizeGB, "location_id": row.LocationID}})
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

// serverVM is srv's VM, which must carry the server's tags.
func serverVM(ctx context.Context, pc pve.Client, srv *ent.Server) (pve.VM, error) {
	if srv.Vmid == nil {
		return pve.VM{}, servers.PermanentCode(actions.CodeServerMissing, "server %d has no vm", srv.ID)
	}
	v, err := servers.OwnedVM(ctx, pc, srv.ID, *srv.Vmid)
	if errors.Is(err, servers.ErrMissing) {
		return pve.VM{}, servers.PermanentCode(actions.CodeServerMissing, "server %d has no vm %d in proxmox", srv.ID, *srv.Vmid)
	}
	return v, err
}

func (o *WorkerOptions) setServerStatus(ctx context.Context, id int64, st string) error {
	return o.Ent.Server.UpdateOneID(id).SetStatus(server.Status(st)).Exec(ctx)
}

// power runs op on vmid and waits for its task, at most PowerWait.
func (o *WorkerOptions) power(ctx context.Context, pc pve.Client, node string, vmid int, op pve.PowerOp) error {
	up, err := pc.Power(ctx, node, vmid, op)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, o.PowerWait)
	defer cancel()
	err = pc.WaitTask(wctx, node, up)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return servers.PermanentCode(actions.CodeTimedOut, "%s of vm %d did not finish within %s", op, vmid, o.PowerWait)
	}
	return err
}

// waitState polls until vmid is listed in state want; false once limit has passed.
func (o *WorkerOptions) waitState(ctx context.Context, pc pve.Client, vmid int, want string, limit time.Duration) (bool, error) {
	deadline := time.Now().Add(limit)
	for {
		v, found, err := findVM(ctx, pc, vmid)
		if err != nil {
			return false, err
		}
		if found && v.Status == want {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(o.PollInterval):
		}
	}
}

// startServer starts srv's VM v unless it runs, keeping the server's status in step.
func (o *WorkerOptions) startServer(ctx context.Context, pc pve.Client, srv *ent.Server, v pve.VM) error {
	if v.Status != "running" {
		if err := o.setServerStatus(ctx, srv.ID, servers.StatusStarting); err != nil {
			return err
		}
		if err := o.awaitStart(ctx, pc, v); err != nil {
			o.syncServerStatus(ctx, pc, srv.ID, v.VMID)
			return err
		}
	}
	return o.setServerStatus(ctx, srv.ID, servers.StatusRunning)
}

func (o *WorkerOptions) awaitStart(ctx context.Context, pc pve.Client, v pve.VM) error {
	if err := o.power(ctx, pc, v.Node, v.VMID, pve.PowerStart); err != nil {
		return err
	}
	ok, err := o.waitState(ctx, pc, v.VMID, "running", o.PowerWait)
	if err != nil {
		return err
	}
	if !ok {
		return servers.PermanentCode(actions.CodeTimedOut, "vm %d did not start within %s", v.VMID, o.PowerWait)
	}
	return nil
}

// syncServerStatus sets the server's status from its VM's listed state, or off when that can't be read (pc nil included).
func (o *WorkerOptions) syncServerStatus(ctx context.Context, pc pve.Client, serverID int64, vmid int) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	st := servers.StatusOff
	if pc != nil {
		if v, found, err := findVM(ctx, pc, vmid); err == nil && found && v.Status == "running" {
			st = servers.StatusRunning
		}
	}
	if err := o.setServerStatus(ctx, serverID, st); err != nil {
		o.Logger.Error("snapshots: set server status", "server_id", serverID, "err", err)
	}
}

// restartSource starts the snapshot's source server if it was running, then clears was_running; it returns the updated row.
func (o *WorkerOptions) restartSource(ctx context.Context, pc pve.Client, row *ent.Snapshot) (*ent.Snapshot, error) {
	if !row.WasRunning || row.ServerID == nil {
		return nil, nil
	}
	srv, err := o.Ent.Server.Get(ctx, *row.ServerID)
	if ent.IsNotFound(err) || (err == nil && srv.Vmid == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := servers.OwnedVM(ctx, pc, srv.ID, *srv.Vmid)
	if errors.Is(err, servers.ErrMissing) {
		o.Logger.Warn("snapshots: source server vm is gone; nothing to start", "snapshot_id", row.ID, "server_id", srv.ID)
		return nil, nil
	}
	if err != nil {
		// Unreachable: a status left at stopping or starting would never settle.
		o.syncServerStatus(ctx, pc, srv.ID, *srv.Vmid)
		return nil, err
	}
	if err := o.startServer(ctx, pc, srv, v); err != nil {
		return nil, err
	}
	return o.Ent.Snapshot.UpdateOneID(row.ID).SetWasRunning(false).Save(ctx)
}

// upidNode is the node field of a UPID ("UPID:node:...").
func upidNode(upid string) string {
	parts := strings.SplitN(upid, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// budgetFor is waitBudget, cut short so a wait ends before the job deadline.
func budgetFor(ctx context.Context) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		return min(waitBudget, time.Until(dl)-waitMargin)
	}
	return waitBudget
}

// waitTask waits on a task within waitBudget, snoozing the job when the budget runs out first.
func waitTask(ctx context.Context, pc pve.Client, upid string) error {
	budget := budgetFor(ctx)
	if budget <= 0 {
		return river.JobSnooze(snoozeFor)
	}
	wctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	err := pc.WaitTask(wctx, upidNode(upid), pve.UPID(upid))
	if err != nil && wctx.Err() != nil && ctx.Err() == nil {
		return river.JobSnooze(snoozeFor)
	}
	return err
}

func alreadyExists(err error) bool {
	var pe *pve.Error
	var te *pve.TaskError
	return (errors.As(err, &pe) || errors.As(err, &te)) && strings.Contains(err.Error(), "already exists")
}

// lostLock reports a create task that timed out on vmid's config lock: another create or clone took the VMID.
func lostLock(err error, vmid int) bool {
	var te *pve.TaskError
	return errors.As(err, &te) && strings.Contains(te.ExitStatus, fmt.Sprintf("lock-%d.conf", vmid))
}
