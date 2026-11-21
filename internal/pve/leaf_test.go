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

package pve_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
)

func TestLeafFingerprint(t *testing.T) {
	conn := fake.New(t).Conn()
	got, err := pve.LeafFingerprint(context.Background(), conn.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if want := pve.NormFP(conn.CAFingerprint); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestLeafFingerprintBadURL(t *testing.T) {
	if _, err := pve.LeafFingerprint(context.Background(), "https://127.0.0.1:1"); err == nil {
		t.Fatal("want error")
	}
}
