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

package backups_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/ent"
	entbackup "github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type env struct {
	*testutil.ServerEnv
	svc *backups.Service
	srv *servers.Service
	prj *projects.Service
	vol int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	a := e.App
	ctx := context.Background()
	require.NoError(t, a.Ent.Location.UpdateOneID(e.Location).SetBackupStorage("pbs").SetBackupStorageOk(true).Exec(ctx))
	require.NoError(t, a.Ent.Project.UpdateOneID(e.Project).SetMaxBackups(10).Exec(ctx))
	prj := projects.New(a.Ent, projects.Options{Hooks: projects.Hooks{Usage: backups.UsageWith(servers.Usage)}})
	svc := backups.New(backups.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Jobs: a.Jobs})
	srv := servers.New(servers.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: prj, Jobs: a.Jobs, BackupInfo: svc.BackupInfo})
	return &env{ServerEnv: e, svc: svc, srv: srv, prj: prj}
}

func requireAPIErr(t *testing.T, err error, status int, code string) {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "%v", err)
	require.Equal(t, status, ae.Status, ae.Msg)
	require.Equal(t, code, ae.Code, ae.Msg)
}

func (e *env) create(t *testing.T, name string, b *servers.BackupSettings) servers.Server {
	t.Helper()
	s, act, err := e.srv.Create(context.Background(), e.Project, servers.CreateInput{Name: name, ServerType: "small", Image: "ubuntu",
		Location: "eu-1", Backups: b})
	require.NoError(t, err)
	require.NoError(t, actions.Succeed(context.Background(), e.App.Ent, act.ID))
	return s
}

func (e *env) usage(t *testing.T) projects.Usage {
	t.Helper()
	u, err := backups.UsageWith(servers.Usage)(context.Background(), e.App.Ent, e.Project)
	require.NoError(t, err)
	return u
}

// running settles a server the way RunningServer does and turns its backups on.
func (e *env) running(t *testing.T, name string) int64 {
	t.Helper()
	s := e.RunningServer(name)
	_, err := e.srv.Update(context.Background(), e.Project, s.ServerID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: true, Keep: 3}})
	require.NoError(t, err)
	return s.ServerID
}

func (e *env) put(t *testing.T, serverID int64, st entbackup.Status) *ent.Backup {
	t.Helper()
	srv := e.App.Ent.Server.GetX(context.Background(), serverID)
	c := e.App.Ent.Backup.Create().SetProjectID(e.Project).SetServerID(serverID).SetLocationID(e.Location).SetStorage("pbs").
		SetNode(srv.Node).SetKind(entbackup.KindManual).SetStatus(st).SetInstanceGeneration(servers.InstanceGeneration(srv))
	if st != entbackup.StatusQueued && st != entbackup.StatusCreating {
		e.vol++
		c.SetVolid(fmt.Sprintf("pbs:backup/vm/%d/%d", e.vol, e.vol))
	}
	b, err := c.Save(context.Background())
	require.NoError(t, err)
	return b
}

func TestEnableBackupsReservesKeep(t *testing.T) {
	e := newEnv(t)
	a := e.create(t, "a", &servers.BackupSettings{Enabled: true, Keep: 7})
	require.True(t, a.Backups.Enabled)
	require.Equal(t, 7, a.Backups.Keep)
	require.Equal(t, 7, e.usage(t).BackupsReserved)
	row := e.App.Ent.Server.GetX(context.Background(), a.ID)
	require.Equal(t, int(a.ID*887%1440), row.BackupSlot)
	require.NotNil(t, row.BackupsEnabledAt)

	_, _, err := e.srv.Create(context.Background(), e.Project, servers.CreateInput{Name: "b", ServerType: "small", Image: "ubuntu",
		Location: "eu-1", Backups: &servers.BackupSettings{Enabled: true, Keep: 7}})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded")

	b := e.create(t, "c", nil)
	_, err = e.srv.Update(context.Background(), e.Project, b.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: true, Keep: 7}})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded")
	// A smaller keep fits, and raising it past the limit does not.
	_, err = e.srv.Update(context.Background(), e.Project, b.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: true, Keep: 3}})
	require.NoError(t, err)
	require.Equal(t, 10, e.usage(t).BackupsReserved)
	_, err = e.srv.Update(context.Background(), e.Project, b.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: true, Keep: 4}})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "quota_exceeded")
}

