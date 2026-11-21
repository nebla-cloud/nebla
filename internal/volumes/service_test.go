package volumes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

type env struct {
	*testutil.ServerEnv
	svc *volumes.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	a := e.App
	require.NoError(t, a.Ent.Location.UpdateOneID(e.Location).SetVolumeStorageOk(true).Exec(context.Background()))
	proj := projects.New(a.Ent, projects.Options{Hooks: projects.Hooks{Usage: volumes.UsageWith(servers.Usage)}})
	svc := volumes.New(volumes.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs})
	return &env{ServerEnv: e, svc: svc}
}

// localStorage points the location's volumes at node-local local-lvm.
func (e *env) localStorage(t *testing.T) {
	t.Helper()
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorage("local-lvm").Exec(context.Background()))
}

func (e *env) row(t *testing.T, id int64) *ent.Volume {
	t.Helper()
	v, err := e.App.Ent.Volume.Get(context.Background(), id)
	require.NoError(t, err)
	return v
}

// put inserts a settled volume row directly.
func (e *env) put(t *testing.T, project int64, name, nodeName, storage string, sizeGB int, serverID *int64, device string) int64 {
	t.Helper()
	st := volume.StatusAvailable
	if serverID != nil {
		st = volume.StatusAttached
	}
	v, err := e.App.Ent.Volume.Create().SetProjectID(project).SetLocationID(e.Location).SetName(name).SetLabels(map[string]string{}).
		SetSizeGB(sizeGB).SetAppliedGB(sizeGB).SetNode(nodeName).SetStorage(storage).SetNillableServerID(serverID).
		SetDevice(device).SetStatus(st).Save(context.Background())
	require.NoError(t, err)
	require.NoError(t, v.Update().SetVolid(vref.Volid(storage, v.ID, false)).Exec(context.Background()))
	return v.ID
}

func requireAPIErr(t *testing.T, err error, status int, code string) apierr.E {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "%v", err)
	require.Equal(t, status, ae.Status, ae.Msg)
	require.Equal(t, code, ae.Code, ae.Msg)
	return ae
}

func ref(id int64) string { return strconv.FormatInt(id, 10) }

func resources(a actions.Action) []actions.Resource { return a.Resources }

func TestCreateStandalonePicksMostFreeNode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.localStorage(t)
	e.App.PVE.SetStorageAvail("pve1", "local-lvm", 20_000)
	e.App.PVE.SetStorageAvail("pve2", "local-lvm", 30_000)

	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1",
		Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)
	require.Equal(t, "data", v.Name)
	require.Equal(t, 10, v.SizeGB)
	require.Equal(t, "creating", v.Status)
	require.Equal(t, "eu-1", v.LocationName)
	require.Nil(t, v.ServerID)
	require.Equal(t, "/dev/disk/by-id/virtio-nb-vol-"+ref(v.ID), v.LinuxDevice)
	require.Equal(t, map[string]string{"env": "prod"}, v.Labels)
	require.Equal(t, "create_volume", act.Command)
	require.Equal(t, []actions.Resource{{ID: v.ID, Type: volumes.ResourceType}}, resources(act))

	r := e.row(t, v.ID)
	require.Equal(t, "pve2", r.Node)
	require.Equal(t, "local-lvm", r.Storage)
	require.Equal(t, 0, r.AppliedGB)

	// A size that only fits on pve1 goes there.
	e.App.PVE.SetStorageAvail("pve1", "local-lvm", 40_000)
	e.App.PVE.SetStorageAvail("pve2", "local-lvm", 1_000)
	v2, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data2", SizeGB: 30, Location: "eu-1"})
	require.NoError(t, err)
	require.Equal(t, "pve1", e.row(t, v2.ID).Node)

	// Nothing fits.
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "big", SizeGB: 100, Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "resource_unavailable")

	// Validation.
	for _, in := range []volumes.CreateInput{
		{Name: "Bad", SizeGB: 10, Location: "eu-1"},
		{Name: "ok", SizeGB: 9, Location: "eu-1"},
		{Name: "ok", SizeGB: 10241, Location: "eu-1"},
		{Name: "ok", SizeGB: 10},
	} {
		_, _, err = e.svc.Create(ctx, e.Project, in)
		requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")
	}
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error")
}

