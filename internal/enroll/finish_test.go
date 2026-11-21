package enroll_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/enroll"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/locationenrollment"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/testutil"
)

const aclImp = "pveum acl delete /storage/imp --users nebla@pve --roles NeblaISO"

func (e *env) finishInput() enroll.FinishInput {
	return enroll.FinishInput{URL: e.pv.Conn().BaseURL, DiskStorage: "ceph", ISOStorage: "local"}
}

// reported creates an enrollment for in, reports it and lets the fake accept its token.
func (e *env) reported(t *testing.T, in enroll.CreateInput) enroll.Created {
	t.Helper()
	c, err := e.svc.Create(context.Background(), in, e.userID)
	require.NoError(t, err)
	e.report(t, c)
	e.pv.AddToken("nebla@pve!"+c.TokenName, testSecret)
	return c
}

func TestFinishCreatesLocation(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.reported(t, enroll.CreateInput{Name: "home", Description: "Home lab"})
	f, err := e.svc.Finish(ctx, c.ID, e.finishInput())
	require.NoError(t, err)
	require.Equal(t, "home", f.Location.Name)
	require.Equal(t, "Home lab", f.Location.Description)
	require.Equal(t, "nebla@pve!"+c.TokenName, f.Location.TokenID)
	require.Equal(t, pve.NormFP(e.pv.Conn().CAFingerprint), pve.NormFP(f.Location.CAFingerprint))
	require.False(t, f.Location.InsecureTLS)
	require.Equal(t, "ceph", f.Location.DiskStorage)
	require.Equal(t, "local", f.Location.ISOStorage)
	require.Len(t, f.Location.Nodes, 2)
	require.Equal(t, []string{aclImp}, f.Cleanup)

	_, err = e.ent.LocationEnrollment.Get(ctx, c.ID)
	require.True(t, ent.IsNotFound(err))
	// The stored secret is the reported one: a resync authenticates with it.
	_, err = e.cat.SyncNodes(ctx, f.Location.ID)
	require.NoError(t, err)
}

