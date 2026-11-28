// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimaryIPsListGet(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/primary_ips", 200, "getPrimaryIps")
	f.handle("GET", "/v1/primary_ips/6", 200, "getPrimaryIp")
	ips, meta, err := c.PrimaryIPs.List(ctx, PrimaryIPListOpts{ListOpts: ListOpts{Page: 2}, Name: "pip", LabelSelector: "env=prod", Server: "web-1", Location: "lon1", Type: IPTypeIPv6})
	require.NoError(t, err)
	r, _ := f.last()
	q := r.URL.Query()
	assert.Equal(t, "2", q.Get("page"))
	assert.Equal(t, "pip", q.Get("name"))
	assert.Equal(t, "env=prod", q.Get("label_selector"))
	assert.Equal(t, "web-1", q.Get("server"))
	assert.Equal(t, "lon1", q.Get("location"))
	assert.Equal(t, "ipv6", q.Get("type"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, ips, 1)

	all, err := c.PrimaryIPs.All(ctx, PrimaryIPListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
	r, _ = f.last()
	assert.Equal(t, "50", r.URL.Query().Get("per_page"))

	ip, err := c.PrimaryIPs.Get(ctx, 6)
	require.NoError(t, err)
	assert.Equal(t, int64(6), ip.ID)
	assert.Equal(t, "pip", ip.Name)
	assert.Equal(t, "main", ip.Description)
	assert.Equal(t, IPTypeIPv6, ip.Type)
	assert.Equal(t, "2001:db8::6", ip.IP)
	assert.Equal(t, "lon1", ip.Location.Name)
	require.NotNil(t, ip.Server)
	assert.Equal(t, PrimaryIPServer{ID: 7, Name: "web-1"}, *ip.Server)
	assert.True(t, ip.AutoDelete)
	assert.Equal(t, PrimaryIPStatusActive, ip.Status)
	assert.Equal(t, map[string]string{"env": "prod"}, ip.Labels)

	ip, err = c.PrimaryIPs.GetByName(ctx, "pip")
	require.NoError(t, err)
	assert.Equal(t, int64(6), ip.ID)
	_, err = c.PrimaryIPs.GetByName(ctx, "pi")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPrimaryIPCreate(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/primary_ips", 201, "postPrimaryIps")
	ip, err := c.PrimaryIPs.Create(ctx, PrimaryIPCreateOpts{Name: "pip", Description: "main", Labels: map[string]string{"a": "b"}, Type: IPTypeIPv6, Location: "lon1", AutoDelete: Ptr(false)})
	require.NoError(t, err)
	assert.Equal(t, int64(6), ip.ID)
	assert.JSONEq(t, `{"name":"pip","description":"main","labels":{"a":"b"},"type":"ipv6","location":"lon1","auto_delete":false}`, bodyString(f))

	_, err = c.PrimaryIPs.Create(ctx, PrimaryIPCreateOpts{Type: IPTypeIPv4, Location: "lon1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"ipv4","location":"lon1"}`, bodyString(f))
}

func TestPrimaryIPUpdateDelete(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("PUT", "/v1/primary_ips/6", 200, "putPrimaryIp")
	f.handle("DELETE", "/v1/primary_ips/6", 204, "")
	ip, err := c.PrimaryIPs.Update(ctx, 6, PrimaryIPUpdateOpts{Name: Ptr("new"), Description: Ptr("d"), Labels: Ptr(map[string]string{}), AutoDelete: Ptr(false)})
	require.NoError(t, err)
	assert.Equal(t, int64(6), ip.ID)
	assert.JSONEq(t, `{"name":"new","description":"d","labels":{},"auto_delete":false}`, bodyString(f))
	_, err = c.PrimaryIPs.Update(ctx, 6, PrimaryIPUpdateOpts{})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, bodyString(f))

	require.NoError(t, c.PrimaryIPs.Delete(ctx, 6))
	r, _ := f.last()
	assert.Equal(t, "DELETE", r.Method)
}

func TestPrimaryIPActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/primary_ips/6/actions", 200, "getPrimaryIpActions")
	f.handle("POST", "/v1/primary_ips/6/actions/assign", 201, "assignPrimaryIp")
	f.handle("POST", "/v1/primary_ips/6/actions/unassign", 201, "unassignPrimaryIp")
	as, meta, err := c.PrimaryIPs.Actions(ctx, 6, ActionListOpts{Sort: "id:asc"})
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.Equal(t, 1, meta.TotalEntries)
	all, err := c.PrimaryIPs.AllActions(ctx, 6, ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)

	a, warnings, err := c.PrimaryIPs.Assign(ctx, 6, PrimaryIPAssignOpts{Server: "web-1"})
	require.NoError(t, err)
	assert.Equal(t, "assign_primary_ip", a.Command)
	assert.Equal(t, []string{"takes effect when the server starts"}, warnings)
	assert.JSONEq(t, `{"server":"web-1"}`, bodyString(f))
	a, warnings, err = c.PrimaryIPs.Unassign(ctx, 6)
	require.NoError(t, err)
	assert.Equal(t, "unassign_primary_ip", a.Command)
	assert.Empty(t, warnings)
	_, body := f.last()
	assert.Empty(t, body)
}
