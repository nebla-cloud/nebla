// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworksListGet(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/networks", 200, "getNetworks")
	f.handle("GET", "/v1/networks/4", 200, "getNetwork")
	ns, meta, err := c.Networks.List(ctx, NetworkListOpts{ListOpts: ListOpts{Page: 2}, Name: "internal", LabelSelector: "env=prod", Location: "lon1"})
	require.NoError(t, err)
	r, _ := f.last()
	q := r.URL.Query()
	assert.Equal(t, "2", q.Get("page"))
	assert.Equal(t, "internal", q.Get("name"))
	assert.Equal(t, "env=prod", q.Get("label_selector"))
	assert.Equal(t, "lon1", q.Get("location"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, ns, 1)

	all, err := c.Networks.All(ctx, NetworkListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
	r, _ = f.last()
	assert.Equal(t, "50", r.URL.Query().Get("per_page"))

	n, err := c.Networks.Get(ctx, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(4), n.ID)
	assert.Equal(t, "internal", n.Name)
	assert.Equal(t, "10.0.0.0/16", n.IPRange)
	assert.Equal(t, "lon1", n.Location.Name)
	assert.Equal(t, map[string]string{"env": "prod"}, n.Labels)
	assert.Equal(t, []NetworkServer{{Server: 7, Name: "web-1", IP: "10.0.0.2"}}, n.Servers)

	n, err = c.Networks.GetByName(ctx, "internal")
	require.NoError(t, err)
	assert.Equal(t, int64(4), n.ID)
	_, err = c.Networks.GetByName(ctx, "intern")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestNetworkCreate(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/networks", 201, "postNetworks")
	n, err := c.Networks.Create(ctx, NetworkCreateOpts{Name: "internal", Location: "lon1", IPRange: "10.0.0.0/16", Labels: map[string]string{"a": "b"}})
	require.NoError(t, err)
	assert.Equal(t, int64(4), n.ID)
	assert.JSONEq(t, `{"name":"internal","location":"lon1","ip_range":"10.0.0.0/16","labels":{"a":"b"}}`, bodyString(f))

	_, err = c.Networks.Create(ctx, NetworkCreateOpts{Name: "internal", Location: "lon1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"internal","location":"lon1"}`, bodyString(f))
}

func TestNetworkUpdateDelete(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("PUT", "/v1/networks/4", 200, "putNetwork")
	f.handle("DELETE", "/v1/networks/4", 204, "")
	f.handle("GET", "/v1/networks/4/actions", 200, "getNetworkActions")
	n, err := c.Networks.Update(ctx, 4, NetworkUpdateOpts{Name: Ptr("new")})
	require.NoError(t, err)
	assert.Equal(t, int64(4), n.ID)
	assert.JSONEq(t, `{"name":"new"}`, bodyString(f))
	_, err = c.Networks.Update(ctx, 4, NetworkUpdateOpts{Labels: Ptr(map[string]string{})})
	require.NoError(t, err)
	assert.JSONEq(t, `{"labels":{}}`, bodyString(f))

	require.NoError(t, c.Networks.Delete(ctx, 4))
	r, _ := f.last()
	assert.Equal(t, "DELETE", r.Method)

	as, meta, err := c.Networks.Actions(ctx, 4, ActionListOpts{Status: ActionStatusSuccess, Sort: "id:asc"})
	require.NoError(t, err)
	r, _ = f.last()
	assert.Equal(t, "success", r.URL.Query().Get("status"))
	assert.Equal(t, "id:asc", r.URL.Query().Get("sort"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, as, 1)
	all, err := c.Networks.AllActions(ctx, 4, ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
