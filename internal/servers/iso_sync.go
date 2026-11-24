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

	"github.com/nebla-cloud/nebla/internal/cloudinit"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
)

const (
	KindSyncISO    = "sync_iso"
	syncISOTimeout = 5 * time.Minute
)

// SyncISOArgs is the sync_iso job: rewrite the server's seed ISO if its network config changed.
type SyncISOArgs struct {
	ServerID int64 `json:"server_id"`
}

func (SyncISOArgs) Kind() string { return KindSyncISO }

func (SyncISOArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: jobs.LiveStates}}
}

// errBuildInFlight means a create or rebuild owns the ISO; it queues a sync when it ends.
var errBuildInFlight = errors.New("a build owns the seed iso")

// SyncISOWorker runs syncISO for one server, waiting out a build in flight.
type SyncISOWorker struct {
	river.WorkerDefaults[SyncISOArgs]
	o WorkerOptions
}

func NewSyncISOWorker(o WorkerOptions) *SyncISOWorker {
	o.defaults()
	return &SyncISOWorker{o: o}
}

func (*SyncISOWorker) Timeout(*river.Job[SyncISOArgs]) time.Duration { return syncISOTimeout }

func (w *SyncISOWorker) Work(ctx context.Context, job *river.Job[SyncISOArgs]) error {
	row, err := w.o.Ent.Server.Get(ctx, job.Args.ServerID)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	pc, err := w.o.Catalog.ProxmoxFor(ctx, row.LocationID)
	if err != nil {
		return err
	}
	_, err = w.o.syncISO(ctx, pc, row, false)
	var pe *permanentError
	switch {
	case errors.Is(err, errBuildInFlight):
		return river.JobSnooze(buildSnooze)
	case errors.As(err, &pe):
		return river.JobCancel(err)
	}
	return err
}

const (
	// isoLease outlives any one upload and ide2 swap.
	isoLease     = 2 * time.Minute
	isoLeasePoll = 50 * time.Millisecond
	isoLeaseWait = 30 * time.Second
)

// errServerGone means the server row was deleted.
var errServerGone = errors.New("server gone")

// isoLeaseHeld is a claimed iso_lease_until; no DB connection is held between its statements.
type isoLeaseHeld struct {
	db    *sql.DB
	id    int64
	until time.Time
}

// claimISOLease waits up to isoLeaseWait for server id's seed ISO lease.
func claimISOLease(ctx context.Context, sqlDB *sql.DB, id int64) (*isoLeaseHeld, error) {
	deadline := time.Now().Add(isoLeaseWait)
	for {
		l := &isoLeaseHeld{db: sqlDB, id: id}
		err := sqlDB.QueryRowContext(ctx, `update servers set iso_lease_until = now() + make_interval(secs => $2)
			where id = $1 and (iso_lease_until is null or iso_lease_until < now()) returning iso_lease_until`,
			id, isoLease.Seconds()).Scan(&l.until)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		var exists bool
		if err := sqlDB.QueryRowContext(ctx, `select exists(select 1 from servers where id = $1)`, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, errServerGone
		}
		if time.Now().After(deadline) {
			return nil, transient("seed iso of server %d is busy", id)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(isoLeasePoll):
		}
	}
}

// renew extends the lease, failing if another writer took it.
func (l *isoLeaseHeld) renew(ctx context.Context) error {
	err := l.db.QueryRowContext(ctx, `update servers set iso_lease_until = now() + make_interval(secs => $3)
		where id = $1 and iso_lease_until = $2 returning iso_lease_until`, l.id, l.until, isoLease.Seconds()).Scan(&l.until)
	if errors.Is(err, sql.ErrNoRows) {
		return transient("seed iso lease on server %d was lost", l.id)
	}
	return err
}

func (l *isoLeaseHeld) release(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	_, _ = l.db.ExecContext(ctx, `update servers set iso_lease_until = null where id = $1 and iso_lease_until = $2`, l.id, l.until)
}

// withISOLease runs fn holding server id's seed ISO lease; a deleted server runs nothing.
func (o *WorkerOptions) withISOLease(ctx context.Context, id int64, fn func(*isoLeaseHeld) error) error {
	l, err := claimISOLease(ctx, o.SQL, id)
	if errors.Is(err, errServerGone) {
		return nil
	}
	if err != nil {
		return err
	}
	defer l.release(ctx)
	return fn(l)
}

// isoSteady reports a server with a finished VM whose seed ISO a sync may rewrite.
func isoSteady(row *ent.Server) bool {
	if row.Vmid == nil || row.CreateStep < stepDone {
		return false
	}
	return row.Status != StatusDeleting && row.Status != StatusMissing
}

