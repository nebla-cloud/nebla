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
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/riverqueue/river"
)

const (
	KindRebuildServer = "rebuild_server"
	rebuildTimeout    = 15 * time.Minute
)

// Rebuild steps after the shared build steps, persisted in servers.rebuild_step.
const (
	// stepSwap makes the new VM the server's; rebuild_* then holds the old one.
	stepSwap = stepFirewall + 1 + iota
	stepRetire
	stepRestart
	stepRebuilt
)

var rebuildProgress = map[int]int{stepVMID: 10, stepClone: 40, stepConfig: 50, stepResize: 55, stepISO: 60, stepFirewall: 65,
	stepSwap: 75, stepRetire: 85, stepRestart: 95}

// RebuildServerArgs is the rebuild_server job; the template was chosen at request time.
type RebuildServerArgs struct {
	ServerID     int64 `json:"server_id"`
	ActionID     int64 `json:"action_id"`
	ImageID      int64 `json:"image_id"`
	TemplateVMID int   `json:"template_vmid"`
	// StartAfter is whether the server was running when the rebuild was requested.
	StartAfter bool `json:"start_after"`
	// InstanceGeneration pins the cloud-init instance-id generation; 0 uses the new ISO generation.
	InstanceGeneration int `json:"instance_generation,omitempty"`
}

func (RebuildServerArgs) Kind() string                 { return KindRebuildServer }
func (RebuildServerArgs) InsertOpts() river.InsertOpts { return insertOpts() }

// Rebuild queues a reinstall from image onto a new VM with the same name, MAC and IPs.
func (s *Service) Rebuild(ctx context.Context, projectID, id int64, imageRef string) (actions.Action, error) {
	img, err := s.o.Catalog.ImageByRef(ctx, imageRef)
	if err != nil {
		return actions.Action{}, err
	}
	return s.queue(ctx, projectID, id, KindRebuildServer, func(etx *ent.Client, row *ent.Server) (func(int64) river.JobArgs, error) {
		switch {
		case row.Status == StatusMissing:
			return nil, ErrMissing
		case row.Vmid == nil || (row.Status != StatusRunning && row.Status != StatusOff):
			return nil, errNotReady
		}
		tmpls, err := s.o.Catalog.TemplatesFor(ctx, img.ID, row.LocationID)
		if err != nil {
			return nil, err
		}
		tmpl := 0
		for _, t := range tmpls {
			if t.Node == row.Node || (t.Node == "" && tmpl == 0) {
				tmpl = t.VMID
			}
		}
		if tmpl == 0 {
			return nil, invalid("image %s is not available on this server's node", img.Name)
		}
		if err := etx.Server.UpdateOneID(id).SetStatus(StatusRebuilding).SetRebuildStep(0).Exec(ctx); err != nil {
			return nil, err
		}
		if err := dropQueuedBackups(ctx, etx, id); err != nil {
			return nil, err
		}
		running := row.Status == StatusRunning
		return func(actionID int64) river.JobArgs {
			return RebuildServerArgs{ServerID: id, ActionID: actionID, ImageID: img.ID, TemplateVMID: tmpl, StartAfter: running}
		}, nil
	})
}

// RebuildServerWorker builds the new VM beside the old one, swaps them, then retires the old one.
type RebuildServerWorker struct {
	river.WorkerDefaults[RebuildServerArgs]
	o         WorkerOptions
	afterStep func(step int)
}

func NewRebuildServerWorker(o WorkerOptions) *RebuildServerWorker {
	o.defaults()
	return &RebuildServerWorker{o: o}
}

func (*RebuildServerWorker) Timeout(*river.Job[RebuildServerArgs]) time.Duration {
	return rebuildTimeout
}

// rebuildRun builds a new VM beside the server's and swaps it in; rebuild and restore differ in source and bookkeeping.
type rebuildRun struct {
	o          *WorkerOptions
	afterStep  func(int)
	afterPost  func()
	afterUpid  func()
	name       string
	actionID   int64
	startAfter bool
	instGen    int
	// image is the rebuild's new image; nil keeps the server's.
	image *rebuildImage
	// archive, when set, restores this backup instead of cloning the image's template.
	archive string
	// finishTx runs in finish's transaction, afterFinish once it commits; onFail runs in cleanup before the action fails.
	finishTx    func(context.Context, *ent.Client) error
	afterFinish func(context.Context)
	onFail      func(context.Context) error
	row         *ent.Server
	b           *builder
}

type rebuildImage struct {
	id   int64
	tmpl int
}

func (w *RebuildServerWorker) Work(ctx context.Context, job *river.Job[RebuildServerArgs]) error {
	act, row, ret, stop := w.o.begin(ctx, job.Args.ActionID, job.Args.ServerID)
	if stop {
		return ret
	}
	a := job.Args
	r := &rebuildRun{o: &w.o, afterStep: w.afterStep, name: "rebuild", actionID: a.ActionID, startAfter: a.StartAfter,
		instGen: a.InstanceGeneration, image: &rebuildImage{a.ImageID, a.TemplateVMID}, row: row}
	return contract{job: job.JobRow, name: "rebuild", finished: act.Finished != nil, log: actionLog(w.o.Logger, act, row.ID),
		run: r.run, fail: r.cleanup}.work(ctx)
}