func TestCreateWithServerPinsToServerNode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.localStorage(t)
	s := e.RunningServer("web")
	e.App.PVE.MoveVM(100+int(s.ServerID), "pve2")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetNode("pve2").Exec(ctx))
	e.App.PVE.SetStorageAvail("pve1", "local-lvm", 90_000)

	v, act, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Server: "web"})
	require.NoError(t, err)
	require.Equal(t, "eu-1", v.LocationName)
	require.Nil(t, v.ServerID, "the job records the attachment")
	require.Equal(t, []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}, {ID: v.ID, Type: volumes.ResourceType}}, resources(act))
	r := e.row(t, v.ID)
	require.Equal(t, "pve2", r.Node)
	require.Equal(t, "local-lvm", r.Storage)

	// The server's action now holds it.
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data2", SizeGB: 10, Server: ref(s.ServerID)})
	requireAPIErr(t, err, http.StatusConflict, "locked")

	// A location that isn't the server's is refused.
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	pv2 := fake.New(t)
	testutil.SeedPVE(pv2)
	other, err := e.App.Catalog.CreateLocation(ctx, testutil.LocationInput(pv2, "eu-2"))
	require.NoError(t, err)
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data3", SizeGB: 10, Server: "web", Location: other.Name})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "volume_server_mismatch")
	v4, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data4", SizeGB: 10, Server: "web", Location: "eu-1"})
	require.NoError(t, err)
	require.Equal(t, "pve2", e.row(t, v4.ID).Node)
}

func TestCreateSharedStorageNoPin(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	v, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	r := e.row(t, v.ID)
	require.Equal(t, "", r.Node)
	require.Equal(t, "ceph", r.Storage)

	s := e.RunningServer("web")
	v2, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data2", SizeGB: 10, Server: "web"})
	require.NoError(t, err)
	r = e.row(t, v2.ID)
	require.Equal(t, "", r.Node)
	require.Equal(t, "ceph", r.Storage)
	require.NotZero(t, s.ServerID)
}

func TestVolumeStorageFallsBackToNodeOverride(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	require.NoError(t, e.App.Ent.Node.Update().Where(node.Name("pve1")).SetDiskStorageOverride("local-lvm").Exec(ctx))
	require.NoError(t, e.App.Ent.Node.Update().Where(node.Name("pve2")).SetEnabled(false).Exec(ctx))
	v, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	r := e.row(t, v.ID)
	require.Equal(t, "pve1", r.Node)
	require.Equal(t, "local-lvm", r.Storage)

	// An explicit volume storage wins over the override.
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorage("ceph").Exec(ctx))
	v2, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "data2", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	r = e.row(t, v2.ID)
	require.Equal(t, "", r.Node)
	require.Equal(t, "ceph", r.Storage)
}

func TestAttachDifferentNodeRejected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	vol := e.put(t, e.Project, "data", "pve2", "local-lvm", 10, nil, "")
	_, err := e.svc.Attach(ctx, e.Project, vol, "web")
	ae := requireAPIErr(t, err, http.StatusUnprocessableEntity, "volume_server_mismatch")
	require.Equal(t, "This volume is on a different host in this location", ae.Msg)

	same := e.put(t, e.Project, "data2", "pve1", "local-lvm", 10, nil, "")
	act, err := e.svc.Attach(ctx, e.Project, same, ref(s.ServerID))
	require.NoError(t, err)
	require.Equal(t, "attach_volume", act.Command)
	require.Equal(t, []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}, {ID: same, Type: volumes.ResourceType}}, resources(act))
	require.Equal(t, volume.StatusAvailable, e.row(t, same).Status, "attach keeps the status until the job")
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))

	shared := e.put(t, e.Project, "data3", "", "ceph", 10, nil, "")
	act, err = e.svc.Attach(ctx, e.Project, shared, "web")
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))

	attached := e.put(t, e.Project, "data4", "", "ceph", 10, &s.ServerID, "virtio1")
	_, err = e.svc.Attach(ctx, e.Project, attached, "web")
	requireAPIErr(t, err, http.StatusConflict, "volume_attached")
}

