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

package snapshots_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestProjectDeleteRefusedWithSnapshots(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName("s").
		SetLabels(map[string]string{}).SetSizeGB(10).SaveX(ctx)

	res := e.C.Send(http.MethodDelete, "/v1/projects/"+strconv.FormatInt(e.Project, 10), nil)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, res.StatusCode, string(raw))
	var v struct {
		Error struct{ Code, Message string }
	}
	require.NoError(t, json.Unmarshal(raw, &v))
	require.Equal(t, "project_not_empty", v.Error.Code)
	require.Equal(t, "Delete the project's servers, volumes, primary IPs, floating IPs, networks and snapshots first.", v.Error.Message)
}

func TestUsageCountsSnapshots(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	for i, gb := range []int{10, 15} {
		e.App.Ent.Snapshot.Create().SetProjectID(e.Project).SetLocationID(e.Location).SetName("s" + strconv.Itoa(i)).
			SetLabels(map[string]string{}).SetSizeGB(gb).SaveX(ctx)
	}
	u, err := snapshots.UsageWith(servers.Usage)(ctx, e.App.Ent, e.Project)
	require.NoError(t, err)
	require.Equal(t, projects.Usage{Snapshots: 2, SnapshotGB: 25}, u)
}
