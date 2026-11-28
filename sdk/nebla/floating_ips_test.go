// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFloatingIPsListGet(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/floating_ips", 200, "getFloatingIps")
	f.handle("GET", "/v1/floating_ips/5", 200, "getFloatingIp")
	ips, meta, err := c.FloatingIPs.List(ctx, FloatingIPListOpts{ListOpts: ListOpts{Page: 2}, Name: "vip", LabelSelector: "env=prod", Server: "web-1", Location: "lon1"})
	require.NoError(t, err)
	r, _ := f.last()
	q := r.URL.Query()
	assert.Equal(t, "2", q.Get("page"))
	assert.Equal(t, "vip", q.Get("name"))
	assert.Equal(t, "env=prod", q.Get("label_selector"))
	assert.Equal(t, "web-1", q.Get("server"))
	assert.Equal(t, "lon1", q.Get("location"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, ips, 1)

	all, err := c.FloatingIPs.All(ctx, FloatingIPListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
	r, _ = f.last()
	assert.Equal(t, "50", r.URL.Query().Get("per_page"))

	ip, err := c.FloatingIPs.Get(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(5), ip.ID)
	assert.Equal(t, "vip", ip.Name)
	assert.Equal(t, "front", ip.Description)
	assert.Equal(t, IPTypeIPv4, ip.Type)
	assert.Equal(t, "203.0.113.5/32", ip.IP)
	assert.Equal(t, "lon1", ip.Location.Name)
	require.NotNil(t, ip.Server)
	assert.Equal(t, int64(7), *ip.Server)
	assert.Equal(t, FloatingIPStatusActive, ip.Status)
	assert.Equal(t, map[string]string{"env": "prod"}, ip.Labels)
	require.NotNil(t, ip.GuestConfig)
	assert.Equal(t, "web-1", *ip.GuestConfig.Server)
	assert.Equal(t, "echo persist", *ip.GuestConfig.Persist)
	assert.Nil(t, ip.GuestConfig.PreviousServer)

	ip, err = c.FloatingIPs.GetByName(ctx, "vip")
	require.NoError(t, err)
	assert.Equal(t, int64(5), ip.ID)
	_, err = c.FloatingIPs.GetByName(ctx, "vi")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFloatingIPCreate(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/floating_ips", 201, "postFloatingIps")
	res, err := c.FloatingIPs.Create(ctx, FloatingIPCreateOpts{Name: "vip", Description: "front", Labels: map[string]string{"a": "b"}, Type: IPTypeIPv4, Server: "web-1"})
	require.NoError(t, err)
	assert.Equal(t, int64(5), res.FloatingIP.ID)
	require.NotNil(t, res.Action)
	assert.Equal(t, "assign_floating_ip", res.Action.Command)
	assert.JSONEq(t, `{"name":"vip","description":"front","labels":{"a":"b"},"type":"ipv4","server":"web-1"}`, bodyString(f))

	f.handleFunc("POST", "/v1/floating_ips", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"floating_ip":{"id":5,"name":"vip","description":"","labels":{},"type":"ipv6","ip":"2001:db8::5/128","location":{"id":1,"name":"lon1","description":"F"},"server":null,"status":"active","guest_config":null,"created":"2026-01-01T00:00:00Z"},"action":null}`))
	})
	res, err = c.FloatingIPs.Create(ctx, FloatingIPCreateOpts{Type: IPTypeIPv6, Location: "lon1"})
	require.NoError(t, err)
	assert.Nil(t, res.Action)
	assert.Nil(t, res.FloatingIP.Server)
	assert.Nil(t, res.FloatingIP.GuestConfig)
	assert.JSONEq(t, `{"type":"ipv6","location":"lon1"}`, bodyString(f))
}

func TestFloatingIPUpdateDelete(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("PUT", "/v1/floating_ips/5", 200, "putFloatingIp")
	f.handle("DELETE", "/v1/floating_ips/5", 200, "deleteFloatingIp")
	ip, err := c.FloatingIPs.Update(ctx, 5, FloatingIPUpdateOpts{Name: Ptr("new"), Description: Ptr(""), Labels: Ptr(map[string]string{})})
	require.NoError(t, err)
	assert.Equal(t, int64(5), ip.ID)
	assert.JSONEq(t, `{"name":"new","description":"","labels":{}}`, bodyString(f))
	_, err = c.FloatingIPs.Update(ctx, 5, FloatingIPUpdateOpts{})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, bodyString(f))

	a, err := c.FloatingIPs.Delete(ctx, 5)
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "delete_floating_ip", a.Command)
	r, _ := f.last()
	assert.Equal(t, "DELETE", r.Method)
}

func TestFloatingIPActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/floating_ips/5/actions", 200, "getFloatingIpActions")
	f.handle("POST", "/v1/floating_ips/5/actions/assign", 201, "assignFloatingIp")
	f.handle("POST", "/v1/floating_ips/5/actions/unassign", 201, "unassignFloatingIp")
	as, meta, err := c.FloatingIPs.Actions(ctx, 5, ActionListOpts{Sort: "id:asc"})
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.Equal(t, 1, meta.TotalEntries)
	r, _ := f.last()
	assert.Equal(t, "id:asc", r.URL.Query().Get("sort"))
	all, err := c.FloatingIPs.AllActions(ctx, 5, ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)

	a, err := c.FloatingIPs.Assign(ctx, 5, FloatingIPAssignOpts{Server: "web-1"})
	require.NoError(t, err)
	assert.Equal(t, "assign_floating_ip", a.Command)
	assert.JSONEq(t, `{"server":"web-1"}`, bodyString(f))
	a, err = c.FloatingIPs.Unassign(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, "unassign_floating_ip", a.Command)
	r, body := f.last()
	assert.Equal(t, "POST", r.Method)
	assert.Empty(t, body)
}
