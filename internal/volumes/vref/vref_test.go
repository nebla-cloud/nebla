// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package vref_test

import (
	"testing"

	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

func TestVrefNames(t *testing.T) {
	require.Equal(t, "vm-999999999-nb-vol-12", vref.Name(12))
	require.Equal(t, "vm-999999999-nb-vol-12", vref.Filename(12, false))
	require.Equal(t, "vm-999999999-nb-vol-12.raw", vref.Filename(12, true))
	require.Equal(t, "local-lvm:vm-999999999-nb-vol-12", vref.Volid("local-lvm", 12, false))
	require.Equal(t, "local:999999999/vm-999999999-nb-vol-12.raw", vref.Volid("local", 12, true))
	require.Equal(t, "nb-vol-12", vref.Serial(12))
	require.Equal(t, "local-lvm:vm-999999999-nb-vol-12,serial=nb-vol-12,backup=0",
		vref.ConfigValue("local-lvm:vm-999999999-nb-vol-12", 12))
	require.Equal(t, "nfs:999999999/vm-999999999-nb-vol-12.raw,serial=nb-vol-12,backup=0",
		vref.ConfigValue(vref.Volid("nfs", 12, true), 12))
	require.Equal(t, "/dev/disk/by-id/virtio-nb-vol-12", vref.LinuxDevice(12))
	require.Equal(t, 14, vref.MaxPerServer)
	k := vref.Keys()
	require.Len(t, k, 15)
	require.Equal(t, "virtio1", k[0])
	require.Equal(t, "virtio15", k[14])
}

func TestVrefIsFileStorage(t *testing.T) {
	for _, typ := range []string{"dir", "nfs", "cifs", "glusterfs", "btrfs", "cephfs"} {
		require.True(t, vref.IsFileStorage(typ), typ)
	}
	for _, typ := range []string{"lvmthin", "lvm", "zfspool", "rbd", "iscsi", "iscsidirect", "zfs", ""} {
		require.False(t, vref.IsFileStorage(typ), typ)
	}
}

func TestVrefParse(t *testing.T) {
	for in, want := range map[string]int64{
		"local-lvm:vm-999999999-nb-vol-12,serial=nb-vol-12,backup=0": 12,
		"vm-999999999-nb-vol-7":                                            7,
		"local-lvm:vm-999999999-nb-vol-7":                                  7,
		"local:999999999/vm-999999999-nb-vol-7.raw":                        7,
		"999999999/vm-999999999-nb-vol-7.raw":                              7,
		"nfs:999999999/vm-999999999-nb-vol-7.raw,serial=nb-vol-7,backup=0": 7,
		"nfs:999999999/vm-999999999-nb-vol-7.raw,size=20G":                 7,
	} {
		id, ok := vref.Parse(in)
		require.True(t, ok, in)
		require.Equal(t, want, id, in)
	}
	for _, bad := range []string{
		"vm-999999999-nb-vol-12x", "vm-100-disk-0", "vm-99999999-nb-vol-12", "local:vm-999999999-nb-vol-",
		"vm-999999999-nb-vol-012", "vm-999999999-nb-vol--1", "x-vm-999999999-nb-vol-12", "a:b:vm-999999999-nb-vol-12", "",
		"vm-999999999-nb-vol-99999999999999999999",
		"local:999999999/vm-999999999-nb-vol-7.qcow2", "local:999999999/vm-999999999-nb-vol-7.vmdk",
		"local:999999999/vm-999999999-nb-vol-7", "local:vm-999999999-nb-vol-7.raw",
		"local:100/vm-999999999-nb-vol-7.raw", "local:0999999999/vm-999999999-nb-vol-7.raw",
		"local:999999999/vm-999999999-nb-vol-07.raw", "local:999999999//vm-999999999-nb-vol-7.raw",
		"local:images/999999999/vm-999999999-nb-vol-7.raw", "local:999999999/vm-999999999-nb-vol-.raw",
		"local:999999999/vm-999999999-nb-vol-7.raw.raw", "local:999999999/x/vm-999999999-nb-vol-7.raw",
	} {
		_, ok := vref.Parse(bad)
		require.False(t, ok, bad)
	}
}
