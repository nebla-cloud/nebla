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

//go:build uiharness

package uiharness

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/backups"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/osimages"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/ratelimit"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/riverqueue/river"
	"golang.org/x/time/rate"
)

type pveOut struct {
	URL           string `json:"url"`
	TokenID       string `json:"token_id"`
	TokenSecret   string `json:"token_secret"`
	CAFingerprint string `json:"ca_fingerprint"`
}

type harnessOut struct {
	URL string `json:"url"`
	PVE pveOut `json:"pve"`
	// FailChecksum arms the upstream to 404 the next checksum request on each POST.
	FailChecksum string `json:"fail_checksum"`
	// AddToken registers a token with the fake Proxmox: POST {id, secret}.
	AddToken string `json:"add_token"`
	// FailNext fails the next fake Proxmox request ending in pattern: POST {pattern, status}.
	FailNext string `json:"fail_next"`
	// EnableBackups points every location at the PBS storage and lifts the project backup limit: POST.
	EnableBackups string `json:"enable_backups"`
}

// TestUIHarness serves the app until the stop file exists; run with -tags uiharness.
func TestUIHarness(t *testing.T) {
	outPath, stopPath := os.Getenv("NEBLA_UI_HARNESS_OUT"), os.Getenv("NEBLA_UI_HARNESS_STOP")
	if outPath == "" || stopPath == "" {
		t.Skip("set NEBLA_UI_HARNESS_OUT and NEBLA_UI_HARNESS_STOP")
	}
	sum := strings.Repeat("ab12", 32)
	sum256 := strings.Repeat("cd34", 16)
	var failNext atomic.Int32
	var app *testutil.App
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_harness/fail-checksum" && r.Method == http.MethodPost:
			failNext.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/_harness/pve-token" && r.Method == http.MethodPost:
			var in struct{ ID, Secret string }
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == "" || in.Secret == "" {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			app.PVE.AddToken(in.ID, in.Secret)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/_harness/pve-fail-next" && r.Method == http.MethodPost:
			var in struct {
				Pattern string
				Status  int
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Pattern == "" || in.Status == 0 {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			app.PVE.FailNext(in.Pattern, in.Status)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/_harness/enable-backups" && r.Method == http.MethodPost:
			ctx := r.Context()
			if err := app.Ent.Location.Update().SetBackupStorage("pbs").SetBackupStorageOk(true).Exec(ctx); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := app.Ent.Project.Update().SetMaxBackups(35).Exec(ctx); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/debian/SHA512SUMS":
			if n := failNext.Load(); n > 0 && failNext.CompareAndSwap(n, n-1) {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, "%s  debian.qcow2\n", sum)
		case r.URL.Path == "/ubuntu/SHA256SUMS":
			fmt.Fprintf(w, "%s  ubuntu.qcow2\n", sum256)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	entries := []osimages.Entry{{Key: "debian-13", Description: "Debian 13", OSFlavor: "debian", OSVersion: "13",
		Dirs: []string{up.URL + "/debian/"}, ChecksumName: "SHA512SUMS", File: regexp.MustCompile(`^debian\.qcow2$`),
		Algorithm: "sha512", Format: "qcow2"},
		{Key: "ubuntu-24.04", Description: "Ubuntu 24.04", OSFlavor: "ubuntu", OSVersion: "24.04",
			Dirs: []string{up.URL + "/ubuntu/"}, ChecksumName: "SHA256SUMS", File: regexp.MustCompile(`^ubuntu\.qcow2$`),
			Algorithm: "sha256", Format: "qcow2"}}
	workers := func(w *river.Workers, d *httpapi.Deps, pv *fake.Server) {
		testutil.ServerWorkers(w, d, pv)
		volumes.RegisterWorkers(w, volumes.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		backups.RegisterWorkers(w, backups.WorkerOptions{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Bus: d.Bus,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Jobs: func() *jobs.Jobs { return d.Jobs }})
	}
	app = testutil.NewAppWithWorkers(t, workers, testutil.WithOSImages(entries, up.Client()), testutil.Localhost,
		// The specs share one admin principal; the production limit would starve later specs.
		func(d *httpapi.Deps) { d.APILimiter = ratelimit.New(rate.Limit(10000), 100000) })
	app.SeedPVE()
	for _, n := range []string{"pve1", "pve2"} {
		app.PVE.AddStorage(n, "imp", []string{"import"}, false)
		app.PVE.AddStorage(n, "pbs", []string{"backup"}, true)
		app.PVE.SetStorageType(n, "pbs", "pbs")
	}

	conn := app.PVE.Conn()
	b, err := json.Marshal(harnessOut{URL: app.URL, FailChecksum: up.URL + "/_harness/fail-checksum", AddToken: up.URL + "/_harness/pve-token",
		FailNext: up.URL + "/_harness/pve-fail-next", EnableBackups: up.URL + "/_harness/enable-backups",
		PVE: pveOut{URL: conn.BaseURL, TokenID: conn.TokenID, TokenSecret: conn.Secret, CAFingerprint: conn.CAFingerprint}})
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(filepath.Dir(outPath), ".harness-out.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		t.Fatal(err)
	}
	t.Logf("serving %s", app.URL)
	for {
		if _, err := os.Stat(stopPath); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
