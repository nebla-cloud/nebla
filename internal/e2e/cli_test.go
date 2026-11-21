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
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

// TestCLISmoke drives the nebla CLI against the panel and the target Proxmox.
func TestCLISmoke(t *testing.T) {
	e := newEnv(t)
	token := e.admin.CreateToken(e.proj, "read_write")
	vars := map[string]string{"NEBLA_ENDPOINT": e.app.URL, "NEBLA_TOKEN": token}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		code := cli.Run(ctx, args, cli.Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(k string) string { return vars[k] }, ConfigDir: dir})
		require.Equal(t, 0, code, "nebla %s\nstdout: %s\nstderr: %s", strings.Join(args, " "), out.String(), errb.String())
		return out.String()
	}

	name := fmt.Sprintf("nebla-cli-e2e-%d", time.Now().Unix())
	args := []string{"server", "create", "--name", name, "--type", "e2e-small", "--image", "e2e-debian",
		"--location", "e2e", "--ssh-key", "e2e", "-o", "json"}
	if !e.pubNet["ipv4"] {
		args = append(args, "--without-ipv4", "--with-ipv6")
	}
	var created struct {
		Server struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"server"`
	}
	deleted := false
	t.Cleanup(func() {
		var l struct {
			Servers []struct {
				ID int64 `json:"id"`
			} `json:"servers"`
		}
		call(t, e.app, e.c, http.MethodGet, "/v1/servers?name="+name, nil, http.StatusOK, &l)
		for _, s := range l.Servers {
			if !deleted {
				cleanupServer(t, e.app, e.c, s.ID)
			}
			if left := taggedVMs(t, e.pc, s.ID); len(left) > 0 {
				t.Errorf("VMs tagged %s left on the target: %+v", servers.ServerTag(s.ID), left)
			}
		}
	})
	out := run(args...)
	require.NoError(t, json.Unmarshal([]byte(out), &created), out)
	sid := created.Server.ID
	require.Equal(t, "running", created.Server.Status)
	require.Len(t, taggedVMs(t, e.pc, sid), 1)

	status := func() string {
		var s struct {
			Status string `json:"status"`
		}
		o := run("server", "describe", name, "-o", "json")
		require.NoError(t, json.Unmarshal([]byte(o), &s), o)
		return s.Status
	}
	require.Contains(t, run("server", "list"), name)
	run("server", "poweroff", name)
	require.Equal(t, "off", status())
	run("server", "poweron", name)
	require.Equal(t, "running", status())

	run("server", "delete", name)
	deleted = true
	require.Empty(t, taggedVMs(t, e.pc, sid))
	require.NotContains(t, run("server", "list"), name)
}
