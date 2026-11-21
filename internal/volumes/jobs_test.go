package volumes_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

type recorder struct {
	mu  sync.Mutex
	evs []ext.Event
}

func (r *recorder) Publish(_ context.Context, e ext.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evs = append(r.evs, e)
}

func (r *recorder) all() []ext.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.evs)
}

func (r *recorder) kinds() []ext.EventKind {
	var out []ext.EventKind
	for _, e := range r.all() {
		out = append(out, e.Kind)
	}
	return out
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func (e *env) opts(rec *recorder) volumes.WorkerOptions {
	o := volumes.WorkerOptions{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Logger: quiet()}
	if rec != nil {
		o.Bus = rec
	}
	return o
}

func jobOf[T river.JobArgs](args T, attempt int) *river.Job[T] {
	return &river.Job[T]{JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: servers.MaxAttempts + 2}, Args: args}
}

// queuedArgs decodes the River args of a queued action's job.
func queuedArgs[T any](t *testing.T, e *env, actionID int64) T {
	t.Helper()
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, actionID)
	require.NoError(t, err)
	require.NotNil(t, a.RiverJobID)
	j, err := e.App.Jobs.Client.JobGet(context.Background(), *a.RiverJobID)
	require.NoError(t, err)
	var v T
	require.NoError(t, json.Unmarshal(j.EncodedArgs, &v))
	return v
}

// work runs the queued action's job once with o.
func (e *env) work(ctx context.Context, t *testing.T, o volumes.WorkerOptions, actionID int64, attempt int) error {
	t.Helper()
	a, err := actions.Get(context.Background(), e.App.Ent, e.Project, actionID)
	require.NoError(t, err)
	switch a.Command {
	case volumes.KindCreateVolume:
		return volumes.NewCreateVolumeWorker(o).Work(ctx, jobOf(queuedArgs[volumes.CreateVolumeArgs](t, e, actionID), attempt))
	case volumes.KindAttachVolume:
		return volumes.NewAttachVolumeWorker(o).Work(ctx, jobOf(queuedArgs[volumes.AttachVolumeArgs](t, e, actionID), attempt))
	case volumes.KindDetachVolume:
		return volumes.NewDetachVolumeWorker(o).Work(ctx, jobOf(queuedArgs[volumes.DetachVolumeArgs](t, e, actionID), attempt))
	case volumes.KindResizeVolume:
		return volumes.NewResizeVolumeWorker(o).Work(ctx, jobOf(queuedArgs[volumes.ResizeVolumeArgs](t, e, actionID), attempt))
	case volumes.KindDeleteVolume:
		return volumes.NewDeleteVolumeWorker(o).Work(ctx, jobOf(queuedArgs[volumes.DeleteVolumeArgs](t, e, actionID), attempt))
	}
	t.Fatalf("unknown command %s", a.Command)
	return nil
}

// finish runs the action's job and returns the finished action.
func (e *env) finish(t *testing.T, o volumes.WorkerOptions, act actions.Action) actions.Action {
	t.Helper()
	_ = e.work(context.Background(), t, o, act.ID, 1)
	got, err := actions.Get(context.Background(), e.App.Ent, e.Project, act.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Finished, "action still running")
	return got
}

func (e *env) ok(t *testing.T, o volumes.WorkerOptions, act actions.Action, err error) {
	t.Helper()
	require.NoError(t, err)
	got := e.finish(t, o, act)
	require.Equal(t, actions.StatusSuccess, got.Status, "%+v", got.Error)
}

// pinPVE1 makes standalone local-lvm volumes land on pve1.
func (e *env) pinPVE1(t *testing.T) {
	t.Helper()
	e.localStorage(t)
	e.App.PVE.SetStorageAvail("pve2", "local-lvm", 1)
}

func vmidOf(s testutil.CreatedServer) int { return 100 + int(s.ServerID) }

// keysFor lists the VM's config keys referencing volume id.
func (e *env) keysFor(t *testing.T, vmid int, id int64) map[string]string {
	t.Helper()
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	out := map[string]string{}
	for k, v := range vm.Config {
		if got, ok := vref.Parse(v); ok && got == id {
			out[k] = v
		}
	}
	return out
}

