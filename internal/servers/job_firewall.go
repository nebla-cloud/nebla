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
	"net/netip"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
)

const (
	applyTimeout = 90 * time.Second
	// fwLease outlives applyTimeout so a live apply never loses it.
	fwLease     = 2 * time.Minute
	buildSnooze = 10 * time.Second
	leaseSnooze = 2 * time.Second
	maxRenders  = 3
)

// A failed create never resumes, so the firewall step numbers must agree.
const _ = uint(stepDone-firewalls.CreateDone) + uint(firewalls.CreateDone-stepDone)

// failedCreate reports a server whose create failed; no build will run again, only delete.
func failedCreate(row *ent.Server) bool {
	return row.Status == StatusError && row.CreateStep < stepDone
}

// Building reports a create or rebuild that owns the server's VM; none owns an errored server's VM.
func Building(row *ent.Server) bool {
	if row.Status == StatusError {
		return false
	}
	return row.CreateStep < stepDone || row.RebuildStep > 0 || row.Status == StatusRebuilding
}

// unenforcedDetail is the admin detail when the datacenter firewall is off.
const unenforcedDetail = "datacenter firewall is disabled at this location"

// vmGoneError is retried like a transient error and, once out of attempts, reported as server_missing.
type vmGoneError struct{ msg string }

func (e *vmGoneError) Error() string { return e.msg }
func (e *vmGoneError) Unwrap() []error {
	return []error{&transientError{e.msg}, &permanentError{actions.CodeServerMissing, e.msg}}
}

// ApplyFirewallsWorker brings a server's VM firewall up to its desired generation; it never takes LockServer.
type ApplyFirewallsWorker struct {
	river.WorkerDefaults[firewalls.ApplyArgs]
	o                        WorkerOptions
	beforeLease, beforeApply func()
}

func NewApplyFirewallsWorker(o WorkerOptions) *ApplyFirewallsWorker {
	o.defaults()
	return &ApplyFirewallsWorker{o: o}
}

func (*ApplyFirewallsWorker) Timeout(*river.Job[firewalls.ApplyArgs]) time.Duration {
	return applyTimeout
}