// renderISO is the seed ISO input for row from its current IPs, with instance-id generation instGen.
func renderISO(ctx context.Context, c *ent.Client, row *ent.Server, instGen int, netRefresh bool) (cloudinit.Input, error) {
	assign, err := ipam.Assignments(ctx, c, row.ID)
	if err != nil {
		return cloudinit.Input{}, err
	}
	in, err := seedInput(ctx, c, row, assign)
	if err != nil {
		return cloudinit.Input{}, err
	}
	in.InstanceID = fmt.Sprintf("nb-srv-%d-%d", row.ID, instGen)
	in.NetRefresh = netRefresh
	return in, nil
}

// isoBeforeUpdate is a test hook run between the ISO upload and the server update.
var isoBeforeUpdate func(serverID int64)

// syncISO writes a new seed ISO when the network config changed, bumpInstance is set or a bump is pending.
func (o *WorkerOptions) syncISO(ctx context.Context, pc pve.Client, row *ent.Server, bumpInstance bool) (changed bool, err error) {
	err = o.withISOLease(ctx, row.ID, func(l *isoLeaseHeld) error {
		cur, err := o.Ent.Server.Get(ctx, row.ID)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if Building(cur) {
			return errBuildInFlight
		}
		if !isoSteady(cur) {
			return nil
		}
		bumpInstance = bumpInstance || cur.InstanceBumpPending
		// Keeping the instance writes InstanceGeneration(cur) back, so a 0 stops following the new iso_generation.
		gen, inst := cur.IsoGeneration+1, InstanceGeneration(cur)
		if bumpInstance {
			inst = gen
		}
		in, err := renderISO(ctx, o.Ent, cur, inst, cur.NetRefresh)
		if err != nil {
			return err
		}
		hash := cloudinit.NetHash(in)
		if hash == cur.IsoNetHash && !bumpInstance {
			return nil
		}
		iso, err := cloudinit.Build(in)
		if err != nil {
			return permanent("%s", err)
		}
		loc, err := o.Catalog.GetLocation(ctx, cur.LocationID)
		if err != nil {
			return err
		}
		volid := isoVolid(loc.ISOStorage, cur.ID, gen)
		// A retry may find its own earlier upload; errors here surface on the upload.
		_ = pc.DeleteVolume(ctx, cur.Node, loc.ISOStorage, volid)
		if err := pc.UploadISO(ctx, cur.Node, loc.ISOStorage, isoName(cur.ID, gen), iso); err != nil {
			return err
		}
		if err := l.renew(ctx); err != nil {
			return err
		}
		if err := pc.SetConfig(ctx, cur.Node, *cur.Vmid, map[string]string{"ide2": volid + ",media=cdrom"}); err != nil {
			return err
		}
		if isoBeforeUpdate != nil {
			isoBeforeUpdate(cur.ID)
		}
		// A bump requested after cur was read must survive; the retry applies it.
		n, err := o.Ent.Server.Update().Where(server.ID(cur.ID), server.IsoGeneration(cur.IsoGeneration), server.IsoLeaseUntil(l.until),
			server.InstanceBumpPending(cur.InstanceBumpPending)).
			SetIsoGeneration(gen).SetIsoVolid(volid).SetIsoNetHash(hash).SetInstanceGeneration(inst).SetInstanceBumpPending(false).Save(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return transient("server %d changed during its iso sync", cur.ID)
		}
		changed = true
		prev := isoVolid(loc.ISOStorage, cur.ID, cur.IsoGeneration)
		if cur.IsoVolid != nil {
			prev = *cur.IsoVolid
		}
		if prev != volid {
			if err := deleteISO(ctx, pc, cur.Node, loc.ISOStorage, prev); err != nil {
				o.Logger.Warn("iso sync: delete old iso", "server_id", cur.ID, "volid", prev, "err", err)
			}
		}
		return nil
	})
	return changed, err
}

// ISOSyncDue reports a steady server whose seed ISO no longer holds its network config; an unknown hash is never due.
func ISOSyncDue(ctx context.Context, c *ent.Client, row *ent.Server) (bool, error) {
	if row.IsoNetHash == "" || !isoSteady(row) || Building(row) {
		return false, nil
	}
	in, err := renderISO(ctx, c, row, InstanceGeneration(row), row.NetRefresh)
	if err != nil {
		return false, err
	}
	return cloudinit.NetHash(in) != row.IsoNetHash, nil
}

// QueueISOSync queues a sync_iso job for each server; nil j queues nothing.
func QueueISOSync(ctx context.Context, sqlDB *sql.DB, j *jobs.Jobs, ids ...int64) error {
	if j == nil || len(ids) == 0 {
		return nil
	}
	return db.WithTx(ctx, sqlDB, func(tx *sql.Tx, _ *ent.Client) error { return queueISOSyncTx(ctx, tx, j, ids...) })
}

func queueISOSyncTx(ctx context.Context, tx *sql.Tx, j *jobs.Jobs, ids ...int64) error {
	if j == nil {
		return nil
	}
	for _, id := range ids {
		if _, err := j.InsertTx(ctx, tx, SyncISOArgs{ServerID: id}, nil); err != nil {
			return err
		}
	}
	return nil
}