// disks lists the storage items named for volume id on pve1 local-lvm.
func (e *env) disks(t *testing.T, id int64) []pve.StorageItem {
	t.Helper()
	pc, err := e.App.Catalog.ProxmoxFor(context.Background(), e.Location)
	require.NoError(t, err)
	items, err := pc.StorageContent(context.Background(), "pve1", "local-lvm", vref.OwnerVMID)
	require.NoError(t, err)
	return slices.DeleteFunc(items, func(it pve.StorageItem) bool { return it.Volid != "local-lvm:"+vref.Name(id) })
}

func TestCreateAttachDetachDeleteHappyPath(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	rec := &recorder{}
	o := e.opts(rec)
	s := e.RunningServer("web")
	vmid := vmidOf(s)

	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Server: "web"})
	e.ok(t, o, act, err)
	id := v.ID
	want := "local-lvm:vm-999999999-nb-vol-" + ref(id) + ",serial=nb-vol-" + ref(id) + ",backup=0"
	vm, _ := e.App.PVE.VM(vmid)
	require.Equal(t, want, vm.Config["virtio1"])
	r := e.row(t, id)
	require.Equal(t, volume.StatusAttached, r.Status)
	require.Equal(t, s.ServerID, *r.ServerID)
	require.Equal(t, "virtio1", r.Device)
	require.Equal(t, 10, r.AppliedGB)
	require.Len(t, e.disks(t, id), 1)

	act, err = e.svc.Resize(ctx, e.Project, id, 20)
	e.ok(t, o, act, err)
	require.Equal(t, 20, e.row(t, id).AppliedGB)
	require.EqualValues(t, int64(20)<<30, e.disks(t, id)[0].Size)

	act, err = e.svc.Detach(ctx, e.Project, id)
	e.ok(t, o, act, err)
	require.Empty(t, e.keysFor(t, vmid, id))
	r = e.row(t, id)
	require.Equal(t, volume.StatusAvailable, r.Status)
	require.Nil(t, r.ServerID)
	require.Equal(t, "", r.Device)

	act, err = e.svc.Attach(ctx, e.Project, id, "web")
	e.ok(t, o, act, err)
	require.Equal(t, map[string]string{"virtio1": want}, e.keysFor(t, vmid, id))
	act, err = e.svc.Detach(ctx, e.Project, id)
	e.ok(t, o, act, err)

	act, err = e.svc.Delete(ctx, e.Project, id)
	e.ok(t, o, act, err)
	_, err = e.App.Ent.Volume.Get(ctx, id)
	require.True(t, ent.IsNotFound(err))
	require.Empty(t, e.disks(t, id))

	require.Equal(t, []ext.EventKind{ext.VolumeCreated, ext.VolumeResized, ext.VolumeDeleted}, rec.kinds())
	evs := rec.all()
	for _, ev := range evs {
		require.Equal(t, e.Project, ev.ProjectID)
		require.Equal(t, volumes.ResourceType, ev.ResourceType)
		require.Equal(t, id, ev.ResourceID)
		require.Zero(t, ev.ServerID)
		require.False(t, ev.At.IsZero())
	}
	require.Equal(t, map[string]any{"size_gb": 10, "location_id": e.Location, "attached": true}, evs[0].Data)
	require.Equal(t, map[string]any{"size_gb": 20, "location_id": e.Location, "attached": true}, evs[1].Data)
	require.Equal(t, map[string]any{"size_gb": 20, "location_id": e.Location, "attached": false}, evs[2].Data)
}

// crashPVE runs each call, then simulates a crash after the crashAt-th one.
type crashPVE struct {
	pve.Client
	calls, crashAt int
	cancel         context.CancelFunc
	crashed        bool
}

func (c *crashPVE) after(err error) error {
	c.calls++
	if c.calls == c.crashAt {
		c.crashed = true
		c.cancel()
		return context.Canceled
	}
	return err
}

func (c *crashPVE) VMs(ctx context.Context) ([]pve.VM, error) {
	v, err := c.Client.VMs(ctx)
	return v, c.after(err)
}

func (c *crashPVE) GuestExists(ctx context.Context, vmid int) (bool, error) {
	v, err := c.Client.GuestExists(ctx, vmid)
	return v, c.after(err)
}

func (c *crashPVE) Config(ctx context.Context, node string, vmid int) (map[string]string, error) {
	v, err := c.Client.Config(ctx, node, vmid)
	return v, c.after(err)
}

func (c *crashPVE) SetConfigDigest(ctx context.Context, node string, vmid int, kv map[string]string, digest string) error {
	return c.after(c.Client.SetConfigDigest(ctx, node, vmid, kv, digest))
}