func TestAttachLimit14(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	for i := 1; i <= 14; i++ {
		e.put(t, e.Project, fmt.Sprintf("v%d", i), "", "ceph", 10, &s.ServerID, fmt.Sprintf("virtio%d", i))
	}
	extra := e.put(t, e.Project, "extra", "", "ceph", 10, nil, "")
	_, err := e.svc.Attach(ctx, e.Project, extra, "web")
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "volume_limit")
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "new", SizeGB: 10, Server: "web"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "volume_limit")

	// Already attached elsewhere, and detach of a detached volume.
	_, err = e.svc.Attach(ctx, e.Project, e.put(t, e.Project, "on", "", "ceph", 10, &s.ServerID, "virtio15"), "web")
	requireAPIErr(t, err, http.StatusConflict, "volume_attached")
	_, err = e.svc.Detach(ctx, e.Project, extra)
	requireAPIErr(t, err, http.StatusConflict, "volume_not_attached")
}

func TestVolumeQuota(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	require.NoError(t, e.App.Ent.Project.UpdateOneID(e.Project).SetMaxVolumes(2).SetMaxVolumeGB(30).Exec(ctx))
	a, _, err := e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "a", SizeGB: 10, Location: "eu-1"})
	require.NoError(t, err)
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "b", SizeGB: 30, Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded")
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "b", SizeGB: 20, Location: "eu-1"})
	require.NoError(t, err)
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "c", SizeGB: 10, Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded")

	// Resize reserves the extra GB.
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(a.ID).SetStatus(volume.StatusAvailable).Exec(ctx))
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, lastAction(t, e, a.ID)))
	_, err = e.svc.Resize(ctx, e.Project, a.ID, 11)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded")
	require.NoError(t, e.App.Ent.Project.UpdateOneID(e.Project).SetMaxVolumeGB(31).Exec(ctx))
	_, err = e.svc.Resize(ctx, e.Project, a.ID, 11)
	require.NoError(t, err)
	require.Equal(t, 11, e.row(t, a.ID).SizeGB)
}

func lastAction(t *testing.T, e *env, volumeID int64) int64 {
	t.Helper()
	list, _, err := e.svc.Actions(context.Background(), e.Project, volumeID, actions.ListFilter{}, 1, 50)
	require.NoError(t, err)
	require.NotEmpty(t, list)
	return list[len(list)-1].ID
}

func TestDeleteAttachedRejected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	on := e.put(t, e.Project, "on", "", "ceph", 10, &s.ServerID, "virtio1")
	_, err := e.svc.Delete(ctx, e.Project, on)
	requireAPIErr(t, err, http.StatusConflict, "volume_attached")

	off := e.put(t, e.Project, "off", "", "ceph", 10, nil, "")
	act, err := e.svc.Delete(ctx, e.Project, off)
	require.NoError(t, err)
	require.Equal(t, "delete_volume", act.Command)
	require.Equal(t, []actions.Resource{{ID: off, Type: volumes.ResourceType}}, resources(act))
	require.Equal(t, volume.StatusDeleting, e.row(t, off).Status)
	_, err = e.svc.Delete(ctx, e.Project, off)
	requireAPIErr(t, err, http.StatusConflict, "locked")

	// Error volumes may only be deleted.
	bad := e.put(t, e.Project, "bad", "", "ceph", 10, nil, "")
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(bad).SetStatus(volume.StatusError).Exec(ctx))
	_, err = e.svc.Attach(ctx, e.Project, bad, "web")
	requireAPIErr(t, err, http.StatusConflict, "volume_not_ready")
	_, err = e.svc.Resize(ctx, e.Project, bad, 20)
	requireAPIErr(t, err, http.StatusConflict, "volume_not_ready")
	_, err = e.svc.Delete(ctx, e.Project, bad)
	require.NoError(t, err)

	// Detach of the attached one locks the server first.
	act, err = e.svc.Detach(ctx, e.Project, on)
	require.NoError(t, err)
	require.Equal(t, "detach_volume", act.Command)
	require.Equal(t, []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}, {ID: on, Type: volumes.ResourceType}}, resources(act))
	require.Equal(t, volume.StatusAttached, e.row(t, on).Status)
}

