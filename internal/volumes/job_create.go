package volumes

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/riverqueue/river"
)

// CreateVolumeWorker allocates the disk and, with a server, attaches it in the same action.
type CreateVolumeWorker struct {
	river.WorkerDefaults[CreateVolumeArgs]
	o WorkerOptions
}

func NewCreateVolumeWorker(o WorkerOptions) *CreateVolumeWorker {
	o.defaults()
	return &CreateVolumeWorker{o: o}
}

func (*CreateVolumeWorker) Timeout(*river.Job[CreateVolumeArgs]) time.Duration { return jobTimeout }

func (w *CreateVolumeWorker) Work(ctx context.Context, job *river.Job[CreateVolumeArgs]) error {
	a := job.Args
	act, row, ret, stop := w.o.begin(ctx, a.ActionID, a.VolumeID)
	if stop {
		return ret
	}
	run := func(ctx context.Context) error {
		pc, vms, err := w.o.connect(ctx, row.LocationID)
		if err != nil {
			return err
		}
		row := row
		if row.Status == volume.StatusCreating {
			if row, err = w.alloc(ctx, pc, row, a.ServerID, a.File); err != nil {
				return err
			}
			if err := actions.Progress(ctx, w.o.Ent, a.ActionID, 30); err != nil {
				return err
			}
		}
		attached := a.ServerID != nil
		if attached {
			if row, err = w.o.plug(ctx, pc, vms, row, *a.ServerID, a.ActionID, 60); err != nil {
				return err
			}
		}
		err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
			if err := etx.Volume.UpdateOneID(row.ID).SetStatus(settledStatus(row)).Exec(ctx); err != nil {
				return err
			}
			return actions.Succeed(ctx, etx, a.ActionID)
		})
		if err != nil {
			return err
		}
		w.o.publish(ctx, ext.VolumeCreated, row, row.SizeGB, attached)
		return nil
	}
	fail := func(ctx context.Context, f servers.Failure) error {
		cur, err := w.o.Ent.Volume.Get(ctx, row.ID)
		if err != nil {
			return err
		}
		st := settledStatus(cur)
		if cur.Status == volume.StatusCreating {
			st = volume.StatusError
		}
		if err := cur.Update().SetStatus(st).Exec(ctx); err != nil {
			return err
		}
		if err := failAction(ctx, w.o.Ent, a.ActionID, f); err != nil {
			return err
		}
		// The disk exists and counts toward usage even though the attach failed.
		if st != volume.StatusError {
			w.o.publish(ctx, ext.VolumeCreated, cur, cur.SizeGB, cur.ServerID != nil)
		}
		return nil
	}
	return w.o.contract(job.JobRow, "create", act, row.ID, run, fail).Work(ctx)
}

// alloc creates the disk, on shared storage through the server's node if any, and records its volid.
// "already exists" with our exact name counts as done.
func (w *CreateVolumeWorker) alloc(ctx context.Context, pc pve.Client, row *ent.Volume, serverID *int64, file bool) (*ent.Volume, error) {
	var srv *ent.Server
	if serverID != nil {
		var err error
		if srv, err = w.o.Ent.Server.Get(ctx, *serverID); err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
	}
	node, err := w.o.storageNode(ctx, row, srv)
	if err != nil {
		return nil, err
	}
	volid := vref.Volid(row.Storage, row.ID, file)
	got, err := pc.AllocVolume(ctx, node, row.Storage, vref.OwnerVMID, vref.Filename(row.ID, file), row.SizeGB)
	switch {
	case err == nil && got != volid:
		if id, ok := vref.Parse(got); !ok || id != row.ID || !strings.HasPrefix(got, row.Storage+":") {
			return nil, servers.Permanent("allocate volume: storage %s returned volid %q, want %q", row.Storage, got, volid)
		}
		volid = got
	case err != nil && !alreadyExists(err):
		return nil, servers.StepError("allocate volume", err)
	case err != nil:
		_, ok, lerr := diskPresent(ctx, pc, node, row.Storage, volid, row.ID)
		if lerr != nil {
			return nil, servers.StepError("list storage", lerr)
		}
		if !ok {
			return nil, servers.StepError("allocate volume", err)
		}
	}
	return row.Update().SetVolid(volid).SetAppliedGB(row.SizeGB).SetStatus(volume.StatusAvailable).Save(ctx)
}
