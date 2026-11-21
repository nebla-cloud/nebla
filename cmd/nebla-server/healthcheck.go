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
	"cmp"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// healthURL is the loopback healthz URL for a PANEL_LISTEN value.
func healthURL(listen string) (string, error) {
	_, port, err := net.SplitHostPort(cmp.Or(listen, ":8080"))
	if err != nil {
		return "", err
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + "/v1/healthz", nil
}

func checkHealth(url string) error {
	hc := &http.Client{Timeout: 5 * time.Second}
	res, err := hc.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", res.StatusCode)
	}
	return nil
}

// healthcheck backs `nebla-server healthcheck` for container healthchecks; it returns the exit code.
func healthcheck() int {
	url, err := healthURL(os.Getenv("PANEL_LISTEN"))
	if err == nil {
		err = checkHealth(url)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	return 0
}
