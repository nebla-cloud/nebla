// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackups(t *testing.T) {
	c, f := newFake(t)
	ctx := context.Background()
	f.handle("GET", "/v1/servers/7/backups", 200, "getServerBackups")
	f.handle("GET", "/v1/servers/7/backups/8", 200, "getServerBackup")
	f.handle("DELETE", "/v1/servers/7/backups/8", 200, "deleteServerBackup")

	bs, err := c.Backups.List(ctx, 7, BackupListOpts{Status: BackupStatusAvailable, Kind: BackupKindManual})
	require.NoError(t, err)
	r, _ := f.last()
	assert.Equal(t, "available", r.URL.Query().Get("status"))
	assert.Equal(t, "manual", r.URL.Query().Get("kind"))
	require.Len(t, bs, 1)
	b := bs[0]
	assert.Equal(t, int64(8), b.ID)
	assert.Equal(t, int64(7), b.Server)
	assert.Equal(t, BackupKindManual, b.Kind)
	assert.Equal(t, int64(1048576), b.Size)
	assert.NotNil(t, b.Completed)
	assert.True(t, b.RestoreResetsIdentity)

	_, err = c.Backups.List(ctx, 7, BackupListOpts{})
	require.NoError(t, err)
	r, _ = f.last()
	assert.Empty(t, r.URL.RawQuery)

	g, err := c.Backups.Get(ctx, 7, 8)
	require.NoError(t, err)
	assert.Equal(t, BackupStatusAvailable, g.Status)

	a, err := c.Backups.Delete(ctx, 7, 8)
	require.NoError(t, err)
	assert.Equal(t, "delete_backup", a.Command)
}

func TestBackupCreate(t *testing.T) {
	for _, status := range []int{200, 202} {
		c, f := newFake(t)
		f.handle("POST", "/v1/servers/7/backups", status, "postServerBackups")
		b, err := c.Backups.Create(context.Background(), 7)
		require.NoError(t, err)
		assert.Equal(t, int64(8), b.ID)
	}
}
