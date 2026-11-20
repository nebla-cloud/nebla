package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestAdminIPPoolHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	app.SeedPVE()
	admin := app.LoginAdmin()
	loc := admin.CreateLocation("eu-1")
	body := func(gw string) map[string]any {
		return map[string]any{"location_id": loc, "family": "ipv4", "bridge": "vmbr0", "vlan_tag": 20, "cidr": "10.0.0.0/24",
			"gateway": gw, "dns": []string{"1.1.1.1"}, "range_start": "10.0.0.10", "range_end": "10.0.0.20"}
	}

	res := admin.Send(http.MethodPost, "/v1/admin/ip_pools", body("192.168.0.1"))
	require.Equal(t, 422, res.StatusCode)
	require.Equal(t, "invalid_input", errCode(t, app, res))

	res = admin.Send(http.MethodPost, "/v1/admin/ip_pools", body("10.0.0.1"))
	require.Equal(t, 201, res.StatusCode)
	pool := decode[struct {
		ID        int64  `json:"id"`
		CIDR      string `json:"cidr"`
		VlanTag   int    `json:"vlan_tag"`
		Allocated int    `json:"allocated"`
	}](t, app, res)
	require.Equal(t, "10.0.0.0/24", pool.CIDR)
	require.Equal(t, 20, pool.VlanTag)

	res = admin.Send(http.MethodPost, "/v1/admin/ip_pools", body("10.0.0.1"))
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "pool_overlap", errCode(t, app, res))

	res = admin.Get("/v1/admin/ip_pools?location_id=" + itoa(loc) + "&per_page=1")
	require.Equal(t, 200, res.StatusCode)
	require.Equal(t, 1, decode[page](t, app, res).Meta.Pagination.TotalEntries)

	res = admin.Send(http.MethodDelete, "/v1/admin/locations/"+itoa(loc), nil)
	require.Equal(t, 409, res.StatusCode)
	require.Equal(t, "location_in_use", errCode(t, app, res))

	app.CreateUser("u@example.com", false)
	res = app.Login("u@example.com").Get("/v1/admin/ip_pools")
	require.Equal(t, 403, res.StatusCode)
	res.Body.Close()

	res = admin.Send(http.MethodDelete, "/v1/admin/ip_pools/"+itoa(pool.ID), nil)
	require.Equal(t, 204, res.StatusCode)
	res.Body.Close()
	res = admin.Get("/v1/admin/ip_pools/" + itoa(pool.ID))
	require.Equal(t, 404, res.StatusCode)
	res.Body.Close()
}
