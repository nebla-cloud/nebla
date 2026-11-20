package osimages_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/catalogbuild"
	"github.com/Hexeption/nebla/internal/osimages"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

// failedHalfBuilt fails both debian-13 rows; the pve1 row keeps a tagged VM 7000 and its import volume.
func (e *env) failedHalfBuilt(loc int64) (*ent.CatalogBuild, string) {
	e.t.Helper()
	ctx := context.Background()
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(e.t, err)
	rows := e.rows("debian-13", loc)
	require.Len(e.t, rows, 2)
	for _, r := range rows {
		u := r.Update().SetStatus(catalogbuild.StatusFailed).SetErrorCode("build_failed").SetErrorMessage("boom")
		if r.Node == "pve1" {
			u.SetNewVmid(7000).SetNewNonce("aa").SetBuildNode("pve1").SetNewFile("debian.qcow2").SetNewChecksum(debianSum)
		}
		require.NoError(e.t, u.Exec(ctx))
	}
	row := rows[0]
	e.pve.AddVM("pve1", 7000, "nebla-debian-13")
	e.pve.SetVMConfig(7000, "tags", "nebla-tpl;"+osimages.TemplateTag(row.ID, "aa"))
	vol := fmt.Sprintf("imp:import/nebla-debian-13-%s-%d.qcow2", debianSum[:12], row.ID)
	e.pve.PutVolume(vol, []byte("img"))
	return row, vol
}

func (e *env) cleanupJobs() []osimages.CleanupArgs {
	e.t.Helper()
	var out []osimages.CleanupArgs
	for _, raw := range e.jobArgs(osimages.KindCleanup) {
		var a osimages.CleanupArgs
		require.NoError(e.t, json.Unmarshal(raw, &a))
		out = append(out, a)
	}
	return out
}

func (e *env) cleanupWork(a osimages.CleanupArgs) error {
	job := &river.Job[osimages.CleanupArgs]{JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, MaxAttempts: 25}, Args: a}
	return osimages.NewCleanupWorker(e.svc).Work(context.Background(), job)
}

func (e *env) vmExists(vmid int) bool {
	_, ok := e.pve.VM(vmid)
	return ok
}

func (e *env) volExists(vol string) bool {
	_, ok := e.pve.Volume(vol)
	return ok
}

func TestEnableRetryEnqueuesCleanup(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	row, vol := e.failedHalfBuilt(loc)
	_, err := e.svc.Enable(context.Background(), "debian-13", loc)
	require.NoError(t, err)
	require.Equal(t, []osimages.CleanupArgs{{BuildID: row.ID, LocationID: loc, Node: "pve1", VMID: 7000,
		Tag: osimages.TemplateTag(row.ID, "aa"), Volume: vol}}, e.cleanupJobs())
	r, err := e.ent.CatalogBuild.Get(context.Background(), row.ID)
	require.NoError(t, err)
	require.Equal(t, catalogbuild.StatusBuilding, r.Status)
	require.Nil(t, r.NewVmid)
}

func TestCleanupWaitsWhileRowBuilding(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	row, vol := e.failedHalfBuilt(loc)
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	a := e.cleanupJobs()[0]
	var snooze *rivertype.JobSnoozeError
	require.ErrorAs(t, e.cleanupWork(a), &snooze)
	require.True(t, e.vmExists(7000))
	require.True(t, e.volExists(vol))

	require.NoError(t, e.ent.CatalogBuild.UpdateOneID(row.ID).SetStatus(catalogbuild.StatusFailed).Exec(ctx))
	require.NoError(t, e.cleanupWork(a))
	require.False(t, e.vmExists(7000))
	require.False(t, e.volExists(vol))
}

func TestDisableEnqueuesCleanup(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	row, vol := e.failedHalfBuilt(loc)
	_, err := e.svc.Disable(context.Background(), "debian-13", loc)
	require.NoError(t, err)
	require.Empty(t, e.rows("debian-13", loc))
	jobs := e.cleanupJobs()
	require.Equal(t, []osimages.CleanupArgs{{BuildID: row.ID, LocationID: loc, Node: "pve1", VMID: 7000,
		Tag: osimages.TemplateTag(row.ID, "aa"), Volume: vol}}, jobs)
	require.NoError(t, e.cleanupWork(jobs[0]))
	require.False(t, e.vmExists(7000))
	require.False(t, e.volExists(vol))
}

