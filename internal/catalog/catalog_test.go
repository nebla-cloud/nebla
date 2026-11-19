package catalog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/pve/fake"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T, hooks ...catalog.Hooks) (*catalog.Service, *fake.Server, *ent.Client) {
	t.Helper()
	pv := fake.New(t)
	testutil.SeedPVE(pv)
	var h catalog.Hooks
	if len(hooks) > 0 {
		h = hooks[0]
	}
	svc, client := testutil.NewCatalog(t, h)
	return svc, pv, client
}

func requireCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e apierr.E
	require.True(t, errors.As(err, &e), "want apierr, got %v", err)
	require.Equal(t, status, e.Status, e.Msg)
	require.Equal(t, code, e.Code, e.Msg)
}

func TestCreateLocationDiscoversNodes(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	require.Len(t, loc.Nodes, 2)
	require.Equal(t, "pve1", loc.Nodes[0].Name)
	require.Equal(t, 16, loc.Nodes[0].MaxCPU)
	require.Equal(t, int64(65536)<<20, loc.Nodes[0].MaxMem)
	require.Equal(t, "online", loc.Nodes[0].Status)
	require.True(t, loc.Nodes[0].Enabled)
	require.True(t, loc.HasSecret)

	nodes, err := svc.Nodes(ctx, loc.ID)
	require.NoError(t, err)
	require.Len(t, nodes, 2)

	_, err = svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	requireCode(t, err, 409, "uniqueness_error")
}

func TestCreateLocationBadToken(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	pv.FailNext("GET /version", 401)
	_, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	requireCode(t, err, 422, "pve_unreachable")

	in := testutil.LocationInput(pv, "eu-1")
	in.PVE.TokenSecret = new("wrong")
	_, err = svc.CreateLocation(ctx, in)
	requireCode(t, err, 422, "pve_unreachable")
}

func TestCreateLocationValidatesStorages(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	in := testutil.LocationInput(pv, "eu-1")
	in.DiskStorage = "missing"
	_, err := svc.CreateLocation(ctx, in)
	requireCode(t, err, 422, "invalid_input")

	in = testutil.LocationInput(pv, "eu-1")
	in.ISOStorage = "local-lvm"
	_, err = svc.CreateLocation(ctx, in)
	requireCode(t, err, 422, "invalid_input")

	in = testutil.LocationInput(pv, "eu-1")
	in.DiskStorage = "local"
	_, err = svc.CreateLocation(ctx, in)
	requireCode(t, err, 422, "invalid_input")
}

func TestTokenSecretEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	svc, pv, client := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)

	rows, err := client.QueryContext(ctx, "SELECT token_secret FROM locations WHERE id = $1", loc.ID)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var stored []byte
	require.NoError(t, rows.Scan(&stored))
	require.NotEmpty(t, stored)
	require.NotContains(t, string(stored), pv.Conn().Secret)

	c, err := svc.ProxmoxFor(ctx, loc.ID)
	require.NoError(t, err)
	_, err = c.Version(ctx)
	require.NoError(t, err)

	_, err = svc.ProxmoxFor(ctx, loc.ID+100)
	requireCode(t, err, 404, "not_found")
}

func TestUpdateLocationKeepsSecret(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)

	in := testutil.LocationInput(pv, "eu-1")
	in.PVE.TokenSecret = nil
	in.Description = "Primary site"
	loc, err = svc.UpdateLocation(ctx, loc.ID, in)
	require.NoError(t, err)
	require.Equal(t, "Primary site", loc.Description)
	c, err := svc.ProxmoxFor(ctx, loc.ID)
	require.NoError(t, err)
	_, err = c.Version(ctx)
	require.NoError(t, err)

	in.PVE.TokenSecret = new("wrong")
	_, err = svc.UpdateLocation(ctx, loc.ID, in)
	requireCode(t, err, 422, "pve_unreachable")
}

