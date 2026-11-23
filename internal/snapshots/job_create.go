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

package snapshots

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Create steps, saved in snapshots.step once done.
const (
	stepRecord = iota + 1
	stepShutdown
	stepAllocate
	stepCreate
	stepTemplate
	stepStart
	stepFinish
)

var stepNames = map[int]string{stepRecord: "record power state", stepShutdown: "shut down server", stepAllocate: "allocate vmid",
	stepCreate: "create template vm", stepTemplate: "convert to template", stepStart: "start server", stepFinish: "finish"}

const maxAllocations = 5

// hardwareKeys are copied to the template when the source has them.
var hardwareKeys = []string{"ostype", "bios", "machine", "scsihw", "cpu", "boot", "serial0", "vga"}

// CreateSnapshotWorker copies a server's boot disk into a new template VM.
type CreateSnapshotWorker struct {
	river.WorkerDefaults[CreateSnapshotArgs]
	o WorkerOptions
}

func NewCreateSnapshotWorker(o WorkerOptions) *CreateSnapshotWorker {
	o.defaults()
	return &CreateSnapshotWorker{o: o}
}

func (*CreateSnapshotWorker) Timeout(*river.Job[CreateSnapshotArgs]) time.Duration { return jobTimeout }

func (w *CreateSnapshotWorker) Work(ctx context.Context, job *river.Job[CreateSnapshotArgs]) error {
	a := job.Args
	act, row, ret, stop := w.o.begin(ctx, a.ActionID, a.SnapshotID)
	if stop {
		return ret
	}
	c := &create{o: &w.o, row: row, actionID: a.ActionID,
		log: w.o.Logger.With("snapshot_id", row.ID, "action_id", a.ActionID)}
	run := func(ctx context.Context) error {
		if c.row.Status == snapshot.StatusError {
			// RescueOrphaned marks a lost create this way so its job only cleans up.
			return servers.PermanentCode(actions.CodeOrphaned, "%s", jobs.OrphanedMessage)
		}
		return c.run(ctx)
	}
	return w.o.contract(job.JobRow, "snapshot", act, row.ID, run, c.compensate).Work(ctx)
}

// create is one attempt at one snapshot.
type create struct {
	o        *WorkerOptions
	row      *ent.Snapshot
	actionID int64
	log      *slog.Logger
	pc       pve.Client
	srv      *ent.Server
}

func (c *create) run(ctx context.Context) error {
	if err := c.connect(ctx); err != nil {
		return err
	}
	for c.row.Step < stepFinish {
		n := c.row.Step + 1
		if err := c.do(ctx, n); err != nil {
			var snooze *rivertype.JobSnoozeError
			if errors.As(err, &snooze) {
				return err
			}
			return servers.StepError(stepNames[n], err)
		}
		if c.o.afterStep != nil {
			c.o.afterStep(n)
		}
		if n < stepFinish {
			if err := actions.Progress(ctx, c.o.Ent, c.actionID, n*100/stepFinish); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *create) do(ctx context.Context, n int) error {
	switch n {
	case stepRecord:
		return c.record(ctx)
	case stepShutdown:
		return c.shutdown(ctx)
	case stepAllocate:
		return c.allocate(ctx)
	case stepCreate:
		return c.create(ctx)
	case stepTemplate:
		return c.template(ctx)
	case stepStart:
		if c.row.WasRunning {
			v, err := serverVM(ctx, c.pc, c.srv)
			if err != nil {
				return err
			}
			if err := c.o.startServer(ctx, c.pc, c.srv, v); err != nil {
				return err
			}
		}
		return c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetStep(stepStart) })
	case stepFinish:
		return c.finish(ctx)
	}
	return fmt.Errorf("unknown step %d", n)
}

func (c *create) connect(ctx context.Context) error {
	pc, err := c.o.pveFor(ctx, c.row.LocationID)
	if err != nil {
		return servers.StepError("connect to proxmox", err)
	}
	c.pc = pc
	if c.row.ServerID == nil {
		return servers.PermanentCode(actions.CodeServerGone, "the source server no longer exists")
	}
	srv, err := c.o.Ent.Server.Get(ctx, *c.row.ServerID)
	if ent.IsNotFound(err) {
		return servers.PermanentCode(actions.CodeServerGone, "server %d no longer exists", *c.row.ServerID)
	}
	if err != nil {
		return err
	}
	c.srv = srv
	return nil
}