func (r *rebuildRun) load(ctx context.Context) error {
	loc, pc, assign, err := r.o.loadTarget(ctx, r.row)
	if err != nil {
		return err
	}
	tmpl := r.row.TemplateVmid
	if r.image != nil {
		tmpl = r.image.tmpl
	}
	r.b = &builder{pc: pc, loc: loc, assign: assign, row: r.row, tmpl: tmpl, archive: r.archive, isoGen: r.row.IsoGeneration + 1, nextGen: true,
		instGen: cmp.Or(r.instGen, r.row.IsoGeneration+1), pinInst: r.instGen,
		st:   buildState{Vmid: r.row.RebuildVmid, Upid: r.row.RebuildCloneUpid, Nonce: r.row.RebuildCloneNonce, Step: r.row.RebuildStep},
		save: r.save, poll: r.o.PollInterval, o: r.o, afterClonePost: r.afterPost, afterUpidSave: r.afterUpid}
	return nil
}

// save writes the build state to the rebuild columns.
func (r *rebuildRun) save(ctx context.Context, st buildState) error {
	u := r.o.Ent.Server.UpdateOneID(r.row.ID).SetRebuildStep(st.Step)
	setSlot(u.SetRebuildVmid, u.ClearRebuildVmid, st.Vmid)
	setSlot(u.SetRebuildCloneUpid, u.ClearRebuildCloneUpid, st.Upid)
	setSlot(u.SetRebuildCloneNonce, u.ClearRebuildCloneNonce, st.Nonce)
	row, err := u.Save(ctx)
	if err != nil {
		return err
	}
	r.row, r.b.row = row, row
	return nil
}

// setSlot sets a nillable column from v, clearing it when v is nil.
func setSlot[T any](set func(T) *ent.ServerUpdateOne, clear func() *ent.ServerUpdateOne, v *T) {
	if v == nil {
		clear()
		return
	}
	set(*v)
}

func (r *rebuildRun) run(ctx context.Context) error {
	if err := r.load(ctx); err != nil {
		return err
	}
	if r.b.st.Step == 0 && r.b.st.Vmid != nil {
		// A VM left behind by an earlier failed rebuild.
		if err := destroyOrForget(ctx, r.b.pc, r.row.ID, *r.b.st.Vmid, r.b.st.proof()); err != nil {
			return &stepError{"remove leftover vm", err}
		}
		if err := r.b.update(ctx, func(s *buildState) { *s = buildState{} }); err != nil {
			return err
		}
	}
	steps := append(r.b.steps(),
		step{stepSwap, "switch to new vm", r.swap},
		step{stepRetire, "remove old vm", r.retire},
		step{stepRestart, "start vm", r.restart},
		step{stepRebuilt, "finish", r.finish})
	progress := func(ctx context.Context, n int) error {
		if p, ok := rebuildProgress[n]; ok {
			return actions.Progress(ctx, r.o.Ent, r.actionID, p)
		}
		return nil
	}
	return runSteps(ctx, func() int { return r.b.st.Step }, steps, progress, r.afterStep)
}

// swap makes the new VM the server's in one UPDATE and keeps the old one in rebuild_* to retire.
func (r *rebuildRun) swap(ctx context.Context) error {
	if err := r.b.firewallCurrent(ctx, r.b.vmid()); err != nil {
		return err
	}
	// The new VM must stay stopped until retire has unplugged the old one.
	if err := r.plugNewVM(ctx); err != nil {
		return &stepError{"attach volumes to new vm", err}
	}
	old := buildState{Vmid: r.row.Vmid, Upid: r.row.CloneUpid, Nonce: r.row.CloneNonce, Step: stepSwap}
	nu := r.b.st
	var row *ent.Server
	err := db.WithTx(ctx, r.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		u := etx.Server.UpdateOneID(r.row.ID)
		if r.image != nil {
			u.SetImageID(r.image.id).SetTemplateVmid(r.image.tmpl)
		}
		u.SetIsoGeneration(r.b.isoGen).SetInstanceGeneration(r.b.instGen).SetIsoVolid(r.b.volid()).SetCreateStep(stepDone).SetRebuildStep(stepSwap).
			SetInstanceGen(int64(r.b.instGen)).SetNetRefresh(true).SetIsoNetHash(r.b.netHash)
		setSlot(u.SetVmid, u.ClearVmid, nu.Vmid)
		setSlot(u.SetCloneUpid, u.ClearCloneUpid, nu.Upid)
		setSlot(u.SetCloneNonce, u.ClearCloneNonce, nu.Nonce)
		setSlot(u.SetRebuildVmid, u.ClearRebuildVmid, old.Vmid)
		setSlot(u.SetRebuildCloneUpid, u.ClearRebuildCloneUpid, old.Upid)
		setSlot(u.SetRebuildCloneNonce, u.ClearRebuildCloneNonce, old.Nonce)
		var err error
		if row, err = u.Save(ctx); err != nil {
			return err
		}
		return r.b.raiseApplied(ctx, tx)
	})
	if err != nil {
		return err
	}
	r.row, r.b.row, r.b.st = row, row, old
	return nil
}

