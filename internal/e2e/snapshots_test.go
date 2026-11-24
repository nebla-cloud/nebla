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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

type snapshotOut struct {
	ID                   int64  `json:"id"`
	Name                 string `json:"name"`
	Status               string `json:"status"`
	Size                 int    `json:"size"`
	Server               *int64 `json:"server"`
	RestoreKeepsIdentity *bool  `json:"restore_keeps_identity"`
}

const (
	bootMarkerPath = "/var/lib/nb-boot-marker"
	volMarkerFile  = "nb-vol-marker"
	hostKeyCmd     = "sudo ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub | awk '{print $2}'"
)

var diskKeyRE = regexp.MustCompile(`^(virtio|scsi|sata|ide|nvme)\d+$`)

// snapshotVMs lists VMs carrying the snapshot template tag, minus the ids in skip.
func snapshotVMs(t *testing.T, pc pve.Client, skip map[int]bool) []pve.VM {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	vms, err := pc.VMs(ctx)
	require.NoError(t, err)
	return slices.DeleteFunc(vms, func(v pve.VM) bool {
		return skip[v.VMID] || !slices.Contains(servers.SplitTags(v.Tags), snapshots.TagSnapshot)
	})
}

func getSnapshot(t *testing.T, e fipEnv, id int64) snapshotOut {
	t.Helper()
	var s snapshotOut
	call(t, e.app, e.c, http.MethodGet, fmt.Sprintf("/v1/snapshots/%d", id), nil, http.StatusOK, &s)
	return s
}

// cleanupSnapshot best-effort deletes a snapshot left behind by a failed run, retrying while a server is still built from it.
func cleanupSnapshot(t *testing.T, e fipEnv, id int64) {
	path := fmt.Sprintf("/v1/snapshots/%d", id)
	deadline := time.Now().Add(actionTimeout)
	for time.Now().Before(deadline) {
		res := e.c.Send(http.MethodDelete, path, nil)
		b := e.app.Body(res)
		switch res.StatusCode {
		case http.StatusNotFound:
			return
		case http.StatusOK:
			var a actionBody
			if err := json.Unmarshal([]byte(b), &a); err != nil {
				t.Errorf("cleanup delete snapshot: %v: %s", err, b)
				return
			}
			waitAction(t, e.app, e.c, a.Action.ID)
			return
		}
		t.Logf("cleanup delete snapshot %d: %d %s; retrying", id, res.StatusCode, b)
		time.Sleep(10 * time.Second)
	}
	t.Errorf("cleanup: snapshot %d was not deleted within %s", id, actionTimeout)
}

// serialLogin opens a serial shell on the server's VM once it runs.
func serialLogin(t *testing.T, e fipEnv, sid int64, name, pw string) *serialConsole {
	t.Helper()
	waitStatus(t, e.app, e.c, sid, "running")
	vms := taggedVMs(t, e.pc, sid)
	require.Len(t, vms, 1)
	con := openSerial(t, e.tg, name, vms[0].Node, vms[0].VMID)
	con.login(pw)
	return con
}

// volumeCmd mounts the volume at /mnt/nb once its device shows up, runs body, and unmounts.
func volumeCmd(dev, body string) string {
	return fmt.Sprintf("for i in $(seq 60); do [ -e %[1]s ] && break; sleep 1; done; sudo mkdir -p /mnt/nb; "+
		"sudo mount %[1]s /mnt/nb; %[2]s; sudo umount /mnt/nb", dev, body)
}

