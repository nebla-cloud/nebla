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

package servers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

// The disk grew in Proxmox before the success commit failed; the row keeps the grown size.
func TestChangeTypeFailureKeepsGrownDisk(t *testing.T) {
	ctx := context.Background()
	e, cw := directEnv(t)
	mediumID := e.Admin.CreateServerType("medium", 4, 4096, 40)
	s := builtServer(t, e, cw, "web")
	vmid := vmidOf(t, e, s.ServerID)
	e.App.PVE.SetVMStatus(vmid, "stopped")
	setStatus(t, e, s.ServerID, servers.StatusOff)
	id := startAction(t, e, s.ServerID, "change_type", map[string]any{"server_type": "medium", "upgrade_disk": true})
	args := queuedArgs[servers.ChangeTypeArgs](t, e, id)
	_, err := e.App.SQL.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION r2v_fail() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'injected db fault'; END $$ LANGUAGE plpgsql;
	CREATE TRIGGER r2v_t BEFORE UPDATE ON servers FOR EACH ROW WHEN (NEW.server_type_id = %d) EXECUTE FUNCTION r2v_fail();`, mediumID))
	require.NoError(t, err)
	w := servers.NewChangeTypeWorker(opts(e))
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, jobOf(args, servers.MaxAttempts)), &cancel), "terminal failure expected")
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	vm, _ := e.App.PVE.VM(vmid)
	require.Contains(t, vm.Config["scsi0"], "size=40G")
	require.Equal(t, 40, row.DiskGB)
	require.Equal(t, 2, row.Cores)
	require.Equal(t, "2", vm.Config["cores"])
}

func TestChangeTypeArgsRestored(t *testing.T) {
	from, to := servers.Spec{TypeID: 1, Cores: 2, DiskGB: 20}, servers.Spec{TypeID: 2, Cores: 4, DiskGB: 40}
	grow := servers.ChangeTypeArgs{UpgradeDisk: true, From: from, To: to}
	require.Equal(t, 40, grow.Restored(nil).DiskGB, "size unknown: the requested growth is kept")
	require.Equal(t, 20, servers.ChangeTypeArgs{From: from, To: to}.Restored(nil).DiskGB)
	require.Equal(t, int64(1), grow.Restored(nil).TypeID)
	require.Equal(t, 20, grow.Restored(new(20)).DiskGB, "the disk never grew")
	require.Equal(t, 40, grow.Restored(new(40)).DiskGB, "the disk grew")
	require.Equal(t, 20, grow.Restored(new(10)).DiskGB, "never below From")
}