func TestDisableKeepsStoredReservation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.create(t, "a", &servers.BackupSettings{Enabled: true, Keep: 7})
	for range 3 {
		e.put(t, s.ID, entbackup.StatusAvailable)
	}
	// Queued and failed rows are not stored.
	e.put(t, s.ID, entbackup.StatusQueued)
	e.put(t, s.ID, entbackup.StatusError)
	u := e.usage(t)
	require.Equal(t, 3, u.Backups)
	require.Equal(t, 7, u.BackupsReserved)

	out, err := e.srv.Update(ctx, e.Project, s.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: false, Keep: 7}})
	require.NoError(t, err)
	require.False(t, out.Backups.Enabled)
	require.Equal(t, 3, out.Backups.Count)
	require.Equal(t, 3, e.usage(t).BackupsReserved)
}

func TestKeepBounds(t *testing.T) {
	e := newEnv(t)
	s := e.create(t, "a", nil)
	for _, keep := range []int{0, 15, -1} {
		_, err := e.srv.Update(context.Background(), e.Project, s.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: true, Keep: keep}})
		requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")
	}
	_, _, err := e.srv.Create(context.Background(), e.Project, servers.CreateInput{Name: "b", ServerType: "small", Image: "ubuntu",
		Location: "eu-1", Backups: &servers.BackupSettings{Enabled: true, Keep: 15}})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")
	for _, keep := range []int{1, 14} {
		_, err := e.srv.Update(context.Background(), e.Project, s.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: false, Keep: keep}})
		require.NoError(t, err)
	}
}

func TestEnableWithoutStorage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.create(t, "a", nil)
	require.NoError(t, e.App.Ent.Location.UpdateOneID(e.Location).SetBackupStorageOk(false).Exec(ctx))
	_, err := e.srv.Update(ctx, e.Project, s.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: true, Keep: 7}})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "backups_unavailable")
	_, _, err = e.srv.Create(ctx, e.Project, servers.CreateInput{Name: "b", ServerType: "small", Image: "ubuntu", Location: "eu-1",
		Backups: &servers.BackupSettings{Enabled: true, Keep: 7}})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "backups_unavailable")
	// Leaving them off is fine.
	_, err = e.srv.Update(ctx, e.Project, s.ID, servers.UpdateInput{Backups: &servers.BackupSettings{Enabled: false, Keep: 7}})
	require.NoError(t, err)
}

func TestBackUpNowReturnsPending(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.running(t, "a")
	b, created, err := e.svc.BackUpNow(ctx, e.Project, id)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "queued", b.Status)
	require.Equal(t, "manual", b.Kind)
	require.Equal(t, id, b.ServerID)

	again, created, err := e.svc.BackUpNow(ctx, e.Project, id)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, b.ID, again.ID)

	n, err := e.App.Ent.Backup.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	var jobs int
	require.NoError(t, e.App.SQL.QueryRow(`select count(*) from river_job where kind = 'create_backup'`).Scan(&jobs))
	require.Equal(t, 1, jobs)
	// No server lock and no action yet.
	acts, err := e.App.Ent.Action.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, acts, "only the create_server action")
}

func TestBackUpNowDisabled(t *testing.T) {
	e := newEnv(t)
	s := e.RunningServer("a")
	_, _, err := e.svc.BackUpNow(context.Background(), e.Project, s.ServerID)
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "backups_disabled")
	_, _, err = e.svc.BackUpNow(context.Background(), e.Project, s.ServerID+999)
	requireAPIErr(t, err, http.StatusNotFound, "not_found")
}

func TestRestoreRequiresAvailable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.running(t, "a")
	for _, st := range []entbackup.Status{entbackup.StatusQueued, entbackup.StatusCreating, entbackup.StatusRestoring,
		entbackup.StatusDeleting, entbackup.StatusError, entbackup.StatusMissing} {
		b := e.put(t, id, st)
		_, err := e.svc.Restore(ctx, e.Project, id, b.ID)
		requireAPIErr(t, err, http.StatusConflict, "backup_busy")
		require.NoError(t, e.App.Ent.Backup.DeleteOne(b).Exec(ctx))
	}
	b := e.put(t, id, entbackup.StatusAvailable)
	_, err := e.svc.Restore(ctx, e.Project, id+999, b.ID)
	requireAPIErr(t, err, http.StatusNotFound, "not_found")
	_, err = e.svc.Restore(ctx, e.Project, id, b.ID+999)
	requireAPIErr(t, err, http.StatusNotFound, "not_found")

	// The server must be running or off.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusError).Exec(ctx))
	_, err = e.svc.Restore(ctx, e.Project, id, b.ID)
	requireAPIErr(t, err, http.StatusConflict, "server_not_ready")
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusOff).Exec(ctx))
	act, err := e.svc.Restore(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	require.Equal(t, "restore_backup", act.Command)
	require.Equal(t, entbackup.StatusRestoring, e.App.Ent.Backup.GetX(ctx, b.ID).Status)
	require.Equal(t, server.StatusRebuilding, e.App.Ent.Server.GetX(ctx, id).Status)

	// A running restore locks the server.
	_, err = e.svc.Restore(ctx, e.Project, id, b.ID)
	requireAPIErr(t, err, http.StatusConflict, "locked")
}

