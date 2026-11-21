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

package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestEndpointChangeRequiresSecret(t *testing.T) {
	var hits atomic.Int32
	atk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(500)
	}))
	defer atk.Close()

	cases := map[string]func(p *catalog.PVEInput){
		"url":            func(p *catalog.PVEInput) { p.URL = atk.URL },
		"token_id":       func(p *catalog.PVEInput) { p.TokenID = "attacker@pve!tok" },
		"ca_fingerprint": func(p *catalog.PVEInput) { p.CAFingerprint = strings.Repeat("AB:", 31) + "AB" },
		"insecure_tls":   func(p *catalog.PVEInput) { p.InsecureTLS = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			svc, pv, _ := setup(t)
			loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
			require.NoError(t, err)

			up := testutil.LocationInput(pv, "eu-1")
			up.PVE.TokenSecret = nil
			mutate(&up.PVE)
			_, err = svc.UpdateLocation(ctx, loc.ID, up)
			requireCode(t, err, 422, "invalid_input")
			require.Zero(t, hits.Load())
		})
	}
}

func TestFingerprintFormatIsNotAChange(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)

	up := testutil.LocationInput(pv, "eu-1")
	up.PVE.TokenSecret = nil
	up.PVE.CAFingerprint = strings.ToLower(strings.ReplaceAll(up.PVE.CAFingerprint, ":", " "))
	_, err = svc.UpdateLocation(ctx, loc.ID, up)
	require.NoError(t, err)
}

func TestRenameKeepsSecret(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)

	up := testutil.LocationInput(pv, "eu-renamed")
	up.Description = "new description"
	up.PVE.TokenSecret = nil
	got, err := svc.UpdateLocation(ctx, loc.ID, up)
	require.NoError(t, err)
	require.Equal(t, "eu-renamed", got.Name)
	require.Equal(t, "new description", got.Description)
}