func TestSyncNodesDisablesMissing(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)

	pv.AddNode("pve3", 4, 8192)
	pv.RemoveNode("pve2")
	pv.SetNodeLoad("pve1", 0.25, 1024)
	loc, err = svc.SyncNodes(ctx, loc.ID)
	require.NoError(t, err)
	require.Len(t, loc.Nodes, 3)
	byName := map[string]catalog.Node{}
	for _, n := range loc.Nodes {
		byName[n.Name] = n
	}
	require.True(t, byName["pve1"].Enabled)
	require.InDelta(t, 0.25, byName["pve1"].CPU, 0.001)
	require.Equal(t, int64(1024)<<20, byName["pve1"].Mem)
	require.False(t, byName["pve2"].Enabled)
	require.True(t, byName["pve3"].Enabled)

	n, err := svc.UpdateNode(ctx, loc.ID, byName["pve1"].ID, catalog.NodeUpdate{DiskStorageOverride: new("local-lvm")})
	require.NoError(t, err)
	require.Equal(t, "local-lvm", n.DiskStorageOverride)
	_, err = svc.UpdateNode(ctx, loc.ID, byName["pve1"].ID, catalog.NodeUpdate{DiskStorageOverride: new("local")})
	requireCode(t, err, 422, "invalid_input")
}

func TestDeleteLocationInUse(t *testing.T) {
	ctx := context.Background()
	inUse := false
	svc, pv, _ := setup(t, catalog.Hooks{LocationInUse: func(context.Context, *ent.Client, int64) (bool, error) { return inUse, nil }})
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	img, err := svc.CreateImage(ctx, catalog.ImageInput{Name: "ubuntu-24.04", OSFlavor: "ubuntu", OSVersion: "24.04",
		Templates: []catalog.TemplateInput{{LocationID: loc.ID, VMID: 9000}}})
	require.NoError(t, err)

	requireCode(t, svc.DeleteLocation(ctx, loc.ID), 409, "location_in_use")
	require.NoError(t, svc.DeleteImage(ctx, img.ID))
	st, err := svc.CreateServerType(ctx, catalog.ServerTypeInput{Name: "small", Cores: 1, MemoryMB: 1024, DiskGB: 10, Locations: []int64{loc.ID}})
	require.NoError(t, err)
	requireCode(t, svc.DeleteLocation(ctx, loc.ID), 409, "location_in_use")
	require.NoError(t, svc.DeleteServerType(ctx, st.ID))
	inUse = true
	requireCode(t, svc.DeleteLocation(ctx, loc.ID), 409, "location_in_use")
	inUse = false
	require.NoError(t, svc.DeleteLocation(ctx, loc.ID))
	_, err = svc.GetLocation(ctx, loc.ID)
	requireCode(t, err, 404, "not_found")
}

func TestImageLocalTemplateNeedsNode(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	pv.AddTemplate("pve2", 9001, "local-lvm")

	in := catalog.ImageInput{Name: "debian-12", OSFlavor: "debian", OSVersion: "12",
		Templates: []catalog.TemplateInput{{LocationID: loc.ID, VMID: 9001}}}
	_, err = svc.CreateImage(ctx, in)
	requireCode(t, err, 422, "template_not_shared")

	in.Templates[0].Node = "pve2"
	img, err := svc.CreateImage(ctx, in)
	require.NoError(t, err)
	tmpls, err := svc.TemplatesFor(ctx, img.ID, loc.ID)
	require.NoError(t, err)
	require.Equal(t, []catalog.Template{{Node: "pve2", VMID: 9001}}, tmpls)
}