func TestCleanupSkipsForeignVM(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	_, vol := e.failedHalfBuilt(loc)
	e.pve.SetVMConfig(7000, "tags", "nebla-tpl;nebla-tpl-9-zz")
	_, err := e.svc.Disable(context.Background(), "debian-13", loc)
	require.NoError(t, err)
	require.NoError(t, e.cleanupWork(e.cleanupJobs()[0]))
	require.True(t, e.vmExists(7000))
	require.False(t, e.volExists(vol))
}

func TestCleanupDestroysTaggedTemplate(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.failedHalfBuilt(loc)
	e.pve.SetVMConfig(7000, "template", "1")
	_, err := e.svc.Disable(context.Background(), "debian-13", loc)
	require.NoError(t, err)
	require.NoError(t, e.cleanupWork(e.cleanupJobs()[0]))
	require.False(t, e.vmExists(7000))
}

func TestCleanupLockedVMErrors(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.failedHalfBuilt(loc)
	e.pve.SetVMConfig(7000, "lock", "create")
	_, err := e.svc.Disable(context.Background(), "debian-13", loc)
	require.NoError(t, err)
	err = e.cleanupWork(e.cleanupJobs()[0])
	require.Error(t, err)
	var snooze *rivertype.JobSnoozeError
	require.NotErrorAs(t, err, &snooze)
	require.True(t, e.vmExists(7000))
}

func TestCleanupRetiredEntryNoVolume(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	row, _ := e.failedHalfBuilt(loc)
	_, err := e.sql.Exec(`update catalog_builds set catalog_key = 'debian-11' where location_id = $1`, loc)
	require.NoError(t, err)
	_, err = e.svc.Disable(ctx, "debian-11", loc)
	require.NoError(t, err)
	require.Equal(t, []osimages.CleanupArgs{{BuildID: row.ID, LocationID: loc, Node: "pve1", VMID: 7000,
		Tag: osimages.TemplateTag(row.ID, "aa")}}, e.cleanupJobs())
}

func TestCleanupVolumeDeleteExcludesEnable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.failedHalfBuilt(loc)
	e.pve.DeleteVM(7000)
	_, err := e.svc.Disable(ctx, "debian-13", loc)
	require.NoError(t, err)
	a := e.cleanupJobs()[0]
	row, _ := e.failedHalfBuilt(loc)
	a.BuildID = row.ID
	a.Volume = fmt.Sprintf("imp:import/nebla-debian-13-%s-%d.qcow2", debianSum[:12], row.ID)
	e.pve.SetTaskDelay(400 * time.Millisecond)

	var cleanupDone time.Time
	errc := make(chan error, 1)
	go func() {
		err := e.cleanupWork(a)
		cleanupDone = time.Now()
		errc <- err
	}()
	time.Sleep(150 * time.Millisecond)
	_, err = e.svc.Enable(ctx, "debian-13", loc)
	enableDone := time.Now()
	require.NoError(t, err)
	require.NoError(t, <-errc)
	require.False(t, e.volExists(a.Volume))
	require.False(t, cleanupDone.After(enableDone), "Enable must wait for the volume delete")
}

func TestDisableCleansOrphanVolume(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	row, vol := e.failedHalfBuilt(loc)
	e.pve.DeleteVM(7000)
	require.NoError(t, row.Update().ClearNewVmid().ClearNewNonce().Exec(ctx))
	_, err := e.svc.Disable(ctx, "debian-13", loc)
	require.NoError(t, err)
	jobs := e.cleanupJobs()
	require.Equal(t, []osimages.CleanupArgs{{BuildID: row.ID, LocationID: loc, Node: "pve1", Volume: vol}}, jobs)
	require.NoError(t, e.cleanupWork(jobs[0]))
	require.False(t, e.volExists(vol))
}
