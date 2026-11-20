package servers

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
)

const (
	KindChangeType    = "change_server_type"
	changeTypeTimeout = 5 * time.Minute
)

// Spec is a server's type snapshot.
type Spec struct {
	TypeID   int64 `json:"server_type_id"`
	Cores    int   `json:"cores"`
	MemoryMB int   `json:"memory_mb"`
	DiskGB   int   `json:"disk_gb"`
}

// ChangeTypeArgs is the change_server_type job: To is applied on success, From restored on failure.
type ChangeTypeArgs struct {
	ServerID    int64 `json:"server_id"`
	ActionID    int64 `json:"action_id"`
	UpgradeDisk bool  `json:"upgrade_disk"`
	From        Spec  `json:"from"`
	To          Spec  `json:"to"`
}

func (ChangeTypeArgs) Kind() string                 { return KindChangeType }
func (ChangeTypeArgs) InsertOpts() river.InsertOpts { return insertOpts() }

func specOf(r *ent.Server) Spec {
	return Spec{TypeID: r.ServerTypeID, Cores: r.Cores, MemoryMB: r.MemoryMB, DiskGB: r.DiskGB}
}

func (s Spec) apply(u *ent.ServerUpdateOne) *ent.ServerUpdateOne {
	return u.SetServerTypeID(s.TypeID).SetCores(s.Cores).SetMemoryMB(s.MemoryMB).SetDiskGB(s.DiskGB)
}

// Restored is From with the disk at its actual size, or at any requested growth when actualGB is unknown.
func (a ChangeTypeArgs) Restored(actualGB *int) Spec {
	s := a.From
	switch {
	case actualGB != nil:
		s.DiskGB = max(a.From.DiskGB, *actualGB)
	case a.UpgradeDisk:
		s.DiskGB = max(a.From.DiskGB, a.To.DiskGB)
	}
	return s
}

// BootDiskGB is v's boot disk size in GB, rounded up; nil when it cannot be read.
func BootDiskGB(ctx context.Context, pc pve.Client, v pve.VM) *int {
	cfg, err := pc.Config(ctx, v.Node, v.VMID)
	if err != nil {
		return nil
	}
	disk := catalog.BootDisk(cfg)
	if disk == "" {
		return nil
	}
	b, ok := diskBytes(cfg[disk])
	if !ok {
		return nil
	}
	gb := int((b + 1<<30 - 1) >> 30)
	return &gb
}

// ChangeType reserves max(from, to) per dimension on the row and queues the job; only success frees a downsize.
func (s *Service) ChangeType(ctx context.Context, projectID, id int64, typeRef string, upgradeDisk bool) (actions.Action, error) {
	typ, err := s.o.Catalog.ServerTypeByRef(ctx, typeRef)
	if err != nil {
		return actions.Action{}, err
	}
	return s.queue(ctx, projectID, id, KindChangeType, func(etx *ent.Client, row *ent.Server) (func(int64) river.JobArgs, error) {
		to, err := targetSpec(row, typ, upgradeDisk)
		if err != nil {
			return nil, err
		}
		from := specOf(row)
		held := Spec{TypeID: from.TypeID, Cores: max(from.Cores, to.Cores), MemoryMB: max(from.MemoryMB, to.MemoryMB), DiskGB: max(from.DiskGB, to.DiskGB)}
		delta := projects.Usage{VCPU: held.Cores - from.Cores, MemoryMB: held.MemoryMB - from.MemoryMB, DiskGB: held.DiskGB - from.DiskGB}
		if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, delta); err != nil {
			return nil, err
		}
		if err := held.apply(etx.Server.UpdateOneID(id)).Exec(ctx); err != nil {
			return nil, writeErr(err)
		}
		return func(actionID int64) river.JobArgs {
			return ChangeTypeArgs{ServerID: id, ActionID: actionID, UpgradeDisk: upgradeDisk, From: from, To: to}
		}, nil
	})
}

// targetSpec checks a change to typ; disks never shrink and only grow with upgradeDisk.
func targetSpec(row *ent.Server, typ catalog.ServerType, upgradeDisk bool) (Spec, error) {
	switch {
	case row.Status != StatusOff:
		return Spec{}, errNotOff
	case row.Vmid == nil:
		return Spec{}, errNotReady
	case typ.Deprecated:
		return Spec{}, invalid("server type %s is deprecated", typ.Name)
	case !typ.AvailableIn(row.LocationID):
		return Spec{}, invalid("server type %s is not offered in this server's location", typ.Name)
	case upgradeDisk && typ.DiskGB < row.DiskGB:
		return Spec{}, errShrink
	}
	to := Spec{TypeID: typ.ID, Cores: typ.Cores, MemoryMB: typ.MemoryMB, DiskGB: row.DiskGB}
	if upgradeDisk {
		to.DiskGB = typ.DiskGB
	}
	return to, nil
}

