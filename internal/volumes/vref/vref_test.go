package vref_test

import (
	"testing"

	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

func TestVrefNames(t *testing.T) {
	require.Equal(t, "vm-999999999-nb-vol-12", vref.Name(12))
	require.Equal(t, "nb-vol-12", vref.Serial(12))
	require.Equal(t, "local-lvm:vm-999999999-nb-vol-12,serial=nb-vol-12,backup=0", vref.ConfigValue("local-lvm", 12))
	require.Equal(t, "/dev/disk/by-id/virtio-nb-vol-12", vref.LinuxDevice(12))
	require.Equal(t, 14, vref.MaxPerServer)
	k := vref.Keys()
	require.Len(t, k, 15)
	require.Equal(t, "virtio1", k[0])
	require.Equal(t, "virtio15", k[14])
}

func TestVrefParse(t *testing.T) {
	id, ok := vref.Parse("local-lvm:vm-999999999-nb-vol-12,serial=nb-vol-12,backup=0")
	require.True(t, ok)
	require.EqualValues(t, 12, id)
	id, ok = vref.Parse("vm-999999999-nb-vol-7")
	require.True(t, ok)
	require.EqualValues(t, 7, id)
	for _, bad := range []string{
		"vm-999999999-nb-vol-12x", "vm-100-disk-0", "vm-99999999-nb-vol-12", "local:vm-999999999-nb-vol-",
		"vm-999999999-nb-vol-012", "vm-999999999-nb-vol--1", "x-vm-999999999-nb-vol-12", "a:b:vm-999999999-nb-vol-12", "",
		"vm-999999999-nb-vol-99999999999999999999",
	} {
		_, ok := vref.Parse(bad)
		require.False(t, ok, bad)
	}
}
