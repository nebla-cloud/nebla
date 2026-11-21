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

//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/reconcile"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

// watchUserData installs a unit that powers the guest off once any Nebla volume appears under its stable path,
// so the test sees the guest found the device through Proxmox alone.
const watchUserData = `#cloud-config
write_files:
  - path: /usr/local/sbin/nebla-e2e-watch
    permissions: "0755"
    content: |
      #!/bin/sh
      while :; do
        for d in /dev/disk/by-id/virtio-nb-vol-*; do
          if [ -e "$d" ]; then
            ls -l /dev/disk/by-id/ > /dev/console 2>&1 || true
            systemctl poweroff
            exit 0
          fi
        done
        sleep 2
      done
  - path: /etc/systemd/system/nebla-e2e-watch.service
    content: |
      [Unit]
      Description=Nebla e2e volume watcher
      [Service]
      ExecStart=/usr/local/sbin/nebla-e2e-watch
      [Install]
      WantedBy=multi-user.target
runcmd:
  - [systemctl, daemon-reload]
  - [systemctl, enable, --now, --no-block, nebla-e2e-watch.service]
`

const ownerPrefix = "vm-999999999-"

type volumeOut struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Server *int64 `json:"server"`
	Size   int    `json:"size"`
}

// ownerDisks lists the disks on storage owned by the reserved volume VMID.
func ownerDisks(t *testing.T, pc pve.Client, node, storage string) []pve.StorageItem {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	items, err := pc.StorageContent(ctx, node, storage, vref.OwnerVMID)
	require.NoError(t, err)
	return slices.DeleteFunc(items, func(it pve.StorageItem) bool {
		_, name, _ := strings.Cut(it.Volid, ":")
		// File storages keep the disk under the owner's directory.
		name = strings.TrimPrefix(name, fmt.Sprintf("%d/", vref.OwnerVMID))
		return !strings.HasPrefix(name, ownerPrefix)
	})
}

func diskOf(t *testing.T, pc pve.Client, node, storage string, id int64) (pve.StorageItem, bool) {
	t.Helper()
	for _, it := range ownerDisks(t, pc, node, storage) {
		if got, ok := vref.Parse(it.Volid); ok && got == id {
			return it, true
		}
	}
	return pve.StorageItem{}, false
}

// volumeKey is the config key on the server's VM that references volume id, or "".
func volumeKey(t *testing.T, pc pve.Client, sid, id int64) string {
	t.Helper()
	vms := taggedVMs(t, pc, sid)
	require.Len(t, vms, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := pc.Config(ctx, vms[0].Node, vms[0].VMID)
	require.NoError(t, err)
	for k, v := range cfg {
		if got, ok := vref.Parse(v); ok && got == id && strings.HasPrefix(k, "virtio") {
			require.Contains(t, v, "serial="+vref.Serial(id), "%s=%s", k, v)
			return k
		}
	}
	return ""
}

func diskHotplug(t *testing.T, pc pve.Client, vm pve.VM) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := pc.Config(ctx, vm.Node, vm.VMID)
	require.NoError(t, err)
	h, ok := cfg["hotplug"]
	switch {
	case !ok || h == "1":
		return true
	case h == "0":
		return false
	}
	return slices.Contains(strings.Split(h, ","), "disk")
}

func getVolume(t *testing.T, app *testutil.App, c *testutil.Client, id int64) volumeOut {
	t.Helper()
	var v volumeOut
	call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/volumes/%d", id), nil, http.StatusOK, &v)
	return v
}

// waitVMStopped waits for the server's VM to stop on its own.
func waitVMStopped(t *testing.T, pc pve.Client, sid int64, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		vms := taggedVMs(t, pc, sid)
		require.Len(t, vms, 1)
		if vms[0].Status == "stopped" {
			return true
		}
		time.Sleep(5 * time.Second)
	}
	return false
}