func TestSnapshots(t *testing.T) {
	if strings.Contains(envOr("E2E_POOL_CIDR", ""), ":") {
		t.Skip("the snapshots e2e needs an IPv4 pool")
	}
	img := envOr("E2E_SNAPSHOT_IMAGE", "debian-13")
	e := newFIPEnv(t, []string{img})
	app, c, pc := e.app, e.c, e.pc

	var loc struct {
		VolumeStorageOK bool `json:"volume_storage_ok"`
	}
	call(t, app, e.admin, http.MethodGet, fmt.Sprintf("/v1/admin/locations/%d", e.locID), nil, http.StatusOK, &loc)
	if !loc.VolumeStorageOK {
		t.Skip("location does not offer volumes: grant the e2e token Datastore.Allocate on the disk storage")
	}
	preexisting := map[int]bool{}
	for _, v := range snapshotVMs(t, pc, nil) {
		preexisting[v.VMID] = true
	}

	pwb := make([]byte, 12)
	_, err := rand.Read(pwb)
	require.NoError(t, err)
	pw := hex.EncodeToString(pwb)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	spub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	userData := fipUserData(t, pw, priv, spub)
	tokenA, tokenB := hex.EncodeToString(pwb[:6]), hex.EncodeToString(pwb[6:])

	var sids []int64
	var vid, snapID int64
	snapGone, volGone := false, false
	// Registered first so it runs last, after the servers, volume and snapshot are gone.
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Minute)
		for {
			left := snapshotVMs(t, pc, preexisting)
			if len(left) == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("VMs tagged %s left on the target: %+v", snapshots.TagSnapshot, left)
				return
			}
			time.Sleep(5 * time.Second)
		}
	})
	t.Cleanup(func() {
		if snapID != 0 && !snapGone {
			cleanupSnapshot(t, e, snapID)
		}
	})
	t.Cleanup(func() {
		if vid != 0 && !volGone {
			cleanupVolume(t, app, c, vid)
		}
	})
	t.Cleanup(func() {
		for _, id := range sids {
			cleanupServer(t, app, c, id)
			if left := taggedVMs(t, pc, id); len(left) > 0 {
				t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(id), left)
			}
		}
	})
	create := func(name string, extra map[string]any) (int64, action) {
		var created struct {
			Server struct {
				ID int64 `json:"id"`
			} `json:"server"`
			Action action `json:"action"`
		}
		body := map[string]any{"name": name, "server_type": "e2e-small", "location": e.loc, "user_data": userData,
			"public_net": map[string]bool{"ipv4": true, "ipv6": false}}
		for k, v := range extra {
			body[k] = v
		}
		call(t, app, c, http.MethodPost, "/v1/servers", body, http.StatusCreated, &created)
		sids = append(sids, created.Server.ID)
		return created.Server.ID, created.Action
	}

	// Step 1: a server with a volume attached, and a marker on each disk.
	stamp := time.Now().Unix()
	aName := fmt.Sprintf("nebla-snap-a-%d", stamp)
	aID, act := create(aName, map[string]any{"image": img})
	waitAction(t, app, c, act.ID)
	waitStatus(t, app, c, aID, "running")
	vms := taggedVMs(t, pc, aID)
	require.Len(t, vms, 1)
	hotplug := diskHotplug(t, pc, vms[0])
	var a actionBody
	if !hotplug {
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweroff", aID), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		waitStatus(t, app, c, aID, "off")
	}
	var vc struct {
		Volume volumeOut `json:"volume"`
		Action action    `json:"action"`
	}
	call(t, app, c, http.MethodPost, "/v1/volumes", map[string]any{"name": "e2e-snap-data", "size": 1, "location": e.loc,
		"server": fmt.Sprint(aID)}, http.StatusCreated, &vc)
	vid = vc.Volume.ID
	waitAction(t, app, c, vc.Action.ID)
	require.NotEmpty(t, volumeKey(t, pc, aID, vid), "no virtio key references volume %d", vid)
	if !hotplug {
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweron", aID), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
	}
	dev := vref.LinuxDevice(vid)

	A := serialLogin(t, e, aID, aName, pw)
	A.mustExec(fmt.Sprintf("echo %s | sudo tee %s >/dev/null; sync", tokenA, bootMarkerPath), time.Minute)
	A.mustExec(fmt.Sprintf("for i in $(seq 60); do [ -e %[1]s ] && break; sleep 1; done; sudo mkfs.ext4 -q -F %[1]s", dev), 2*time.Minute)
	A.mustExec(volumeCmd(dev, fmt.Sprintf("echo %s | sudo tee /mnt/nb/%s >/dev/null; sync", tokenA, volMarkerFile)), time.Minute)
	hostKey := A.mustExec(hostKeyCmd, time.Minute)
	require.NotEmpty(t, hostKey)
	bootID := A.mustExec("cat /proc/sys/kernel/random/boot_id", time.Minute)

	// Step 2: snapshot while running; the server goes down and comes back, and the template keeps the boot disk only.
	snapName := fmt.Sprintf("e2e-snap-%d", stamp)
	var sc struct {
		Snapshot snapshotOut `json:"snapshot"`
		Action   action      `json:"action"`
	}
	call(t, app, c, http.MethodPost, "/v1/snapshots", map[string]any{"server": fmt.Sprint(aID), "name": snapName,
		"description": "e2e"}, http.StatusCreated, &sc)
	snapID = sc.Snapshot.ID
	waitAction(t, app, c, sc.Action.ID)
	snap := getSnapshot(t, e, snapID)
	require.Equal(t, "available", snap.Status)
	require.NotNil(t, snap.Server)
	require.Equal(t, aID, *snap.Server)
	require.NotNil(t, snap.RestoreKeepsIdentity)
	require.True(t, *snap.RestoreKeepsIdentity)
	A.close()
	A = serialLogin(t, e, aID, aName, pw)
	require.NotEqual(t, bootID, A.mustExec("cat /proc/sys/kernel/random/boot_id", time.Minute), "the server did not restart")
	require.Equal(t, tokenA, A.mustExec("cat "+bootMarkerPath, time.Minute))
	require.NotEmpty(t, volumeKey(t, pc, aID, vid), "the snapshot detached volume %d from the server", vid)

	tpl := slices.DeleteFunc(snapshotVMs(t, pc, preexisting), func(v pve.VM) bool {
		return !slices.Contains(servers.SplitTags(v.Tags), snapshots.SnapshotTag(snapID))
	})
	require.Len(t, tpl, 1, "template VMs tagged %s", snapshots.SnapshotTag(snapID))
	require.True(t, tpl[0].Template, "the snapshot VM is not a template: %+v", tpl[0])
	require.Contains(t, servers.SplitTags(tpl[0].Tags), snapshots.TagSnapshot)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cfg, err := pc.Config(ctx, tpl[0].Node, tpl[0].VMID)
	cancel()
	require.NoError(t, err)
	var disks []string
	for k := range cfg {
		if diskKeyRE.MatchString(k) {
			disks = append(disks, k)
		}
	}
	require.Len(t, disks, 1, "the template should hold only the boot disk: %v", cfg)
	require.NotContains(t, cfg, "virtio1")
	require.NotContains(t, cfg, "ide2")

	// Step 3: a second server from the snapshot has the boot marker and no volume.
	bName := fmt.Sprintf("nebla-snap-b-%d", stamp)
	bID, act := create(bName, map[string]any{"snapshot": fmt.Sprint(snapID)})
	waitAction(t, app, c, act.ID)
	B := serialLogin(t, e, bID, bName, pw)
	require.Equal(t, tokenA, B.mustExec("cat "+bootMarkerPath, time.Minute))
	B.mustExec("test -z \"$(ls /dev/disk/by-id | grep nb-vol || true)\"", time.Minute)
	require.Empty(t, volumeKey(t, pc, bID, vid))
	B.close()
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", bID), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Empty(t, taggedVMs(t, pc, bID))

	// Step 4: change both markers, restore, and the boot disk is back while the volume keeps its newer marker.
	A.mustExec(fmt.Sprintf("echo %s | sudo tee %s >/dev/null; sync", tokenB, bootMarkerPath), time.Minute)
	A.mustExec(volumeCmd(dev, fmt.Sprintf("echo %s | sudo tee /mnt/nb/%s >/dev/null; sync", tokenB, volMarkerFile)), time.Minute)
	A.close()
	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/rebuild", aID), map[string]any{"snapshot": fmt.Sprint(snapID)},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Len(t, taggedVMs(t, pc, aID), 1)
	A = serialLogin(t, e, aID, aName, pw)
	require.Equal(t, tokenA, A.mustExec("cat "+bootMarkerPath, time.Minute), "the boot disk was not restored")
	require.NotEmpty(t, volumeKey(t, pc, aID, vid), "the restore detached volume %d", vid)
	v := getVolume(t, app, c, vid)
	require.Equal(t, "attached", v.Status)
	require.NotNil(t, v.Server)
	require.Equal(t, aID, *v.Server)
	require.Equal(t, tokenB, A.mustExec(volumeCmd(dev, "cat /mnt/nb/"+volMarkerFile), 2*time.Minute), "the volume was rolled back")
	require.Equal(t, hostKey, A.mustExec(hostKeyCmd, time.Minute), "the SSH host key changed across the restore")
	A.close()

	// Step 5: the snapshot outlives its source server.
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", aID), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	require.Empty(t, taggedVMs(t, pc, aID))
	snap = getSnapshot(t, e, snapID)
	require.Equal(t, "available", snap.Status)
	require.Nil(t, snap.Server)
	var list struct {
		Snapshots []snapshotOut `json:"snapshots"`
	}
	call(t, app, c, http.MethodGet, "/v1/snapshots?name="+snapName, nil, http.StatusOK, &list)
	require.Len(t, list.Snapshots, 1)
	require.Equal(t, snapID, list.Snapshots[0].ID)

	// Step 6: deleting the snapshot removes its template; the cleanup asserts none is left.
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/volumes/%d", vid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	volGone = true
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/snapshots/%d", snapID), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	snapGone = true
	call(t, app, c, http.MethodGet, fmt.Sprintf("/v1/snapshots/%d", snapID), nil, http.StatusNotFound, nil)
	require.Empty(t, snapshotVMs(t, pc, preexisting))
}