func (c *crashPVE) Pending(ctx context.Context, node string, vmid int) ([]pve.PendingItem, error) {
	v, err := c.Client.Pending(ctx, node, vmid)
	return v, c.after(err)
}

func (c *crashPVE) AllocVolume(ctx context.Context, node, storage string, owner int, filename string, gb int) (string, error) {
	v, err := c.Client.AllocVolume(ctx, node, storage, owner, filename, gb)
	return v, c.after(err)
}

func (c *crashPVE) StorageContent(ctx context.Context, node, storage string, vmid int) ([]pve.StorageItem, error) {
	v, err := c.Client.StorageContent(ctx, node, storage, vmid)
	return v, c.after(err)
}

func (c *crashPVE) ResizeDisk(ctx context.Context, node string, vmid int, disk string, gb int) error {
	return c.after(c.Client.ResizeDisk(ctx, node, vmid, disk, gb))
}

func (c *crashPVE) DeleteVolume(ctx context.Context, node, storage, volid string) error {
	return c.after(c.Client.DeleteVolume(ctx, node, storage, volid))
}

func TestVolumeJobsResumeEveryStep(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	s := e.RunningServer("web")
	vmid := vmidOf(s)

	for crashAt := 1; ; crashAt++ {
		anyCrash := false
		// step runs the action crashing after the crashAt-th Proxmox call, then resumes it.
		step := func(act actions.Action, err error) {
			t.Helper()
			require.NoError(t, err)
			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			cp := &crashPVE{crashAt: crashAt, cancel: cancel}
			o := volumes.SetPVE(e.opts(nil), func(ctx context.Context, loc int64) (pve.Client, error) {
				pc, err := e.App.Catalog.ProxmoxFor(ctx, loc)
				cp.Client = pc
				return cp, err
			})
			err = e.work(cctx, t, o, act.ID, 1)
			if cp.crashed {
				anyCrash = true
				require.Error(t, err)
				require.NoError(t, e.work(ctx, t, e.opts(nil), act.ID, 2))
			}
			got, gerr := actions.Get(ctx, e.App.Ent, e.Project, act.ID)
			require.NoError(t, gerr)
			require.Equal(t, actions.StatusSuccess, got.Status, "crash after call %d of %s: %+v", crashAt, got.Command, got.Error)
		}
		check := func(id int64, attached bool, applied int) {
			t.Helper()
			keys := e.keysFor(t, vmid, id)
			r := e.row(t, id)
			if attached {
				require.Len(t, keys, 1, "crash at %d", crashAt)
				require.Equal(t, volume.StatusAttached, r.Status)
				_, ok := keys[r.Device]
				require.True(t, ok)
			} else {
				require.Empty(t, keys, "crash at %d", crashAt)
				require.Equal(t, volume.StatusAvailable, r.Status)
			}
			require.Len(t, e.disks(t, id), 1, "crash at %d", crashAt)
			require.Equal(t, applied, r.AppliedGB)
		}

		v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "v" + ref(int64(crashAt)), SizeGB: 10, Server: "web"})
		step(act, err)
		id := v.ID
		check(id, true, 10)
		act, err = e.svc.Resize(ctx, e.Project, id, 20)
		step(act, err)
		check(id, true, 20)
		act, err = e.svc.Detach(ctx, e.Project, id)
		step(act, err)
		check(id, false, 20)
		act, err = e.svc.Resize(ctx, e.Project, id, 30)
		step(act, err)
		check(id, false, 20)
		act, err = e.svc.Attach(ctx, e.Project, id, "web")
		step(act, err)
		check(id, true, 30)
		act, err = e.svc.Detach(ctx, e.Project, id)
		step(act, err)
		check(id, false, 30)
		act, err = e.svc.Delete(ctx, e.Project, id)
		step(act, err)
		require.Empty(t, e.disks(t, id))
		_, err = e.App.Ent.Volume.Get(ctx, id)
		require.True(t, ent.IsNotFound(err))
		if !anyCrash {
			require.Greater(t, crashAt, 3)
			return
		}
	}
}

// bumpPVE writes an unrelated key before the first attach write, so its digest is stale once.
type bumpPVE struct {
	pve.Client
	vmid   int
	bumped bool
}