// ChangeTypeWorker applies the server's snapshot to its stopped VM.
type ChangeTypeWorker struct {
	river.WorkerDefaults[ChangeTypeArgs]
	o WorkerOptions
}

func NewChangeTypeWorker(o WorkerOptions) *ChangeTypeWorker {
	o.defaults()
	return &ChangeTypeWorker{o: o}
}

func (*ChangeTypeWorker) Timeout(*river.Job[ChangeTypeArgs]) time.Duration { return changeTypeTimeout }

func (w *ChangeTypeWorker) Work(ctx context.Context, job *river.Job[ChangeTypeArgs]) error {
	act, row, ret, stop := w.o.begin(ctx, job.Args.ActionID, job.Args.ServerID)
	if stop {
		return ret
	}
	a := job.Args
	var pc pve.Client
	run := func(ctx context.Context) error {
		var err error
		if pc, err = w.o.Catalog.ProxmoxFor(ctx, row.LocationID); err != nil {
			return &stepError{"connect to proxmox", err}
		}
		return w.run(ctx, pc, row, a)
	}
	fail := func(ctx context.Context, f failure) error {
		if pc == nil {
			pc, _ = w.o.Catalog.ProxmoxFor(ctx, row.LocationID)
		}
		var actualGB *int
		if pc != nil && row.Vmid != nil {
			// Best effort: put cores and memory back; a grown disk cannot shrink and was the last step.
			if v, found, err := findVM(ctx, pc, *row.Vmid); err == nil && found && ownedBy(v, row.ID) {
				kv := map[string]string{"cores": strconv.Itoa(a.From.Cores), "memory": strconv.Itoa(a.From.MemoryMB)}
				if err := pc.SetConfig(ctx, v.Node, v.VMID, kv); err != nil {
					w.o.Logger.Error("change type: restore config", "server_id", row.ID, "err", err)
				}
				actualGB = BootDiskGB(ctx, pc, v)
			}
		}
		if err := a.Restored(actualGB).apply(w.o.Ent.Server.UpdateOneID(row.ID)).Exec(ctx); err != nil {
			return err
		}
		return failAction(ctx, w.o.Ent, a.ActionID, f)
	}
	return contract{job: job.JobRow, name: "change type", finished: act.Finished != nil, log: actionLog(w.o.Logger, act, row.ID),
		run: run, fail: fail}.work(ctx)
}

func (w *ChangeTypeWorker) run(ctx context.Context, pc pve.Client, row *ent.Server, a ChangeTypeArgs) error {
	if row.Vmid == nil {
		return missing("server has no VM")
	}
	v, found, err := findVM(ctx, pc, *row.Vmid)
	switch {
	case err != nil:
		return &stepError{"find vm", err}
	case !found || !ownedBy(v, row.ID):
		return missing("vm %d is missing", *row.Vmid)
	case v.Status != "stopped":
		return permanent("vm %d must be stopped", v.VMID)
	}
	kv := map[string]string{"cores": strconv.Itoa(a.To.Cores), "memory": strconv.Itoa(a.To.MemoryMB)}
	if err := pc.SetConfig(ctx, v.Node, v.VMID, kv); err != nil {
		return &stepError{"configure vm", err}
	}
	if a.UpgradeDisk {
		if err := growDisk(ctx, pc, v, a.To.DiskGB); err != nil {
			return &stepError{"resize disk", err}
		}
	}
	var done *ent.Server
	err = db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		var err error
		if done, err = a.To.apply(etx.Server.UpdateOneID(row.ID)).Save(ctx); err != nil {
			return err
		}
		return actions.Succeed(ctx, etx, a.ActionID)
	})
	if err != nil {
		return err
	}
	w.o.publish(ctx, ext.ServerTypeChanged, done, map[string]any{"cores": done.Cores, "memory_mb": done.MemoryMB, "disk_gb": done.DiskGB,
		"previous_server_type_id": a.From.TypeID})
	return nil
}

// growDisk resizes v's boot disk to gb unless it is already that large.
func growDisk(ctx context.Context, pc pve.Client, v pve.VM, gb int) error {
	cfg, err := pc.Config(ctx, v.Node, v.VMID)
	if err != nil {
		return err
	}
	disk := catalog.BootDisk(cfg)
	if disk == "" {
		return permanent("vm %d has no boot disk", v.VMID)
	}
	if cur, ok := diskBytes(cfg[disk]); ok && int64(gb)<<30 <= cur {
		return nil
	}
	return pc.ResizeDisk(ctx, v.Node, v.VMID, disk, gb)
}
