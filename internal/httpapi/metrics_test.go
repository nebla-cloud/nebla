package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type metricsBody struct {
	Metrics struct {
		Start      string                  `json:"start"`
		End        string                  `json:"end"`
		Step       int                     `json:"step"`
		TimeSeries map[string][][2]float64 `json:"time_series"`
	} `json:"metrics"`
}

func metricsPath(id int64, q string) string {
	return "/v1/servers/" + strconv.FormatInt(id, 10) + "/metrics?" + q
}

// TestMetricsMapsRRD checks the fake's RRD points map to percent cpu and bytes/s series.
func TestMetricsMapsRRD(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	s := e.RunningServer("web")
	// Read-only callers may read metrics.
	ro := e.App.TokenClient(e.Admin.CreateToken(e.Project, "read"))
	res := ro.Get(metricsPath(s.ServerID, "type=cpu,disk,network&timeframe=hour"))
	body := e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	var m metricsBody
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	require.Equal(t, "2023-11-14T22:13:20Z", m.Metrics.Start)
	require.Equal(t, "2023-11-14T22:17:20Z", m.Metrics.End)
	require.Equal(t, 60, m.Metrics.Step)
	ts := m.Metrics.TimeSeries
	require.Len(t, ts, 5)
	for i := range 5 {
		at := float64(1_700_000_000 + i*60)
		for k, want := range map[string]float64{
			"cpu": 10 * float64(i+1), "disk.read": 0, "disk.write": 2048 * float64(i),
			"network.in": 1000 * float64(i), "network.out": 500 * float64(i),
		} {
			require.Len(t, ts[k], 5, k)
			require.Equal(t, at, ts[k][i][0], k)
			require.InDelta(t, want, ts[k][i][1], 1e-9, k)
		}
	}

	res = e.C.Get(metricsPath(s.ServerID, "type=cpu&timeframe=day"))
	body = e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	m = metricsBody{}
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	require.Len(t, m.Metrics.TimeSeries, 1)
	require.Contains(t, m.Metrics.TimeSeries, "cpu")

	res = e.C.Get(metricsPath(s.ServerID, "type=network&type=disk&timeframe=week"))
	body = e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	m = metricsBody{}
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	require.Len(t, m.Metrics.TimeSeries, 4)
}

func TestMetricsRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	s := e.RunningServer("web")
	for _, q := range []string{"type=memory&timeframe=hour", "type=cpu&timeframe=year", "timeframe=hour", "type=cpu", "type=,&timeframe=hour"} {
		res := e.C.Get(metricsPath(s.ServerID, q))
		body := e.App.Body(res)
		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode, "%s: %s", q, body)
		require.Contains(t, body, `"invalid_input"`, q)
	}

	fresh := e.CreateServer("fresh", nil)
	require.NoError(t, actions.Succeed(ctx, e.App.Ent, fresh.ActionID))
	res := e.C.Get(metricsPath(fresh.ServerID, "type=cpu&timeframe=hour"))
	body := e.App.Body(res)
	require.Equal(t, http.StatusConflict, res.StatusCode, body)
	require.Contains(t, body, `"server_not_ready"`)

	res = e.C.Get(metricsPath(999999, "type=cpu&timeframe=hour"))
	require.Equal(t, http.StatusNotFound, res.StatusCode, e.App.Body(res))
}

// TestMetricsGapsAreNull checks Proxmox nulls and NaN come out as JSON null, not 0.
func TestMetricsGapsAreNull(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	s := e.RunningServer("web")
	e.App.PVE.SetRRD([]map[string]any{
		{"time": 1_700_000_000, "cpu": 0.25, "netin": 10, "netout": 0},
		{"time": 1_700_000_060, "cpu": nil, "netin": "NaN"},
	})
	res := e.C.Get(metricsPath(s.ServerID, "type=cpu,network&timeframe=hour"))
	body := e.App.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	var m struct {
		Metrics struct {
			TimeSeries map[string][][]*float64 `json:"time_series"`
		} `json:"metrics"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	ts := m.Metrics.TimeSeries
	require.InDelta(t, 25, *ts["cpu"][0][1], 1e-9)
	require.Equal(t, 0.0, *ts["network.out"][0][1])
	for _, k := range []string{"cpu", "network.in", "network.out"} {
		require.Equal(t, 1_700_000_060.0, *ts[k][1][0], k)
		require.Nil(t, ts[k][1][1], k)
	}
	require.Contains(t, body, `[1700000060,null]`)
}

// TestMetricsRequiresOwnedVM checks a reused vmid holding a foreign or untagged VM is never read.
func TestMetricsRequiresOwnedVM(t *testing.T) {
	e := testutil.NewServerEnv(testutil.NewApp(t, testutil.QueueOnlyJobs(t)))
	s := e.RunningServer("web")
	row, err := e.App.Ent.Server.Get(context.Background(), s.ServerID)
	require.NoError(t, err)
	for _, tags := range []string{"nebla;nb-srv-99999", ""} {
		e.App.PVE.SetVMConfig(*row.Vmid, "tags", tags)
		res := e.C.Get(metricsPath(s.ServerID, "type=cpu&timeframe=hour"))
		body := e.App.Body(res)
		require.Equal(t, http.StatusConflict, res.StatusCode, "tags %q: %s", tags, body)
		require.Contains(t, body, `"server_missing"`)
	}
}