func TestRestoreCancelsQueued(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.running(t, "a")
	b := e.put(t, id, entbackup.StatusAvailable)
	e.put(t, id, entbackup.StatusQueued)
	_, err := e.svc.Restore(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	n, err := e.App.Ent.Backup.Query().Where(entbackup.StatusEQ(entbackup.StatusQueued)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func restoreArgs(t *testing.T, e *env, actionJobID *int64) servers.RestoreServerArgs {
	t.Helper()
	var raw []byte
	require.NoError(t, e.App.SQL.QueryRow(`select args from river_job where id = $1`, *actionJobID).Scan(&raw))
	var args servers.RestoreServerArgs
	require.NoError(t, json.Unmarshal(raw, &args))
	return args
}

func TestRestoreIdentity(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.running(t, "a")
	row := e.App.Ent.Server.GetX(ctx, id)
	fp, err := servers.NetworkFingerprint(ctx, e.App.Ent, row)
	require.NoError(t, err)

	b := e.put(t, id, entbackup.StatusAvailable)
	got, err := e.svc.Get(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	require.True(t, got.RestoreResetsIdentity, "an empty fingerprint counts as changed")

	require.NoError(t, e.App.Ent.Backup.UpdateOneID(b.ID).SetNetworkFingerprint(fp).SetInstanceGeneration(5).Exec(ctx))
	got, err = e.svc.Get(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	require.False(t, got.RestoreResetsIdentity)
	list, err := e.svc.List(ctx, e.Project, id, backups.ListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.False(t, list[0].RestoreResetsIdentity)

	act, err := e.svc.Restore(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	args := restoreArgs(t, e, act.RiverJobID)
	require.Equal(t, 5, args.InstanceGeneration)
	require.Equal(t, b.ID, args.BackupID)
	require.Equal(t, id, args.ServerID)
	require.Equal(t, act.ID, args.ActionID)
	require.True(t, args.StartAfter)

	// A different stored fingerprint gets a new generation.
	b2 := e.put(t, id, entbackup.StatusAvailable)
	require.NoError(t, e.App.Ent.Backup.UpdateOneID(b2.ID).SetNetworkFingerprint("other").SetInstanceGeneration(5).Exec(ctx))
	require.NoError(t, e.App.Ent.Server.UpdateOneID(id).SetStatus(server.StatusRunning).Exec(ctx))
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, act.ID))
	act, err = e.svc.Restore(ctx, e.Project, id, b2.ID)
	require.NoError(t, err)
	require.Zero(t, restoreArgs(t, e, act.RiverJobID).InstanceGeneration)
}

func TestDeleteBackupBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.running(t, "a")
	for _, st := range []entbackup.Status{entbackup.StatusQueued, entbackup.StatusCreating, entbackup.StatusRestoring, entbackup.StatusDeleting} {
		b := e.put(t, id, st)
		_, err := e.svc.Delete(ctx, e.Project, id, b.ID)
		requireAPIErr(t, err, http.StatusConflict, "backup_busy")
		require.NoError(t, e.App.Ent.Backup.DeleteOne(b).Exec(ctx))
	}
	b := e.put(t, id, entbackup.StatusAvailable)
	act, err := e.svc.Delete(ctx, e.Project, id, b.ID)
	require.NoError(t, err)
	require.Equal(t, "delete_backup", act.Command)
	require.Equal(t, entbackup.StatusDeleting, e.App.Ent.Backup.GetX(ctx, b.ID).Status)
}

func TestListFilterAndInfo(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	id := e.running(t, "a")
	first := e.put(t, id, entbackup.StatusAvailable)
	second := e.put(t, id, entbackup.StatusMissing)
	all, err := e.svc.List(ctx, e.Project, id, backups.ListFilter{})
	require.NoError(t, err)
	require.Equal(t, []int64{second.ID, first.ID}, []int64{all[0].ID, all[1].ID})
	only, err := e.svc.List(ctx, e.Project, id, backups.ListFilter{Status: "missing"})
	require.NoError(t, err)
	require.Len(t, only, 1)
	_, err = e.svc.List(ctx, e.Project, id, backups.ListFilter{Status: "bogus"})
	requireAPIErr(t, err, http.StatusUnprocessableEntity, "invalid_input")

	got, err := e.srv.Get(ctx, e.Project, id)
	require.NoError(t, err)
	require.True(t, got.Backups.Enabled)
	require.Equal(t, 3, got.Backups.Keep)
	require.Equal(t, 2, got.Backups.Count, "available and missing rows are stored")
	require.NotNil(t, got.Backups.NextBackupAt)
	require.Nil(t, got.Backups.LastBackupAt)
}