// cleanupVolume best-effort deletes a volume left behind by a failed run, detaching it first if needed.
func cleanupVolume(t *testing.T, app *testutil.App, c *testutil.Client, id int64) {
	path := fmt.Sprintf("/v1/volumes/%d", id)
	deadline := time.Now().Add(actionTimeout)
	for time.Now().Before(deadline) {
		res := c.Send(http.MethodDelete, path, nil)
		b := app.Body(res)
		switch res.StatusCode {
		case http.StatusNotFound:
			return
		case http.StatusOK:
			var a actionBody
			if err := json.Unmarshal([]byte(b), &a); err != nil {
				t.Errorf("cleanup delete volume: %v: %s", err, b)
				return
			}
			waitAction(t, app, c, a.Action.ID)
			return
		}
		t.Logf("cleanup delete volume %d: %d %s; detaching and retrying", id, res.StatusCode, b)
		det := c.Send(http.MethodPost, path+"/actions/detach", nil)
		if db := app.Body(det); det.StatusCode == http.StatusCreated {
			var a actionBody
			if json.Unmarshal([]byte(db), &a) == nil {
				waitAction(t, app, c, a.Action.ID)
			}
		}
		time.Sleep(10 * time.Second)
	}
	t.Errorf("cleanup: volume %d was not deleted within %s", id, actionTimeout)
}

