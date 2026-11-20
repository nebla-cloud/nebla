package osimages_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/catalogbuild"
	"github.com/Hexeption/nebla/internal/ent/image"
	"github.com/Hexeption/nebla/internal/ent/imagetemplate"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/osimages"
	"github.com/Hexeption/nebla/internal/pve/fake"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

type env struct {
	t    *testing.T
	pve  *fake.Server
	pool *pgxpool.Pool
	sql  *sql.DB
	ent  *ent.Client
	cat  *catalog.Service
	svc  *osimages.Service
	jobs *jobs.Jobs
	up   *upstream
}

// upstream serves checksum files for two fake entries; sums is keyed by entry dir, e.g. "debian".
type upstream struct {
	mu      sync.Mutex
	sums    map[string]string
	entries []osimages.Entry
	client  *http.Client
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{sums: map[string]string{"debian": debianSum, "alma": almaSum}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		defer u.mu.Unlock()
		switch r.URL.Path {
		case "/debian/SHA512SUMS":
			fmt.Fprintf(w, "%s  debian.qcow2\n", u.sums["debian"])
		case "/alma/CHECKSUM":
			fmt.Fprintf(w, "SHA256 (alma.qcow2) = %s\n", u.sums["alma"])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u.client = srv.Client()
	u.entries = []osimages.Entry{
		{Key: "debian-13", Description: "Debian 13", OSFlavor: "debian", OSVersion: "13", Dirs: []string{srv.URL + "/debian/"},
			ChecksumName: "SHA512SUMS", File: regexp.MustCompile(`^debian\.qcow2$`), Algorithm: "sha512", Format: "qcow2"},
		{Key: "alma-10", Description: "AlmaLinux 10", OSFlavor: "alma", OSVersion: "10", Dirs: []string{srv.URL + "/alma/"},
			ChecksumName: "CHECKSUM", File: regexp.MustCompile(`^alma\.qcow2$`), Algorithm: "sha256", Format: "qcow2", CPU: "x86-64-v3"},
	}
	return u
}

var (
	debianSum = strings.Repeat("ab12", 32)
	almaSum   = strings.Repeat("cd34", 16)
)

// newEnv seeds pve1 and pve2 with an import storage "imp" and builds the service over two fake entries.
func newEnv(t *testing.T) *env { return buildEnvWith(t, false, nil) }

// runEnv is newEnv with the build worker registered and the job runtime running.
func runEnv(t *testing.T) *env { return buildEnvWith(t, true, nil) }

// buildEnvWith is the env builder; tweak may adjust the catalog options.
func buildEnvWith(t *testing.T, run bool, tweak func(*catalog.Options)) *env {
	t.Helper()
	e := &env{t: t, up: newUpstream(t)}
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	pv := fake.New(t)
	testutil.SeedPVE(pv)
	for _, n := range []string{"pve1", "pve2"} {
		pv.AddStorage(n, "imp", []string{"import", "iso"}, false)
	}
	opts := testutil.CatalogOptions(catalog.Hooks{})
	if tweak != nil {
		tweak(&opts)
	}
	cat := catalog.New(sqlDB, client, opts)
	svc := osimages.New(osimages.Options{SQL: sqlDB, Ent: client, Catalog: cat, Entries: e.up.entries, HTTP: e.up.client,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	var j *jobs.Jobs
	if run {
		workers := river.NewWorkers()
		osimages.Register(workers, svc)
		j = testutil.StartJobs(t, pool, sqlDB, workers)
	} else {
		var err error
		j, err = jobs.New(pool, sqlDB, nil)
		require.NoError(t, err)
	}
	svc.SetJobs(j)
	e.pve, e.pool, e.sql, e.ent, e.cat, e.svc, e.jobs = pv, pool, sqlDB, client, cat, svc, j
	return e
}

func (e *env) location(name, disk, imp string) int64 {
	e.t.Helper()
	in := testutil.LocationInput(e.pve, name)
	in.DiskStorage, in.ImportStorage = disk, imp
	l, err := e.cat.CreateLocation(context.Background(), in)
	require.NoError(e.t, err)
	return l.ID
}

func (e *env) rows(key string, locID int64) []*ent.CatalogBuild {
	e.t.Helper()
	rows, err := e.ent.CatalogBuild.Query().Where(catalogbuild.CatalogKey(key), catalogbuild.LocationID(locID)).
		Order(catalogbuild.ByNode()).All(context.Background())
	require.NoError(e.t, err)
	return rows
}

func (e *env) jobArgs(kind string) []json.RawMessage {
	e.t.Helper()
	rs, err := e.sql.Query(`select args from river_job where kind = $1 order by id`, kind)
	require.NoError(e.t, err)
	defer rs.Close()
	var out []json.RawMessage
	for rs.Next() {
		var b []byte
		require.NoError(e.t, rs.Scan(&b))
		out = append(out, b)
	}
	require.NoError(e.t, rs.Err())
	return out
}

func (e *env) markReady(id int64, vmid int, nonce string) {
	e.t.Helper()
	require.NoError(e.t, e.ent.CatalogBuild.UpdateOneID(id).SetStatus(catalogbuild.StatusReady).SetVmid(vmid).SetNonce(nonce).
		SetFile("debian.qcow2").SetChecksum("abc").Exec(context.Background()))
}

func requireCode(t *testing.T, err error, status int, code string) apierr.E {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "want apierr, got %v", err)
	require.Equal(t, status, ae.Status)
	require.Equal(t, code, ae.Code)
	return ae
}

func locState(t *testing.T, list []osimages.EntryState, key string, locID int64) osimages.LocationState {
	t.Helper()
	for _, es := range list {
		if es.Key != key {
			continue
		}
		for _, ls := range es.Locations {
			if ls.LocationID == locID {
				return ls
			}
		}
	}
	t.Fatalf("no state for %s in location %d", key, locID)
	return osimages.LocationState{}
}

func TestAvailability(t *testing.T) {
	ctx := context.Background()
	t.Run("no import storage", func(t *testing.T) {
		e := newEnv(t)
		loc := e.location("eu-1", "local-lvm", "")
		list, err := e.svc.List(ctx)
		require.NoError(t, err)
		ls := locState(t, list, "debian-13", loc)
		require.False(t, ls.Available)
		require.Equal(t, "location has no import_storage", ls.Reason)
		require.Equal(t, "unavailable", ls.State)
		_, err = e.svc.Enable(ctx, "debian-13", loc)
		ae := requireCode(t, err, http.StatusUnprocessableEntity, "catalog_unavailable")
		require.Equal(t, "location has no import_storage", ae.Msg)
	})
	t.Run("all nodes below 8.4", func(t *testing.T) {
		e := newEnv(t)
		e.pve.SetNodeVersion("pve1", "8.3.2")
		e.pve.SetNodeVersion("pve2", "8.3.2")
		loc := e.location("eu-1", "local-lvm", "imp")
		list, err := e.svc.List(ctx)
		require.NoError(t, err)
		ls := locState(t, list, "debian-13", loc)
		require.False(t, ls.Available)
		require.Equal(t, "no online node runs Proxmox VE 8.4 or later", ls.Reason)
		_, err = e.svc.Enable(ctx, "debian-13", loc)
		ae := requireCode(t, err, http.StatusUnprocessableEntity, "catalog_unavailable")
		require.Equal(t, "no online node runs Proxmox VE 8.4 or later", ae.Msg)
	})
	t.Run("one node below 8.4", func(t *testing.T) {
		e := newEnv(t)
		e.pve.SetNodeVersion("pve1", "8.3.2")
		loc := e.location("eu-1", "local-lvm", "imp")
		list, err := e.svc.List(ctx)
		require.NoError(t, err)
		require.True(t, locState(t, list, "debian-13", loc).Available)
		_, err = e.svc.Enable(ctx, "debian-13", loc)
		require.NoError(t, err)
		rows := e.rows("debian-13", loc)
		require.Len(t, rows, 1)
		require.Equal(t, "pve2", rows[0].Node)
	})
	t.Run("unreachable", func(t *testing.T) {
		e := newEnv(t)
		loc := e.location("eu-1", "local-lvm", "imp")
		e.pve.SetDown(true)
		list, err := e.svc.List(ctx)
		require.NoError(t, err)
		ls := locState(t, list, "debian-13", loc)
		require.False(t, ls.Available)
		require.Equal(t, "Proxmox is unreachable", ls.Reason)
	})
}

func TestEnableLocalStorageFansOut(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	ls, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	require.Equal(t, "building", ls.State)
	require.True(t, ls.Available)
	require.Len(t, ls.Nodes, 2)
	rows := e.rows("debian-13", loc)
	require.Len(t, rows, 2)
	require.Equal(t, "pve1", rows[0].Node)
	require.Equal(t, "pve2", rows[1].Node)
	for _, r := range rows {
		require.Equal(t, catalogbuild.StatusBuilding, r.Status)
		require.NotNil(t, r.RiverJobID)
	}
	args := e.jobArgs(osimages.KindBuild)
	require.Len(t, args, 2)
	var a osimages.BuildArgs
	require.NoError(t, json.Unmarshal(args[0], &a))
	require.Equal(t, rows[0].ID, a.BuildID)
}

func TestEnableSharedStorageOneRow(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	ls, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	require.Equal(t, "building", ls.State)
	rows := e.rows("debian-13", loc)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].Node)
	require.Len(t, e.jobArgs(osimages.KindBuild), 1)
}

func TestEnableIdempotentFillsGaps(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	before := e.rows("debian-13", loc)
	e.markReady(before[0].ID, 9101, "n1")
	e.markReady(before[1].ID, 9102, "n2")
	before = e.rows("debian-13", loc)

	e.pve.AddNode("pve3", 16, 65536)
	e.pve.AddStorage("pve3", "local-lvm", []string{"images"}, false)
	_, err = e.cat.SyncNodes(ctx, loc)
	require.NoError(t, err)
	list, err := e.svc.List(ctx)
	require.NoError(t, err)
	require.Equal(t, "partial", locState(t, list, "debian-13", loc).State)

	ls, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	require.Equal(t, "building", ls.State)
	after := e.rows("debian-13", loc)
	require.Len(t, after, 3)
	for i := range 2 {
		b, a := before[i], after[i]
		require.Equal(t, []any{b.ID, b.Status, *b.Vmid, *b.Nonce, *b.RiverJobID, b.UpdatedAt},
			[]any{a.ID, a.Status, *a.Vmid, *a.Nonce, *a.RiverJobID, a.UpdatedAt})
	}
	require.Equal(t, "pve3", after[2].Node)
	require.Equal(t, catalogbuild.StatusBuilding, after[2].Status)
	require.Len(t, e.jobArgs(osimages.KindBuild), 3)
}

func TestEnableResetsFailedRows(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	row := e.rows("debian-13", loc)[0]
	require.NoError(t, row.Update().SetStatus(catalogbuild.StatusFailed).SetStep(4).SetErrorCode("x").SetErrorMessage("boom").
		SetNewVmid(9200).SetNewNonce("nn").Exec(ctx))
	list, err := e.svc.List(ctx)
	require.NoError(t, err)
	ls := locState(t, list, "debian-13", loc)
	require.Equal(t, "failed", ls.State)
	require.Equal(t, "boom", ls.Nodes[0].Error)

	_, err = e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	row = e.rows("debian-13", loc)[0]
	require.Equal(t, catalogbuild.StatusBuilding, row.Status)
	require.Zero(t, row.Step)
	require.Nil(t, row.ErrorCode)
	require.Nil(t, row.ErrorMessage)
	require.Nil(t, row.NewVmid)
	require.Nil(t, row.NewNonce)
}

func TestEnableWhileBuildingConflicts(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	_, err = e.svc.Enable(ctx, "debian-13", loc)
	ae := requireCode(t, err, http.StatusConflict, "conflict")
	require.Equal(t, "a build is already running", ae.Msg)
	_, err = e.svc.Refresh(ctx, "debian-13", loc)
	requireCode(t, err, http.StatusConflict, "conflict")
	_, err = e.svc.Disable(ctx, "debian-13", loc)
	requireCode(t, err, http.StatusConflict, "conflict")
}

func TestEnableUnknownKey(t *testing.T) {
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	_, err := e.svc.Enable(context.Background(), "nope-1", loc)
	requireCode(t, err, http.StatusNotFound, "not_found")
}

func TestEnableHandImageNameConflicts(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	require.NoError(t, e.ent.Image.Create().SetName("debian-13").SetOsFlavor("debian").SetOsVersion("13").Exec(ctx))
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	requireCode(t, err, http.StatusConflict, "conflict")
	require.Empty(t, e.rows("debian-13", loc))
}

func TestRefresh(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	_, err := e.svc.Refresh(ctx, "debian-13", loc)
	requireCode(t, err, http.StatusConflict, "conflict")

	_, err = e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	rows := e.rows("debian-13", loc)
	e.markReady(rows[0].ID, 9101, "n1")
	e.markReady(rows[1].ID, 9102, "n2")
	require.NoError(t, e.ent.CatalogBuild.UpdateOneID(rows[0].ID).SetStep(7).Exec(ctx))

	ls, err := e.svc.Refresh(ctx, "debian-13", loc)
	require.NoError(t, err)
	require.Equal(t, "building", ls.State)
	for i, r := range e.rows("debian-13", loc) {
		require.Equal(t, catalogbuild.StatusBuilding, r.Status)
		require.Zero(t, r.Step)
		require.Equal(t, 9101+i, *r.Vmid)
		require.Equal(t, "debian.qcow2", *r.File)
		require.Equal(t, "abc", *r.Checksum)
		require.NotNil(t, r.Nonce)
	}
	require.Len(t, e.jobArgs(osimages.KindBuild), 4)
}

func TestRefreshWhilePreviousJobStillRunning(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	row := e.rows("debian-13", loc)[0]
	oldJob := *row.RiverJobID
	// The build set the row ready but its job has not returned yet.
	_, err = e.sql.Exec(`update river_job set state = 'running', attempt = 1, attempted_at = now() where id = $1`, oldJob)
	require.NoError(t, err)
	e.markReady(row.ID, 9101, "n1")

	_, err = e.svc.Refresh(ctx, "debian-13", loc)
	require.NoError(t, err)
	var ids []int64
	rs, err := e.sql.Query(`select id from river_job where kind = $1 and (args->>'build_id')::bigint = $2 order by id`, osimages.KindBuild, row.ID)
	require.NoError(t, err)
	defer rs.Close()
	for rs.Next() {
		var id int64
		require.NoError(t, rs.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rs.Err())
	require.Len(t, ids, 2)
	row = e.rows("debian-13", loc)[0]
	require.Equal(t, catalogbuild.StatusBuilding, row.Status)
	require.NotEqual(t, oldJob, *row.RiverJobID)
	require.Equal(t, ids[1], *row.RiverJobID)
}

func TestDisableWhileProxmoxDown(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.readyWithImage(loc)
	e.pve.SetDown(true)
	ls, err := e.svc.Disable(ctx, "debian-13", loc)
	require.NoError(t, err)
	require.Equal(t, "off", ls.State)
	require.True(t, ls.Available)
	require.Empty(t, ls.Reason)
	require.Len(t, e.jobArgs(osimages.KindRetire), 2)
}

// readyWithImage enables debian-13 on local storage, marks both rows ready and registers the image and its templates.
func (e *env) readyWithImage(loc int64) int64 {
	e.t.Helper()
	ctx := context.Background()
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(e.t, err)
	rows := e.rows("debian-13", loc)
	e.markReady(rows[0].ID, 9101, "n1")
	e.markReady(rows[1].ID, 9102, "n2")
	var imgID int64
	require.NoError(e.t, db.WithTx(ctx, e.sql, func(_ *sql.Tx, etx *ent.Client) error {
		var err error
		imgID, err = e.cat.CreateCatalogImage(ctx, etx, "debian-13", "debian-13", "Debian 13", "debian", "13")
		if err != nil {
			return err
		}
		for i, r := range rows {
			if err := etx.ImageTemplate.Create().SetImageID(imgID).SetLocationID(loc).SetNode(r.Node).SetVmid(9101 + i).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	}))
	return imgID
}

func TestDisableRemovesTemplatesAndImage(t *testing.T) {
	ctx := context.Background()
	t.Run("no servers", func(t *testing.T) {
		e := newEnv(t)
		loc := e.location("eu-1", "local-lvm", "imp")
		imgID := e.readyWithImage(loc)
		rows := e.rows("debian-13", loc)

		ls, err := e.svc.Disable(ctx, "debian-13", loc)
		require.NoError(t, err)
		require.Equal(t, "off", ls.State)
		require.Empty(t, e.rows("debian-13", loc))
		n, err := e.ent.ImageTemplate.Query().Where(imagetemplate.ImageID(imgID)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
		exists, err := e.ent.Image.Query().Where(image.ID(imgID)).Exist(ctx)
		require.NoError(t, err)
		require.False(t, exists)

		args := e.jobArgs(osimages.KindRetire)
		require.Len(t, args, 2)
		for i, raw := range args {
			var a osimages.RetireArgs
			require.NoError(t, json.Unmarshal(raw, &a))
			require.Equal(t, osimages.RetireArgs{LocationID: loc, Node: rows[i].Node, VMID: 9101 + i,
				Tag: osimages.TemplateTag(rows[i].ID, []string{"n1", "n2"}[i])}, a)
		}
	})
	t.Run("server keeps image", func(t *testing.T) {
		e := newEnv(t)
		loc := e.location("eu-1", "local-lvm", "imp")
		imgID := e.readyWithImage(loc)
		p, err := e.ent.Project.Create().SetName("p").Save(ctx)
		require.NoError(t, err)
		st, err := e.ent.ServerType.Create().SetName("small").SetCores(1).SetMemoryMB(512).SetDiskGB(10).Save(ctx)
		require.NoError(t, err)
		require.NoError(t, e.ent.Server.Create().SetProjectID(p.ID).SetName("s").SetLocationID(loc).SetTemplateVmid(9101).
			SetServerTypeID(st.ID).SetImageID(imgID).SetMAC("bc:24:11:00:00:01").SetLabels(map[string]string{}).
			SetCores(1).SetMemoryMB(512).SetDiskGB(10).SetSSHKeys([]string{}).Exec(ctx))

		_, err = e.svc.Disable(ctx, "debian-13", loc)
		require.NoError(t, err)
		exists, err := e.ent.Image.Query().Where(image.ID(imgID)).Exist(ctx)
		require.NoError(t, err)
		require.True(t, exists)
		n, err := e.ent.ImageTemplate.Query().Where(imagetemplate.LocationID(loc)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, n)
		require.Len(t, e.jobArgs(osimages.KindRetire), 2)
	})
}

func TestListRetiredKey(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	require.NoError(t, db.WithTx(ctx, e.sql, func(_ *sql.Tx, etx *ent.Client) error {
		_, err := e.cat.CreateCatalogImage(ctx, etx, "old-1", "old-1", "Old One", "debian", "1")
		return err
	}))
	b, err := e.ent.CatalogBuild.Create().SetCatalogKey("old-1").SetLocationID(loc).SetNode("pve1").
		SetStatus(catalogbuild.StatusReady).SetVmid(9150).SetNonce("zz").Save(ctx)
	require.NoError(t, err)

	list, err := e.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 3)
	old := list[2]
	require.Equal(t, "old-1", old.Key)
	require.True(t, old.Retired)
	require.Equal(t, "Old One", old.Description)
	require.Equal(t, "debian", old.OSFlavor)
	require.Equal(t, "1", old.OSVersion)
	require.Equal(t, "retired", locState(t, list, "old-1", loc).State)
	require.False(t, list[0].Retired)

	_, err = e.svc.Enable(ctx, "old-1", loc)
	requireCode(t, err, http.StatusConflict, "catalog_retired")
	_, err = e.svc.Refresh(ctx, "old-1", loc)
	requireCode(t, err, http.StatusConflict, "catalog_retired")

	_, err = e.svc.Disable(ctx, "old-1", loc)
	require.NoError(t, err)
	args := e.jobArgs(osimages.KindRetire)
	require.Len(t, args, 1)
	var a osimages.RetireArgs
	require.NoError(t, json.Unmarshal(args[0], &a))
	require.Equal(t, osimages.TemplateTag(b.ID, "zz"), a.Tag)
}

func TestTemplateTag(t *testing.T) {
	require.Equal(t, "nebla-tpl-42-abc", osimages.TemplateTag(42, "abc"))
}

func TestDisableTakesFinalizeLock(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "local-lvm", "imp")
	e.readyWithImage(loc)
	tx, err := e.sql.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `select pg_advisory_xact_lock(hashtext('osimages:' || $1))`, "debian-13")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Disable(ctx, "debian-13", loc)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("disable did not wait for the lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, tx.Rollback())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("disable still blocked after the lock was released")
	}
}

func TestDeleteLocationWaitsForTemplateJobs(t *testing.T) {
	ctx := context.Background()
	hook := func(o *catalog.Options) { o.Hooks.TemplatesPending = osimages.TemplatesPending }
	t.Run("retire", func(t *testing.T) {
		e := buildEnvWith(t, false, hook)
		loc := e.location("eu-1", "local-lvm", "imp")
		imgID := e.readyWithImage(loc)
		_, err := e.svc.Disable(ctx, "debian-13", loc)
		require.NoError(t, err)
		exists, err := e.ent.Image.Query().Where(image.ID(imgID)).Exist(ctx)
		require.NoError(t, err)
		require.False(t, exists)
		requireCode(t, e.cat.DeleteLocation(ctx, loc), 409, "location_in_use")
		_, err = e.sql.Exec(`update river_job set state = 'retryable' where kind = $1`, osimages.KindRetire)
		require.NoError(t, err)
		requireCode(t, e.cat.DeleteLocation(ctx, loc), 409, "location_in_use")
		_, err = e.sql.Exec(`update river_job set state = 'completed', finalized_at = now() where kind = $1`, osimages.KindRetire)
		require.NoError(t, err)
		e.insertJob(osimages.CleanupArgs{BuildID: 1, LocationID: loc + 1000, VMID: 7000, Tag: "nebla-tpl-1-aa"})
		require.NoError(t, e.cat.DeleteLocation(ctx, loc))
	})
	t.Run("cleanup", func(t *testing.T) {
		e := buildEnvWith(t, false, hook)
		loc := e.location("eu-1", "local-lvm", "imp")
		e.insertJob(osimages.CleanupArgs{BuildID: 1, LocationID: loc, VMID: 7000, Tag: "nebla-tpl-1-aa"})
		requireCode(t, e.cat.DeleteLocation(ctx, loc), 409, "location_in_use")
	})
}

func (e *env) insertJob(args river.JobArgs) {
	e.t.Helper()
	tx, err := e.sql.Begin()
	require.NoError(e.t, err)
	_, err = e.jobs.InsertTx(context.Background(), tx, args, nil)
	require.NoError(e.t, err)
	require.NoError(e.t, tx.Commit())
}
