package servers_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func TestUsageEvents(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	e := workerEnv(t, rec)
	a := e.CreateServer("web", nil)
	waitOK(t, e, a.ActionID)
	b := e.CreateServer("db", map[string]any{"start_after_create": false})
	waitOK(t, e, b.ActionID)

	o := opts(e)
	o.Bus = ext.NewBus(rec)
	t.Cleanup(o.Bus.(*ext.Bus).Close)
	w := servers.NewUsageWorker(o)
	created := time.Date(2026, 11, 19, 14, 59, 59, 0, time.UTC)
	require.NoError(t, w.Work(ctx, &river.Job[servers.UsageArgs]{JobRow: &rivertype.JobRow{CreatedAt: created}}))
	o.Bus.(*ext.Bus).Close()

	usage := slices.DeleteFunc(rec.all(), func(ev ext.Event) bool { return ev.Kind != ext.ServerUsage })
	require.Len(t, usage, 2)
	byID := map[int64]ext.Event{}
	for _, ev := range usage {
		require.Equal(t, e.Project, ev.ProjectID)
		require.False(t, ev.At.IsZero())
		byID[ev.ServerID] = ev
	}
	period := "2026-11-19T14:00:00Z"
	require.Equal(t, map[string]any{"server_type": "small", "status": "running", "cores": 2, "memory_mb": 2048, "disk_gb": 20,
		"period": period, "location_id": e.Location}, byID[a.ServerID].Data)
	require.Equal(t, period, byID[b.ServerID].Data["period"])
	require.Equal(t, "off", byID[b.ServerID].Data["status"])
}

func TestServerEventsCarryResource(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	e := workerEnv(t, rec)
	a := e.CreateServer("web", nil)
	waitOK(t, e, a.ActionID)
	o := opts(e)
	o.Bus = ext.NewBus(rec)
	w := servers.NewUsageWorker(o)
	require.NoError(t, w.Work(ctx, &river.Job[servers.UsageArgs]{JobRow: &rivertype.JobRow{CreatedAt: time.Now()}}))
	o.Bus.(*ext.Bus).Close()

	kinds := map[ext.EventKind]bool{}
	for _, ev := range rec.all() {
		kinds[ev.Kind] = true
		require.Equal(t, "server", ev.ResourceType, ev.Kind)
		require.Equal(t, ev.ServerID, ev.ResourceID, ev.Kind)
	}
	require.True(t, kinds[ext.ServerCreated])
	require.True(t, kinds[ext.ServerUsage])
}