func TestVolumes(t *testing.T) {
	e := newEnv(t)
	app, admin, c, pc, tg := e.app, e.admin, e.c, e.pc, e.tg

	var loc struct {
		VolumeStorage   string `json:"volume_storage"`
		VolumeStorageOK bool   `json:"volume_storage_ok"`
	}
	call(t, app, admin, http.MethodGet, fmt.Sprintf("/v1/admin/locations/%d", e.loc), nil, http.StatusOK, &loc)
	storage := loc.VolumeStorage
	if storage == "" {
		storage = tg.diskStorage
	}
	if !loc.VolumeStorageOK {
		t.Skipf("location e2e does not offer volumes: grant the e2e token Datastore.Allocate on /storage/%s "+
			"(e.g. the NeblaISO role there), and make sure %s holds the images content type", storage, storage)
	}
	if left := ownerDisks(t, pc, tg.node, storage); len(left) > 0 {
		t.Fatalf("disks named %s* already exist on %s/%s; remove them before running: %+v", ownerPrefix, tg.node, storage, left)
	}

	var created struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
		Action action `json:"action"`
	}
	name := fmt.Sprintf("nebla-e2e-vol-%d", time.Now().Unix())
	call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small",
		"image": "e2e-debian", "location": "e2e", "ssh_keys": []string{"e2e"}, "public_net": e.pubNet,
		"user_data": watchUserData}, http.StatusCreated, &created)
	sid := created.Server.ID
	var vid int64
	serverGone, volumeGone := false, false
	// Registered first so it runs last, after both deletes.
	t.Cleanup(func() {
		if left := taggedVMs(t, pc, sid); len(left) > 0 {
			t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(sid), left)
		}
		if left := ownerDisks(t, pc, tg.node, storage); len(left) > 0 {
			t.Errorf("disks named %s* left on %s/%s: %+v", ownerPrefix, tg.node, storage, left)
		}
	})
	t.Cleanup(func() {
		if vid != 0 && !volumeGone {
			cleanupVolume(t, app, c, vid)
		}
	})
	t.Cleanup(func() {
		if !serverGone {
			cleanupServer(t, app, c, sid)
		}
	})
	waitAction(t, app, c, created.Action.ID)
	waitStatus(t, app, c, sid, "running")
	vms := taggedVMs(t, pc, sid)
	require.Len(t, vms, 1)
	hotplug := diskHotplug(t, pc, vms[0])

	// The watcher must not fire while no volume is attached.
	time.Sleep(2 * guestBootWait)
	vms = taggedVMs(t, pc, sid)
	require.Len(t, vms, 1)
	require.Equal(t, "running", vms[0].Status, "the guest stopped before any volume was attached")

	var a actionBody
	if !hotplug {
		t.Log("the VM has no disk hotplug; attaching while off and booting with the volume")
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweroff", sid), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		waitStatus(t, app, c, sid, "off")
	}

	var vc struct {
		Volume volumeOut `json:"volume"`
		Action action    `json:"action"`
	}
	call(t, app, c, http.MethodPost, "/v1/volumes", map[string]any{"name": "e2e-data", "size": 10, "location": "e2e",
		"server": fmt.Sprint(sid)}, http.StatusCreated, &vc)
	vid = vc.Volume.ID
	waitAction(t, app, c, vc.Action.ID)
	v := getVolume(t, app, c, vid)
	require.Equal(t, "attached", v.Status)
	require.NotNil(t, v.Server)
	require.Equal(t, sid, *v.Server)
	require.NotEmpty(t, volumeKey(t, pc, sid, vid), "no virtio key references volume %d", vid)
	disk, ok := diskOf(t, pc, tg.node, storage, vid)
	require.True(t, ok, "volume %d has no disk on %s/%s", vid, tg.node, storage)
	require.Equal(t, int64(10)<<30, disk.Size)

	if !hotplug {
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweron", sid), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
	}
	// The guest powers itself off once it sees /dev/disk/by-id/virtio-nb-vol-<id>.
	stopped := waitVMStopped(t, pc, sid, 3*time.Minute)
	if !stopped {
		// It may have stopped just after the last poll; a stopped VM is already the proof.
		vms = taggedVMs(t, pc, sid)
		require.Len(t, vms, 1)
		stopped = vms[0].Status == "stopped"
	}
	if !stopped {
		t.Log("no stop after a live attach; resetting so the guest boots with the volume")
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/reset", sid), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		require.True(t, waitVMStopped(t, pc, sid, 5*time.Minute), "the guest never saw %s", vref.LinuxDevice(vid))
	}
	t.Logf("the guest saw %s and powered off", vref.LinuxDevice(vid))
	// Proxmox now shows the VM stopped; a reconcile pass brings the server's status in line.
	rec := reconcile.New(reconcile.Options{SQL: app.SQL, Ent: app.Ent, Catalog: app.Catalog, Logger: e.log})
	deadline := time.Now().Add(2 * time.Minute)
	for serverStatus(t, app, c, sid) != "off" {
		if time.Now().After(deadline) {
			t.Fatalf("server %d is not off after the guest powered off", sid)
		}
		require.NoError(t, rec.Run(context.Background(), e.loc))
		time.Sleep(2 * time.Second)
	}

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/volumes/%d/actions/resize", vid), map[string]any{"size": 20},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Equal(t, 20, getVolume(t, app, c, vid).Size)
	disk, ok = diskOf(t, pc, tg.node, storage, vid)
	require.True(t, ok)
	require.Equal(t, int64(20)<<30, disk.Size)

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/volumes/%d/actions/detach", vid), nil, http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	v = getVolume(t, app, c, vid)
	require.Equal(t, "available", v.Status)
	require.Nil(t, v.Server)
	require.Empty(t, volumeKey(t, pc, sid, vid))

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/rebuild", sid), map[string]string{"image": "e2e-debian"},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Len(t, taggedVMs(t, pc, sid), 1)

	// The rebuilt guest reapplies the watcher user data and may power itself off with the volume back; that is expected,
	// and nothing below assumes it is running.
	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/volumes/%d/actions/attach", vid), map[string]any{"server": fmt.Sprint(sid)},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Equal(t, "attached", getVolume(t, app, c, vid).Status)
	require.NotEmpty(t, volumeKey(t, pc, sid, vid), "the rebuilt VM has no key for volume %d", vid)

	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	serverGone = true
	require.Empty(t, taggedVMs(t, pc, sid))
	v = getVolume(t, app, c, vid)
	require.Equal(t, "available", v.Status)
	require.Nil(t, v.Server)
	_, ok = diskOf(t, pc, tg.node, storage, vid)
	require.True(t, ok, "volume %d's disk went with the server", vid)

	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/volumes/%d", vid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	volumeGone = true
	call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/volumes/%d", vid), nil, http.StatusNotFound, nil)
	_, ok = diskOf(t, pc, tg.node, storage, vid)
	require.False(t, ok, "volume %d's disk is still on %s/%s", vid, tg.node, storage)
}