func TestImageTemplateNotFound(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	pv.AddVM("pve1", 120, "plain")

	for _, tc := range []catalog.TemplateInput{
		{LocationID: loc.ID, VMID: 4242},
		{LocationID: loc.ID, VMID: 120},
		{LocationID: loc.ID, VMID: 120, Node: "pve1"},
		{LocationID: loc.ID, VMID: 9000, Node: "pve2"},
	} {
		_, err = svc.CreateImage(ctx, catalog.ImageInput{Name: "x", OSFlavor: "x", OSVersion: "1", Templates: []catalog.TemplateInput{tc}})
		requireCode(t, err, 422, "template_not_found")
	}
	_, err = svc.CreateImage(ctx, catalog.ImageInput{Name: "x", OSFlavor: "x", OSVersion: "1",
		Templates: []catalog.TemplateInput{{LocationID: loc.ID + 99, VMID: 9000}}})
	requireCode(t, err, 422, "invalid_input")
}

func TestImagesAndTypes(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	a, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	b, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-2"))
	require.NoError(t, err)

	img, err := svc.CreateImage(ctx, catalog.ImageInput{Name: "ubuntu-24.04", OSFlavor: "ubuntu", OSVersion: "24.04",
		Templates: []catalog.TemplateInput{{LocationID: a.ID, VMID: 9000}}})
	require.NoError(t, err)
	_, err = svc.CreateImage(ctx, catalog.ImageInput{Name: "debian-12", OSFlavor: "debian", OSVersion: "12",
		Templates: []catalog.TemplateInput{{LocationID: b.ID, VMID: 9000}}})
	require.NoError(t, err)

	all, total, err := svc.ListImages(ctx, 0, 1, 25)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, all, 2)
	inA, total, err := svc.ListImages(ctx, a.ID, 1, 25)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, img.ID, inA[0].ID)
	require.Equal(t, []int64{a.ID}, inA[0].Locations())

	none, err := svc.TemplatesFor(ctx, img.ID, b.ID)
	require.NoError(t, err)
	require.Empty(t, none)

	img, err = svc.UpdateImage(ctx, img.ID, catalog.ImageInput{Name: "ubuntu-24.04", OSFlavor: "ubuntu", OSVersion: "24.04",
		Templates: []catalog.TemplateInput{{LocationID: a.ID, VMID: 9000}, {LocationID: b.ID, VMID: 9000}}})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{a.ID, b.ID}, img.Locations())

	_, err = svc.CreateServerType(ctx, catalog.ServerTypeInput{Name: "Bad Name", Cores: 1, MemoryMB: 1024, DiskGB: 10})
	requireCode(t, err, 422, "invalid_input")
	_, err = svc.CreateServerType(ctx, catalog.ServerTypeInput{Name: "small", Cores: 0, MemoryMB: 1024, DiskGB: 10})
	requireCode(t, err, 422, "invalid_input")
	_, err = svc.CreateServerType(ctx, catalog.ServerTypeInput{Name: "small", Cores: 1, MemoryMB: 1024, DiskGB: 10, Locations: []int64{b.ID + 50}})
	requireCode(t, err, 422, "invalid_input")

	st, err := svc.CreateServerType(ctx, catalog.ServerTypeInput{Name: "small", Cores: 1, MemoryMB: 1024, DiskGB: 10, Locations: []int64{a.ID}})
	require.NoError(t, err)
	require.True(t, st.AvailableIn(a.ID))
	require.False(t, st.AvailableIn(b.ID))
	_, err = svc.CreateServerType(ctx, catalog.ServerTypeInput{Name: "small", Cores: 2, MemoryMB: 2048, DiskGB: 20})
	requireCode(t, err, 409, "uniqueness_error")

	st, err = svc.UpdateServerType(ctx, st.ID, catalog.ServerTypeInput{Name: "small", Cores: 1, MemoryMB: 1024, DiskGB: 10, Deprecated: true})
	require.NoError(t, err)
	require.True(t, st.Deprecated)
	require.True(t, st.AvailableIn(b.ID), "no locations means everywhere")
	types, total, err := svc.ListServerTypes(ctx, 1, 25)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.True(t, types[0].Deprecated)

	require.NoError(t, svc.DeleteServerType(ctx, st.ID))
	_, err = svc.GetServerType(ctx, st.ID)
	requireCode(t, err, 404, "not_found")
}
