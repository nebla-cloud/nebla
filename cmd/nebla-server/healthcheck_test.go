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

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthURL(t *testing.T) {
	for listen, want := range map[string]string{
		"":             "http://127.0.0.1:8080/v1/healthz",
		":8080":        "http://127.0.0.1:8080/v1/healthz",
		"0.0.0.0:9000": "http://127.0.0.1:9000/v1/healthz",
		"[::]:9001":    "http://127.0.0.1:9001/v1/healthz",
	} {
		got, err := healthURL(listen)
		require.NoError(t, err, listen)
		require.Equal(t, want, got, listen)
	}
	_, err := healthURL("nope")
	require.Error(t, err)
}

func TestCheckHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/healthz", r.URL.Path)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ok.Close()
	require.NoError(t, checkHealth(ok.URL+"/v1/healthz"))

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	require.ErrorContains(t, checkHealth(bad.URL+"/v1/healthz"), "503")

	down := httptest.NewServer(nil)
	url := down.URL
	down.Close()
	require.Error(t, checkHealth(url+"/v1/healthz"))
}

func TestHealthcheckExitCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	t.Setenv("PANEL_LISTEN", srv.Listener.Addr().String())
	require.Equal(t, 0, healthcheck())
	srv.Close()
	require.Equal(t, 1, healthcheck())
}
