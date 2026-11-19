package servers

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/riverqueue/river"
)

const (
	KindPowerServer = "power_server"
	powerWait       = 2 * time.Minute
	powerTimeout    = 5 * time.Minute
)

// PowerServerArgs is the power_server job; From is the status when it was requested.
type PowerServerArgs struct {
	ServerID int64       `json:"server_id"`
	ActionID int64       `json:"action_id"`
	Op       pve.PowerOp `json:"op"`
	From     string      `json:"from_status"`
}

func (PowerServerArgs) Kind() string                 { return KindPowerServer }
func (PowerServerArgs) InsertOpts() river.InsertOpts { return insertOpts() }

// powerCommand is the action command for each power op.
var powerCommand = map[pve.PowerOp]string{
	pve.PowerStart: "start_server", pve.PowerStop: "stop_server", pve.PowerShutdown: "shutdown_server",
	pve.PowerReboot: "reboot_server", pve.PowerReset: "reset_server",
}

// stops reports whether op leaves the VM stopped.
func stops(op pve.PowerOp) bool { return op == pve.PowerStop || op == pve.PowerShutdown }

// Power queues a power op; the job skips Proxmox when the VM is already in the target state.
func (s *Service) Power(ctx context.Context, projectID, id int64, op pve.PowerOp) (actions.Action, error) {
	cmd, ok := powerCommand[op]
	if !ok {
		return actions.Action{}, invalid("unknown power op %q", op)
	}
	return s.queue(ctx, projectID, id, cmd, func(_ *ent.Client, row *ent.Server) (func(int64) river.JobArgs, error) {
		switch {
		case row.Status == StatusMissing:
			return nil, ErrMissing
		case row.Vmid == nil:
			return nil, errNotReady
		}
		from := string(row.Status)
		return func(actionID int64) river.JobArgs {
			return PowerServerArgs{ServerID: id, ActionID: actionID, Op: op, From: from}
		}, nil
	})
}

// PowerServerWorker runs one power op and sets the status Proxmox ends in.
type PowerServerWorker struct {
	river.WorkerDefaults[PowerServerArgs]
	o WorkerOptions
}

func NewPowerServerWorker(o WorkerOptions) *PowerServerWorker {
	o.defaults()
	return &PowerServerWorker{o: o}
}

func (*PowerServerWorker) Timeout(*river.Job[PowerServerArgs]) time.Duration { return powerTimeout }

func (w *PowerServerWorker) Work(ctx context.Context, job *river.Job[PowerServerArgs]) error {
	act, row, ret, stop := w.o.begin(ctx, job.Args.ActionID, job.Args.ServerID)
	if stop {
		return ret
	}
	var pc pve.Client
	a := job.Args
	run := func(ctx context.Context) error {
		var err error
		if pc, err = w.o.Catalog.ProxmoxFor(ctx, row.LocationID); err != nil {
			return &stepError{"connect to proxmox", err}
		}
		return w.run(ctx, pc, row, a)
	}
	fail := func(ctx context.Context, msg string) error {
		if pc == nil {
			pc, _ = w.o.Catalog.ProxmoxFor(ctx, row.LocationID)
		}
		st := realStatus(ctx, pc, row.ID, row.Vmid, cmp.Or(a.From, string(row.Status)))
		if err := w.o.Ent.Server.UpdateOneID(row.ID).SetStatus(server.Status(st)).Exec(ctx); err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, a.ActionID, msg)
	}
	return contract{job: job.JobRow, name: string(a.Op), finished: act.Finished != nil, log: w.o.Logger.With("server_id", row.ID),
		run: run, fail: fail}.work(ctx)
}

func (w *PowerServerWorker) run(ctx context.Context, pc pve.Client, row *ent.Server, a PowerServerArgs) error {
	if row.Vmid == nil {
		return permanent("server has no VM")
	}
	vmid := *row.Vmid
	v, found, err := findVM(ctx, pc, vmid)
	if err != nil {
		return &stepError{"find vm", err}
	}
	if !found || !ownedBy(v, row.ID) {
		return permanent("vm %d is missing", vmid)
	}
	want, mid := "running", StatusStarting
	if stops(a.Op) {
		want, mid = "stopped", StatusStopping
	}
	if a.Op == pve.PowerStart || stops(a.Op) {
		if err := w.o.Ent.Server.UpdateOneID(row.ID).SetStatus(server.Status(mid)).Exec(ctx); err != nil {
			return err
		}
	}
	if v.Status != want || (a.Op != pve.PowerStart && !stops(a.Op)) {
		if err := w.power(ctx, pc, v.Node, vmid, a.Op); err != nil {
			return &stepError{string(a.Op) + " vm", err}
		}
	}
	if err := waitStatus(ctx, pc, vmid, want, w.o.PowerWait, w.o.PollInterval); err != nil {
		return &stepError{string(a.Op) + " vm", err}
	}
	status := StatusRunning
	if want == "stopped" {
		status = StatusOff
	}
	return db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if err := etx.Server.UpdateOneID(row.ID).SetStatus(server.Status(status)).Exec(ctx); err != nil {
			return err
		}
		return actions.Succeed(ctx, etx, a.ActionID)
	})
}

// power runs op and waits for its task, at most PowerWait.
func (w *PowerServerWorker) power(ctx context.Context, pc pve.Client, node string, vmid int, op pve.PowerOp) error {
	up, err := pc.Power(ctx, node, vmid, op)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, w.o.PowerWait)
	defer cancel()
	err = pc.WaitTask(wctx, node, up)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return permanent("%s did not finish within %s", op, w.o.PowerWait)
	}
	return err
}
