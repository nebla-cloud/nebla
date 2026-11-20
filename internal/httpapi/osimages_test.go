package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/osimages"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type catalogLocation struct {
	LocationID int64  `json:"location_id"`
	Available  bool   `json:"available"`
	Reason     string `json:"reason"`
	State      string `json:"state"`
	Nodes      []struct {
		Node   string `json:"node"`
		Status string `json:"status"`
		Vmid   *int   `json:"vmid"`
		File   string `json:"file"`
		Error  string `json:"error"`
	} `json:"nodes"`
}

// catalogApp runs the API over one fake catalog entry served by a fake upstream.
func catalogApp(t *testing.T) *testutil.App {
	t.Helper()
	sum := strings.Repeat("ab12", 32)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debian/SHA512SUMS" {
			fmt.Fprintf(w, "%s  debian.qcow2\n", sum)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(up.Close)
	entries := []osimages.Entry{{Key: "debian-13", Description: "Debian 13", OSFlavor: "debian", OSVersion: "13",
		Dirs: []string{up.URL + "/debian/"}, ChecksumName: "SHA512SUMS", File: regexp.MustCompile(`^debian\.qcow2$`),
		Algorithm: "sha512", Format: "qcow2"}}
	return testutil.NewApp(t, testutil.WithOSImages(entries, up.Client()))
}

func TestCatalogAdminOnly(t *testing.T) {
	app := catalogApp(t)
	app.CreateUser("u@example.com", false)
	c := app.Login("u@example.com")
	require.Equal(t, 403, c.Get("/v1/admin/catalog").StatusCode)
	for _, op := range []string{"enable", "refresh", "disable"} {
		res := c.Send(http.MethodPost, "/v1/admin/catalog/debian-13/locations/1/"+op, nil)
		require.Equal(t, 403, res.StatusCode, op)
	}
}

func TestCatalogEnableFlow(t *testing.T) {
	app := catalogApp(t)
	app.SeedPVE()
	for _, n := range []string{"pve1", "pve2"} {
		app.PVE.AddStorage(n, "imp", []string{"import", "iso"}, false)
	}
	admin := app.LoginAdmin()
	conn := app.PVE.Conn()
	res := admin.Send(http.MethodPost, "/v1/admin/locations", map[string]any{
		"name": "eu-1", "disk_storage": "ceph", "iso_storage": "local", "import_storage": "imp",
		"pve": map[string]any{"url": conn.BaseURL, "token_id": conn.TokenID, "token_secret": conn.Secret, "ca_fingerprint": conn.CAFingerprint}})
	require.Equal(t, 201, res.StatusCode)
	loc := decode[struct {
		ID int64 `json:"id"`
	}](t, app, res).ID
	base := "/v1/admin/catalog/debian-13/locations/" + itoa(loc)

	res = admin.Send(http.MethodPost, base+"/enable", nil)
	require.Equal(t, 200, res.StatusCode)
	st := decode[struct {
		Location catalogLocation `json:"location"`
	}](t, app, res)
	require.Equal(t, "building", st.Location.State)
	require.Equal(t, loc, st.Location.LocationID)

	var cur catalogLocation
	deadline := time.Now().Add(30 * time.Second)
	for {
		res = admin.Get("/v1/admin/catalog")
		require.Equal(t, 200, res.StatusCode)
		list := decode[struct {
			Catalog []struct {
				Key       string            `json:"key"`
				Retired   bool              `json:"retired"`
				Locations []catalogLocation `json:"locations"`
			} `json:"catalog"`
		}](t, app, res)
		require.Len(t, list.Catalog, 1)
		require.Equal(t, "debian-13", list.Catalog[0].Key)
		cur = list.Catalog[0].Locations[0]
		if cur.State != "building" {
			break
		}
		require.True(t, time.Now().Before(deadline), "still building")
		time.Sleep(50 * time.Millisecond)
	}
	require.Equal(t, "ready", cur.State, fmt.Sprintf("%+v", cur.Nodes))

	app.CreateUser("u@example.com", false)
	u := app.Login("u@example.com")
	c := app.TokenClient(u.CreateToken(u.CreateProject("Web"), "read"))
	listed := func() bool {
		res := c.Get("/v1/images?location=" + itoa(loc))
		require.Equal(t, 200, res.StatusCode)
		imgs := decode[struct {
			Images []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"images"`
		}](t, app, res)
		for _, i := range imgs.Images {
			if i.Name == "debian-13" {
				return true
			}
		}
		return false
	}
	require.True(t, listed())

	res = admin.Get("/v1/admin/images")
	imgID := decode[struct {
		Images []struct {
			ID         int64  `json:"id"`
			CatalogKey string `json:"catalog_key"`
		} `json:"images"`
	}](t, app, res).Images[0].ID
	res = admin.Send(http.MethodPut, "/v1/admin/images/"+itoa(imgID), map[string]any{"name": "x", "os_flavor": "debian", "os_version": "13",
		"templates": []map[string]any{{"location_id": loc, "vmid": 1}}})
	require.Equal(t, 409, res.StatusCode)
	require.Contains(t, app.Body(res), "catalog_managed")

	res = admin.Send(http.MethodPost, base+"/disable", nil)
	require.Equal(t, 200, res.StatusCode)
	require.False(t, listed())

	res = admin.Send(http.MethodPost, "/v1/admin/catalog/nope/locations/"+itoa(loc)+"/enable", nil)
	require.Equal(t, 404, res.StatusCode)
}
