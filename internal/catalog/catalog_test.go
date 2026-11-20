package catalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/testutil"
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

func TestSyncNodesTracksPresence(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	byName := func(l catalog.Location) map[string]catalog.Node {
		m := map[string]catalog.Node{}
		for _, n := range l.Nodes {
			m[n.Name] = n
		}
		return m
	}
	nodes := byName(loc)
	_, err = svc.UpdateNode(ctx, loc.ID, nodes["pve1"].ID, catalog.NodeUpdate{Enabled: new(false)})
	require.NoError(t, err)

	pv.AddNode("pve3", 4, 8192)
	pv.RemoveNode("pve2")
	pv.SetNodeLoad("pve1", 0.25, 1024)
	loc, err = svc.SyncNodes(ctx, loc.ID)
	require.NoError(t, err)
	require.Len(t, loc.Nodes, 3)
	nodes = byName(loc)
	require.InDelta(t, 0.25, nodes["pve1"].CPU, 0.001)
	require.Equal(t, int64(1024)<<20, nodes["pve1"].Mem)
	require.False(t, nodes["pve1"].Enabled, "sync keeps admin intent")
	require.Equal(t, catalog.NodeMissing, nodes["pve2"].Status)
	require.True(t, nodes["pve2"].Enabled, "presence lives in status only")
	require.True(t, nodes["pve3"].Enabled)
	require.Equal(t, "online", nodes["pve3"].Status)

	pv.AddNode("pve2", 16, 65536)
	loc, err = svc.SyncNodes(ctx, loc.ID)
	require.NoError(t, err)
	nodes = byName(loc)
	require.Equal(t, "online", nodes["pve2"].Status, "reappearing node gets its real status")
	require.True(t, nodes["pve2"].Enabled)
	require.False(t, nodes["pve1"].Enabled)

	n, err := svc.UpdateNode(ctx, loc.ID, nodes["pve1"].ID, catalog.NodeUpdate{DiskStorageOverride: new("local-lvm")})
	require.NoError(t, err)
	require.Equal(t, "local-lvm", n.DiskStorageOverride)
	_, err = svc.UpdateNode(ctx, loc.ID, nodes["pve1"].ID, catalog.NodeUpdate{DiskStorageOverride: new("local")})
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

func TestDeleteLocationRefusedWhileCatalogBuildExists(t *testing.T) {
	ctx := context.Background()
	svc, pv, client := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	b, err := client.CatalogBuild.Create().SetCatalogKey("debian-13").SetLocationID(loc.ID).Save(ctx)
	require.NoError(t, err)

	requireCode(t, svc.DeleteLocation(ctx, loc.ID), 409, "location_in_use")
	require.NoError(t, client.CatalogBuild.DeleteOneID(b.ID).Exec(ctx))
	require.NoError(t, svc.DeleteLocation(ctx, loc.ID))
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

func TestImageTemplateAllVolumesShared(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	c := pve.New(pv.Conn())
	img := func(name string, vmid int, node string) error {
		_, err := svc.CreateImage(ctx, catalog.ImageInput{Name: name, OSFlavor: "ubuntu", OSVersion: "24.04",
			Templates: []catalog.TemplateInput{{LocationID: loc.ID, VMID: vmid, Node: node}}})
		return err
	}

	pv.AddTemplate("pve1", 9100, "ceph")
	require.NoError(t, c.SetConfig(ctx, "pve1", 9100, map[string]string{
		"efidisk0": "ceph:base-9100-disk-1,efitype=4m,size=4M",
		"ide2":     "ceph:vm-9100-cloudinit,media=cdrom",
		"ide0":     "local:iso/ubuntu.iso,media=cdrom",
		"sata1":    "none,media=cdrom",
		"unused0":  "ceph:base-9100-disk-2",
	}))
	require.NoError(t, img("all-shared", 9100, ""), "ISOs and empty drives are ignored")

	pv.AddTemplate("pve1", 9101, "ceph")
	require.NoError(t, c.SetConfig(ctx, "pve1", 9101, map[string]string{"efidisk0": "local-lvm:base-9101-disk-1,efitype=4m,size=4M"}))
	err = img("uefi-local", 9101, "")
	requireCode(t, err, 422, "template_not_shared")
	require.Contains(t, err.Error(), "efidisk0")
	require.NoError(t, img("uefi-local", 9101, "pve1"), "node-pinned templates may use local storage")

	pv.AddTemplate("pve1", 9102, "ceph")
	require.NoError(t, c.SetConfig(ctx, "pve1", 9102, map[string]string{"ide2": "local-lvm:vm-9102-cloudinit,media=cdrom"}))
	err = img("ci-local", 9102, "")
	requireCode(t, err, 422, "template_not_shared")
	require.Contains(t, err.Error(), "ide2")

	pv.AddTemplate("pve1", 9103, "ceph")
	require.NoError(t, c.SetConfig(ctx, "pve1", 9103, map[string]string{"delete": "scsi0", "ide2": "local:iso/x.iso,media=cdrom"}))
	requireCode(t, img("diskless", 9103, ""), 422, "template_not_found")
	requireCode(t, img("diskless", 9103, "pve1"), 422, "template_not_found")
}

func TestBootDisk(t *testing.T) {
	for _, tc := range []struct {
		cfg  map[string]string
		want string
	}{
		{map[string]string{"scsi0": "ceph:d0", "virtio0": "ceph:d1", "boot": "order=virtio0;scsi0;net0"}, "virtio0"},
		{map[string]string{"scsi0": "ceph:d0", "ide2": "local:iso/x.iso,media=cdrom", "boot": "order=ide2;scsi0"}, "scsi0"},
		{map[string]string{"virtio0": "ceph:d1", "scsi0": "ceph:d0"}, "scsi0"},
		{map[string]string{"sata0": "ceph:d0", "boot": "order=net0"}, "sata0"},
		{map[string]string{"ide2": "ceph:cloudinit,media=cdrom"}, ""},
	} {
		require.Equal(t, tc.want, catalog.BootDisk(tc.cfg), "%v", tc.cfg)
	}
}

// Create locks a location's nodes in name order; a sync must take them in the same order.
func TestSyncNodesLocksInNameOrder(t *testing.T) {
	ctx := context.Background()
	svc, pv, client := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	// pve1 vanishes, so an unordered sync would lock pve2 (upsert) before pve1 (mark missing).
	pv.RemoveNode("pve1")

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Node.Query().Where(node.LocationID(loc.ID), node.Name("pve1")).ForUpdate().Only(ctx)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := svc.SyncNodes(ctx, loc.ID)
		done <- err
	}()
	waitForLockWait(t, client)
	_, err = tx.Node.Query().Where(node.LocationID(loc.ID), node.Name("pve2")).ForUpdate().Only(ctx)
	require.NoError(t, err, "the create-side lock order must not deadlock with sync")
	require.NoError(t, tx.Commit())
	require.NoError(t, <-done)

	nodes, err := svc.Nodes(ctx, loc.ID)
	require.NoError(t, err)
	require.Equal(t, catalog.NodeMissing, nodes[0].Status)
	require.Equal(t, "online", nodes[1].Status)
}

func waitForLockWait(t *testing.T, client *ent.Client) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := client.QueryContext(context.Background(),
			"select count(*) from pg_stat_activity where wait_event_type = 'Lock' and datname = current_database()")
		require.NoError(t, err)
		var n int
		rows.Next()
		require.NoError(t, rows.Scan(&n))
		rows.Close()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync never waited on the node lock")
}