// save updates the row and keeps the updated copy.
func (c *create) save(ctx context.Context, f func(*ent.SnapshotUpdateOne)) error {
	u := c.o.Ent.Snapshot.UpdateOneID(c.row.ID)
	f(u)
	row, err := u.Save(ctx)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.ConstraintName == "snapshots_location_vmid_key" {
		return servers.Transient("another snapshot took the vmid: %v", err)
	}
	if err != nil {
		return err
	}
	c.row = row
	return nil
}

// record saves whether the server runs now, and where the template will live; nothing is touched yet.
func (c *create) record(ctx context.Context) error {
	v, err := serverVM(ctx, c.pc, c.srv)
	if err != nil {
		return err
	}
	loc, err := c.o.Catalog.GetLocation(ctx, c.row.LocationID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(loc.Nodes, func(n catalog.Node) bool { return n.Name == v.Node })
	if i < 0 {
		return servers.Permanent("node %s is not in location %d", v.Node, loc.ID)
	}
	storage := ResolveStorage(loc, loc.Nodes[i])
	list, err := c.pc.Storages(ctx, v.Node)
	if err != nil {
		return err
	}
	j := slices.IndexFunc(list, func(st pve.Storage) bool { return st.ID == storage && slices.Contains(st.Content, "images") })
	if j < 0 {
		return servers.Permanent("storage %s on node %s does not hold images", storage, v.Node)
	}
	node := v.Node
	if list[j].Shared {
		node = ""
	}
	return c.save(ctx, func(u *ent.SnapshotUpdateOne) {
		u.SetWasRunning(v.Status == "running").SetStorage(storage).SetNode(node).SetStep(stepRecord)
	})
}

func (c *create) shutdown(ctx context.Context) error {
	if !c.row.WasRunning {
		return c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetStep(stepShutdown) })
	}
	v, err := serverVM(ctx, c.pc, c.srv)
	if err != nil {
		return err
	}
	if v.Status != "stopped" {
		if err := c.o.setServerStatus(ctx, c.srv.ID, servers.StatusStopping); err != nil {
			return err
		}
		stopped, err := c.shutdownVM(ctx, v)
		if err != nil {
			return err
		}
		if !stopped {
			if !c.row.ForceStop {
				return servers.PermanentCode(actions.CodeShutdownTimeout, "vm %d did not shut down within %s", v.VMID, c.o.ShutdownTimeout)
			}
			c.log.Info("clean shutdown timed out; stopping the vm", "vmid", v.VMID)
			if err := c.o.power(ctx, c.pc, v.Node, v.VMID, pve.PowerStop); err != nil {
				return err
			}
			ok, err := c.o.waitState(ctx, c.pc, v.VMID, "stopped", c.o.PowerWait)
			if err != nil {
				return err
			}
			if !ok {
				return servers.PermanentCode(actions.CodeTimedOut, "vm %d did not stop within %s", v.VMID, c.o.PowerWait)
			}
		}
	}
	if err := c.o.setServerStatus(ctx, c.srv.ID, servers.StatusOff); err != nil {
		return err
	}
	return c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetStep(stepShutdown) })
}

// shutdownVM asks the guest to shut down and reports whether it stopped within ShutdownTimeout.
func (c *create) shutdownVM(ctx context.Context, v pve.VM) (bool, error) {
	deadline := time.Now().Add(c.o.ShutdownTimeout)
	up, err := c.pc.Power(ctx, v.Node, v.VMID, pve.PowerShutdown)
	if err != nil {
		return false, err
	}
	wctx, cancel := context.WithDeadline(ctx, deadline)
	err = c.pc.WaitTask(wctx, v.Node, up)
	cancel()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		// The VM's state decides, not the task's.
		c.log.Info("shutdown task did not succeed", "vmid", v.VMID, "err", err)
	}
	return c.o.waitState(ctx, c.pc, v.VMID, "stopped", time.Until(deadline))
}

func (c *create) allocate(ctx context.Context) error {
	id, err := c.freeVMID(ctx)
	if err != nil {
		return err
	}
	nonce := rand.Text()
	return c.save(ctx, func(u *ent.SnapshotUpdateOne) {
		u.SetVmid(id).SetNonce(nonce).SetStep(stepAllocate).ClearUpid()
	})
}

