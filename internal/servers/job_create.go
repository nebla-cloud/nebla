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
	"fmt"
	"log/slog"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/riverqueue/river"
)

const (
	spareAttempts = 2
	// cleanupPending prefixes a job error whose cleanup must run again.
	cleanupPending = "nebla: cleanup pending: "
	createTimeout  = 10 * time.Minute
	startWait      = 2 * time.Minute
	maxVMIDTries   = 3
)

// Create-only steps after the shared build steps.
const (
	stepStart = stepFirewall + 1 + iota
	stepDone
)

var stepProgress = map[int]int{stepVMID: 10, stepClone: 40, stepConfig: 55, stepResize: 65, stepISO: 75, stepFirewall: 85}

// Publisher receives domain events; *ext.Bus implements it.
type Publisher interface {
	Publish(ctx context.Context, e ext.Event)
}

type WorkerOptions struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// Bus nil drops events.
	Bus    Publisher
	Logger *slog.Logger
	// PollInterval spaces the wait-for-status checks; 1s when zero.
	PollInterval time.Duration
	// PowerWait bounds a power task and the wait for its state; 2m when zero.
	PowerWait time.Duration
	// Jobs returns the job runtime, built after the workers; the floating IP worker bumps firewalls with it.
	Jobs func() *jobs.Jobs
}

// RegisterWorkers adds the server job workers; they follow the jobs.StartAction worker contract.
func RegisterWorkers(w *river.Workers, o WorkerOptions) {
	river.AddWorker(w, NewCreateServerWorker(o))
	river.AddWorker(w, NewPowerServerWorker(o))
	river.AddWorker(w, NewDeleteServerWorker(o))
	river.AddWorker(w, NewRebuildServerWorker(o))
	river.AddWorker(w, NewRestoreServerWorker(o))
	river.AddWorker(w, NewUsageWorker(o))
	river.AddWorker(w, NewChangeTypeWorker(o))
	river.AddWorker(w, NewApplyFirewallsWorker(o))
	river.AddWorker(w, NewFloatingIPWorker(o))
	nw := NewNetworkWorker(o)
	river.AddWorker(w, nw.Attach())
	river.AddWorker(w, nw.Detach())
}

func (o *WorkerOptions) defaults() {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.PollInterval <= 0 {
		o.PollInterval = time.Second
	}
	if o.PowerWait <= 0 {
		o.PowerWait = powerWait
	}
}

func (CreateServerArgs) InsertOpts() river.InsertOpts { return insertOpts() }

// CreateServerWorker builds a server's VM from its row snapshot, resuming at servers.create_step.
type CreateServerWorker struct {
	river.WorkerDefaults[CreateServerArgs]
	o              WorkerOptions
	afterStep      func(step int)
	afterClonePost func()
	cleanupTxHook  func() error
}

func NewCreateServerWorker(o WorkerOptions) *CreateServerWorker {
	o.defaults()
	return &CreateServerWorker{o: o}
}

func (*CreateServerWorker) Timeout(*river.Job[CreateServerArgs]) time.Duration { return createTimeout }

// createRun is one attempt's state.
type createRun struct {
	w    *CreateServerWorker
	args CreateServerArgs
	row  *ent.Server
	b    *builder
}

func (w *CreateServerWorker) Work(ctx context.Context, job *river.Job[CreateServerArgs]) error {
	act, row, ret, stop := w.o.begin(ctx, job.Args.ActionID, job.Args.ServerID)
	if stop {
		return ret
	}
	c := &createRun{w: w, args: job.Args, row: row}
	return contract{job: job.JobRow, name: "create", finished: act.Finished != nil, log: actionLog(w.o.Logger, act, row.ID),
		run: c.run, fail: c.cleanup}.work(ctx)
}

func (c *createRun) load(ctx context.Context) error {
	loc, pc, assign, err := c.w.o.loadTarget(ctx, c.row)
	if err != nil {
		return err
	}
	c.b = &builder{pc: pc, loc: loc, assign: assign, row: c.row, tmpl: c.row.TemplateVmid, isoGen: c.row.IsoGeneration, instGen: InstanceGeneration(c.row),
		st:   buildState{Vmid: c.row.Vmid, Upid: c.row.CloneUpid, Nonce: c.row.CloneNonce, Step: c.row.CreateStep},
		save: c.save, afterClonePost: c.w.afterClonePost, poll: c.w.o.PollInterval, o: &c.w.o}
	return nil
}

// save writes the build state to the create columns.
func (c *createRun) save(ctx context.Context, st buildState) error {
	u := c.w.o.Ent.Server.UpdateOneID(c.row.ID).SetCreateStep(st.Step)
	setSlot(u.SetVmid, u.ClearVmid, st.Vmid)
	setSlot(u.SetCloneUpid, u.ClearCloneUpid, st.Upid)
	setSlot(u.SetCloneNonce, u.ClearCloneNonce, st.Nonce)
	if st.Step >= stepISO {
		u.SetIsoVolid(c.b.volid())
	}
	row, err := u.Save(ctx)
	if err != nil {
		return err
	}
	c.row, c.b.row = row, row
	return nil
}

