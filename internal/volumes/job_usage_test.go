package volumes_test

import (
	"context"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func TestVolumeUsageEvents(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.RunningServer("web")
	off := e.put(t, e.Project, "off", "", "ceph", 10, nil, "")
	on := e.put(t, e.Project, "on", "", "ceph", 25, &s.ServerID, "virtio1")
	// Volumes without a disk yet, or that never got one, are not billed.
	pending := e.put(t, e.Project, "pending", "", "ceph", 10, nil, "")
	broken := e.put(t, e.Project, "broken", "", "ceph", 10, nil, "")
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(pending).SetStatus(volume.StatusCreating).Exec(ctx))
	require.NoError(t, e.App.Ent.Volume.UpdateOneID(broken).SetStatus(volume.StatusError).Exec(ctx))

	rec := &recorder{}
	w := volumes.NewUsageWorker(e.opts(rec))
	created := time.Date(2026, 11, 19, 14, 59, 59, 0, time.UTC)
	require.NoError(t, w.Work(ctx, &river.Job[volumes.UsageArgs]{JobRow: &rivertype.JobRow{CreatedAt: created}}))

	evs := rec.all()
	require.Len(t, evs, 2)
	byID := map[int64]ext.Event{}
	for _, ev := range evs {
		require.Equal(t, ext.VolumeUsage, ev.Kind)
		require.Equal(t, e.Project, ev.ProjectID)
		require.Equal(t, volumes.ResourceType, ev.ResourceType)
		require.Zero(t, ev.ServerID)
		require.False(t, ev.At.IsZero())
		byID[ev.ResourceID] = ev
	}
	// The same period format as server.usage, so consumers dedupe both the same way.
	period := created.UTC().Truncate(servers.UsageInterval).Format(time.RFC3339)
	require.Equal(t, "2026-11-19T14:00:00Z", period)
	require.Equal(t, map[string]any{"size_gb": 10, "location_id": e.Location, "attached": false, "period": period,
		"status": "available"}, byID[off].Data)
	require.Equal(t, map[string]any{"size_gb": 25, "location_id": e.Location, "attached": true, "period": period,
		"status": "attached"}, byID[on].Data)

	srv, vol := servers.UsageArgs{}.InsertOpts(), volumes.UsageArgs{}.InsertOpts()
	require.Equal(t, srv.UniqueOpts.ByPeriod, vol.UniqueOpts.ByPeriod)
	require.Equal(t, srv.UniqueOpts.ByState, vol.UniqueOpts.ByState)
	require.Equal(t, srv.Queue, vol.Queue)
	require.NotNil(t, volumes.UsagePeriodicJob())
	require.NotEqual(t, servers.KindUsage, volumes.UsageArgs{}.Kind())
}
