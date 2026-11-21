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
	"testing"

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestLocationNetworks(t *testing.T) {
	app := testutil.NewApp(t)
	app.SeedPVE()
	admin := app.LoginAdmin()
	loc := admin.CreateLocation("eu-1")
	admin.CreateIPPool(loc, "ipv4", "10.0.0.0/24", "10.0.0.1", "10.0.0.10", "10.0.0.20")
	c := admin.WithProject(admin.CreateProject("p"))

	res := c.Get("/v1/locations")
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, app.Body(res), `"networks":{"ipv4":true,"ipv6":false}`)
}