func (c *createRun) run(ctx context.Context) error {
	if err := c.load(ctx); err != nil {
		return err
	}
	steps := append(c.b.steps(), step{stepStart, "start vm", c.start}, step{stepDone, "finish", c.finish})
	progress := func(ctx context.Context, n int) error {
		if p, ok := stepProgress[n]; ok {
			return actions.Progress(ctx, c.w.o.Ent, c.args.ActionID, p)
		}
		return nil
	}
	return runSteps(ctx, func() int { return c.b.st.Step }, steps, progress, c.w.afterStep)
}

func (c *createRun) start(ctx context.Context) error {
	if err := c.b.protect(ctx, c.b.vmid()); err != nil {
		return err
	}
	if c.args.StartAfter {
		if err := c.w.o.Ent.Server.UpdateOneID(c.row.ID).SetStatus(StatusStarting).Exec(ctx); err != nil {
			return err
		}
		if err := startVM(ctx, c.b.pc, c.b.vmid(), c.w.o.PollInterval); err != nil {
			return err
		}
	}
	return c.b.done(ctx, stepStart)
}

func (c *createRun) finish(ctx context.Context) error {
	status := StatusOff
	if c.args.StartAfter {
		status = StatusRunning
	}
	err := db.WithTx(ctx, c.w.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if err := etx.Server.UpdateOneID(c.row.ID).SetStatus(server.Status(status)).SetCreateStep(stepDone).Exec(ctx); err != nil {
			return err
		}
		if err := markAttached(ctx, etx, c.row.ID); err != nil {
			return err
		}
		if err := c.b.raiseApplied(ctx, tx); err != nil {
			return err
		}
		return actions.Succeed(ctx, etx, c.args.ActionID)
	})
	if err != nil {
		return err
	}
	c.publish(ctx)
	return nil
}

func (c *createRun) publish(ctx context.Context) {
	data := map[string]any{"location": c.b.loc.Name, "cores": c.row.Cores, "memory_mb": c.row.MemoryMB, "disk_gb": c.row.DiskGB}
	c.w.o.publish(ctx, ext.ServerCreated, c.row, data)
}

// publish sends a server event, adding the type name when it can be read.
func (o *WorkerOptions) publish(ctx context.Context, kind ext.EventKind, row *ent.Server, data map[string]any) {
	if o.Bus == nil {
		return
	}
	if t, err := o.Catalog.GetServerType(ctx, row.ServerTypeID); err == nil {
		data["server_type"] = t.Name
	}
	o.Bus.Publish(ctx, ext.Event{Kind: kind, ProjectID: row.ProjectID, ServerID: row.ID, ResourceType: "server", ResourceID: row.ID, At: time.Now(), Data: data})
}

// cleanup undoes a failed create; IPs are released only once the VM is confirmed gone.
func (c *createRun) cleanup(ctx context.Context, f failure) error {
	w := c.w
	log := w.o.Logger.With("server_id", c.row.ID, "action_id", c.args.ActionID)
	log.Warn("create server failed", "err", f.detail, "code", f.code)
	if c.b == nil {
		if err := c.load(ctx); err != nil && retryable(err) {
			return err
		}
	}
	gone := c.row.Vmid == nil
	isoStorage, volid := "", ""
	if c.b != nil {
		isoStorage, volid = c.b.loc.ISOStorage, c.b.volid()
	}
	if c.b != nil && c.b.pc != nil {
		gone = CleanupVM(ctx, log, c.b.pc, c.row.ID, c.row.Node, c.row.Vmid, CloneProof{UPID: c.row.CloneUpid, Nonce: c.row.CloneNonce}, isoStorage, volid)
	}
	if !gone {
		f.detail += fmt.Sprintf("; cleanup incomplete: VM %d could not be removed", *c.row.Vmid)
	}
	err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if w.cleanupTxHook != nil {
			if err := w.cleanupTxHook(); err != nil {
				return err
			}
		}
		u := etx.Server.UpdateOneID(c.row.ID).SetStatus(server.Status(StatusError))
		if gone {
			if err := ipam.Release(ctx, etx, c.row.ID); err != nil {
				return err
			}
			if _, err := etx.ServerNetwork.Delete().Where(servernetwork.ServerID(c.row.ID), servernetwork.StateEQ(servernetwork.StateAttaching)).Exec(ctx); err != nil {
				return err
			}
			u.ClearVmid().ClearCloneUpid().ClearCloneNonce().ClearIsoVolid().SetCreateStep(0)
		}
		return u.Exec(ctx)
	})
	if err != nil {
		return err
	}
	return failAction(ctx, w.o.Ent, c.args.ActionID, f)
}
