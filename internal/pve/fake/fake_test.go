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

package fake_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestFakePermissions(t *testing.T) {
	ctx := context.Background()
	s, c := volEnv(t)
	got, err := c.Permissions(ctx, "/storage/local-lvm")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["/storage/local-lvm"]["Datastore.Allocate"]; !ok {
		t.Fatalf("default should grant everything: %+v", got)
	}
	s.DenyAllocateOn("local-lvm")
	got, _ = c.Permissions(ctx, "/storage/local-lvm")
	if _, ok := got["/storage/local-lvm"]["Datastore.Allocate"]; ok || len(got["/storage/local-lvm"]) == 0 {
		t.Fatalf("deny: %+v", got)
	}
	if other, _ := c.Permissions(ctx, "/storage/ceph"); other["/storage/ceph"]["Datastore.Allocate"] != 1 {
		t.Fatalf("deny leaked: %+v", other)
	}
	s.SetPermissions("/storage/local-lvm", map[string]int{})
	s.SetPermissions("/", map[string]int{"Datastore.Allocate": 1})
	got, _ = c.Permissions(ctx, "/storage/local-lvm")
	if len(got["/storage/local-lvm"]) != 0 || got["/"]["Datastore.Allocate"] != 1 {
		t.Fatalf("explicit: %+v", got)
	}
}

func TestFakeFileStorageVolume(t *testing.T) {
	ctx := context.Background()
	s, c := volEnv(t)
	s.AddStorage("pve1", "local", []string{"images", "iso"}, false)
	s.SetStorageType("pve1", "local", "dir")
	ss, err := c.Storages(ctx, "pve1")
	if err != nil || len(ss) != 2 || ss[0].ID != "local" || ss[0].Type != "dir" || ss[1].Type != "lvmthin" {
		t.Fatalf("storages = %+v err=%v", ss, err)
	}
	if _, err := c.AllocVolume(ctx, "pve1", "local", 999999999, "vm-999999999-nb-vol-7", 20); err == nil {
		t.Fatal("file storage accepted a name without an extension")
	}
	volid, err := c.AllocVolume(ctx, "pve1", "local", 999999999, "vm-999999999-nb-vol-7.raw", 20)
	if err != nil || volid != "local:999999999/vm-999999999-nb-vol-7.raw" {
		t.Fatalf("volid = %q err=%v", volid, err)
	}
	if err := c.SetConfig(ctx, "pve1", 101, map[string]string{"virtio1": volid}); err != nil {
		t.Fatal(err)
	}
	if err := c.ResizeDisk(ctx, "pve1", 101, "virtio1", 30); err != nil {
		t.Fatal(err)
	}
	items, err := c.StorageContent(ctx, "pve1", "local", 999999999)
	if err != nil || len(items) != 1 || items[0].Volid != volid || items[0].Size != 30<<30 {
		t.Fatalf("items = %+v err=%v", items, err)
	}
	if err := c.DeleteVolume(ctx, "pve1", "local", volid); err != nil {
		t.Fatal(err)
	}
	if all, _ := c.StorageContent(ctx, "pve1", "local", 0); len(all) != 0 {
		t.Fatalf("still there: %+v", all)
	}
}