func (b *bumpPVE) Config(ctx context.Context, node string, vmid int) (map[string]string, error) {
	cfg, err := b.Client.Config(ctx, node, vmid)
	if err == nil && vmid == b.vmid && !b.bumped {
		b.bumped = true
		if err := b.Client.SetConfig(ctx, node, vmid, map[string]string{"net1": "virtio,bridge=vmbr1"}); err != nil {
			return nil, err
		}
	}
	return cfg, err
}

func TestAttachRetriesOnDigestMismatch(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	s := e.RunningServer("web")
	vmid := vmidOf(s)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)

	bp := &bumpPVE{vmid: vmid}
	bo := volumes.SetPVE(o, func(ctx context.Context, loc int64) (pve.Client, error) {
		pc, err := e.App.Catalog.ProxmoxFor(ctx, loc)
		bp.Client = pc
		return bp, err
	})
	before := len(e.App.PVE.Calls())
	act, err = e.svc.Attach(ctx, e.Project, v.ID, "web")
	e.ok(t, bo, act, err)
	require.True(t, bp.bumped)
	vm, _ := e.App.PVE.VM(vmid)
	require.Equal(t, "virtio,bridge=vmbr1", vm.Config["net1"])
	require.Equal(t, vref.ConfigValue(vref.Volid("local-lvm", v.ID, false), v.ID), vm.Config["virtio1"])
	puts := 0
	for _, c := range e.App.PVE.Calls()[before:] {
		if strings.HasPrefix(c, "PUT /nodes/pve1/qemu/") && strings.HasSuffix(c, "/config") {
			puts++
		}
	}
	require.Equal(t, 3, puts, "the bump, the stale write and the retry")
}

func TestAttachNoHotplugRunning(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	s := e.RunningServer("web")
	vmid := vmidOf(s)
	e.App.PVE.SetVMConfig(vmid, "hotplug", "network,usb")
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)

	act, err = e.svc.Attach(ctx, e.Project, v.ID, "web")
	require.NoError(t, err)
	got := e.finish(t, o, act)
	require.Equal(t, actions.StatusError, got.Status)
	require.Equal(t, actions.CodePowerOffRequired, got.Error.Code)
	require.Equal(t, "Power off the server to attach the volume.", got.Error.Message)
	require.Contains(t, got.Error.Detail, "hotplug")
	require.Empty(t, e.keysFor(t, vmid, v.ID))
	r := e.row(t, v.ID)
	require.Equal(t, volume.StatusAvailable, r.Status)
	require.Nil(t, r.ServerID)

	// Stopped, it attaches.
	e.App.PVE.SetVMStatus(vmid, "stopped")
	e.App.PVE.SetVMConfig(vmid, "hotplug", "network,disk,usb")
	act, err = e.svc.Attach(ctx, e.Project, v.ID, "web")
	e.ok(t, o, act, err)
	require.Len(t, e.keysFor(t, vmid, v.ID), 1)
}

func TestAttachAppliesDeferredGrow(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	rec := &recorder{}
	o := e.opts(rec)
	s := e.RunningServer("web")
	vmid := vmidOf(s)
	e.App.PVE.SetVMConfig(vmid, "virtio1", "local-lvm:vm-"+ref(int64(vmid))+"-disk-1,size=5G")
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)

	act, err = e.svc.Resize(ctx, e.Project, v.ID, 30)
	e.ok(t, o, act, err)
	r := e.row(t, v.ID)
	require.Equal(t, 30, r.SizeGB)
	require.Equal(t, 10, r.AppliedGB)
	require.EqualValues(t, int64(10)<<30, e.disks(t, v.ID)[0].Size)

	act, err = e.svc.Attach(ctx, e.Project, v.ID, "web")
	e.ok(t, o, act, err)
	r = e.row(t, v.ID)
	require.Equal(t, 30, r.AppliedGB)
	require.Equal(t, "virtio2", r.Device, "virtio1 is taken")
	require.EqualValues(t, int64(30)<<30, e.disks(t, v.ID)[0].Size)
	vm, _ := e.App.PVE.VM(vmid)
	require.Equal(t, vref.ConfigValue(vref.Volid("local-lvm", v.ID, false), v.ID)+",size=30G", vm.Config["virtio2"])
	require.Equal(t, "local-lvm:vm-"+ref(int64(vmid))+"-disk-1,size=5G", vm.Config["virtio1"])
	require.Equal(t, []ext.EventKind{ext.VolumeCreated, ext.VolumeResized}, rec.kinds())
}

