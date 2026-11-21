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

package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestManifestDefaults(t *testing.T) {
	res := testutil.NewApp(t).Get("/v1/ui/manifest")
	require.Equal(t, 200, res.StatusCode)
	require.JSONEq(t, `{"branding":{"product_name":"nebla","logo_url":"","primary_color":""},"admin_sections":[],"setup_required":true,"signup_enabled":false,"passkey_origin":null}`, body(res))
}

func TestManifestSetupRequiredFalseAfterSetup(t *testing.T) {
	app := testutil.NewApp(t)
	app.LoginAdmin()
	res := app.Get("/v1/ui/manifest")
	require.Equal(t, 200, res.StatusCode)
	require.JSONEq(t, `{"branding":{"product_name":"nebla","logo_url":"","primary_color":""},"admin_sections":[],"setup_required":false,"signup_enabled":false,"passkey_origin":null}`, body(res))
}

func TestSPAFallbackOnlyForGetAndHead(t *testing.T) {
	app := testutil.NewApp(t)
	req, err := http.NewRequest(http.MethodPost, app.URL+"/servers/12", nil)
	require.NoError(t, err)
	res := app.Do(req)
	require.Equal(t, 404, res.StatusCode)
	require.JSONEq(t, `{"error":{"code":"not_found","message":"not found"}}`, body(res))

	req, err = http.NewRequest(http.MethodHead, app.URL+"/servers/12", nil)
	require.NoError(t, err)
	require.Equal(t, 200, app.Do(req).StatusCode)
}
