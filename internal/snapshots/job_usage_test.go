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
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func TestSnapshotUsageEvents(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.RunningServer("web1")
	ids := map[snapshot.Status]int64{}
	for i, st := range []snapshot.Status{snapshot.StatusAvailable, snapshot.StatusMissing, snapshot.StatusCreating, snapshot.StatusError} {
		snap, _, err := e.svc.Create(ctx, e.Project, snapshots.CreateInput{Server: "web1", Name: string(st)})
		require.NoError(t, err)
		require.NoError(t, e.App.Ent.Snapshot.UpdateOneID(snap.ID).SetStatus(st).SetSizeGB(10+i).Exec(ctx))
		e.settle(t)
		ids[st] = snap.ID
	}

	rec := &recorder{}
	w := snapshots.NewUsageWorker(e.opts(rec))
	created := time.Date(2026, 11, 23, 14, 59, 59, 0, time.UTC)
	require.NoError(t, w.Work(ctx, &river.Job[snapshots.UsageArgs]{JobRow: &rivertype.JobRow{CreatedAt: created}}))

	evs := rec.all()
	require.Len(t, evs, 2, "snapshots still creating, or failed, are not billed")
	byID := map[int64]ext.Event{}
	for _, ev := range evs {
		require.Equal(t, ext.SnapshotUsage, ev.Kind)
		require.Equal(t, e.Project, ev.ProjectID)
		require.Equal(t, snapshots.ResourceType, ev.ResourceType)
		require.Zero(t, ev.ServerID)
		require.False(t, ev.At.IsZero())
		byID[ev.ResourceID] = ev
	}
	period := created.UTC().Truncate(servers.UsageInterval).Format(time.RFC3339)
	require.Equal(t, map[string]any{"size_gb": 10, "location_id": e.Location, "period": period, "status": "available"},
		byID[ids[snapshot.StatusAvailable]].Data)
	require.Equal(t, map[string]any{"size_gb": 11, "location_id": e.Location, "period": period, "status": "missing"},
		byID[ids[snapshot.StatusMissing]].Data)

	vol, snap := volumes.UsageArgs{}.InsertOpts(), snapshots.UsageArgs{}.InsertOpts()
	require.Equal(t, vol.UniqueOpts.ByPeriod, snap.UniqueOpts.ByPeriod)
	require.Equal(t, vol.UniqueOpts.ByState, snap.UniqueOpts.ByState)
	require.Equal(t, vol.Queue, snap.Queue)
	require.Equal(t, vol.MaxAttempts, snap.MaxAttempts)
	require.NotNil(t, snapshots.UsagePeriodicJob())
	require.NotEqual(t, volumes.KindUsage, snapshots.UsageArgs{}.Kind())
}
