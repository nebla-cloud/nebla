package servers

import (
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

type rebuildRun struct {
	w    *RebuildServerWorker
	args RebuildServerArgs
	row  *ent.Server
	b    *builder
}

func (w *RebuildServerWorker) Work(ctx context.Context, job *river.Job[RebuildServerArgs]) error {
	act, row, ret, stop := w.o.begin(ctx, job.Args.ActionID, job.Args.ServerID)
	if stop {
		return ret
	}
	r := &rebuildRun{w: w, args: job.Args, row: row}
	return contract{job: job.JobRow, name: "rebuild", finished: act.Finished != nil, log: w.o.Logger.With("server_id", row.ID),
		run: r.run, fail: r.cleanup}.work(ctx)
}

func (r *rebuildRun) load(ctx context.Context) error {
	loc, pc, assign, err := r.w.o.loadTarget(ctx, r.row)
	if err != nil {
		return err
	}
	r.b = &builder{pc: pc, loc: loc, assign: assign, row: r.row, tmpl: r.args.TemplateVMID, isoGen: r.row.IsoGeneration + 1,
		st:   buildState{Vmid: r.row.RebuildVmid, Upid: r.row.RebuildCloneUpid, Nonce: r.row.RebuildCloneNonce, Step: r.row.RebuildStep},
		save: r.save, poll: r.w.o.PollInterval}
	return nil
}

// save writes the build state to the rebuild columns.
func (r *rebuildRun) save(ctx context.Context, st buildState) error {
	u := r.w.o.Ent.Server.UpdateOneID(r.row.ID).SetRebuildStep(st.Step)
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
			return actions.Progress(ctx, r.w.o.Ent, r.args.ActionID, p)
		}
		return nil
	}
	return runSteps(ctx, func() int { return r.b.st.Step }, steps, progress, r.w.afterStep)
}

// swap makes the new VM the server's in one UPDATE and keeps the old one in rebuild_* to retire.
func (r *rebuildRun) swap(ctx context.Context) error {
	old := buildState{Vmid: r.row.Vmid, Upid: r.row.CloneUpid, Nonce: r.row.CloneNonce, Step: stepSwap}
	nu := r.b.st
	u := r.w.o.Ent.Server.UpdateOneID(r.row.ID).
		SetImageID(r.args.ImageID).SetTemplateVmid(r.args.TemplateVMID).
		SetIsoGeneration(r.b.isoGen).SetIsoVolid(r.b.volid()).SetCreateStep(stepDone).SetRebuildStep(stepSwap)
	setSlot(u.SetVmid, u.ClearVmid, nu.Vmid)
	setSlot(u.SetCloneUpid, u.ClearCloneUpid, nu.Upid)
	setSlot(u.SetCloneNonce, u.ClearCloneNonce, nu.Nonce)
	setSlot(u.SetRebuildVmid, u.ClearRebuildVmid, old.Vmid)
	setSlot(u.SetRebuildCloneUpid, u.ClearRebuildCloneUpid, old.Upid)
	setSlot(u.SetRebuildCloneNonce, u.ClearRebuildCloneNonce, old.Nonce)
	row, err := u.Save(ctx)
	if err != nil {
		return err
	}
	r.row, r.b.row, r.b.st = row, row, old
	return nil
}

func (r *rebuildRun) retire(ctx context.Context) error {
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
	if r.args.StartAfter {
		if err := startVM(ctx, r.b.pc, *r.row.Vmid, r.w.o.PollInterval); err != nil {
			return err
		}
	}
	return r.b.done(ctx, stepRestart)
}

func (r *rebuildRun) finish(ctx context.Context) error {
	status := StatusOff
	if r.args.StartAfter {
		status = StatusRunning
	}
	return db.WithTx(ctx, r.w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if err := etx.Server.UpdateOneID(r.row.ID).SetStatus(server.Status(status)).SetRebuildStep(0).Exec(ctx); err != nil {
			return err
		}
		return actions.Succeed(ctx, etx, r.args.ActionID)
	})
}

// cleanup removes whichever VM rebuild_* holds: the new one before the swap, the old one after.
func (r *rebuildRun) cleanup(ctx context.Context, msg string) error {
	log := r.w.o.Logger.With("server_id", r.row.ID, "action_id", r.args.ActionID)
	log.Warn("rebuild server failed", "err", msg)
	cur, err := r.w.o.Ent.Server.Get(ctx, r.row.ID)
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
			log.Error("rebuild cleanup: destroy vm", "vmid", *cur.RebuildVmid, "err", err)
		}
	}
	swapped := cur.RebuildStep >= stepSwap
	if !swapped && r.b != nil {
		gen := cur.IsoGeneration + 1
		if err := deleteISO(ctx, r.b.pc, cur.Node, r.b.loc.ISOStorage, isoVolid(r.b.loc.ISOStorage, cur.ID, gen)); err != nil {
			log.Error("rebuild cleanup: delete iso", "err", err)
		}
	}
	switch {
	case !gone && swapped:
		msg += fmt.Sprintf("; old VM %d could not be removed", *cur.RebuildVmid)
	case !gone:
		msg += fmt.Sprintf("; cleanup incomplete: VM %d could not be removed", *cur.RebuildVmid)
	}
	fallback := StatusOff
	if r.args.StartAfter {
		fallback = StatusRunning
	}
	var st string
	if r.b != nil {
		st = realStatus(ctx, r.b.pc, cur.ID, cur.Vmid, fallback)
	} else {
		st = fallback
	}
	u := r.w.o.Ent.Server.UpdateOneID(cur.ID).SetStatus(server.Status(st)).SetRebuildStep(0)
	if gone {
		u.ClearRebuildVmid().ClearRebuildCloneUpid().ClearRebuildCloneNonce()
	}
	if err := u.Exec(ctx); err != nil {
		return err
	}
	return failAction(ctx, r.w.o.Ent, r.args.ActionID, msg)
}