func TestResizeFailureRevertsSize(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	e.RunningServer("web")
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Server: "web"})
	e.ok(t, o, act, err)

	act, err = e.svc.Resize(ctx, e.Project, v.ID, 20)
	require.NoError(t, err)
	require.Equal(t, 10, queuedArgs[volumes.ResizeVolumeArgs](t, e, act.ID).PrevSizeGB)
	e.App.PVE.FailNext("/resize", http.StatusBadRequest)
	got := e.finish(t, o, act)
	require.Equal(t, actions.StatusError, got.Status)
	r := e.row(t, v.ID)
	require.Equal(t, 10, r.SizeGB, "quota is given back")
	require.Equal(t, 10, r.AppliedGB)
	require.Equal(t, volume.StatusAttached, r.Status)

	// The disk grew although the call failed: keep the new size and record it.
	act, err = e.svc.Resize(ctx, e.Project, v.ID, 20)
	require.NoError(t, err)
	got = e.finish(t, taskFail(e, o), act)
	require.Equal(t, actions.StatusError, got.Status)
	r = e.row(t, v.ID)
	require.Equal(t, 20, r.SizeGB)
	require.Equal(t, 20, r.AppliedGB)
	require.EqualValues(t, int64(20)<<30, e.disks(t, v.ID)[0].Size)

	// Proxmox unreadable after the resize failed: the action still fails and size_gb stays.
	act, err = e.svc.Resize(ctx, e.Project, v.ID, 30)
	require.NoError(t, err)
	bad := &pve.Error{Status: http.StatusBadRequest, Message: "storage unreadable"}
	hp := &hookPVE{resizeErr: bad, contentErr: bad}
	got = e.finish(t, hp.opts(e, o), act)
	require.Equal(t, actions.StatusError, got.Status)
	r = e.row(t, v.ID)
	require.Equal(t, 30, r.SizeGB, "kept rather than guessed")
	require.Equal(t, 20, r.AppliedGB)
}

func TestReservedVMIDGuard(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, volumes.ReservedVMIDGuard(ctx, pc))

	e.App.PVE.AddVM("pve2", vref.OwnerVMID, "squatter")
	err = volumes.ReservedVMIDGuard(ctx, pc)
	require.ErrorContains(t, err, "a VM occupies reserved VMID 999999999")
	require.Equal(t, actions.CodeFailed, servers.Classify(err).Code)
	e.App.PVE.DeleteVM(vref.OwnerVMID)
	require.NoError(t, volumes.ReservedVMIDGuard(ctx, pc))

	// QEMU and LXC share VMIDs, so a container there would own the volumes too.
	e.App.PVE.AddContainer("pve2", vref.OwnerVMID)
	err = volumes.ReservedVMIDGuard(ctx, pc)
	require.ErrorContains(t, err, "a VM occupies reserved VMID 999999999")

	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	got := e.finish(t, o, act)
	require.Equal(t, actions.StatusError, got.Status)
	require.Equal(t, actions.CodeFailed, got.Error.Code)
	require.Contains(t, got.Error.Detail, "a VM occupies reserved VMID 999999999")
	require.Equal(t, volume.StatusError, e.row(t, v.ID).Status)
	for _, c := range e.App.PVE.Calls() {
		require.False(t, strings.HasPrefix(c, "POST /nodes/pve1/storage/"), c)
	}
}

func TestDeleteVolume404IsDone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.localStorage(t)
	rec := &recorder{}
	o := e.opts(rec)
	// Absent from storage: done without a delete call, whatever Proxmox would answer.
	id := e.put(t, e.Project, "gone", "pve1", "local-lvm", 10, nil, "")
	e.App.PVE.FailNext("/storage/local-lvm/content/local-lvm:"+vref.Name(id), http.StatusInternalServerError)
	act, err := e.svc.Delete(ctx, e.Project, id)
	e.ok(t, o, act, err)
	_, err = e.App.Ent.Volume.Get(ctx, id)
	require.True(t, ent.IsNotFound(err))
	require.Equal(t, []ext.EventKind{ext.VolumeDeleted}, rec.kinds())

	// Listed, but the delete itself answers 404: also done.
	e.pinPVE1(t)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "raced", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)
	hp := &hookPVE{deleteErr: &pve.Error{Status: http.StatusNotFound, Message: "no such volume"}}
	act, err = e.svc.Delete(ctx, e.Project, v.ID)
	e.ok(t, hp.opts(e, o), act, err)
	require.Equal(t, 1, hp.deletes)
	_, err = e.App.Ent.Volume.Get(ctx, v.ID)
	require.True(t, ent.IsNotFound(err))
}