func TestIPSetHolders(t *testing.T) {
	ctx := context.Background()
	s, c := volEnv(t)
	s.AddVM("pve1", 102, "db")
	ip := netip.MustParseAddr("10.0.0.50")
	var mu sync.Mutex
	var seen []int
	s.OnIPSetWrite(func() {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, len(s.IPSetHolders(ip.String())))
	})
	if err := c.SetFirewall(ctx, "pve1", 101, []netip.Addr{ip}); err != nil {
		t.Fatal(err)
	}
	if got := s.IPSetHolders(ip.String()); !slices.Equal(got, []int{101}) {
		t.Fatalf("holders = %v", got)
	}
	if err := c.SetFirewall(ctx, "pve1", 101, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFirewall(ctx, "pve1", 102, []netip.Addr{ip}); err != nil {
		t.Fatal(err)
	}
	if got := s.IPSetHolders(ip.String()); !slices.Equal(got, []int{102}) {
		t.Fatalf("holders = %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(seen, []int{1, 0, 1}) {
		t.Fatalf("holders after each entry write = %v", seen)
	}

	// A blocking hook blocks the caller's SetFirewall.
	release := make(chan struct{})
	s.OnIPSetWrite(func() { <-release })
	done := make(chan error, 1)
	go func() { done <- c.SetFirewall(ctx, "pve1", 101, []netip.Addr{netip.MustParseAddr("10.0.0.51")}) }()
	select {
	case <-done:
		t.Fatal("SetFirewall returned while the hook blocked")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func backupEnv(t *testing.T) (*fake.Server, pve.Client) {
	t.Helper()
	s := fake.New(t)
	s.AddNode("pve1", 4, 8192)
	s.AddStorage("pve1", "local-lvm", []string{"images"}, false)
	s.AddStorage("pve1", "nebla-pbs", []string{"backup"}, true)
	s.AddVM("pve1", 101, "web")
	s.SetVMConfig(101, "tags", "nebla;nebla-s-1")
	s.SetVMConfig(101, "description", "hello")
	s.SetVMConfig(101, "net0", "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr0")
	s.SetVMConfig(101, "virtio1", "local-lvm:vm-999999999-nb-vol-1,backup=0,size=5G")
	s.SetVMFirewall(101, map[string]string{"enable": "1"})
	return s, pve.New(s.Conn())
}

func TestFakeBackupRestoreDropsExcludedDisk(t *testing.T) {
	ctx := context.Background()
	s, c := backupEnv(t)
	up, err := c.Vzdump(ctx, "pve1", 101, "nebla-pbs", "nebla server 1 backup 2")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	items, err := c.BackupContent(ctx, "pve1", "nebla-pbs", 101)
	if err != nil || len(items) != 1 || items[0].Notes != "nebla server 1 backup 2" || items[0].VMID != 101 || items[0].Size <= 0 || items[0].Ctime.IsZero() {
		t.Fatalf("items = %+v err=%v", items, err)
	}
	if !strings.HasPrefix(items[0].Volid, "nebla-pbs:backup/vm/101/") {
		t.Fatalf("volid = %q", items[0].Volid)
	}
	if other, _ := c.BackupContent(ctx, "pve1", "nebla-pbs", 102); len(other) != 0 {
		t.Fatalf("filter leaked %+v", other)
	}
	up, err = c.RestoreVM(ctx, "pve1", 202, items[0].Volid, "local-lvm")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	src, _ := s.VM(101)
	got, ok := s.VM(202)
	if !ok {
		t.Fatal("restored vm missing")
	}
	if _, has := got.Config["virtio1"]; has {
		t.Fatalf("virtio1 kept: %v", got.Config)
	}
	if !strings.Contains(got.Config["description"], "virtio1: ") || !strings.HasPrefix(got.Config["description"], "hello") {
		t.Fatalf("description = %q", got.Config["description"])
	}
	if got.Tags != src.Tags || got.Name != src.Name {
		t.Fatalf("tags/name = %q/%q", got.Tags, got.Name)
	}
	if got.Config["net0"] == src.Config["net0"] || !strings.Contains(got.Config["net0"], "bridge=vmbr0") {
		t.Fatalf("net0 = %q", got.Config["net0"])
	}
	if got.Firewall["enable"] != "1" {
		t.Fatalf("firewall = %v", got.Firewall)
	}
	if _, ok := s.VM(101); !ok {
		t.Fatal("source vm gone")
	}
	if _, err := c.RestoreVM(ctx, "pve1", 202, items[0].Volid, "local-lvm"); err == nil {
		t.Fatal("restore over an existing vmid must fail")
	}
}

func TestFakeBackupRestoreCopiesRules(t *testing.T) {
	ctx := context.Background()
	s, c := backupEnv(t)
	if err := c.AddFirewallRule(ctx, "pve1", 101, pve.FirewallRule{Type: "in", Action: "ACCEPT", Proto: "tcp", Dport: "22", Enable: true}); err != nil {
		t.Fatal(err)
	}
	up, _ := c.Vzdump(ctx, "pve1", 101, "nebla-pbs", "n")
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	items, _ := c.BackupContent(ctx, "pve1", "nebla-pbs", 0)
	up, _ = c.RestoreVM(ctx, "pve1", 203, items[0].Volid, "local-lvm")
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	got, _ := s.VM(203)
	if len(got.Rules) != 1 || got.Rules[0].Dport != "22" {
		t.Fatalf("rules = %+v", got.Rules)
	}
}

func TestFakeDeleteBackup(t *testing.T) {
	ctx := context.Background()
	_, c := backupEnv(t)
	up, _ := c.Vzdump(ctx, "pve1", 101, "nebla-pbs", "n")
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	items, _ := c.BackupContent(ctx, "pve1", "nebla-pbs", 0)
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if err := c.DeleteVolume(ctx, "pve1", "nebla-pbs", items[0].Volid); err != nil {
		t.Fatal(err)
	}
	if items, _ = c.BackupContent(ctx, "pve1", "nebla-pbs", 0); len(items) != 0 {
		t.Fatalf("still listed: %+v", items)
	}
}

func TestFakeBackupFailures(t *testing.T) {
	ctx := context.Background()
	s, c := backupEnv(t)
	s.FailVzdump = true
	up, err := c.Vzdump(ctx, "pve1", 101, "nebla-pbs", "n")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx, "pve1", up); !errors.Is(err, pve.ErrTaskFailed) {
		t.Fatalf("err = %v", err)
	}
	s.FailVzdump = false
	s.SetVMConfig(101, "lock", "backup")
	if _, err := c.Vzdump(ctx, "pve1", 101, "nebla-pbs", "n"); err == nil {
		t.Fatal("vzdump on a locked vm must fail")
	}
	s.SetVMConfig(101, "lock", "")
	up, _ = c.Vzdump(ctx, "pve1", 101, "nebla-pbs", "n")
	if err := c.WaitTask(ctx, "pve1", up); err != nil {
		t.Fatal(err)
	}
	items, _ := c.BackupContent(ctx, "pve1", "nebla-pbs", 0)
	s.FailRestore = true
	up, err = c.RestoreVM(ctx, "pve1", 204, items[0].Volid, "local-lvm")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WaitTask(ctx, "pve1", up); !errors.Is(err, pve.ErrTaskFailed) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := s.VM(204); ok {
		t.Fatal("failed restore left a vm")
	}
}
