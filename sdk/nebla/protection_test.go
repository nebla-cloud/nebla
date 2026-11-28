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

func TestProtectionDecoded(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/servers/7", 200, "getServer")
	f.handle("GET", "/v1/volumes/3", 200, "getVolume")
	f.handle("GET", "/v1/floating_ips/5", 200, "getFloatingIp")
	f.handle("GET", "/v1/primary_ips/6", 200, "getPrimaryIp")
	f.handle("GET", "/v1/snapshots/9", 200, "getSnapshot")

	s, err := c.Servers.Get(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, ServerProtection{Delete: true}, s.Protection)
	v, err := c.Volumes.Get(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, Protection{Delete: true}, v.Protection)
	fip, err := c.FloatingIPs.Get(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, Protection{Delete: true}, fip.Protection)
	pip, err := c.PrimaryIPs.Get(ctx, 6)
	require.NoError(t, err)
	assert.Equal(t, Protection{Delete: true}, pip.Protection)
	snap, err := c.Snapshots.Get(ctx, 9)
	require.NoError(t, err)
	assert.Equal(t, Protection{Delete: true}, snap.Protection)
}

func TestProtectionSent(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/servers", 201, "postServers")
	f.handle("PUT", "/v1/servers/7", 200, "putServer")
	f.handle("POST", "/v1/volumes", 201, "postVolumes")
	f.handle("PUT", "/v1/volumes/3", 200, "putVolume")
	f.handle("POST", "/v1/floating_ips", 201, "postFloatingIps")
	f.handle("PUT", "/v1/floating_ips/5", 200, "putFloatingIp")
	f.handle("POST", "/v1/primary_ips", 201, "postPrimaryIps")
	f.handle("PUT", "/v1/primary_ips/6", 200, "putPrimaryIp")
	f.handle("POST", "/v1/snapshots", 201, "postSnapshots")
	f.handle("PUT", "/v1/snapshots/9", 200, "putSnapshot")
	on := &ProtectionOpts{Delete: Ptr(true)}

	_, err := c.Servers.Create(ctx, ServerCreateOpts{Name: "web-1", ServerType: "small", Image: "ubuntu-24.04", Protection: &ServerProtectionOpts{Delete: Ptr(true), Rebuild: Ptr(true)}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"web-1","server_type":"small","image":"ubuntu-24.04","protection":{"delete":true,"rebuild":true}}`, bodyString(f))
	_, err = c.Servers.Update(ctx, 7, ServerUpdateOpts{Protection: &ServerProtectionOpts{Rebuild: Ptr(false)}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"protection":{"rebuild":false}}`, bodyString(f))

	_, err = c.Volumes.Create(ctx, VolumeCreateOpts{Name: "data", Size: 10, Location: "lon1", Protection: on})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"data","size":10,"location":"lon1","protection":{"delete":true}}`, bodyString(f))
	_, err = c.Volumes.Update(ctx, 3, VolumeUpdateOpts{Protection: &ProtectionOpts{Delete: Ptr(false)}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"protection":{"delete":false}}`, bodyString(f))

	_, err = c.FloatingIPs.Create(ctx, FloatingIPCreateOpts{Type: IPTypeIPv4, Location: "lon1", Protection: on})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"ipv4","location":"lon1","protection":{"delete":true}}`, bodyString(f))
	_, err = c.FloatingIPs.Update(ctx, 5, FloatingIPUpdateOpts{Protection: on})
	require.NoError(t, err)
	assert.JSONEq(t, `{"protection":{"delete":true}}`, bodyString(f))

	_, err = c.PrimaryIPs.Create(ctx, PrimaryIPCreateOpts{Type: IPTypeIPv6, Location: "lon1", Protection: on})
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"ipv6","location":"lon1","protection":{"delete":true}}`, bodyString(f))
	_, err = c.PrimaryIPs.Update(ctx, 6, PrimaryIPUpdateOpts{Protection: on})
	require.NoError(t, err)
	assert.JSONEq(t, `{"protection":{"delete":true}}`, bodyString(f))

	_, err = c.Snapshots.Create(ctx, SnapshotCreateOpts{Server: "web-1", Protection: on})
	require.NoError(t, err)
	assert.JSONEq(t, `{"server":"web-1","protection":{"delete":true}}`, bodyString(f))
	_, err = c.Snapshots.Update(ctx, 9, SnapshotUpdateOpts{Protection: on})
	require.NoError(t, err)
	assert.JSONEq(t, `{"protection":{"delete":true}}`, bodyString(f))
}

func TestProtectedError(t *testing.T) {
	c, f := newFake(t)
	f.handleFunc("DELETE", "/v1/volumes/3", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"protected","message":"Delete protection is on"}}`))
	})
	_, err := c.Volumes.Delete(context.Background(), 3)
	assert.ErrorIs(t, err, ErrProtected)
}