func TestResizeShrinkRejected(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	off := e.put(t, e.Project, "off", "", "ceph", 20, nil, "")
	_, err := e.svc.Resize(ctx, e.Project, off, 10)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "volume_shrink_not_allowed")
	_, err = e.svc.Resize(ctx, e.Project, off, 10241)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")

	act, err := e.svc.Resize(ctx, e.Project, off, 30)
	require.NoError(t, err)
	require.Equal(t, "resize_volume", act.Command)
	require.Equal(t, []actions.Resource{{ID: off, Type: volumes.ResourceType}}, resources(act))
	require.Equal(t, volumes.ResizeVolumeArgs{VolumeID: off, ActionID: act.ID, SizeGB: 30, PrevSizeGB: 20},
		queuedArgs[volumes.ResizeVolumeArgs](t, e, act.ID))
	r := e.row(t, off)
	require.Equal(t, 30, r.SizeGB)
	require.Equal(t, 20, r.AppliedGB)
	require.Equal(t, volume.StatusAvailable, r.Status)

	s := e.RunningServer("web")
	on := e.put(t, e.Project, "on", "", "ceph", 20, &s.ServerID, "virtio1")
	act, err = e.svc.Resize(ctx, e.Project, on, 25)
	require.NoError(t, err)
	require.Equal(t, []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}, {ID: on, Type: volumes.ResourceType}}, resources(act))
}

func TestLockedWhileServerBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	vol := e.put(t, e.Project, "data", "", "ceph", 10, nil, "")
	on := e.put(t, e.Project, "on", "", "ceph", 10, &s.ServerID, "virtio1")

	busy, err := actions.Create(ctx, e.App.Ent, e.Project, "reboot_server", []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}})
	require.NoError(t, err)
	_, err = e.svc.Attach(ctx, e.Project, vol, "web")
	requireAPIErr(t, err, http.StatusConflict, "locked")
	_, err = e.svc.Detach(ctx, e.Project, on)
	requireAPIErr(t, err, http.StatusConflict, "locked")
	_, err = e.svc.Resize(ctx, e.Project, on, 20)
	requireAPIErr(t, err, http.StatusConflict, "locked")
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "new", SizeGB: 10, Server: "web"})
	requireAPIErr(t, err, http.StatusConflict, "locked")
	// A detached volume doesn't care.
	_, err = e.svc.Resize(ctx, e.Project, vol, 20)
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, busy.ID))

	// The volume's own running action locks it.
	_, err = e.svc.Attach(ctx, e.Project, vol, "web")
	requireAPIErr(t, err, http.StatusConflict, "locked")

	// A server mid-rebuild, delete or create is locked too.
	for _, st := range []server.Status{server.StatusRebuilding, server.StatusDeleting, server.StatusInitializing} {
		require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus(st).Exec(ctx))
		_, err = e.svc.Detach(ctx, e.Project, on)
		requireAPIErr(t, err, http.StatusConflict, "locked")
	}

	// Creating and deleting volumes are locked.
	c := e.put(t, e.Project, "c", "", "ceph", 10, nil, "")
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(c).SetStatus(volume.StatusCreating).Exec(ctx))
	_, err = e.svc.Delete(ctx, e.Project, c)
	requireAPIErr(t, err, http.StatusConflict, "locked")
}

func TestUsageIncludesVolumes(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.CreateServer("web", nil)
	e.put(t, e.Project, "a", "", "ceph", 10, nil, "")
	e.put(t, e.Project, "b", "", "ceph", 25, nil, "")
	other := e.Admin.CreateProject("other")
	e.put(t, other, "c", "", "ceph", 99, nil, "")

	u, err := volumes.UsageWith(servers.Usage)(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, 2, u.Volumes)
	require.Equal(t, 35, u.VolumeGB)
	require.Equal(t, 1, u.Servers)
	require.Equal(t, 20, u.DiskGB)

	// The API's default hooks count volumes.
	p, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	u, err = p.Usage(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, 2, u.Volumes)
	require.Equal(t, 35, u.VolumeGB)
}

