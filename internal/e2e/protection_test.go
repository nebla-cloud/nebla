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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/cli"
	"github.com/stretchr/testify/require"
)

// TestProtection creates a delete-protected volume with the CLI, checks the delete is refused, then unprotects and deletes it.
func TestProtection(t *testing.T) {
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

	token := e.admin.CreateToken(e.proj, "read_write")
	vars := map[string]string{"NEBLA_ENDPOINT": app.URL, "NEBLA_TOKEN": token}
	dir := t.TempDir()
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		code := cli.Run(ctx, args, cli.Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(k string) string { return vars[k] }, ConfigDir: dir})
		return code, out.String(), errb.String()
	}
	mustRun := func(args ...string) string {
		t.Helper()
		code, out, errb := run(args...)
		require.Equal(t, 0, code, "nebla %s\nstdout: %s\nstderr: %s", strings.Join(args, " "), out, errb)
		return out
	}

	name := fmt.Sprintf("nebla-e2e-prot-%d", time.Now().Unix())
	var vid int64
	gone := false
	t.Cleanup(func() {
		if left := ownerDisks(t, pc, tg.node, storage); len(left) > 0 {
			t.Errorf("disks named %s* left on %s/%s: %+v", ownerPrefix, tg.node, storage, left)
		}
	})
	t.Cleanup(func() {
		if vid != 0 && !gone {
			call(t, app, c, http.MethodPut, fmt.Sprintf("/v1/volumes/%d", vid), map[string]any{"protection": map[string]any{"delete": false}}, http.StatusOK, nil)
			cleanupVolume(t, app, c, vid)
		}
	})

	out := mustRun("volume", "create", "--name", name, "--size", "10", "--location", "e2e", "--protect-delete", "-o", "json")
	var created struct {
		Volume struct {
			ID         int64 `json:"id"`
			Protection struct {
				Delete bool `json:"delete"`
			} `json:"protection"`
		} `json:"volume"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &created), out)
	vid = created.Volume.ID
	require.True(t, created.Volume.Protection.Delete)
	path := fmt.Sprintf("/v1/volumes/%d", vid)
	require.Regexp(t, `Protection:\s+delete`, mustRun("volume", "describe", name))

	// The delete is refused with 409 protected, and the volume's disk is untouched.
	res := c.Send(http.MethodDelete, path, nil)
	b := app.Body(res)
	require.Equal(t, http.StatusConflict, res.StatusCode, b)
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(b), &apiErr), b)
	require.Equal(t, "protected", apiErr.Error.Code, b)
	code, _, errb := run("volume", "delete", name)
	require.NotEqual(t, 0, code)
	require.Contains(t, errb, "Turn off delete protection first.")
	require.Equal(t, "available", getVolume(t, app, c, vid).Status)
	require.NotEmpty(t, ownerDisks(t, pc, tg.node, storage))

	mustRun("volume", "update", name, "--protect-delete=false")
	require.Regexp(t, `Protection:\s+none`, mustRun("volume", "describe", name))
	mustRun("volume", "delete", name)
	gone = true
	require.Equal(t, http.StatusNotFound, c.Send(http.MethodGet, path, nil).StatusCode)
	require.Empty(t, ownerDisks(t, pc, tg.node, storage))
}