// freeVMID is /cluster/nextid, skipping VMIDs other snapshot rows still hold (a missing row keeps its VMID).
func (c *create) freeVMID(ctx context.Context) (int, error) {
	id, err := c.pc.NextID(ctx)
	if err != nil {
		return 0, err
	}
	held, err := c.o.Ent.Snapshot.Query().Where(snapshot.LocationID(c.row.LocationID), snapshot.IDNEQ(c.row.ID),
		snapshot.VmidGTE(id)).Select(snapshot.FieldVmid).Ints(ctx)
	if err != nil || !slices.Contains(held, id) {
		return id, err
	}
	vms, err := c.pc.VMs(ctx)
	if err != nil {
		return 0, err
	}
	for slices.Contains(held, id) || slices.ContainsFunc(vms, func(v pve.VM) bool { return v.VMID == id }) {
		id++
	}
	return id, nil
}

// ours reports whether cfg is this snapshot's VM: our tag and the nonce we sent.
func (c *create) ours(cfg map[string]string) bool {
	return c.row.Nonce != nil && hasTag(cfg["tags"], SnapshotTag(c.row.ID)) &&
		strings.TrimSpace(cfg["description"]) == vmDescription(c.row.ID, *c.row.Nonce)
}

func (c *create) create(ctx context.Context) error {
	for range maxAllocations {
		err := c.createOnce(ctx)
		if !alreadyExists(err) && !lostLock(err, *c.row.Vmid) {
			if err != nil {
				return err
			}
			return c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetStep(stepCreate).ClearUpid() })
		}
		ours, err := c.claim(ctx)
		if err != nil {
			return err
		}
		if ours {
			return c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetStep(stepCreate).ClearUpid() })
		}
		c.log.Info("vmid taken by another VM; allocating again", "vmid", *c.row.Vmid)
		if err := c.allocate(ctx); err != nil {
			return err
		}
	}
	return servers.Transient("no free VMID after %d tries", maxAllocations)
}

func (c *create) createOnce(ctx context.Context) error {
	if c.row.Upid == nil {
		v, err := serverVM(ctx, c.pc, c.srv)
		if err != nil {
			return err
		}
		if v.Status != "stopped" {
			return servers.Permanent("vm %d is %s; its disk is only copied while it is stopped", v.VMID, v.Status)
		}
		cfg, err := c.pc.Config(ctx, v.Node, v.VMID)
		if err != nil {
			return err
		}
		kv, bootKey, err := templateConfig(cfg, c.row.ID, *c.row.Nonce, c.row.Storage)
		if err != nil {
			return err
		}
		up, err := c.pc.CreateVM(ctx, v.Node, *c.row.Vmid, kv)
		if err != nil {
			return err
		}
		if err := c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetUpid(string(up)).SetBootKey(bootKey) }); err != nil {
			return err
		}
	}
	return waitTask(ctx, c.pc, *c.row.Upid)
}

// claim decides whether the VM at our vmid is ours, reading it once no create holds its lock.
func (c *create) claim(ctx context.Context) (bool, error) {
	vmid := *c.row.Vmid
	deadline := time.Now().Add(budgetFor(ctx))
	for {
		v, found, err := findVM(ctx, c.pc, vmid)
		if err != nil {
			return false, err
		}
		if !found {
			return false, servers.Transient("VM %d reported as existing but not listed yet", vmid)
		}
		cfg, err := c.pc.Config(ctx, v.Node, vmid)
		if errors.Is(err, pve.ErrNotFound) {
			return false, servers.Transient("VM %d reported as existing but has no config yet", vmid)
		}
		if err != nil {
			return false, err
		}
		switch {
		case cfg["lock"] == "":
			return c.ours(cfg), nil
		case !c.ours(cfg) && cfg["tags"] != "":
			return false, nil
		case time.Now().After(deadline):
			return false, servers.Transient("VM %d is still locked (%s)", vmid, cfg["lock"])
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(c.o.PollInterval):
		}
	}
}

func (c *create) template(ctx context.Context) error {
	vmid := *c.row.Vmid
	if c.row.Upid == nil {
		v, found, err := findVM(ctx, c.pc, vmid)
		if err != nil {
			return err
		}
		if !found {
			return servers.Permanent("the new VM %d is gone", vmid)
		}
		cfg, err := c.pc.Config(ctx, v.Node, vmid)
		if err != nil {
			return err
		}
		if !c.ours(cfg) {
			return servers.Permanent("VM %d is not this snapshot's", vmid)
		}
		if cfg["template"] != "1" {
			up, err := c.pc.ConvertToTemplate(ctx, v.Node, vmid)
			if err != nil {
				return err
			}
			if up != "" {
				if err := c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetUpid(string(up)) }); err != nil {
					return err
				}
			}
		}
	}
	if c.row.Upid != nil {
		if err := waitTask(ctx, c.pc, *c.row.Upid); err != nil {
			return err
		}
	}
	if err := c.awaitListed(ctx); err != nil {
		return err
	}
	return c.save(ctx, func(u *ent.SnapshotUpdateOne) { u.SetStep(stepTemplate).ClearUpid() })
}

