package servers_test

import (
	"context"
	"slices"
	"testing"

	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/servers"
	"github.com/riverqueue/river"
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
	require.NoError(t, w.Work(ctx, &river.Job[servers.UsageArgs]{}))
	o.Bus.(*ext.Bus).Close()

	usage := slices.DeleteFunc(rec.all(), func(ev ext.Event) bool { return ev.Kind != ext.ServerUsage })
	require.Len(t, usage, 2)
	byID := map[int64]ext.Event{}
	for _, ev := range usage {
		require.Equal(t, e.Project, ev.ProjectID)
		require.False(t, ev.At.IsZero())
		byID[ev.ServerID] = ev
	}
	require.Equal(t, map[string]any{"server_type": "small", "status": "running", "cores": 2, "memory_mb": 2048, "disk_gb": 20},
		byID[a.ServerID].Data)
	require.Equal(t, "off", byID[b.ServerID].Data["status"])
}