func (r *rebuildRun) retire(ctx context.Context) error {
	if err := r.unplugOldVM(ctx); err != nil {
		return &stepError{"detach volumes from old vm", err}
	}
	if r.b.st.Vmid != nil {
		if err := destroyOrForget(ctx, r.b.pc, r.row.ID, *r.b.st.Vmid, CloneProof{}); err != nil {
			return err
		}
	}
	if gen := r.row.IsoGeneration - 1; gen >= 1 {
		if err := deleteISO(ctx, r.b.pc, r.row.Node, r.b.loc.ISOStorage, isoVolid(r.b.loc.ISOStorage, r.row.ID, gen)); err != nil {
			return err
		}
	}
	return r.b.update(ctx, func(s *buildState) { *s = buildState{Step: stepRetire} })
}

func (r *rebuildRun) restart(ctx context.Context) error {
	if err := r.b.protect(ctx, *r.row.Vmid); err != nil {
		return err
	}
	if r.startAfter {
		if err := startVM(ctx, r.b.pc, *r.row.Vmid, r.o.PollInterval); err != nil {
			return err
		}
	}
	return r.b.done(ctx, stepRestart)
}

func (r *rebuildRun) finish(ctx context.Context) error {
	status := StatusOff
	if r.startAfter {
		status = StatusRunning
	}
	err := db.WithTx(ctx, r.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if err := etx.Server.UpdateOneID(r.row.ID).SetStatus(server.Status(status)).SetRebuildStep(0).Exec(ctx); err != nil {
			return err
		}
		if err := markAttached(ctx, etx, r.row.ID); err != nil {
			return err
		}
		if err := r.b.raiseApplied(ctx, tx); err != nil {
			return err
		}
		// Network changes made after the ISO step reach the ISO through this sync.
		if err := queueISOSyncTx(ctx, tx, r.o.jobs(), r.row.ID); err != nil {
			return err
		}
		if r.finishTx != nil {
			if err := r.finishTx(ctx, etx); err != nil {
				return err
			}
		}
		return actions.Succeed(ctx, etx, r.actionID)
	})
	if err == nil && r.afterFinish != nil {
		r.afterFinish(ctx)
	}
	return err
}

// cleanup removes whichever VM rebuild_* holds: the new one before the swap, the old one after.
func (r *rebuildRun) cleanup(ctx context.Context, f failure) error {
	log := r.o.Logger.With("server_id", r.row.ID, "action_id", r.actionID)
	log.Warn(r.name+" server failed", "err", f.detail, "code", f.code)
	cur, err := r.o.Ent.Server.Get(ctx, r.row.ID)
	if err != nil {
		return err
	}
	r.row = cur
	if r.b == nil {
		if err := r.load(ctx); err != nil && retryable(err) {
			return err
		}
	}
	gone := cur.RebuildVmid == nil
	if !gone && r.b != nil {
		err := destroyOrForget(ctx, r.b.pc, cur.ID, *cur.RebuildVmid, CloneProof{UPID: cur.RebuildCloneUpid, Nonce: cur.RebuildCloneNonce})
		if gone = err == nil; !gone {
			log.Error(r.name+" cleanup: destroy vm", "vmid", *cur.RebuildVmid, "err", err)
		}
	}
	swapped := cur.RebuildStep >= stepSwap
	if !swapped && r.b != nil {
		gen := cur.IsoGeneration + 1
		if err := deleteISO(ctx, r.b.pc, cur.Node, r.b.loc.ISOStorage, isoVolid(r.b.loc.ISOStorage, cur.ID, gen)); err != nil {
			log.Error(r.name+" cleanup: delete iso", "err", err)
		}
	}
	switch {
	case !gone && swapped:
		f.detail += fmt.Sprintf("; old VM %d could not be removed", *cur.RebuildVmid)
	case !gone:
		f.detail += fmt.Sprintf("; cleanup incomplete: VM %d could not be removed", *cur.RebuildVmid)
	}
	fallback := StatusOff
	if r.startAfter {
		fallback = StatusRunning
	}
	var st string
	if r.b != nil {
		st = realStatus(ctx, r.b.pc, cur.ID, cur.Vmid, fallback)
	} else {
		st = fallback
	}
	u := r.o.Ent.Server.UpdateOneID(cur.ID).SetStatus(server.Status(st)).SetRebuildStep(0)
	if gone {
		u.ClearRebuildVmid().ClearRebuildCloneUpid().ClearRebuildCloneNonce()
	}
	if err := u.Exec(ctx); err != nil {
		return err
	}
	// Syncs waited for the rebuild or restore; the kept VM's ISO may be behind.
	if err := QueueISOSync(ctx, r.o.SQL, r.o.jobs(), cur.ID); err != nil {
		log.Warn(r.name+" cleanup: queue iso sync", "err", err)
	}
	if r.onFail != nil {
		if err := r.onFail(ctx); err != nil {
			return err
		}
	}
	return failAction(ctx, r.o.Ent, r.actionID, f)
}