func (w *ApplyFirewallsWorker) Work(ctx context.Context, job *river.Job[firewalls.ApplyArgs]) error {
	a := job.Args
	act, err := w.o.Ent.Action.Get(ctx, a.ActionID)
	if ent.IsNotFound(err) {
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	_, pending := pendingCleanup(job.JobRow)
	if act.Finished != nil && !pending {
		return nil
	}
	if !pending {
		row, err := w.o.Ent.Server.Get(ctx, a.ServerID)
		if ent.IsNotFound(err) {
			return actions.Succeed(ctx, w.o.Ent, a.ActionID)
		}
		if err != nil {
			return err
		}
		switch {
		case row.Status == StatusDeleting, failedCreate(row):
			return actions.Succeed(ctx, w.o.Ent, a.ActionID)
		case Building(row):
			return river.JobSnooze(buildSnooze)
		}
		if w.beforeLease != nil {
			w.beforeLease()
		}
		l, ok, err := claimLease(ctx, w.o.SQL, a.ServerID)
		if err != nil {
			return err
		}
		if !ok {
			return river.JobSnooze(leaseSnooze)
		}
		defer l.release(ctx)
		run := func(ctx context.Context) error { return w.run(ctx, l, a) }
		return w.contract(job, act, run).work(ctx)
	}
	return w.contract(job, act, func(context.Context) error { return nil }).work(ctx)
}

func (w *ApplyFirewallsWorker) contract(job *river.Job[firewalls.ApplyArgs], act *ent.Action, run func(context.Context) error) contract {
	a := job.Args
	return contract{job: job.JobRow, name: "apply firewalls", finished: act.Finished != nil, log: actionLog(w.o.Logger, act, a.ServerID),
		run: run, fail: func(ctx context.Context, f failure) error { return failAction(ctx, w.o.Ent, a.ActionID, f) }}
}

// run applies under the lease, re-reading the server so a swapped VM or newer generation is seen.
func (w *ApplyFirewallsWorker) run(ctx context.Context, l *lease, a firewalls.ApplyArgs) error {
	for i := range maxRenders {
		row, err := w.o.Ent.Server.Get(ctx, a.ServerID)
		if ent.IsNotFound(err) {
			return actions.Succeed(ctx, w.o.Ent, a.ActionID)
		}
		if err != nil {
			return err
		}
		// After a render, go again only if the desired generation moved meanwhile.
		done := row.FirewallGenApplied >= a.Gen
		if i > 0 {
			done = row.FirewallGenApplied >= row.FirewallGenDesired
		}
		if done || row.Status == StatusDeleting || row.Vmid == nil {
			return actions.Succeed(ctx, w.o.Ent, a.ActionID)
		}
		// A build that began meanwhile owns the VM until it finishes.
		if Building(row) {
			return river.JobSnooze(buildSnooze)
		}
		pc, err := w.o.Catalog.ProxmoxFor(ctx, row.LocationID)
		if err != nil {
			return &stepError{"connect to proxmox", err}
		}
		v, err := OwnedVM(ctx, pc, row.ID, *row.Vmid)
		if errors.Is(err, ErrMissing) {
			return w.vmGone(ctx, a, *row.Vmid)
		}
		if err != nil {
			return &stepError{"find vm", err}
		}
		if w.beforeApply != nil {
			w.beforeApply()
		}
		gen, _, err := w.o.applyFirewallsNow(ctx, pc, row.ID, v.Node, *row.Vmid)
		if errors.Is(err, pve.ErrNotFound) {
			return w.vmGone(ctx, a, *row.Vmid)
		}
		if err != nil {
			return err
		}
		if err := l.raise(ctx, gen); err != nil {
			return err
		}
	}
	row, err := w.o.Ent.Server.Get(ctx, a.ServerID)
	if err != nil || row.FirewallGenApplied < a.Gen {
		return errors.Join(err, transient("firewall generation %d not reached", a.Gen))
	}
	return actions.Succeed(ctx, w.o.Ent, a.ActionID)
}

// vmGone succeeds when the server is being deleted or gone, and retries otherwise.
func (w *ApplyFirewallsWorker) vmGone(ctx context.Context, a firewalls.ApplyArgs, vmid int) error {
	row, err := w.o.Ent.Server.Get(ctx, a.ServerID)
	if ent.IsNotFound(err) || err == nil && row.Status == StatusDeleting {
		return actions.Succeed(ctx, w.o.Ent, a.ActionID)
	}
	if err != nil {
		return err
	}
	return &vmGoneError{"vm " + strconv.Itoa(vmid) + " is missing"}
}

// applyFirewallsNow renders server id's effective firewalls onto vmid and returns the desired generation it rendered.
func (o *WorkerOptions) applyFirewallsNow(ctx context.Context, pc pve.Client, id int64, node string, vmid int) (gen int64, anyFW bool, err error) {
	cur, err := o.Ent.Server.Get(ctx, id)
	if err != nil {
		return 0, false, err
	}
	gen = cur.FirewallGenDesired
	rules, anyFW, err := firewalls.RulesFor(ctx, o.Ent, id)
	if err != nil {
		return 0, false, err
	}
	if anyFW {
		on, err := pc.ClusterFirewallEnabled(ctx)
		if err != nil {
			return 0, false, &stepError{"check datacenter firewall", err}
		}
		if !on {
			return 0, false, &permanentError{actions.CodeFirewallUnenforced, unenforcedDetail}
		}
	}
	switch err := firewalls.Sync(ctx, pc, node, vmid, firewalls.Render(rules, anyFW)); {
	case errors.Is(err, firewalls.ErrStoredDiffers):
		return 0, false, permanent("apply firewall: %v", err)
	case errors.Is(err, firewalls.ErrRuleGone):
		return 0, false, transient("apply firewall: %v", err)
	case err != nil:
		return 0, false, &stepError{"apply firewall", err}
	}
	// The v1 anti-spoofing ipset is restored with every apply; Sync already set its flags.
	assign, err := ipam.Assignments(ctx, o.Ent, id)
	if err != nil {
		return 0, false, err
	}
	ips := make([]netip.Addr, len(assign))
	for i, a := range assign {
		ips[i] = a.IP
	}
	if err := pc.SetFirewall(ctx, node, vmid, ips); err != nil {
		return 0, false, &stepError{"apply anti-spoofing", err}
	}
	return gen, anyFW, nil
}

// lease is a claimed firewall_lease_until on a server.
type lease struct {
	db    *sql.DB
	id    int64
	until time.Time
}

// claimLease takes the server's firewall lease unless a live one is held.
func claimLease(ctx context.Context, db *sql.DB, id int64) (*lease, bool, error) {
	l := &lease{db: db, id: id}
	err := db.QueryRowContext(ctx, `update servers set firewall_lease_until = now() + make_interval(secs => $2)
		where id = $1 and (firewall_lease_until is null or firewall_lease_until < now()) returning firewall_lease_until`,
		id, fwLease.Seconds()).Scan(&l.until)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return l, true, nil
}

// raise records gen as applied, never lowering it, and extends the lease.
func (l *lease) raise(ctx context.Context, gen int64) error {
	err := l.db.QueryRowContext(ctx, `update servers set firewall_gen_applied = greatest(firewall_gen_applied, $2),
		firewall_lease_until = now() + make_interval(secs => $4)
		where id = $1 and firewall_lease_until = $3 returning firewall_lease_until`, l.id, gen, l.until, fwLease.Seconds()).Scan(&l.until)
	if errors.Is(err, sql.ErrNoRows) {
		return transient("firewall lease on server %d was lost", l.id)
	}
	return err
}

// release clears the lease if it is still ours.
func (l *lease) release(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	_, _ = l.db.ExecContext(ctx, `update servers set firewall_lease_until = null where id = $1 and firewall_lease_until = $2`, l.id, l.until)
}
