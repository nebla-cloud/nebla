package servers

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/cloudinit"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/ipam"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/riverqueue/river"
)

const (
	// MaxCreateAttempts is the logical retry cap; River gets spare attempts that only clean up.
	MaxCreateAttempts = 3
	spareAttempts     = 2
	// cleanupPending prefixes a job error whose cleanup must run again.
	cleanupPending = "nebla: cleanup pending: "
	createTimeout  = 10 * time.Minute
	startWait      = 2 * time.Minute
	maxVMIDTries   = 3
)

// Create job steps, persisted in servers.create_step.
const (
	stepVMID = iota + 1
	stepClone
	stepConfig
	stepResize
	stepISO
	stepFirewall
	stepStart
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
	// PollInterval spaces the wait-for-running checks; 1s when zero.
	PollInterval time.Duration
}

// RegisterWorkers adds the server job workers; they follow the jobs.StartAction worker contract.
func RegisterWorkers(w *river.Workers, o WorkerOptions) {
	river.AddWorker(w, NewCreateServerWorker(o))
}

func (CreateServerArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: MaxCreateAttempts + spareAttempts}
}

// CreateServerWorker builds a server's VM from its row snapshot, resuming at servers.create_step.
type CreateServerWorker struct {
	river.WorkerDefaults[CreateServerArgs]
	o         WorkerOptions
	afterStep func(step int)
}

func NewCreateServerWorker(o WorkerOptions) *CreateServerWorker {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.PollInterval <= 0 {
		o.PollInterval = time.Second
	}
	return &CreateServerWorker{o: o}
}

func (*CreateServerWorker) Timeout(*river.Job[CreateServerArgs]) time.Duration { return createTimeout }

// createRun is one attempt's state.
type createRun struct {
	w      *CreateServerWorker
	args   CreateServerArgs
	row    *ent.Server
	loc    catalog.Location
	pc     pve.Client
	tnode  string
	tcfg   map[string]string
	assign []ipam.Assignment
}

