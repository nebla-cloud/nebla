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

package enroll_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/enroll"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/locationenrollment"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
	"github.com/nebla-cloud/nebla/internal/testutil"
)

const (
	panelURL   = "https://panel.example"
	testSecret = "3f2b8c1e-5d4a-4e7b-9a10-0c6d2e8f7a11"
)

type env struct {
	svc    *enroll.Service
	cat    *catalog.Service
	ent    *ent.Client
	sql    *sql.DB
	pv     *fake.Server
	userID int64
	now    time.Time
	mu     sync.Mutex
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = e.now.Add(d)
}

func setup(t *testing.T, fp ...func(context.Context, string) (string, error)) *env {
	t.Helper()
	client, sqlDB := testutil.NewEntSQL(t)
	pv := fake.New(t)
	testutil.SeedPVE(pv)
	cat := catalog.New(sqlDB, client, testutil.CatalogOptions(catalog.Hooks{}))
	u := client.User.Create().SetEmail("admin@example.com").SetName("Admin").SetPasswordHash("x").SetIsAdmin(true).
		SaveX(context.Background())
	e := &env{cat: cat, ent: client, sql: sqlDB, pv: pv, userID: u.ID, now: time.Now().UTC().Truncate(time.Second)}
	o := enroll.Options{SecretKey: []byte(testutil.SecretKey), BaseURL: panelURL, Catalog: cat, Now: e.clock}
	if len(fp) > 0 {
		o.LeafFingerprint = fp[0]
	}
	e.svc = enroll.New(sqlDB, client, o)
	return e
}

func requireCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e apierr.E
	require.True(t, errors.As(err, &e), "want apierr, got %v", err)
	require.Equal(t, status, e.Status, e.Msg)
	require.Equal(t, code, e.Code, e.Msg)
}

func colonFP(c string) string { return strings.TrimSuffix(strings.Repeat(c+":", 32), ":") }

type rstorage struct {
	id, content string
	iso         bool
}

// reportJSON builds a script report for tokenName with pve1's fingerprint fp.
func reportJSON(t *testing.T, tokenName, fp string, storages ...rstorage) []byte {
	t.Helper()
	if storages == nil {
		storages = []rstorage{{"ceph", "images", true}, {"local", "iso,vztmpl", true}, {"imp", "import", true}}
	}
	var st []any
	var iso []string
	for _, s := range storages {
		st = append(st, map[string]any{"storage": s.id, "type": "dir", "content": s.content, "shared": 1})
		if s.iso {
			iso = append(iso, s.id)
		}
	}
	b, err := json.Marshal(map[string]any{
		"v": 1, "pve_version": "9.0.3", "node": "pve1",
		"token": map[string]any{"full-tokenid": "nebla@pve!" + tokenName, "value": testSecret},
		"cluster": []any{
			map[string]any{"type": "node", "name": "pve1", "ip": "10.0.0.1", "online": 1},
			map[string]any{"type": "node", "name": "pve2", "ip": "10.0.0.2", "online": 1},
		},
		"storage":      st,
		"fingerprints": map[string]any{"pve1": fp, "pve2": colonFP("BB")},
		"nebla_iso":    iso,
	})
	require.NoError(t, err)
	return b
}

func (e *env) create(t *testing.T, name string) enroll.Created {
	t.Helper()
	c, err := e.svc.Create(context.Background(), enroll.CreateInput{Name: name}, e.userID)
	require.NoError(t, err)
	return c
}

func (e *env) report(t *testing.T, c enroll.Created) {
	t.Helper()
	require.NoError(t, e.svc.Report(context.Background(), c.Token, reportJSON(t, c.TokenName, e.pv.Conn().CAFingerprint)))
}

func (e *env) row(t *testing.T, id int64) *ent.LocationEnrollment {
	t.Helper()
	r, err := e.ent.LocationEnrollment.Get(context.Background(), id)
	require.NoError(t, err)
	return r
}