func TestDeleteVolumeTaskErrorDiskStillThere(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)
	hp := &hookPVE{deleteErr: &pve.TaskError{UPID: "UPID:x", ExitStatus: "lvremove failed"}}
	act, err = e.svc.Delete(ctx, e.Project, v.ID)
	require.NoError(t, err)
	got := e.finish(t, hp.opts(e, o), act)
	require.Equal(t, actions.StatusError, got.Status)
	require.Equal(t, actions.CodeFailed, got.Error.Code)
	require.Equal(t, volume.StatusAvailable, e.row(t, v.ID).Status)
	require.Len(t, e.disks(t, v.ID), 1)
}

// hookPVE fails the calls given an error without running them.
type hookPVE struct {
	pve.Client
	deleteErr, resizeErr, contentErr error
	deletes                          int
}

func (h *hookPVE) opts(e *env, o volumes.WorkerOptions) volumes.WorkerOptions {
	return volumes.SetPVE(o, func(ctx context.Context, loc int64) (pve.Client, error) {
		pc, err := e.App.Catalog.ProxmoxFor(ctx, loc)
		h.Client = pc
		return h, err
	})
}

func (h *hookPVE) DeleteVolume(ctx context.Context, node, storage, volid string) error {
	h.deletes++
	if h.deleteErr != nil {
		return h.deleteErr
	}
	return h.Client.DeleteVolume(ctx, node, storage, volid)
}

func (h *hookPVE) ResizeDisk(ctx context.Context, node string, vmid int, disk string, gb int) error {
	if h.resizeErr != nil {
		return h.resizeErr
	}
	return h.Client.ResizeDisk(ctx, node, vmid, disk, gb)
}

func (h *hookPVE) StorageContent(ctx context.Context, node, storage string, vmid int) ([]pve.StorageItem, error) {
	if h.contentErr != nil {
		return nil, h.contentErr
	}
	return h.Client.StorageContent(ctx, node, storage, vmid)
}

// taskFailPVE runs the delete or resize, then reports its task as failed.
type taskFailPVE struct{ pve.Client }

func (c *taskFailPVE) DeleteVolume(ctx context.Context, node, storage, volid string) error {
	if err := c.Client.DeleteVolume(ctx, node, storage, volid); err != nil {
		return err
	}
	return &pve.TaskError{UPID: "UPID:x", ExitStatus: "lvremove failed"}
}

func (c *taskFailPVE) ResizeDisk(ctx context.Context, node string, vmid int, disk string, gb int) error {
	if err := c.Client.ResizeDisk(ctx, node, vmid, disk, gb); err != nil {
		return err
	}
	return &pve.Error{Status: http.StatusBadRequest, Message: "resize reported failure"}
}

func taskFail(e *env, o volumes.WorkerOptions) volumes.WorkerOptions {
	return volumes.SetPVE(o, func(ctx context.Context, loc int64) (pve.Client, error) {
		pc, err := e.App.Catalog.ProxmoxFor(ctx, loc)
		return &taskFailPVE{pc}, err
	})
}

func TestDeleteVolumeTaskErrorButGone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)
	act, err = e.svc.Delete(ctx, e.Project, v.ID)
	e.ok(t, taskFail(e, o), act, err)
	require.Empty(t, e.disks(t, v.ID))
	_, err = e.App.Ent.Volume.Get(ctx, v.ID)
	require.True(t, ent.IsNotFound(err))
}

func TestDeleteVolumeForbidden(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pinPVE1(t)
	o := e.opts(nil)
	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	e.ok(t, o, act, err)
	e.App.PVE.DenyDatastoreAllocate = true

	act, err = e.svc.Delete(ctx, e.Project, v.ID)
	require.NoError(t, err)
	got := e.finish(t, o, act)
	require.Equal(t, actions.StatusError, got.Status)
	require.Equal(t, actions.CodeFailed, got.Error.Code)
	require.Contains(t, got.Error.Detail, "local-lvm")
	require.Contains(t, got.Error.Detail, "grant NeblaISO on /storage/local-lvm; re-run the setup command")
	require.Equal(t, volume.StatusAvailable, e.row(t, v.ID).Status)
	require.Len(t, e.disks(t, v.ID), 1)
}