// TestVolumesFileStorage runs a short volume pass on the file storage (dir, nfs, ...) named by E2E_FILE_VOLUME_STORAGE.
func TestVolumesFileStorage(t *testing.T) {
	storage := os.Getenv("E2E_FILE_VOLUME_STORAGE")
	if storage == "" {
		t.Skip("E2E_FILE_VOLUME_STORAGE is not set")
	}
	e := newEnv(t)
	app, c, pc, tg := e.app, e.c, e.pc, e.tg
	ctx := context.Background()
	require.NoError(t, app.Ent.Location.UpdateOneID(e.loc).SetVolumeStorage(storage).SetVolumeStorageOk(true).Exec(ctx))
	if left := ownerDisks(t, pc, tg.node, storage); len(left) > 0 {
		t.Fatalf("disks named %s* already exist on %s/%s; remove them before running: %+v", ownerPrefix, tg.node, storage, left)
	}

	var created struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
		Action action `json:"action"`
	}
	name := fmt.Sprintf("nebla-e2e-filevol-%d", time.Now().Unix())
	call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small",
		"image": "e2e-debian", "location": "e2e", "ssh_keys": []string{"e2e"}, "public_net": e.pubNet},
		http.StatusCreated, &created)
	sid := created.Server.ID
	var vid int64
	serverGone, volumeGone := false, false
	t.Cleanup(func() {
		if left := ownerDisks(t, pc, tg.node, storage); len(left) > 0 {
			t.Errorf("disks named %s* left on %s/%s: %+v", ownerPrefix, tg.node, storage, left)
		}
	})
	t.Cleanup(func() {
		if vid != 0 && !volumeGone {
			cleanupVolume(t, app, c, vid)
		}
	})
	t.Cleanup(func() {
		if !serverGone {
			cleanupServer(t, app, c, sid)
		}
	})
	waitAction(t, app, c, created.Action.ID)
	waitStatus(t, app, c, sid, "running")

	var a actionBody
	vms := taggedVMs(t, pc, sid)
	require.Len(t, vms, 1)
	if !diskHotplug(t, pc, vms[0]) {
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweroff", sid), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		waitStatus(t, app, c, sid, "off")
	}

	var vc struct {
		Volume volumeOut `json:"volume"`
		Action action    `json:"action"`
	}
	call(t, app, c, http.MethodPost, "/v1/volumes", map[string]any{"name": "e2e-filedata", "size": 10, "location": "e2e",
		"server": fmt.Sprint(sid)}, http.StatusCreated, &vc)
	vid = vc.Volume.ID
	waitAction(t, app, c, vc.Action.ID)
	require.Equal(t, "attached", getVolume(t, app, c, vid).Status)
	require.NotEmpty(t, volumeKey(t, pc, sid, vid), "no virtio key references volume %d", vid)
	disk, ok := diskOf(t, pc, tg.node, storage, vid)
	require.True(t, ok, "volume %d has no disk on %s/%s", vid, tg.node, storage)
	require.Equal(t, vref.Volid(storage, vid, true), disk.Volid)
	require.Equal(t, int64(10)<<30, disk.Size)

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/volumes/%d/actions/resize", vid), map[string]any{"size": 20},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	disk, ok = diskOf(t, pc, tg.node, storage, vid)
	require.True(t, ok)
	require.Equal(t, int64(20)<<30, disk.Size)

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/volumes/%d/actions/detach", vid), nil, http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Equal(t, "available", getVolume(t, app, c, vid).Status)
	require.Empty(t, volumeKey(t, pc, sid, vid))

	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/volumes/%d", vid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	volumeGone = true
	_, ok = diskOf(t, pc, tg.node, storage, vid)
	require.False(t, ok, "volume %d's disk is still on %s/%s", vid, tg.node, storage)

	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	serverGone = true
}