// snap is a row's stored columns, comparable with ==.
func (e *env) snap(t *testing.T, id int64) string {
	t.Helper()
	var s string
	require.NoError(t, e.sql.QueryRowContext(context.Background(),
		`SELECT row_to_json(e)::text FROM location_enrollments e WHERE id = $1`, id).Scan(&s))
	return s
}

func TestCreateAndCheck(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.create(t, "home")
	require.True(t, regexp.MustCompile(`^nbe_[A-Za-z0-9_-]{43}$`).MatchString(c.Token), "token format")
	require.Regexp(t, `^nebla-home-[a-z0-9]{6}$`, c.TokenName)
	require.Equal(t, "waiting", c.State)
	require.True(t, enroll.Command(panelURL, c.Token) == c.Command, "command")
	require.True(t, enroll.OfflineScript(panelURL, c.Token, c.TokenName) == c.OfflineScript, "offline script")
	require.Equal(t, e.now.Add(30*time.Minute), c.ExpiresAt.UTC())

	got, err := e.svc.Check(ctx, c.Token)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(got.TokenName, "nebla-home-"))
	require.Equal(t, c.ID, got.ID)

	var dump string
	require.NoError(t, e.sql.QueryRowContext(ctx, `SELECT string_agg(row_to_json(e)::text, '') FROM location_enrollments e`).Scan(&dump))
	if strings.Contains(dump, c.Token) || strings.Contains(dump, strings.TrimPrefix(c.Token, "nbe_")) {
		t.Fatal("a column contains the enrollment token")
	}
}

func TestCreateNameTaken(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, err := e.cat.CreateLocation(ctx, testutil.LocationInput(e.pv, "eu-1"))
	require.NoError(t, err)
	_, err = e.svc.Create(ctx, enroll.CreateInput{Name: "eu-1"}, e.userID)
	requireCode(t, err, 409, "name_taken")

	e.create(t, "home")
	_, err = e.svc.Create(ctx, enroll.CreateInput{Name: "home"}, e.userID)
	requireCode(t, err, 409, "name_taken")

	_, err = e.svc.Create(ctx, enroll.CreateInput{Name: "Bad Name"}, e.userID)
	requireCode(t, err, 422, "invalid_input")
}

func TestCheckExpired(t *testing.T) {
	e := setup(t)
	c := e.create(t, "home")
	e.advance(31 * time.Minute)
	_, err := e.svc.Check(context.Background(), c.Token)
	requireCode(t, err, 410, "enrollment_expired")
	got, err := e.svc.Get(context.Background(), c.ID)
	require.NoError(t, err)
	require.True(t, got.Expired)
}

func TestCheckUnknown(t *testing.T) {
	e := setup(t)
	e.create(t, "home")
	_, err := e.svc.Check(context.Background(), "nbe_"+strings.Repeat("A", 43))
	requireCode(t, err, 401, "enrollment_invalid")
}

func TestReportStoresSealed(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.create(t, "home")
	e.report(t, c)
	r := e.row(t, c.ID)
	require.Equal(t, locationenrollment.StateReported, r.State)
	require.Equal(t, "nebla@pve!"+c.TokenName, r.TokenID)
	require.NotEmpty(t, r.TokenSecret)
	if strings.Contains(string(r.TokenSecret), testSecret) || strings.Contains(string(r.Report), testSecret) {
		t.Fatal("token secret stored in the clear")
	}
	require.Equal(t, e.now.Add(24*time.Hour), r.ExpiresAt.UTC())

	got, err := e.svc.Get(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, "reported", got.State)
	require.NotNil(t, got.Report)
	require.Equal(t, "", got.Report.TokenSecret)
	require.Len(t, got.Report.Nodes, 2)

	_, err = e.svc.Check(ctx, c.Token)
	requireCode(t, err, 409, "enrollment_used")
}

