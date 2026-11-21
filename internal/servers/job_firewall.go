package servers

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/firewalls"
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
		case row.Status == StatusDeleting:
			return actions.Succeed(ctx, w.o.Ent, a.ActionID)
		case row.CreateStep < stepDone || row.RebuildStep > 0 || row.Status == StatusRebuilding:
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
		loc, err := w.o.Catalog.GetLocation(ctx, row.LocationID)
		if err != nil {
			return &stepError{"load location", err}
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
		row.Node = v.Node
		if w.beforeApply != nil {
			w.beforeApply()
		}
		gen, err := w.o.applyFirewallsNow(ctx, pc, loc, row)
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

// applyFirewallsNow renders the server's effective firewalls onto its VM and returns the desired generation it rendered.
func (o *WorkerOptions) applyFirewallsNow(ctx context.Context, pc pve.Client, _ catalog.Location, row *ent.Server) (int64, error) {
	cur, err := o.Ent.Server.Get(ctx, row.ID)
	if err != nil {
		return 0, err
	}
	gen := cur.FirewallGenDesired
	rules, anyFW, err := firewalls.RulesFor(ctx, o.Ent, row.ID)
	if err != nil {
		return 0, err
	}
	if anyFW {
		on, err := pc.ClusterFirewallEnabled(ctx)
		if err != nil {
			return 0, &stepError{"check datacenter firewall", err}
		}
		if !on {
			return 0, &permanentError{actions.CodeFirewallUnenforced, unenforcedDetail}
		}
	}
	if err := firewalls.Sync(ctx, pc, row.Node, *row.Vmid, firewalls.Render(rules, anyFW)); err != nil {
		return 0, &stepError{"apply firewall", err}
	}
	return gen, nil
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
