package osimages_test

import (
	"context"
	"testing"

	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/osimages"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

const retireTag = "nebla-tpl-1-ab"

func (e *env) retireWork(loc int64, vmid int, tag string) error {
	job := &river.Job[osimages.RetireArgs]{JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, MaxAttempts: 25},
		Args: osimages.RetireArgs{LocationID: loc, VMID: vmid, Tag: tag}}
	return osimages.NewRetireWorker(e.svc).Work(context.Background(), job)
}

func (e *env) addTpl(vmid int, tags string) {
	e.pve.AddTemplate("pve1", vmid, "local-lvm")
	e.pve.SetVMConfig(vmid, "tags", tags)
}

func TestRetireDestroysTaggedTemplate(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.addTpl(8001, "nebla-tpl;"+retireTag)
	require.NoError(t, e.retireWork(loc, 8001, retireTag))
	_, ok := e.pve.VM(8001)
	require.False(t, ok)
}

func TestRetireSkipsForeignVM(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.addTpl(8001, "nebla-tpl;nebla-tpl-9-zz")
	e.pve.AddVM("pve1", 8002, "plain")
	e.pve.SetVMConfig(8002, "tags", "nebla-tpl;"+retireTag)
	require.NoError(t, e.retireWork(loc, 8001, retireTag))
	require.NoError(t, e.retireWork(loc, 8002, retireTag))
	for _, id := range []int{8001, 8002} {
		_, ok := e.pve.VM(id)
		require.True(t, ok, "vm %d kept", id)
	}
}

func TestRetireMissingVMOK(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	require.NoError(t, e.retireWork(loc, 8001, retireTag))
}

func TestRetireWaitsForInflightClone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	imgID := e.readyWithImage(loc)
	e.addTpl(8001, "nebla-tpl;"+retireTag)
	p, err := e.ent.Project.Create().SetName("p").Save(ctx)
	require.NoError(t, err)
	st, err := e.ent.ServerType.Create().SetName("small").SetCores(1).SetMemoryMB(512).SetDiskGB(10).Save(ctx)
	require.NoError(t, err)
	srv, err := e.ent.Server.Create().SetProjectID(p.ID).SetName("s").SetLocationID(loc).SetTemplateVmid(8001).
		SetServerTypeID(st.ID).SetImageID(imgID).SetMAC("bc:24:11:00:00:01").SetLabels(map[string]string{}).
		SetCores(1).SetMemoryMB(512).SetDiskGB(10).SetSSHKeys([]string{}).Save(ctx)
	require.NoError(t, err)

	var snooze *rivertype.JobSnoozeError
	require.ErrorAs(t, e.retireWork(loc, 8001, retireTag), &snooze)
	_, ok := e.pve.VM(8001)
	require.True(t, ok)

	require.NoError(t, e.ent.Server.UpdateOneID(srv.ID).SetStatus(server.StatusRunning).Exec(ctx))
	require.NoError(t, e.retireWork(loc, 8001, retireTag))
	_, ok = e.pve.VM(8001)
	require.False(t, ok)
}

func TestRetireWaitsForLiveRebuildJob(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.addTpl(8001, "nebla-tpl;"+retireTag)
	var id int64
	require.NoError(t, e.sql.QueryRow(`insert into river_job (kind, args, state, queue, max_attempts)
		values ('rebuild_server', '{"template_vmid": 8001}', 'available', 'default', 3) returning id`).Scan(&id))

	var snooze *rivertype.JobSnoozeError
	require.ErrorAs(t, e.retireWork(loc, 8001, retireTag), &snooze)
	_, ok := e.pve.VM(8001)
	require.True(t, ok)

	_, err := e.sql.Exec(`update river_job set state = 'completed', finalized_at = now() where id = $1`, id)
	require.NoError(t, err)
	require.NoError(t, e.retireWork(loc, 8001, retireTag))
	_, ok = e.pve.VM(8001)
	require.False(t, ok)
}