func TestReportTwice(t *testing.T) {
	e := setup(t)
	c := e.create(t, "home")
	e.report(t, c)
	err := e.svc.Report(context.Background(), c.Token, reportJSON(t, c.TokenName, colonFP("AA")))
	requireCode(t, err, 409, "enrollment_used")
}

func TestReportConcurrent(t *testing.T) {
	e := setup(t)
	c := e.create(t, "home")
	raw := reportJSON(t, c.TokenName, colonFP("AA"))
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { errs[i] = e.svc.Report(context.Background(), c.Token, raw) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		requireCode(t, err, 409, "enrollment_used")
	}
	require.Equal(t, 1, ok)
}

func TestReportWrongTokenName(t *testing.T) {
	e := setup(t)
	c := e.create(t, "home")
	err := e.svc.Report(context.Background(), c.Token, reportJSON(t, "nebla-home-zzzzzz", colonFP("AA")))
	requireCode(t, err, 400, "invalid_report")
	require.Equal(t, locationenrollment.StateWaiting, e.row(t, c.ID).State)
}

func TestReportBlobOtherEnrollment(t *testing.T) {
	e := setup(t)
	a := e.create(t, "home")
	b := e.create(t, "lab")
	blob, err := enroll.EncodeBlob(a.Token, reportJSON(t, a.TokenName, colonFP("AA")))
	require.NoError(t, err)
	beforeA, beforeB := e.snap(t, a.ID), e.snap(t, b.ID)
	requireCode(t, e.svc.ReportBlob(context.Background(), b.ID, blob), 401, "enrollment_invalid")
	require.True(t, beforeA == e.snap(t, a.ID), "enrollment A changed")
	require.True(t, beforeB == e.snap(t, b.ID), "enrollment B changed")

	require.NoError(t, e.svc.ReportBlob(context.Background(), a.ID, blob))
	require.Equal(t, locationenrollment.StateReported, e.row(t, a.ID).State)
}

func TestNewTokenInvalidatesOld(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	c := e.create(t, "home")
	e.advance(40 * time.Minute)
	n, err := e.svc.NewToken(ctx, c.ID)
	require.NoError(t, err)
	require.True(t, c.Token != n.Token, "new token differs")
	require.NotEqual(t, c.TokenName, n.TokenName)
	require.Regexp(t, regexp.MustCompile(`^nebla-home-[a-z0-9]{6}$`), n.TokenName)
	require.Equal(t, e.now.Add(30*time.Minute), n.ExpiresAt.UTC())

	_, err = e.svc.Check(ctx, c.Token)
	requireCode(t, err, 401, "enrollment_invalid")
	got, err := e.svc.Check(ctx, n.Token)
	require.NoError(t, err)
	require.Equal(t, n.TokenName, got.TokenName)

	require.NoError(t, e.svc.Report(ctx, n.Token, reportJSON(t, n.TokenName, colonFP("AA"))))
	_, err = e.svc.NewToken(ctx, c.ID)
	requireCode(t, err, 409, "enrollment_state")
}

func TestRerunReplacesLiveRerun(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	loc, err := e.cat.CreateLocation(ctx, testutil.LocationInput(e.pv, "eu-1"))
	require.NoError(t, err)
	first, err := e.svc.Create(ctx, enroll.CreateInput{LocationID: &loc.ID}, e.userID)
	require.NoError(t, err)
	require.Equal(t, "eu-1", first.Name)
	require.Regexp(t, `^nebla-eu-1-[a-z0-9]{6}$`, first.TokenName)
	second, err := e.svc.Create(ctx, enroll.CreateInput{LocationID: &loc.ID}, e.userID)
	require.NoError(t, err)

	_, err = e.ent.LocationEnrollment.Get(ctx, first.ID)
	require.True(t, ent.IsNotFound(err))
	_, err = e.svc.Check(ctx, first.Token)
	requireCode(t, err, 401, "enrollment_invalid")
	require.Equal(t, loc.ID, *e.row(t, second.ID).LocationID)

	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.svc.Create(ctx, enroll.CreateInput{LocationID: &loc.ID}, e.userID) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			requireCode(t, err, 409, "enrollment_state")
		}
	}
	n, err := e.ent.LocationEnrollment.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	missing := int64(9999)
	_, err = e.svc.Create(ctx, enroll.CreateInput{LocationID: &missing}, e.userID)
	requireCode(t, err, 404, "not_found")
}