func (w *CreateServerWorker) Work(ctx context.Context, job *river.Job[CreateServerArgs]) error {
	act, err := w.o.Ent.Action.Get(ctx, job.Args.ActionID)
	if ent.IsNotFound(err) {
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	if act.Finished != nil {
		return nil
	}
	row, err := w.o.Ent.Server.Get(ctx, job.Args.ServerID)
	if ent.IsNotFound(err) {
		if ferr := w.failAction(ctx, job.Args.ActionID, "server no longer exists"); ferr != nil {
			return ferr
		}
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	c := &createRun{w: w, args: job.Args, row: row}
	if msg, ok := pendingCleanup(job); ok || job.Attempt > MaxCreateAttempts {
		if !ok {
			msg = "create did not finish after " + strconv.Itoa(MaxCreateAttempts) + " attempts" + lastError(job)
		}
		if err := c.load(ctx); err != nil && retryable(err) {
			return errors.New(cleanupPending + msg)
		}
		return w.finishCleanup(ctx, c, msg, errors.New(msg))
	}
	err = c.run(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	retry := retryable(err) || errors.Is(ctx.Err(), context.DeadlineExceeded)
	if retry && job.Attempt < MaxCreateAttempts {
		return err
	}
	return w.finishCleanup(ctx, c, errorMessage(err), err)
}

// retryable reports errors a later attempt may get past.
func retryable(err error) bool {
	var te *transientError
	return pve.IsTransient(err) || db.IsTransient(err) || errors.As(err, &te)
}

// pendingCleanup returns the action message when the previous attempt's cleanup did not finish.
func pendingCleanup(job *river.Job[CreateServerArgs]) (string, bool) {
	if n := len(job.Errors); n > 0 {
		return strings.CutPrefix(job.Errors[n-1].Error, cleanupPending)
	}
	return "", false
}

func lastError(job *river.Job[CreateServerArgs]) string {
	if n := len(job.Errors); n > 0 {
		return ": " + job.Errors[n-1].Error
	}
	return ""
}

// finishCleanup cancels the job once cleanup is recorded, else retries it.
func (w *CreateServerWorker) finishCleanup(ctx context.Context, c *createRun, msg string, cause error) error {
	if err := w.cleanup(ctx, c, cause, msg); err != nil {
		w.o.Logger.Error("cleanup will be retried", "server_id", c.row.ID, "err", err)
		return errors.New(cleanupPending + msg)
	}
	return river.JobCancel(cause)
}

func (c *createRun) load(ctx context.Context) error {
	var err error
	if c.loc, err = c.w.o.Catalog.GetLocation(ctx, c.row.LocationID); err != nil {
		return &stepError{"load location", err}
	}
	if c.pc, err = c.w.o.Catalog.ProxmoxFor(ctx, c.row.LocationID); err != nil {
		return &stepError{"connect to proxmox", err}
	}
	if c.assign, err = ipam.Assignments(ctx, c.w.o.Ent, c.row.ID); err != nil {
		return &stepError{"load ips", err}
	}
	return nil
}

func (c *createRun) run(ctx context.Context) error {
	if err := c.load(ctx); err != nil {
		return err
	}
	steps := []struct {
		n    int
		name string
		fn   func(context.Context) error
	}{
		{stepVMID, "pick vmid", c.pickVMID},
		{stepClone, "clone template", c.clone},
		{stepConfig, "configure vm", c.configure},
		{stepResize, "resize disk", c.resize},
		{stepISO, "upload cloud-init iso", c.uploadISO},
		{stepFirewall, "set firewall", c.firewall},
		{stepStart, "start vm", c.start},
		{stepDone, "finish", c.finish},
	}
	for _, s := range steps {
		if c.row.CreateStep >= s.n {
			continue
		}
		if err := s.fn(ctx); err != nil {
			return &stepError{s.name, err}
		}
		if p, ok := stepProgress[s.n]; ok {
			if err := actions.Progress(ctx, c.w.o.Ent, c.args.ActionID, p); err != nil {
				return &stepError{"record progress", err}
			}
		}
		if c.w.afterStep != nil {
			c.w.afterStep(s.n)
		}
	}
	return nil
}

// save records a finished step plus any IDs in one UPDATE.
func (c *createRun) save(ctx context.Context, step int, set func(*ent.ServerUpdateOne)) error {
	u := c.w.o.Ent.Server.UpdateOneID(c.row.ID).SetCreateStep(step)
	if set != nil {
		set(u)
	}
	row, err := u.Save(ctx)
	if err != nil {
		return err
	}
	c.row = row
	return nil
}

func (c *createRun) pickVMID(ctx context.Context) error {
	if c.row.Vmid != nil {
		return c.save(ctx, stepVMID, nil)
	}
	vms, err := c.pc.VMs(ctx)
	if err != nil {
		return err
	}
	vmid := 0
	if i := slices.IndexFunc(vms, func(v pve.VM) bool { return ownedBy(v, c.row.ID) }); i >= 0 {
		vmid = vms[i].VMID
	} else if vmid, err = c.pc.NextID(ctx); err != nil {
		return err
	}
	return c.save(ctx, stepVMID, func(u *ent.ServerUpdateOne) { u.SetVmid(vmid) })
}

func (c *createRun) templateNode(ctx context.Context) (string, error) {
	if c.tnode == "" {
		n, err := templateNode(ctx, c.pc, c.row.TemplateVmid, c.row.Node)
		if err != nil {
			return "", err
		}
		c.tnode = n
	}
	return c.tnode, nil
}

func (c *createRun) templateConfig(ctx context.Context) (map[string]string, error) {
	if c.tcfg != nil {
		return c.tcfg, nil
	}
	node, err := c.templateNode(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := c.pc.Config(ctx, node, c.row.TemplateVmid)
	if errors.Is(err, pve.ErrNotFound) {
		return nil, templateGone(c.row.TemplateVmid)
	}
	if err != nil {
		return nil, err
	}
	c.tcfg = cfg
	return cfg, nil
}

func (c *createRun) diskStorage() string {
	for _, n := range c.loc.Nodes {
		if n.Name == c.row.Node {
			return n.DiskStorage(c.loc)
		}
	}
	return c.loc.DiskStorage
}

func alreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already exists")
}

func (c *createRun) clone(ctx context.Context) error {
	if c.row.CloneUpid != nil {
		return c.awaitClone(ctx, *c.row.CloneUpid)
	}
	tnode, err := c.templateNode(ctx)
	if err != nil {
		return err
	}
	for range maxVMIDTries {
		vmid := *c.row.Vmid
		v, found, err := findVM(ctx, c.pc, vmid)
		if err != nil {
			return err
		}
		if found && ownedBy(v, c.row.ID) {
			return c.tag(ctx)
		}
		if found {
			cfg, err := c.pc.Config(ctx, v.Node, vmid)
			if err != nil && !errors.Is(err, pve.ErrNotFound) {
				return err
			}
			if cfg["lock"] != "" {
				return transient("vm %d is locked (%s); retrying", vmid, cfg["lock"])
			}
			if err := c.newVMID(ctx); err != nil {
				return err
			}
			continue
		}
		p := pve.CloneParams{NewID: vmid, Name: c.row.Name, Storage: c.diskStorage(), Full: true}
		if tnode != c.row.Node {
			p.Target = c.row.Node
		}
		up, err := c.pc.Clone(ctx, tnode, c.row.TemplateVmid, p)
		switch {
		case errors.Is(err, pve.ErrNotFound):
			return templateGone(c.row.TemplateVmid)
		case alreadyExists(err):
			continue
		case err != nil:
			return err
		}
		upid := string(up)
		if err := c.saveRow(ctx, func(u *ent.ServerUpdateOne) { u.SetCloneUpid(upid) }); err != nil {
			return err
		}
		return c.awaitClone(ctx, upid)
	}
	return permanent("no free VMID after %d tries", maxVMIDTries)
}

// awaitClone waits on our saved clone task; success proves the VM at vmid is ours.
func (c *createRun) awaitClone(ctx context.Context, upid string) error {
	if err := c.pc.WaitTask(ctx, upidNode(upid), pve.UPID(upid)); err != nil {
		if ctx.Err() != nil || retryable(err) {
			return err
		}
		return permanent("clone task failed: %s", err)
	}
	return c.tag(ctx)
}

// newVMID replaces a VMID that another VM took.
func (c *createRun) newVMID(ctx context.Context) error {
	id, err := c.pc.NextID(ctx)
	if err != nil {
		return err
	}
	return c.saveRow(ctx, func(u *ent.ServerUpdateOne) { u.SetVmid(id) })
}

func (c *createRun) saveRow(ctx context.Context, set func(*ent.ServerUpdateOne)) error {
	u := c.w.o.Ent.Server.UpdateOneID(c.row.ID)
	set(u)
	row, err := u.Save(ctx)
	if err != nil {
		return err
	}
	c.row = row
	return nil
}

func (c *createRun) tag(ctx context.Context) error {
	if err := c.pc.SetConfig(ctx, c.row.Node, *c.row.Vmid, map[string]string{"tags": vmTags(c.row.ID)}); err != nil {
		return err
	}
	return c.save(ctx, stepClone, nil)
}

func (c *createRun) net0(ctx context.Context) (string, error) {
	bridge, vlan := "", (*int)(nil)
	if len(c.assign) > 0 {
		bridge, vlan = c.assign[0].Bridge, c.assign[0].VLAN
	} else {
		cfg, err := c.templateConfig(ctx)
		if err != nil {
			return "", err
		}
		bridge = cmp.Or(bridgeOf(cfg["net0"]), "vmbr0")
	}
	s := "virtio=" + c.row.MAC + ",bridge=" + bridge
	if vlan != nil {
		s += ",tag=" + strconv.Itoa(*vlan)
	}
	return s + ",firewall=1", nil
}

func (c *createRun) configure(ctx context.Context) error {
	net0, err := c.net0(ctx)
	if err != nil {
		return err
	}
	kv := map[string]string{
		"cores":  strconv.Itoa(c.row.Cores),
		"memory": strconv.Itoa(c.row.MemoryMB),
		"name":   c.row.Name,
		"tags":   vmTags(c.row.ID),
		"net0":   net0,
		"agent":  "1",
	}
	if err := c.pc.SetConfig(ctx, c.row.Node, *c.row.Vmid, kv); err != nil {
		return err
	}
	return c.save(ctx, stepConfig, nil)
}

func (c *createRun) resize(ctx context.Context) error {
	cfg, err := c.templateConfig(ctx)
	if err != nil {
		return err
	}
	disk := catalog.BootDisk(cfg)
	if disk == "" {
		return permanent("template %d has no boot disk", c.row.TemplateVmid)
	}
	if cur, ok := diskBytes(cfg[disk]); !ok || int64(c.row.DiskGB)<<30 > cur {
		if err := c.pc.ResizeDisk(ctx, c.row.Node, *c.row.Vmid, disk, c.row.DiskGB); err != nil {
			return err
		}
	}
	return c.save(ctx, stepResize, nil)
}

func (c *createRun) volid() string { return isoVolid(c.loc.ISOStorage, c.row.ID, c.row.IsoGeneration) }

func (c *createRun) uploadISO(ctx context.Context) error {
	sts, err := c.pc.Storages(ctx, c.row.Node)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(sts, func(s pve.Storage) bool { return s.ID == c.loc.ISOStorage && slices.Contains(s.Content, "iso") }) {
		return permanent("ISO storage %q is not available on node %s", c.loc.ISOStorage, c.row.Node)
	}
	iso, err := cloudinit.Build(cloudinit.Input{
		InstanceID: fmt.Sprintf("nb-srv-%d-%d", c.row.ID, c.row.IsoGeneration),
		Hostname:   c.row.Name, SSHKeys: c.row.SSHKeys, UserData: c.row.UserData, MAC: c.row.MAC, IPs: c.assign,
	})
	if err != nil {
		return permanent("%s", err)
	}
	volid := c.volid()
	// A retry may find its own earlier upload; errors here surface on the upload.
	_ = c.pc.DeleteVolume(ctx, c.row.Node, c.loc.ISOStorage, volid)
	if err := c.pc.UploadISO(ctx, c.row.Node, c.loc.ISOStorage, isoName(c.row.ID, c.row.IsoGeneration), iso); err != nil {
		return err
	}
	if err := c.pc.SetConfig(ctx, c.row.Node, *c.row.Vmid, map[string]string{"ide2": volid + ",media=cdrom"}); err != nil {
		return err
	}
	return c.save(ctx, stepISO, func(u *ent.ServerUpdateOne) { u.SetIsoVolid(volid) })
}

func (c *createRun) firewall(ctx context.Context) error {
	ips := make([]netip.Addr, len(c.assign))
	for i, a := range c.assign {
		ips[i] = a.IP
	}
	if err := c.pc.SetFirewall(ctx, c.row.Node, *c.row.Vmid, ips); err != nil {
		return err
	}
	return c.save(ctx, stepFirewall, nil)
}

func (c *createRun) start(ctx context.Context) error {
	if !c.args.StartAfter {
		return c.save(ctx, stepStart, nil)
	}
	if err := c.w.o.Ent.Server.UpdateOneID(c.row.ID).SetStatus(StatusStarting).Exec(ctx); err != nil {
		return err
	}
	node, vmid := c.row.Node, *c.row.Vmid
	v, found, err := findVM(ctx, c.pc, vmid)
	if err != nil {
		return err
	}
	if !found {
		return permanent("vm %d disappeared before start", vmid)
	}
	if v.Status != "running" {
		up, err := c.pc.Power(ctx, node, vmid, pve.PowerStart)
		if err == nil {
			err = c.pc.WaitTask(ctx, node, up)
		}
		if err != nil {
			return err
		}
	}
	deadline := time.Now().Add(startWait)
	for {
		v, found, err := findVM(ctx, c.pc, vmid)
		if err != nil {
			return err
		}
		if found && v.Status == "running" {
			return c.save(ctx, stepStart, nil)
		}
		if time.Now().After(deadline) {
			return permanent("vm %d did not reach running within %s", vmid, startWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.w.o.PollInterval):
		}
	}
}

func (c *createRun) finish(ctx context.Context) error {
	status := StatusOff
	if c.args.StartAfter {
		status = StatusRunning
	}
	err := db.WithTx(ctx, c.w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if err := etx.Server.UpdateOneID(c.row.ID).SetStatus(server.Status(status)).SetCreateStep(stepDone).Exec(ctx); err != nil {
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
	if c.w.o.Bus == nil {
		return
	}
	data := map[string]any{"location": c.loc.Name, "cores": c.row.Cores, "memory_mb": c.row.MemoryMB, "disk_gb": c.row.DiskGB}
	if t, err := c.w.o.Catalog.GetServerType(ctx, c.row.ServerTypeID); err == nil {
		data["server_type"] = t.Name
	}
	c.w.o.Bus.Publish(ctx, ext.Event{Kind: ext.ServerCreated, ProjectID: c.row.ProjectID, ServerID: c.row.ID, At: time.Now(), Data: data})
}

// cleanup undoes a failed create; IPs are released only once the VM is confirmed gone.
func (w *CreateServerWorker) cleanup(ctx context.Context, c *createRun, cause error, msg string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	log := w.o.Logger.With("server_id", c.row.ID, "action_id", c.args.ActionID)
	log.Warn("create server failed", "err", cause)
	gone := c.row.Vmid == nil
	if c.pc != nil {
		gone = CleanupVM(ctx, log, c.pc, c.row.ID, c.row.Node, c.row.Vmid, c.row.CloneUpid, c.loc.ISOStorage, c.volid())
	}
	if !gone {
		msg += fmt.Sprintf("; cleanup incomplete: VM %d could not be removed", *c.row.Vmid)
	}
	err := db.WithTx(ctx, w.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		u := etx.Server.UpdateOneID(c.row.ID).SetStatus(server.Status(StatusError))
		if gone {
			if err := ipam.Release(ctx, etx, c.row.ID); err != nil {
				return err
			}
			u.ClearVmid().ClearCloneUpid().ClearIsoVolid().SetCreateStep(0)
		}
		return u.Exec(ctx)
	})
	return errors.Join(err, w.failAction(ctx, c.args.ActionID, msg))
}

func (w *CreateServerWorker) failAction(ctx context.Context, id int64, msg string) error {
	return actions.Fail(context.WithoutCancel(ctx), w.o.Ent, id, "pve_error", msg)
}
