// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSimpleActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	cases := []struct {
		path, fixture string
		call          func(context.Context, int64) (*Action, error)
	}{
		{"poweron", "poweronServer", c.Servers.PowerOn},
		{"poweroff", "poweroffServer", c.Servers.PowerOff},
		{"shutdown", "shutdownServer", c.Servers.Shutdown},
		{"reboot", "rebootServer", c.Servers.Reboot},
		{"reset", "resetServer", c.Servers.Reset},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			f.handle("POST", "/v1/servers/7/actions/"+tc.path, 201, tc.fixture)
			a, err := tc.call(ctx, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(42), a.ID)
			r, b := f.last()
			assert.Equal(t, "POST", r.Method)
			assert.Empty(t, b)
		})
	}
}

func TestServerRebuild(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/servers/7/actions/rebuild", 201, "rebuildServer")
	a, err := c.Servers.Rebuild(context.Background(), 7, ServerRebuildOpts{Image: "ubuntu-24.04"})
	require.NoError(t, err)
	assert.Equal(t, int64(42), a.ID)
	_, b := f.last()
	assert.JSONEq(t, `{"image":"ubuntu-24.04"}`, string(b))
	_, err = c.Servers.Rebuild(context.Background(), 7, ServerRebuildOpts{Snapshot: "snap"})
	require.NoError(t, err)
	_, b = f.last()
	assert.JSONEq(t, `{"snapshot":"snap"}`, string(b))
}

func TestServerChangeType(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/servers/7/actions/change_type", 201, "changeServerType")
	_, err := c.Servers.ChangeType(context.Background(), 7, ServerChangeTypeOpts{ServerType: "medium"})
	require.NoError(t, err)
	_, b := f.last()
	assert.JSONEq(t, `{"server_type":"medium","upgrade_disk":false}`, string(b))
}

func TestServerRestoreBackup(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/servers/7/actions/restore_backup", 201, "restoreServerBackup")
	a, err := c.Servers.RestoreBackup(context.Background(), 7, 8)
	require.NoError(t, err)
	assert.Equal(t, int64(42), a.ID)
	_, b := f.last()
	assert.JSONEq(t, `{"backup":8}`, string(b))
}

func TestServerNetworkActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/servers/7/actions/attach_to_network", 201, "attachServerToNetwork")
	f.handle("POST", "/v1/servers/7/actions/detach_from_network", 201, "detachServerFromNetwork")
	a, gc, err := c.Servers.AttachToNetwork(ctx, 7, ServerAttachNetworkOpts{Network: "internal", IP: "10.0.0.9"})
	require.NoError(t, err)
	assert.Equal(t, int64(42), a.ID)
	require.NotNil(t, gc)
	assert.Equal(t, "run now", *gc.Note)
	assert.Nil(t, gc.Remove)
	_, b := f.last()
	assert.JSONEq(t, `{"network":"internal","ip":"10.0.0.9"}`, string(b))

	_, _, err = c.Servers.AttachToNetwork(ctx, 7, ServerAttachNetworkOpts{Network: "internal"})
	require.NoError(t, err)
	_, b = f.last()
	assert.JSONEq(t, `{"network":"internal"}`, string(b))

	a, gc, err = c.Servers.DetachFromNetwork(ctx, 7, "internal")
	require.NoError(t, err)
	assert.Equal(t, int64(42), a.ID)
	assert.Nil(t, gc)
	_, b = f.last()
	assert.JSONEq(t, `{"network":"internal"}`, string(b))
}

func TestServerRequestConsole(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/servers/7/actions/request_console", 201, "requestServerConsole")
	g, err := c.Servers.RequestConsole(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, ConsoleGrant{WSSURL: "wss://panel.example/v1/console/tok", Password: "vncpass"}, *g)
}

func TestServerMetrics(t *testing.T) {
	c, f := newFake(t)
	f.handle("GET", "/v1/servers/7/metrics", 200, "getServerMetrics")
	m, err := c.Servers.Metrics(context.Background(), 7, ServerMetricsOpts{Types: []string{"cpu", "network"}, Timeframe: "hour"})
	require.NoError(t, err)
	r, _ := f.last()
	assert.Equal(t, []string{"cpu", "network"}, r.URL.Query()["type"])
	assert.Equal(t, "hour", r.URL.Query().Get("timeframe"))
	assert.Equal(t, 60, m.Step)
	assert.Equal(t, 2026, m.Start.Year())
	assert.Equal(t, 4, m.End.Hour())
	cpu := m.TimeSeries["cpu"]
	require.Len(t, cpu, 2)
	assert.Equal(t, int64(1767322800), cpu[0].Time.Unix())
	require.NotNil(t, cpu[0].Value)
	assert.Equal(t, 12.5, *cpu[0].Value)
	assert.Nil(t, cpu[1].Value)
	assert.Len(t, m.TimeSeries["network.in"], 1)
}
