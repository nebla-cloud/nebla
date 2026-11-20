package httpapi_test

import (
	"testing"

	"github.com/Hexeption/nebla/internal/testutil"
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
