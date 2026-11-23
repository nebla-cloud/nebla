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
	"errors"
	"testing"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
)

func TestVerifyLeafFingerprint(t *testing.T) {
	conn := fake.New(t).Conn()
	want := pve.NormFP(conn.CAFingerprint)
	got, err := pve.VerifyLeafFingerprint(context.Background(), conn.BaseURL, []string{want})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestVerifyLeafFingerprintBadURL(t *testing.T) {
	if _, err := pve.VerifyLeafFingerprint(context.Background(), "https://127.0.0.1:1", nil); err == nil {
		t.Fatal("want error")
	}
}

func TestVerifyLeafFingerprintRejectsUnreportedCertificate(t *testing.T) {
	conn := fake.New(t).Conn()
	got, err := pve.VerifyLeafFingerprint(context.Background(), conn.BaseURL, nil)
	if !errors.Is(err, pve.ErrFingerprint) {
		t.Fatalf("got error %v, want ErrFingerprint", err)
	}
	if want := pve.NormFP(conn.CAFingerprint); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