func TestFinishFingerprintMismatch(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c, err := e.svc.Create(ctx, enroll.CreateInput{Name: "home"}, e.userID)
	require.NoError(t, err)
	require.NoError(t, e.svc.Report(ctx, c.Token, reportJSON(t, c.TokenName, colonFP("AA"))))
	e.pv.AddToken("nebla@pve!"+c.TokenName, testSecret)
	before := e.snap(t, c.ID)

	_, err = e.svc.Finish(ctx, c.ID, e.finishInput())
	requireCode(t, err, 422, "fingerprint_mismatch")
	seen := pve.NormFP(e.pv.Conn().CAFingerprint)
	host := strings.TrimPrefix(e.pv.Conn().BaseURL, "https://")
	require.Contains(t, err.Error(), "the certificate at "+host+" ("+seen+") is not one the node reported")
	require.True(t, before == e.snap(t, c.ID), "row changed")
	n, err := e.ent.Location.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestFinishProbeFailureKeepsRow(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c, err := e.svc.Create(ctx, enroll.CreateInput{Name: "home"}, e.userID)
	require.NoError(t, err)
	raw := reportJSON(t, c.TokenName, e.pv.Conn().CAFingerprint,
		rstorage{"ghost", "images", false}, rstorage{"local", "iso", true})
	require.NoError(t, e.svc.Report(ctx, c.Token, raw))
	e.pv.AddToken("nebla@pve!"+c.TokenName, testSecret)
	before := e.snap(t, c.ID)

	in := e.finishInput()
	in.DiskStorage = "ghost"
	_, err = e.svc.Finish(ctx, c.ID, in)
	requireCode(t, err, 422, "invalid_input")
	require.Contains(t, err.Error(), "ghost")
	require.True(t, before == e.snap(t, c.ID), "row changed")
}

func TestFinishValidates(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	waiting := e.create(t, "lab")
	_, err := e.svc.Finish(ctx, waiting.ID, e.finishInput())
	requireCode(t, err, 409, "enrollment_state")

	c := e.reported(t, enroll.CreateInput{Name: "home"})
	for _, in := range []enroll.FinishInput{
		{URL: e.pv.Conn().BaseURL, DiskStorage: "local", ISOStorage: "local"},
		{URL: e.pv.Conn().BaseURL, DiskStorage: "ceph", ISOStorage: "ceph"},
		{URL: e.pv.Conn().BaseURL, DiskStorage: "ceph", ISOStorage: "local", ImportStorage: "ceph"},
		{URL: e.pv.Conn().BaseURL, DiskStorage: "ceph", ISOStorage: "imp"},
		{URL: "not a url", DiskStorage: "ceph", ISOStorage: "local"},
	} {
		_, err = e.svc.Finish(ctx, c.ID, in)
		requireCode(t, err, 422, "invalid_input")
	}
	require.Equal(t, locationenrollment.StateReported, e.row(t, c.ID).State)

	e.advance(25 * time.Hour)
	_, err = e.svc.Finish(ctx, c.ID, e.finishInput())
	requireCode(t, err, 410, "enrollment_expired")
}

func TestFinishImportStorage(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.pv.AddStorage("pve1", "imp", []string{"import"}, false)
	c := e.reported(t, enroll.CreateInput{Name: "home"})
	in := e.finishInput()
	in.ImportStorage = "imp"
	f, err := e.svc.Finish(ctx, c.ID, in)
	require.NoError(t, err)
	require.Equal(t, "imp", f.Location.ImportStorage)
	require.Empty(t, f.Cleanup)
}

func TestFinishRerunSwitchesToken(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	loc, err := e.cat.CreateLocation(ctx, testutil.LocationInput(e.pv, "eu-1"))
	require.NoError(t, err)
	require.Equal(t, "root@pam!nebla", loc.TokenID)
	c := e.reported(t, enroll.CreateInput{LocationID: &loc.ID})
	f, err := e.svc.Finish(ctx, c.ID, e.finishInput())
	require.NoError(t, err)
	require.Equal(t, loc.ID, f.Location.ID)
	require.Equal(t, "eu-1", f.Location.Name)
	require.Equal(t, "nebla@pve!"+c.TokenName, f.Location.TokenID)
	require.False(t, f.Location.InsecureTLS)
	require.Equal(t, []string{aclImp}, f.Cleanup)
	_, err = e.ent.LocationEnrollment.Get(ctx, c.ID)
	require.True(t, ent.IsNotFound(err))

	e.pv.AddToken("nebla@pve!panel", "panel-secret")
	in := testutil.LocationInput(e.pv, "eu-2")
	in.Description = "second"
	in.PVE.TokenID, in.PVE.TokenSecret = "nebla@pve!panel", new("panel-secret")
	loc2, err := e.cat.CreateLocation(ctx, in)
	require.NoError(t, err)
	c2 := e.reported(t, enroll.CreateInput{LocationID: &loc2.ID})
	f2, err := e.svc.Finish(ctx, c2.ID, e.finishInput())
	require.NoError(t, err)
	require.Equal(t, "second", f2.Location.Description)
	require.Equal(t, []string{aclImp, "pveum user token remove nebla@pve panel"}, f2.Cleanup)
	got, err := e.cat.GetLocation(ctx, loc2.ID)
	require.NoError(t, err)
	require.Equal(t, "nebla@pve!"+c2.TokenName, got.TokenID)
	require.False(t, got.InsecureTLS)
}

// reportedWith is reported with a report of the given storages.
func (e *env) reportedWith(t *testing.T, in enroll.CreateInput, storages ...rstorage) enroll.Created {
	t.Helper()
	c, err := e.svc.Create(context.Background(), in, e.userID)
	require.NoError(t, err)
	require.NoError(t, e.svc.Report(context.Background(), c.Token, reportJSON(t, c.TokenName, e.pv.Conn().CAFingerprint, storages...)))
	e.pv.AddToken("nebla@pve!"+c.TokenName, testSecret)
	return c
}

var withVolume = []rstorage{{"ceph", "images", true}, {"local", "iso,vztmpl", true}, {"imp", "import", true}, {"local-lvm", "images,rootdir", true}}

func TestFinishRequiresNeblaISOOnVolumeStorage(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.reportedWith(t, enroll.CreateInput{Name: "home"},
		rstorage{"ceph", "images", true}, rstorage{"local", "iso,vztmpl", true}, rstorage{"local-lvm", "images,rootdir", false})
	in := e.finishInput()
	in.VolumeStorage = new("local-lvm")
	_, err := e.svc.Finish(ctx, c.ID, in)
	requireCode(t, err, 422, "invalid_input")
	require.Contains(t, err.Error(), "volume storage \"local-lvm\"")

	c = e.reportedWith(t, enroll.CreateInput{Name: "home2"}, withVolume...)
	f, err := e.svc.Finish(ctx, c.ID, in)
	require.NoError(t, err)
	require.Equal(t, "local-lvm", f.Location.VolumeStorage)
	require.True(t, f.Location.VolumeStorageOK)
	// ceph is the disk storage only, so its grant can go; local-lvm's stays.
	require.ElementsMatch(t, []string{aclImp, "pveum acl delete /storage/ceph --users nebla@pve --roles NeblaISO"}, f.Cleanup)
}

func TestFinishDefaultVolumeStorageUsesDiskStorages(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.reportedWith(t, enroll.CreateInput{Name: "home"}, rstorage{"ceph", "images", false}, rstorage{"local", "iso,vztmpl", true})
	_, err := e.svc.Finish(ctx, c.ID, e.finishInput())
	requireCode(t, err, 422, "invalid_input")
	require.Contains(t, err.Error(), "ceph")

	require.NoError(t, e.ent.LocationEnrollment.DeleteOneID(c.ID).Exec(ctx))
	c = e.reportedWith(t, enroll.CreateInput{Name: "home"}, withVolume...)
	f, err := e.svc.Finish(ctx, c.ID, e.finishInput())
	require.NoError(t, err)
	require.True(t, f.Location.VolumeStorageOK)
	require.ElementsMatch(t, []string{aclImp, "pveum acl delete /storage/local-lvm --users nebla@pve --roles NeblaISO"}, f.Cleanup)

	// A re-run checks each node's override too and keeps its grant.
	_, err = e.cat.UpdateNode(ctx, f.Location.ID, f.Location.Nodes[1].ID, catalog.NodeUpdate{DiskStorageOverride: new("local-lvm")})
	require.NoError(t, err)
	c = e.reportedWith(t, enroll.CreateInput{LocationID: &f.Location.ID}, rstorage{"ceph", "images", true}, rstorage{"local", "iso,vztmpl", true},
		rstorage{"local-lvm", "images,rootdir", false})
	_, err = e.svc.Finish(ctx, c.ID, e.finishInput())
	requireCode(t, err, 422, "invalid_input")
	require.Contains(t, err.Error(), "local-lvm")
	require.NoError(t, e.ent.LocationEnrollment.DeleteOneID(c.ID).Exec(ctx))
	c = e.reportedWith(t, enroll.CreateInput{LocationID: &f.Location.ID}, withVolume...)
	f, err = e.svc.Finish(ctx, c.ID, e.finishInput())
	require.NoError(t, err)
	require.Equal(t, aclImp, f.Cleanup[0])
	require.NotContains(t, strings.Join(f.Cleanup, "\n"), "/storage/local-lvm")
}

func TestFinishStoresVolumeStorage(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.reportedWith(t, enroll.CreateInput{Name: "home"}, withVolume...)
	in := e.finishInput()
	in.VolumeStorage = new(" local-lvm ")
	f, err := e.svc.Finish(ctx, c.ID, in)
	require.NoError(t, err)
	require.Equal(t, "local-lvm", f.Location.VolumeStorage)

	// A re-run without volume_storage keeps it.
	c2 := e.reportedWith(t, enroll.CreateInput{LocationID: &f.Location.ID}, withVolume...)
	f2, err := e.svc.Finish(ctx, c2.ID, e.finishInput())
	require.NoError(t, err)
	require.Equal(t, "local-lvm", f2.Location.VolumeStorage)

	c3 := e.reported(t, enroll.CreateInput{LocationID: &f.Location.ID})
	in.VolumeStorage = new("local")
	_, err = e.svc.Finish(ctx, c3.ID, in)
	requireCode(t, err, 422, "invalid_input")
}
