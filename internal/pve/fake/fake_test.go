package fake_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
)

func TestFakeAddToken(t *testing.T) {
	s := fake.New(t)
	c := s.Conn()
	c.TokenID, c.Secret = "nebla@pve!nebla-home-abc123", "s2"
	cl := pve.New(c)
	if _, err := cl.Version(context.Background()); err == nil {
		t.Fatal("want error before AddToken")
	}
	s.AddToken(c.TokenID, c.Secret)
	if _, err := cl.Version(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func volEnv(t *testing.T) (*fake.Server, pve.Client) {
	t.Helper()
	s := fake.New(t)
	s.AddNode("pve1", 4, 8192)
	s.AddStorage("pve1", "local-lvm", []string{"images"}, false)
	s.AddVM("pve1", 101, "web")
	return s, pve.New(s.Conn())
}

func TestFakeVolumeLifecycle(t *testing.T) {
	ctx := context.Background()
	s, c := volEnv(t)
	const name = "vm-999999999-nb-vol-7"
	volid, err := c.AllocVolume(ctx, "pve1", "local-lvm", 999999999, name, 20)
	if err != nil || volid != "local-lvm:"+name {
		t.Fatalf("volid = %q err=%v", volid, err)
	}
	if _, err := c.AllocVolume(ctx, "pve1", "local-lvm", 999999999, name, 20); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("dup err = %v", err)
	}
	if err := c.SetConfig(ctx, "pve1", 101, map[string]string{"virtio1": volid}); err != nil {
		t.Fatal(err)
	}
	if err := c.ResizeDisk(ctx, "pve1", 101, "virtio1", 30); err != nil {
		t.Fatal(err)
	}
	up, err := c.Destroy(ctx, "pve1", 101)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	items, err := c.StorageContent(ctx, "pve1", "local-lvm", 999999999)
	if err != nil || len(items) != 1 || items[0].Volid != volid || items[0].Size != 30<<30 || items[0].VMID != 999999999 {
		t.Fatalf("items = %+v err=%v", items, err)
	}
	if other, _ := c.StorageContent(ctx, "pve1", "local-lvm", 5); len(other) != 0 {
		t.Fatalf("filter leaked: %+v", other)
	}
	s.DenyDatastoreAllocate = true
	if err := c.DeleteVolume(ctx, "pve1", "local-lvm", volid); err == nil || !strings.Contains(err.Error(), "Datastore.Allocate") {
		t.Fatalf("deny err = %v", err)
	}
	s.DenyDatastoreAllocate = false
	if err := c.DeleteVolume(ctx, "pve1", "local-lvm", volid); err != nil {
		t.Fatal(err)
	}
	if all, _ := c.StorageContent(ctx, "pve1", "local-lvm", 0); len(all) != 0 {
		t.Fatalf("still there: %+v", all)
	}
}

func TestFakeDestroyKeepsOwnedByOthers(t *testing.T) {
	ctx := context.Background()
	_, c := volEnv(t)
	if _, err := c.AllocVolume(ctx, "pve1", "local-lvm", 101, "vm-101-disk-5", 1); err != nil {
		t.Fatal(err)
	}
	up, _ := c.Destroy(ctx, "pve1", 101)
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	if items, _ := c.StorageContent(ctx, "pve1", "local-lvm", 101); len(items) != 0 {
		t.Fatalf("own volume survived: %+v", items)
	}
}

func TestFakeDigest(t *testing.T) {
	ctx := context.Background()
	s, c := volEnv(t)
	cfg, err := c.Config(ctx, "pve1", 101)
	if err != nil || cfg["digest"] == "" {
		t.Fatalf("cfg = %v err=%v", cfg, err)
	}
	old := cfg["digest"]
	if err := c.SetConfigDigest(ctx, "pve1", 101, map[string]string{"cores": "2"}, old); err != nil {
		t.Fatal(err)
	}
	if err := c.SetConfigDigest(ctx, "pve1", 101, map[string]string{"cores": "3"}, old); !errors.Is(err, pve.ErrDigestMismatch) {
		t.Fatalf("stale err = %v", err)
	}
	cfg, _ = c.Config(ctx, "pve1", 101)
	if cfg["cores"] != "2" || cfg["digest"] == old {
		t.Fatalf("cfg = %v", cfg)
	}
	if err := c.SetConfigDigest(ctx, "pve1", 101, map[string]string{"cores": "3"}, cfg["digest"]); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.VM(101); v.Config["digest"] != "" {
		t.Fatal("digest stored as config")
	}
}

func TestFakeNoDiskHotplug(t *testing.T) {
	ctx := context.Background()
	s, c := volEnv(t)
	volid, _ := c.AllocVolume(ctx, "pve1", "local-lvm", 999999999, "vm-999999999-nb-vol-1", 5)
	if err := c.SetConfig(ctx, "pve1", 101, map[string]string{"hotplug": "network"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetConfig(ctx, "pve1", 101, map[string]string{"virtio1": volid}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.VM(101); v.Config["virtio1"] != "" {
		t.Fatalf("applied despite no disk hotplug: %v", v.Config)
	}
	pend, err := c.Pending(ctx, "pve1", 101)
	if err != nil || !slices.Contains(pend, pve.PendingItem{Key: "virtio1", Pending: volid}) {
		t.Fatalf("pending = %+v err=%v", pend, err)
	}
	// default hotplug applies at once
	if err := c.SetConfig(ctx, "pve1", 101, map[string]string{"delete": "hotplug"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetConfig(ctx, "pve1", 101, map[string]string{"virtio2": volid}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.VM(101); v.Config["virtio2"] != volid {
		t.Fatalf("not applied: %v", v.Config)
	}
	if err := c.SetConfig(ctx, "pve1", 101, map[string]string{"delete": "virtio2"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.VM(101); v.Config["virtio2"] != "" {
		t.Fatalf("not detached: %v", v.Config)
	}
}

func TestFakeGuestExistsSeesContainers(t *testing.T) {
	ctx := context.Background()
	s, c := volEnv(t)
	s.AddContainer("pve1", 999999999)
	for vmid, want := range map[int]bool{101: true, 999999999: true, 102: false} {
		got, err := c.GuestExists(ctx, vmid)
		if err != nil || got != want {
			t.Fatalf("GuestExists(%d) = %v, %v; want %v", vmid, got, err, want)
		}
	}
	vms, err := c.VMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(vms, func(v pve.VM) bool { return v.VMID == 999999999 }) {
		t.Fatal("VMs lists only QEMU guests")
	}
}