func TestListAndDelete(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a := e.create(t, "home")
	e.create(t, "lab")
	list, err := e.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.NoError(t, e.svc.Delete(ctx, a.ID))
	requireCode(t, e.svc.Delete(ctx, a.ID), 404, "not_found")
	e.advance(30*time.Minute + 24*time.Hour)
	list, err = e.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1, "a row exactly 24h past expiry is listed until swept")
	n, err := e.svc.Sweep(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	e.advance(time.Hour)
	list, err = e.svc.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestSweep(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	old := e.create(t, "old")
	e.advance(2 * time.Hour)
	young := e.create(t, "young")
	// old is now 25h past expiry, young 23h.
	e.advance(23*time.Hour + 30*time.Minute)
	n, err := e.svc.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, err = e.ent.LocationEnrollment.Get(ctx, old.ID)
	require.True(t, ent.IsNotFound(err))
	e.row(t, young.ID)
}

func TestScriptBody(t *testing.T) {
	e := setup(t)
	b, err := e.svc.ScriptBody()
	require.NoError(t, err)
	require.Equal(t, enroll.Script(panelURL), b)
}

func TestBadBaseURL(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	loc, err := e.cat.CreateLocation(ctx, testutil.LocationInput(e.pv, "eu-1"))
	require.NoError(t, err)
	waiting := e.create(t, "home")
	for _, u := range []string{"", "panel.example", "https://panel.example/'x", "https://pa nel"} {
		svc := enroll.New(e.sql, e.ent, enroll.Options{SecretKey: []byte(testutil.SecretKey), BaseURL: u, Catalog: e.cat, Now: e.clock})
		require.NotPanics(t, func() {
			_, err := svc.Create(ctx, enroll.CreateInput{Name: "lab"}, e.userID)
			requireCode(t, err, 500, "internal_error")
			_, err = svc.Create(ctx, enroll.CreateInput{LocationID: &loc.ID}, e.userID)
			requireCode(t, err, 500, "internal_error")
			_, err = svc.NewToken(ctx, waiting.ID)
			requireCode(t, err, 500, "internal_error")
			_, err = svc.ScriptBody()
			requireCode(t, err, 500, "internal_error")
		})
	}
	n, err := e.ent.LocationEnrollment.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "no row created")
	require.Equal(t, waiting.TokenName, e.row(t, waiting.ID).TokenName, "token not replaced")
}

func TestRerunRefusesReportedRerun(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	loc, err := e.cat.CreateLocation(ctx, testutil.LocationInput(e.pv, "eu-1"))
	require.NoError(t, err)
	first, err := e.svc.Create(ctx, enroll.CreateInput{LocationID: &loc.ID}, e.userID)
	require.NoError(t, err)
	e.report(t, first)
	before := e.snap(t, first.ID)
	_, err = e.svc.Create(ctx, enroll.CreateInput{LocationID: &loc.ID}, e.userID)
	requireCode(t, err, 409, "enrollment_state")
	var ae apierr.E
	require.True(t, errors.As(err, &ae))
	require.Contains(t, ae.Msg, "waiting to be finished")
	require.True(t, before == e.snap(t, first.ID), "reported re-run changed")

	// Once the report expires, a new Re-run replaces it.
	e.advance(24 * time.Hour)
	second, err := e.svc.Create(ctx, enroll.CreateInput{LocationID: &loc.ID}, e.userID)
	require.NoError(t, err)
	_, err = e.ent.LocationEnrollment.Get(ctx, first.ID)
	require.True(t, ent.IsNotFound(err))
	require.Equal(t, "waiting", second.State)
}