// awaitListed waits until the cluster listing shows our vmid as a template; server builds read that flag.
func (c *create) awaitListed(ctx context.Context) error {
	vmid := *c.row.Vmid
	deadline := time.Now().Add(budgetFor(ctx))
	for {
		v, found, err := findVM(ctx, c.pc, vmid)
		if err != nil {
			return err
		}
		if !found {
			return servers.Permanent("the new VM %d is gone", vmid)
		}
		if v.Template {
			return nil
		}
		if !time.Now().Add(c.o.PollInterval).Before(deadline) {
			return river.JobSnooze(snoozeFor)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.o.PollInterval):
		}
	}
}

// finish checks the copied size, marks the snapshot available and succeeds the action.
func (c *create) finish(ctx context.Context) error {
	vmid := *c.row.Vmid
	v, found, err := findVM(ctx, c.pc, vmid)
	if err != nil {
		return err
	}
	if !found {
		return servers.Permanent("the template %d is gone", vmid)
	}
	cfg, err := c.pc.Config(ctx, v.Node, vmid)
	if err != nil {
		return err
	}
	// The source was stopped, so its disk could not grow: a difference is only logged.
	const gb = int64(1) << 30
	if b, ok := servers.DiskBytes(cfg[c.row.BootKey]); !ok || int((b+gb-1)/gb) != c.row.SizeGB {
		c.log.Warn("snapshot size differs from the request", "boot_key", c.row.BootKey, "disk", cfg[c.row.BootKey], "size_gb", c.row.SizeGB)
	}
	err = db.WithTx(ctx, c.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := etx.Snapshot.UpdateOneID(c.row.ID).SetStatus(snapshot.StatusAvailable).SetStep(stepFinish).ClearUpid().Save(ctx)
		if err != nil {
			return err
		}
		c.row = row
		return actions.Succeed(ctx, etx, c.actionID)
	})
	if err != nil {
		return err
	}
	c.o.publish(ctx, ext.SnapshotCreated, c.row)
	return nil
}

// compensate restarts the server if it ran, removes our VM and the row; if it can't, the row stays in error with its quota.
func (c *create) compensate(ctx context.Context, f servers.Failure) error {
	row, err := c.o.Ent.Snapshot.Get(ctx, c.row.ID)
	if ent.IsNotFound(err) {
		return failAction(ctx, c.o.Ent, c.actionID, f)
	}
	if err != nil {
		return err
	}
	c.row = row
	if uerr := c.undo(ctx); uerr != nil {
		c.log.Error("snapshot cleanup did not finish", "err", uerr)
		if err := c.o.Ent.Snapshot.UpdateOneID(row.ID).SetStatus(snapshot.StatusError).ClearUpid().Exec(ctx); err != nil {
			return err
		}
		f.Detail += "; cleanup incomplete: " + uerr.Error()
		return failAction(ctx, c.o.Ent, c.actionID, f)
	}
	return db.WithTx(ctx, c.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if err := etx.Snapshot.DeleteOneID(row.ID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			return err
		}
		return actions.Fail(ctx, etx, c.actionID, f.Code, f.Detail)
	})
}

// undo starts the server if it ran, then destroys the VM at our vmid only if tag and nonce prove it ours.
func (c *create) undo(ctx context.Context) error {
	pc := c.pc
	if pc == nil {
		var err error
		if pc, err = c.o.pveFor(ctx, c.row.LocationID); err != nil {
			return fmt.Errorf("connect to proxmox: %w", err)
		}
	}
	var errs []error
	if err := c.restart(ctx, pc); err != nil {
		errs = append(errs, fmt.Errorf("start server: %w", err))
	}
	if c.row.Vmid != nil {
		if err := c.destroy(ctx, pc, *c.row.Vmid); err != nil {
			errs = append(errs, fmt.Errorf("remove vm %d: %w", *c.row.Vmid, err))
		}
	}
	return errors.Join(errs...)
}

