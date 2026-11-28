// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jsonBody(t *testing.T, f *fake) map[string]any {
	t.Helper()
	_, b := f.last()
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m), string(b))
	return m
}

func TestServersListGet(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/servers", 200, "getServers")
	f.handle("GET", "/v1/servers/7", 200, "getServer")
	ss, meta, err := c.Servers.List(ctx, ServerListOpts{ListOpts: ListOpts{Page: 2}, Name: "web-1", LabelSelector: "env=prod", Status: ServerStatusRunning})
	require.NoError(t, err)
	r, _ := f.last()
	q := r.URL.Query()
	assert.Equal(t, "2", q.Get("page"))
	assert.Equal(t, "web-1", q.Get("name"))
	assert.Equal(t, "env=prod", q.Get("label_selector"))
	assert.Equal(t, "running", q.Get("status"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, ss, 1)

	all, err := c.Servers.All(ctx, ServerListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
	r, _ = f.last()
	assert.Equal(t, "50", r.URL.Query().Get("per_page"))

	s, err := c.Servers.Get(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(7), s.ID)
	assert.Equal(t, ServerStatusRunning, s.Status)
	assert.Equal(t, "fsn1", s.Location.Name)
	assert.Equal(t, "cx22", s.ServerType.Name)
	require.NotNil(t, s.Image)
	assert.Equal(t, "ubuntu-24.04", s.Image.Name)
	require.NotNil(t, s.Snapshot)
	assert.Equal(t, "base", s.Snapshot.Name)
	require.NotNil(t, s.PublicNet.IPv4)
	assert.Equal(t, "203.0.113.5", s.PublicNet.IPv4.IP)
	assert.Nil(t, s.PublicNet.IPv6)
	require.Len(t, s.PublicNet.FloatingIPs, 1)
	assert.Equal(t, "203.0.113.9", s.PublicNet.FloatingIPs[0].IP)
	require.Len(t, s.PrivateNet, 1)
	assert.Equal(t, "10.0.0.2", s.PrivateNet[0].IP)
	require.NotNil(t, s.PrivateNet[0].GuestConfig)
	assert.Equal(t, "ip link add", *s.PrivateNet[0].GuestConfig.Apply)
	assert.Nil(t, s.PrivateNet[0].GuestConfig.Persist)
	assert.Equal(t, map[string]string{"env": "prod"}, s.Labels)
	assert.Equal(t, []ServerFirewall{{ID: 2, Name: "web", Via: "direct"}}, s.Firewalls)
	assert.Equal(t, []int64{3}, s.Volumes)
	assert.True(t, s.Backups.Enabled)
	assert.Equal(t, 2, s.Backups.Count)
	require.NotNil(t, s.Backups.LastBackupAt)
	assert.Nil(t, s.Backups.NextBackupAt)
	assert.Equal(t, 40, s.DiskGB)

	s, err = c.Servers.GetByName(ctx, "web-1")
	require.NoError(t, err)
	assert.Equal(t, int64(7), s.ID)
	_, err = c.Servers.GetByName(ctx, "web")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestServerCreate(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/servers", 201, "postServers")
	res, err := c.Servers.Create(context.Background(), ServerCreateOpts{
		Name: "web-1", ServerType: "cx22", Image: "ubuntu-24.04", Location: "fsn1",
		SSHKeys: []string{"k"}, UserData: "#cloud-config", Labels: map[string]string{"a": "b"},
		StartAfterCreate: Ptr(false), Firewalls: []int64{2}, Backups: &BackupSettings{Enabled: true, Keep: 7},
		Networks: []ServerNetworkOpts{{Network: "internal", IP: "10.0.0.5"}, {Network: "other"}},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), res.Server.ID)
	assert.Equal(t, "create_server", res.Action.Command)
	require.Len(t, res.NextActions, 1)
	assert.Equal(t, "start_server", res.NextActions[0].Command)
	m := jsonBody(t, f)
	assert.Equal(t, map[string]any{
		"name": "web-1", "server_type": "cx22", "image": "ubuntu-24.04", "location": "fsn1",
		"ssh_keys": []any{"k"}, "user_data": "#cloud-config", "labels": map[string]any{"a": "b"},
		"start_after_create": false, "firewalls": []any{map[string]any{"firewall": float64(2)}},
		"backups":  map[string]any{"enabled": true, "keep": float64(7)},
		"networks": []any{map[string]any{"network": "internal", "ip": "10.0.0.5"}, map[string]any{"network": "other"}},
	}, m)
}

func TestServerCreatePublicNet(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/servers", 201, "postServers")
	cases := []struct {
		name     string
		v4, v6   *PublicIPChoice
		want     any
		hasField bool
	}{
		{"new", NewPublicIP(), nil, map[string]any{"ipv4": true}, true},
		{"none", NoPublicIP(), nil, map[string]any{"ipv4": false}, true},
		{"primary", UsePrimaryIP("ip-1"), NoPublicIP(), map[string]any{"ipv4": "ip-1", "ipv6": false}, true},
		{"v6 only", nil, NewPublicIP(), map[string]any{"ipv6": true}, true},
		{"omitted", nil, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Servers.Create(context.Background(), ServerCreateOpts{Name: "a", ServerType: "cx22", Image: "i", Location: "l", PublicIPv4: tc.v4, PublicIPv6: tc.v6})
			require.NoError(t, err)
			m := jsonBody(t, f)
			got, ok := m["public_net"]
			assert.Equal(t, tc.hasField, ok)
			if tc.hasField {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestServerUpdate(t *testing.T) {
	c, f := newFake(t)
	f.handle("PUT", "/v1/servers/7", 200, "putServer")
	ctx := context.Background()
	s, err := c.Servers.Update(ctx, 7, ServerUpdateOpts{Name: Ptr("new"), Labels: Ptr(map[string]string{"a": "b"}), Backups: &BackupSettings{Enabled: false, Keep: 3}})
	require.NoError(t, err)
	assert.Equal(t, int64(7), s.ID)
	assert.Equal(t, map[string]any{"name": "new", "labels": map[string]any{"a": "b"}, "backups": map[string]any{"enabled": false, "keep": float64(3)}}, jsonBody(t, f))
}

func TestServerUpdateClearsLabels(t *testing.T) {
	c, f := newFake(t)
	f.handle("PUT", "/v1/servers/7", 200, "putServer")
	ctx := context.Background()
	_, err := c.Servers.Update(ctx, 7, ServerUpdateOpts{Labels: Ptr(map[string]string{})})
	require.NoError(t, err)
	_, b := f.last()
	assert.JSONEq(t, `{"labels":{}}`, string(b))

	_, err = c.Servers.Update(ctx, 7, ServerUpdateOpts{})
	require.NoError(t, err)
	_, b = f.last()
	assert.JSONEq(t, `{}`, string(b))
}

func TestServerDeleteAndActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("DELETE", "/v1/servers/7", 200, "deleteServer")
	f.handle("GET", "/v1/servers/7/actions", 200, "getServerActions")
	a, err := c.Servers.Delete(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "delete_server", a.Command)
	as, meta, err := c.Servers.Actions(ctx, 7, ActionListOpts{Status: ActionStatusSuccess, Sort: "id:asc"})
	require.NoError(t, err)
	r, _ := f.last()
	assert.Equal(t, "success", r.URL.Query().Get("status"))
	assert.Equal(t, "id:asc", r.URL.Query().Get("sort"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, as, 1)
	all, err := c.Servers.AllActions(ctx, 7, ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
