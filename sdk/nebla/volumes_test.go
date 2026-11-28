// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumesListGet(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/volumes", 200, "getVolumes")
	f.handle("GET", "/v1/volumes/3", 200, "getVolume")
	vs, meta, err := c.Volumes.List(ctx, VolumeListOpts{ListOpts: ListOpts{Page: 2}, Name: "data", LabelSelector: "env=prod", Status: VolumeStatusAttached, Server: "web-1", Location: "fsn1"})
	require.NoError(t, err)
	r, _ := f.last()
	q := r.URL.Query()
	assert.Equal(t, "2", q.Get("page"))
	assert.Equal(t, "data", q.Get("name"))
	assert.Equal(t, "env=prod", q.Get("label_selector"))
	assert.Equal(t, "attached", q.Get("status"))
	assert.Equal(t, "web-1", q.Get("server"))
	assert.Equal(t, "fsn1", q.Get("location"))
	assert.Equal(t, 1, meta.TotalEntries)
	require.Len(t, vs, 1)

	all, err := c.Volumes.All(ctx, VolumeListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
	r, _ = f.last()
	assert.Equal(t, "50", r.URL.Query().Get("per_page"))

	v, err := c.Volumes.Get(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(3), v.ID)
	assert.Equal(t, "data", v.Name)
	assert.Equal(t, 50, v.Size)
	assert.Equal(t, VolumeStatusAttached, v.Status)
	assert.Equal(t, "fsn1", v.Location.Name)
	require.NotNil(t, v.Server)
	assert.Equal(t, int64(7), *v.Server)
	assert.Equal(t, "/dev/disk/by-id/virtio-nb-vol-3", v.LinuxDevice)
	assert.Equal(t, map[string]string{"env": "prod"}, v.Labels)

	v, err = c.Volumes.GetByName(ctx, "data")
	require.NoError(t, err)
	assert.Equal(t, int64(3), v.ID)
	_, err = c.Volumes.GetByName(ctx, "dat")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestVolumeCreate(t *testing.T) {
	c, f := newFake(t)
	f.handle("POST", "/v1/volumes", 201, "postVolumes")
	res, err := c.Volumes.Create(context.Background(), VolumeCreateOpts{Name: "data", Size: 50, Location: "fsn1", Server: "web-1", Labels: map[string]string{"a": "b"}})
	require.NoError(t, err)
	assert.Equal(t, int64(3), res.Volume.ID)
	assert.Equal(t, "create_volume", res.Action.Command)
	assert.JSONEq(t, `{"name":"data","size":50,"location":"fsn1","server":"web-1","labels":{"a":"b"}}`, bodyString(f))

	_, err = c.Volumes.Create(context.Background(), VolumeCreateOpts{Name: "data", Size: 10})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"data","size":10}`, bodyString(f))
}

func bodyString(f *fake) string {
	_, b := f.last()
	return string(b)
}

func TestVolumeUpdateDelete(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("PUT", "/v1/volumes/3", 200, "putVolume")
	f.handle("DELETE", "/v1/volumes/3", 200, "deleteVolume")
	v, err := c.Volumes.Update(ctx, 3, VolumeUpdateOpts{Name: Ptr("new"), Labels: Ptr(map[string]string{})})
	require.NoError(t, err)
	assert.Equal(t, int64(3), v.ID)
	assert.JSONEq(t, `{"name":"new","labels":{}}`, bodyString(f))
	_, err = c.Volumes.Update(ctx, 3, VolumeUpdateOpts{})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, bodyString(f))

	a, err := c.Volumes.Delete(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, "delete_volume", a.Command)
	r, _ := f.last()
	assert.Equal(t, "DELETE", r.Method)
}

func TestVolumeActions(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/volumes/3/actions", 200, "getVolumeActions")
	as, meta, err := c.Volumes.Actions(ctx, 3, ActionListOpts{Sort: "id:asc"})
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.Equal(t, 1, meta.TotalEntries)
	r, _ := f.last()
	assert.Equal(t, "id:asc", r.URL.Query().Get("sort"))
	all, err := c.Volumes.AllActions(ctx, 3, ActionListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestVolumeAttachDetachResize(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("POST", "/v1/volumes/3/actions/attach", 201, "attachVolume")
	f.handle("POST", "/v1/volumes/3/actions/detach", 201, "detachVolume")
	f.handle("POST", "/v1/volumes/3/actions/resize", 201, "resizeVolume")

	a, err := c.Volumes.Attach(ctx, 3, VolumeAttachOpts{Server: "web-1"})
	require.NoError(t, err)
	assert.Equal(t, "attach_volume", a.Command)
	assert.JSONEq(t, `{"server":"web-1"}`, bodyString(f))

	a, err = c.Volumes.Detach(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, "detach_volume", a.Command)
	assert.Empty(t, bodyString(f))

	a, err = c.Volumes.Resize(ctx, 3, 100)
	require.NoError(t, err)
	assert.Equal(t, "resize_volume", a.Command)
	assert.JSONEq(t, `{"size":100}`, bodyString(f))
}
