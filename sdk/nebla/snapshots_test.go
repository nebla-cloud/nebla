// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotsListGet(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/snapshots", 200, "getSnapshots")
	f.handle("GET", "/v1/snapshots/9", 200, "getSnapshot")
	ss, meta, err := c.Snapshots.List(ctx, SnapshotListOpts{ListOpts: ListOpts{PerPage: 5}, Name: "base", LabelSelector: "env=prod", Status: SnapshotStatusAvailable, Server: "web-1", Location: "lon1"})
	require.NoError(t, err)
	r, _ := f.last()
	q := r.URL.Query()
	assert.Equal(t, "5", q.Get("per_page"))
	assert.Equal(t, "base", q.Get("name"))
	assert.Equal(t, "env=prod", q.Get("label_selector"))
	assert.Equal(t, "available", q.Get("status"))
	assert.Equal(t, "web-1", q.Get("server"))
	assert.Equal(t, "lon1", q.Get("location"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, ss, 1)

	all, err := c.Snapshots.All(ctx, SnapshotListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)

	s, err := c.Snapshots.Get(ctx, 9)
	require.NoError(t, err)
	assert.Equal(t, int64(9), s.ID)
	assert.Equal(t, "golden", s.Description)
	assert.Equal(t, 40, s.Size)
	assert.Equal(t, SnapshotStatusAvailable, s.Status)
	assert.Equal(t, "lon1", s.Location.Name)
	require.NotNil(t, s.Server)
	assert.Equal(t, int64(7), *s.Server)
	assert.Equal(t, "web-1", s.ServerName)
	require.NotNil(t, s.Image)
	assert.Equal(t, "ubuntu-24.04", s.Image.Name)
	assert.Equal(t, "ubuntu", s.OSFlavor)
	assert.Equal(t, "24.04", s.OSVersion)
	require.NotNil(t, s.RestoreKeepsIdentity)
	assert.True(t, *s.RestoreKeepsIdentity)
	assert.Equal(t, map[string]string{"env": "prod"}, s.Labels)

	s, err = c.Snapshots.GetByName(ctx, "base")
	require.NoError(t, err)
	assert.Equal(t, int64(9), s.ID)
	_, err = c.Snapshots.GetByName(ctx, "BASE")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSnapshotCreate(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/snapshots", 201, "postSnapshots")
	res, err := c.Snapshots.Create(context.Background(), SnapshotCreateOpts{Server: "web-1", Name: "base", Description: "golden", Labels: map[string]string{"a": "b"}, ForceStop: true})
	require.NoError(t, err)
	assert.Equal(t, int64(9), res.Snapshot.ID)
	assert.Equal(t, "create_snapshot", res.Action.Command)
	assert.JSONEq(t, `{"server":"web-1","name":"base","description":"golden","labels":{"a":"b"},"force_stop":true}`, bodyString(f))

	_, err = c.Snapshots.Create(context.Background(), SnapshotCreateOpts{Server: "web-1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"server":"web-1"}`, bodyString(f))
}

func TestSnapshotUpdateDelete(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("PUT", "/v1/snapshots/9", 200, "putSnapshot")
	f.handle("DELETE", "/v1/snapshots/9", 200, "deleteSnapshot")
	s, err := c.Snapshots.Update(ctx, 9, SnapshotUpdateOpts{Name: Ptr("n"), Description: Ptr(""), Labels: Ptr(map[string]string{"x": "y"})})
	require.NoError(t, err)
	assert.Equal(t, int64(9), s.ID)
	assert.JSONEq(t, `{"name":"n","description":"","labels":{"x":"y"}}`, bodyString(f))

	a, err := c.Snapshots.Delete(ctx, 9)
	require.NoError(t, err)
	assert.Equal(t, "delete_snapshot", a.Command)
}

func TestSnapshotActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/snapshots/9/actions", 200, "getSnapshotActions")
	as, _, err := c.Snapshots.Actions(ctx, 9, ActionListOpts{})
	require.NoError(t, err)
	require.Len(t, as, 1)
	all, err := c.Snapshots.AllActions(ctx, 9, ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