// restart starts the source server again when it was running, then forgets that it did.
func (c *create) restart(ctx context.Context, pc pve.Client) error {
	if !c.row.WasRunning || c.row.ServerID == nil {
		return nil
	}
	srv, err := c.o.Ent.Server.Get(ctx, *c.row.ServerID)
	if ent.IsNotFound(err) || (err == nil && srv.Vmid == nil) {
		return nil
	}
	if err != nil {
		return err
	}
	v, err := servers.OwnedVM(ctx, pc, srv.ID, *srv.Vmid)
	if errors.Is(err, servers.ErrMissing) {
		c.log.Warn("source server vm is gone; nothing to start", "server_id", srv.ID)
		return nil
	}
	if err != nil {
		return err
	}
	if err := c.o.startServer(ctx, pc, srv, v); err != nil {
		return err
	}
	row, err := c.o.Ent.Snapshot.UpdateOneID(c.row.ID).SetWasRunning(false).Save(ctx)
	if err != nil {
		return err
	}
	c.row = row
	return nil
}

// destroy removes vmid when it is this snapshot's VM; any other VM there is left alone.
func (c *create) destroy(ctx context.Context, pc pve.Client, vmid int) error {
	v, found, err := findVM(ctx, pc, vmid)
	if err != nil || !found {
		return err
	}
	cfg, err := pc.Config(ctx, v.Node, vmid)
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !c.ours(cfg) {
		if cfg["lock"] != "" && cfg["tags"] == "" {
			return fmt.Errorf("vm %d is locked (%s) and may be our create", vmid, cfg["lock"])
		}
		return nil
	}
	if cfg["lock"] != "" {
		return fmt.Errorf("vm %d is locked (%s)", vmid, cfg["lock"])
	}
	up, err := pc.Destroy(ctx, v.Node, vmid)
	if errors.Is(err, pve.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return pc.WaitTask(ctx, v.Node, up)
}

// templateConfig writes the template VM's config fresh from the source's, and returns the boot disk key.
func templateConfig(cfg map[string]string, id int64, nonce, storage string) (map[string]string, string, error) {
	bootKey := catalog.BootDisk(cfg)
	if bootKey == "" {
		return nil, "", servers.Permanent("the server has no boot disk")
	}
	kv := map[string]string{"name": vmName(id), "tags": vmTags(id), "description": vmDescription(id, nonce)}
	for _, k := range []string{bootKey, "efidisk0", "tpmstate0"} {
		if v, ok := cfg[k]; ok {
			kv[k] = importDisk(storage, v)
		}
	}
	for _, k := range hardwareKeys {
		if v, ok := cfg[k]; ok {
			kv[k] = v
		}
	}
	if n, ok := cfg["net0"]; ok {
		kv["net0"] = bareNIC(n)
	}
	if b, ok := kv["boot"]; ok {
		kv["boot"] = bootOrder(b, kv)
	}
	return kv, bootKey, nil
}

// importDisk imports a disk config value's volume into storage, keeping its options but its size.
func importDisk(storage, disk string) string {
	parts := strings.Split(disk, ",")
	out := []string{storage + ":0", "import-from=" + parts[0]}
	for _, p := range parts[1:] {
		if !strings.HasPrefix(p, "size=") && !strings.HasPrefix(p, "import-from=") && p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// bareNIC keeps a net config value's model, bridge and VLAN tag; the server build sets the MAC and firewall.
func bareNIC(net string) string {
	model := ""
	var keep []string
	for i, p := range strings.Split(net, ",") {
		k, v, _ := strings.Cut(p, "=")
		switch {
		case k == "model":
			model = v
		case k == "bridge", k == "tag":
			keep = append(keep, p)
		case i == 0 && k != "macaddr":
			model = k
		}
	}
	if model == "" {
		model = "virtio"
	}
	return strings.Join(append([]string{model}, keep...), ",")
}

// bootOrder drops boot order devices the template doesn't have, such as the seed ISO.
func bootOrder(boot string, kv map[string]string) string {
	parts := strings.Split(boot, ",")
	for i, p := range parts {
		order, ok := strings.CutPrefix(strings.TrimSpace(p), "order=")
		if !ok {
			continue
		}
		devs := slices.DeleteFunc(strings.Split(order, ";"), func(d string) bool {
			_, ok := kv[strings.TrimSpace(d)]
			return !ok
		})
		parts[i] = "order=" + strings.Join(devs, ";")
	}
	return strings.Join(parts, ",")
}
