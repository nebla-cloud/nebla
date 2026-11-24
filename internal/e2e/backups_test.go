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
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/stretchr/testify/require"
)

// guest runs commands in one VM through the Proxmox guest agent.
type guest struct {
	t    *testing.T
	hc   *http.Client
	pc   pve.Client
	tg   target
	node string
	vmid int
}

func newGuest(t *testing.T, tg target, pc pve.Client, vm pve.VM) *guest {
	return &guest{t: t, pc: pc, tg: tg, node: vm.Node, vmid: vm.VMID,
		hc: &http.Client{Timeout: time.Minute, Transport: &http.Transport{TLSClientConfig: pve.TLSConfig(tg.conn())}}}
}

func (g *guest) api(method, path string, form url.Values, out any) error {
	target := fmt.Sprintf("%s/api2/json/nodes/%s/qemu/%d/agent/%s", strings.TrimRight(g.tg.url, "/"), g.node, g.vmid, path)
	var body io.Reader
	if method == http.MethodGet {
		target += "?" + form.Encode()
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", g.pc.AuthHeader().Get("Authorization"))
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := g.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s: %s", method, path, res.Status, raw)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ping reports whether the agent answers.
func (g *guest) ping() bool { return g.api(http.MethodPost, "ping", url.Values{}, nil) == nil }

// run executes script with sh and returns its trimmed stdout; a non-zero exit fails the test.
func (g *guest) run(script string) string {
	g.t.Helper()
	var started struct {
		Data struct {
			Pid int `json:"pid"`
		} `json:"data"`
	}
	require.NoError(g.t, g.api(http.MethodPost, "exec", url.Values{"command": {"sh", "-c", script}}, &started),
		"guest exec needs VM.GuestAgent.Unrestricted on the e2e token")
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var st struct {
			Data struct {
				Exited   int    `json:"exited"`
				ExitCode int    `json:"exitcode"`
				Out      string `json:"out-data"`
				Err      string `json:"err-data"`
			} `json:"data"`
		}
		require.NoError(g.t, g.api(http.MethodGet, "exec-status", url.Values{"pid": {fmt.Sprint(started.Data.Pid)}}, &st))
		if st.Data.Exited == 1 {
			require.Zero(g.t, st.Data.ExitCode, "guest script %q: %s %s", script, st.Data.Out, st.Data.Err)
			return strings.TrimSpace(st.Data.Out)
		}
		time.Sleep(time.Second)
	}
	g.t.Fatalf("guest script %q did not finish", script)
	return ""
}

// waitAgent waits for the agent to answer on the server's current VM and returns a guest bound to it.
func waitAgent(t *testing.T, e env, sid int64, within time.Duration) *guest {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		vms := taggedVMs(t, e.pc, sid)
		if len(vms) == 1 && vms[0].Status == "running" {
			if g := newGuest(t, e.tg, e.pc, vms[0]); g.ping() {
				return g
			}
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("the guest agent of server %d did not answer within %s; the template needs qemu-guest-agent", sid, within)
	return nil
}

// ensureAgent turns the agent option on and resets the guest if the VM has it off.
func ensureAgent(t *testing.T, e env, sid int64) {
	t.Helper()
	vms := taggedVMs(t, e.pc, sid)
	require.Len(t, vms, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := e.pc.Config(ctx, vms[0].Node, vms[0].VMID)
	require.NoError(t, err)
	if strings.HasPrefix(cfg["agent"], "1") || strings.HasPrefix(cfg["agent"], "enabled=1") {
		return
	}
	t.Log("the VM has the agent option off; enabling it and resetting")
	require.NoError(t, e.pc.SetConfig(ctx, vms[0].Node, vms[0].VMID, map[string]string{"agent": "1"}))
	var a actionBody
	call(t, e.app, e.c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/reset", sid), nil, http.StatusCreated, &a)
	waitAction(t, e.app, e.c, a.Action.ID)
}

type backupOut struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Kind   string `json:"kind"`
}

func listBackups(t *testing.T, e env, sid int64) []backupOut {
	t.Helper()
	var l struct {
		Backups []backupOut `json:"backups"`
	}
	call(t, e.app, e.c, http.MethodGet, fmt.Sprintf("/v1/servers/%d/backups", sid), nil, http.StatusOK, &l)
	return l.Backups
}

// waitBackupAvailable waits for backup id to finish; a failed one fails the test.
func waitBackupAvailable(t *testing.T, e env, sid, id int64) {
	t.Helper()
	deadline := time.Now().Add(actionTimeout)
	for time.Now().Before(deadline) {
		var b backupOut
		call(t, e.app, e.c, http.MethodGet, fmt.Sprintf("/v1/servers/%d/backups/%d", sid, id), nil, http.StatusOK, &b)
		switch b.Status {
		case "available":
			return
		case "error", "missing":
			t.Fatalf("backup %d is %s", id, b.Status)
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("backup %d was not available within %s", id, actionTimeout)
}

// backNow starts a manual backup and waits for it.
func backNow(t *testing.T, e env, sid int64) int64 {
	t.Helper()
	var b backupOut
	call(t, e.app, e.c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/backups", sid), nil, http.StatusAccepted, &b)
	waitBackupAvailable(t, e, sid, b.ID)
	return b.ID
}

// archives lists the archives on storage whose notes name server sid.
func archives(t *testing.T, pc pve.Client, node, storage string, sid int64) []pve.BackupItem {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	items, err := pc.BackupContent(ctx, node, storage, 0)
	require.NoError(t, err)
	var out []pve.BackupItem
	for _, it := range items {
		if strings.HasPrefix(it.Notes, fmt.Sprintf("nebla server %d ", sid)) {
			out = append(out, it)
		}
	}
	return out
}

// eventually polls f until it returns true or within passes.
func eventually(within time.Duration, f func() bool) bool {
	deadline := time.Now().Add(within)
	for {
		if f() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Second)
	}
}

// volumeScript mounts volume id, runs body in the mount and unmounts; format makes the filesystem first.
func volumeScript(id int64, format bool, body string) string {
	dev := vref.LinuxDevice(id)
	mk := ""
	if format {
		mk = fmt.Sprintf("mkfs.ext4 -q -F %s && ", dev)
	}
	return fmt.Sprintf("set -e; %smkdir -p /mnt/nb && mount %s /mnt/nb; trap 'sync; umount /mnt/nb' EXIT; %s", mk, dev, body)
}

func TestBackups(t *testing.T) {
	storage := os.Getenv("E2E_BACKUP_STORAGE")
	if storage == "" {
		t.Skip("E2E_BACKUP_STORAGE is not set")
	}
	e := newEnv(t)
	app, admin, c, pc, tg := e.app, e.admin, e.c, e.pc, e.tg
	ctx := context.Background()
	require.NoError(t, app.Ent.Project.UpdateOneID(e.proj).SetMaxBackups(10).Exec(ctx))

	// Set the location's backup storage the way an admin does, then wait for the check to pass.
	call(t, app, admin, http.MethodPut, fmt.Sprintf("/v1/admin/locations/%d", e.loc), map[string]any{
		"name": "e2e", "disk_storage": tg.diskStorage, "iso_storage": tg.isoStorage, "backup_storage": storage,
		"pve": map[string]any{"url": tg.url, "token_id": tg.tokenID, "token_secret": tg.secret,
			"ca_fingerprint": tg.fingerprint, "insecure_tls": tg.insecure && tg.fingerprint == ""},
	}, http.StatusOK, nil)
	if !eventually(2*time.Minute, func() bool {
		var loc struct {
			BackupStorage   string `json:"backup_storage"`
			BackupStorageOK bool   `json:"backup_storage_ok"`
		}
		call(t, app, admin, http.MethodGet, fmt.Sprintf("/v1/admin/locations/%d", e.loc), nil, http.StatusOK, &loc)
		return loc.BackupStorage == storage && loc.BackupStorageOK
	}) {
		t.Fatalf("backup_storage_ok never became true for %s: check the storage type, content types and the NeblaBackup ACL", storage)
	}

	var created struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
		Action action `json:"action"`
	}
	name := fmt.Sprintf("nebla-e2e-bk-%d", time.Now().Unix())
	call(t, app, c, http.MethodPost, "/v1/servers", map[string]any{"name": name, "server_type": "e2e-small",
		"image": "e2e-debian", "location": "e2e", "ssh_keys": []string{"e2e"}, "public_net": e.pubNet,
		"backups": map[string]any{"enabled": true, "keep": 7}}, http.StatusCreated, &created)
	sid := created.Server.ID
	var vid int64
	serverGone, volumeGone := false, false
	// Archives present before this run's server existed; the sweep never touches them. Unset until checked.
	baseline, baselineSet := map[string]bool{}, false
	ours := func() []pve.BackupItem {
		var out []pve.BackupItem
		for _, it := range archives(t, pc, tg.node, storage, sid) {
			if !baseline[it.Volid] {
				out = append(out, it)
			}
		}
		return out
	}
	// Registered first so it runs last: sweep archives the test or the panel left behind.
	t.Cleanup(func() {
		if !baselineSet {
			return
		}
		// Give the panel's own archive delete time to finish before sweeping.
		eventually(2*time.Minute, func() bool { return len(ours()) == 0 })
		for _, it := range ours() {
			t.Errorf("archive %s (%q) left on %s", it.Volid, it.Notes, storage)
			dctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			if err := pc.DeleteVolume(dctx, tg.node, storage, it.Volid); err != nil {
				t.Logf("cleanup: delete archive %s: %v", it.Volid, err)
			}
			cancel()
		}
		if left := taggedVMs(t, pc, sid); len(left) > 0 {
			t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(sid), left)
		}
		if left := ownerDisks(t, pc, tg.node, tg.diskStorage); len(left) > 0 {
			t.Errorf("disks named %s* left on %s/%s: %+v", ownerPrefix, tg.node, tg.diskStorage, left)
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
	pre := archives(t, pc, tg.node, storage, sid)
	for _, it := range pre {
		baseline[it.Volid] = true
	}
	baselineSet = true
	require.Empty(t, pre, "%s already holds archives for server %d from an earlier run; delete them first", storage, sid)

	vms := taggedVMs(t, pc, sid)
	require.Len(t, vms, 1)
	hotplug := diskHotplug(t, pc, vms[0])
	var a actionBody
	if !hotplug {
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweroff", sid), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
		waitStatus(t, app, c, sid, "off")
	}
	var vc struct {
		Volume volumeOut `json:"volume"`
		Action action    `json:"action"`
	}
	call(t, app, c, http.MethodPost, "/v1/volumes", map[string]any{"name": "e2e-bk-data", "size": 1, "location": "e2e",
		"server": fmt.Sprint(sid)}, http.StatusCreated, &vc)
	vid = vc.Volume.ID
	waitAction(t, app, c, vc.Action.ID)
	require.Equal(t, "attached", getVolume(t, app, c, vid).Status)
	require.NotEmpty(t, volumeKey(t, pc, sid, vid))
	if !hotplug {
		call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/poweron", sid), nil, http.StatusCreated, &a)
		waitAction(t, app, c, a.Action.ID)
	}

	ensureAgent(t, e, sid)
	g := waitAgent(t, e, sid, 5*time.Minute)
	g.run("echo 1 > /root/nb-marker && sync")
	g.run(volumeScript(vid, true, "echo vol1 > /mnt/nb/marker"))
	hostKey := g.run("ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub")
	require.NotEmpty(t, hostKey)
	t.Logf("host key %s", hostKey)

	b1 := backNow(t, e, sid)
	require.Len(t, archives(t, pc, tg.node, storage, sid), 1)
	require.Len(t, listBackups(t, e, sid), 1)

	g.run(volumeScript(vid, false, "echo vol2 > /mnt/nb/marker"))
	g.run("echo 2 > /root/nb-marker && sync")
	require.Equal(t, "2", g.run("cat /root/nb-marker"))

	call(t, app, c, http.MethodPost, fmt.Sprintf("/v1/servers/%d/actions/restore_backup", sid), map[string]any{"backup": b1},
		http.StatusCreated, &a)
	waitAction(t, app, c, a.Action.ID)
	waitStatus(t, app, c, sid, "running")
	g = waitAgent(t, e, sid, 5*time.Minute)
	require.Equal(t, "1", g.run("cat /root/nb-marker"), "the restore did not bring back the first backup's state")
	require.Equal(t, hostKey, g.run("ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub"), "the guest's SSH host key changed")
	v := getVolume(t, app, c, vid)
	require.Equal(t, "attached", v.Status)
	require.NotNil(t, v.Server)
	require.Equal(t, sid, *v.Server)
	require.NotEmpty(t, volumeKey(t, pc, sid, vid), "the restored VM lost volume %d", vid)
	require.Equal(t, "vol2", g.run(volumeScript(vid, false, "cat /mnt/nb/marker")), "the volume was rolled back or its data changed")

	// With keep=1 each new backup prunes the older ones, so a single archive remains.
	call(t, app, c, http.MethodPut, fmt.Sprintf("/v1/servers/%d", sid),
		map[string]any{"backups": map[string]any{"enabled": true, "keep": 1}}, http.StatusOK, nil)
	backNow(t, e, sid)
	backNow(t, e, sid)
	var apiList []backupOut
	if !eventually(5*time.Minute, func() bool {
		apiList = listBackups(t, e, sid)
		return len(apiList) == 1 && len(archives(t, pc, tg.node, storage, sid)) == 1
	}) {
		t.Fatalf("keep=1 left %d backups in the API and %d archives on %s", len(apiList), len(archives(t, pc, tg.node, storage, sid)), storage)
	}
	require.Equal(t, "available", apiList[0].Status)

	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/servers/%d", sid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	serverGone = true
	require.Empty(t, taggedVMs(t, pc, sid))
	if !eventually(5*time.Minute, func() bool { return len(archives(t, pc, tg.node, storage, sid)) == 0 }) {
		t.Fatalf("archives of deleted server %d are still on %s: %+v", sid, storage, archives(t, pc, tg.node, storage, sid))
	}

	// The volume outlives the server.
	require.Equal(t, "available", getVolume(t, app, c, vid).Status)
	call(t, app, c, http.MethodDelete, fmt.Sprintf("/v1/volumes/%d", vid), nil, http.StatusOK, &a)
	waitAction(t, app, c, a.Action.ID)
	volumeGone = true
}