func TestLocationImportStorageProbe(t *testing.T) {
	ctx := context.Background()
	svc, pv, _ := setup(t)
	in := testutil.LocationInput(pv, "eu-1")
	in.ImportStorage = "local"
	_, err := svc.CreateLocation(ctx, in)
	requireCode(t, err, 422, "invalid_input")
	require.Contains(t, err.Error(), `import_storage "local" does not allow the import content type`)

	for _, n := range []string{"pve1", "pve2"} {
		pv.AddStorage(n, "local", []string{"iso", "import"}, false)
	}
	loc, err := svc.CreateLocation(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "local", loc.ImportStorage)

	in = testutil.LocationInput(pv, "eu-2")
	loc, err = svc.CreateLocation(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "", loc.ImportStorage)
}

func TestCatalogImageIsReadOnly(t *testing.T) {
	ctx := context.Background()
	svc, pv, client := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	img, err := client.Image.Create().SetName("debian-13").SetOsFlavor("debian").SetOsVersion("13").
		SetCatalogKey("debian-13").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.ImageTemplate.Create().SetImageID(img.ID).SetLocationID(loc.ID).SetNode("").SetVmid(testutil.TemplateVMID).Exec(ctx))

	_, err = svc.UpdateImage(ctx, img.ID, catalog.ImageInput{Name: "x", OSFlavor: "x", OSVersion: "1",
		Templates: []catalog.TemplateInput{{LocationID: loc.ID, VMID: testutil.TemplateVMID}}})
	requireCode(t, err, 409, "catalog_managed")
	requireCode(t, svc.DeleteImage(ctx, img.ID), 409, "catalog_managed")

	got, err := svc.GetImage(ctx, img.ID)
	require.NoError(t, err)
	require.Equal(t, "debian-13", got.Name)
	require.Equal(t, "debian-13", got.CatalogKey)
	require.Len(t, got.Templates, 1)
}

func TestCreateCatalogImageConflict(t *testing.T) {
	ctx := context.Background()
	svc, pv, client := setup(t)
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	_, err = svc.CreateImage(ctx, catalog.ImageInput{Name: "debian-13", OSFlavor: "debian", OSVersion: "13",
		Templates: []catalog.TemplateInput{{LocationID: loc.ID, VMID: testutil.TemplateVMID}}})
	require.NoError(t, err)
	_, err = svc.CreateCatalogImage(ctx, client, "debian-13", "debian-13", "", "debian", "13")
	requireCode(t, err, 409, "conflict")

	id1, err := svc.CreateCatalogImage(ctx, client, "ubuntu-24.04", "ubuntu-24.04", "d", "ubuntu", "24.04")
	require.NoError(t, err)
	id2, err := svc.CreateCatalogImage(ctx, client, "ubuntu-24.04", "ubuntu-24.04", "d", "ubuntu", "24.04")
	require.NoError(t, err)
	require.Equal(t, id1, id2)
}

func TestDeleteLocationRefusedWhileTemplatesPending(t *testing.T) {
	ctx := context.Background()
	pending := true
	svc, pv, _ := setup(t, catalog.Hooks{TemplatesPending: func(context.Context, *ent.Client, int64) (bool, error) { return pending, nil }})
	loc, err := svc.CreateLocation(ctx, testutil.LocationInput(pv, "eu-1"))
	require.NoError(t, err)
	requireCode(t, svc.DeleteLocation(ctx, loc.ID), 409, "location_in_use")
	pending = false
	require.NoError(t, svc.DeleteLocation(ctx, loc.ID))
}