func TestVolumesUnavailable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	ok, err := e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorageOk(false).Exec(ctx))
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.False(t, ok)
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "a", SizeGB: 10, Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "volumes_unavailable")

	// Available trusts the cached check; create still finds "local" holds no images.
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetVolumeStorageOk(true).SetVolumeStorage("local").Exec(ctx))
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.True(t, ok)
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "a", SizeGB: 10, Location: "eu-1"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "volumes_unavailable")

	// No enabled online node: unavailable.
	require.NoError(t, e.App.Ent.Node.Update().Where(node.LocationID(e.Location)).SetEnabled(false).Exec(ctx))
	ok, err = e.svc.Available(ctx, e.Location)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCrossProjectNotFound(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web")
	other := e.Admin.CreateProject("other")
	res := e.Admin.WithProject(other).Send(http.MethodPost, "/v1/servers",
		map[string]any{"name": "theirs", "server_type": "small", "image": "ubuntu", "location": "eu-1"})
	defer res.Body.Close()
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var created struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&created))
	theirSrv := created.Server
	theirVol := e.put(t, other, "data", "", "ceph", 10, nil, "")
	myVol := e.put(t, e.Project, "data", "", "ceph", 10, nil, "")

	nf := func(err error) { t.Helper(); requireAPIErr(t, err, http.StatusNotFound, "not_found") }
	_, err := e.svc.Get(ctx, e.Project, theirVol)
	nf(err)
	_, err = e.svc.Update(ctx, e.Project, theirVol, volumes.UpdateInput{Name: new("x")})
	nf(err)
	_, err = e.svc.Attach(ctx, e.Project, theirVol, "web")
	nf(err)
	_, err = e.svc.Detach(ctx, e.Project, theirVol)
	nf(err)
	_, err = e.svc.Resize(ctx, e.Project, theirVol, 20)
	nf(err)
	_, err = e.svc.Delete(ctx, e.Project, theirVol)
	nf(err)
	_, err = e.svc.Attach(ctx, e.Project, myVol, ref(theirSrv.ID))
	nf(err)
	_, err = e.svc.Attach(ctx, e.Project, myVol, "theirs")
	nf(err)
	_, _, err = e.svc.Create(ctx, e.Project, volumes.CreateInput{Name: "x", SizeGB: 10, Server: ref(theirSrv.ID)})
	nf(err)

	list, total, err := e.svc.List(ctx, e.Project, volumes.ListFilter{}, 1, 50)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, myVol, list[0].ID)
}

func TestListAndUpdate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	a := e.put(t, e.Project, "a", "", "ceph", 10, &s.ServerID, "virtio1")
	b := e.put(t, e.Project, "b", "", "ceph", 10, nil, "")
	_, err := e.svc.Update(ctx, e.Project, b, volumes.UpdateInput{Labels: &map[string]string{"env": "prod"}})
	require.NoError(t, err)
	_, err = e.svc.Update(ctx, e.Project, a, volumes.UpdateInput{Name: new("b")})
	requireAPIErr(t, err, http.StatusConflict, "uniqueness_error")
	v, err := e.svc.Update(ctx, e.Project, a, volumes.UpdateInput{Name: new("a2")})
	require.NoError(t, err)
	require.Equal(t, "a2", v.Name)
	require.Equal(t, s.ServerID, *v.ServerID)

	ids := func(f volumes.ListFilter) []int64 {
		t.Helper()
		list, _, err := e.svc.List(ctx, e.Project, f, 1, 50)
		require.NoError(t, err)
		out := []int64{}
		for _, v := range list {
			out = append(out, v.ID)
		}
		return out
	}
	require.Equal(t, []int64{a, b}, ids(volumes.ListFilter{}))
	require.Equal(t, []int64{b}, ids(volumes.ListFilter{LabelSelector: "env=prod"}))
	require.Equal(t, []int64{a}, ids(volumes.ListFilter{LabelSelector: "!env"}))
	require.Equal(t, []int64{a}, ids(volumes.ListFilter{Server: "web"}))
	require.Equal(t, []int64{b}, ids(volumes.ListFilter{Status: "available"}))
	require.Equal(t, []int64{a, b}, ids(volumes.ListFilter{Location: "eu-1"}))
	require.Equal(t, []int64{}, ids(volumes.ListFilter{Location: "nowhere"}))
	require.Equal(t, []int64{}, ids(volumes.ListFilter{Server: "nobody"}))
	require.Equal(t, []int64{b}, ids(volumes.ListFilter{Name: "b"}))
	_, _, err = e.svc.List(ctx, e.Project, volumes.ListFilter{Status: "nope"}, 1, 50)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")

	m, err := e.svc.ServerVolumes(ctx, []int64{s.ServerID, 999})
	require.NoError(t, err)
	require.Equal(t, map[int64][]int64{s.ServerID: {a}}, m)
}

func TestPickTiesByNodeName(t *testing.T) {
	c := []volumes.Candidate{{Node: "pve2", Avail: 20 << 30}, {Node: "pve1", Avail: 20 << 30}, {Node: "pve3", Avail: 5 << 30}}
	got, err := volumes.Pick(c, 10)
	require.NoError(t, err)
	require.Equal(t, "pve1", got.Node)
	_, err = volumes.Pick(c, 21)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "resource_unavailable")
}
