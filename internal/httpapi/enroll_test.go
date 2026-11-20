package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/nebla-cloud/nebla/internal/enroll"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

const enrollSecret = "3f2b8c1e-5d4a-4e7b-9a10-0c6d2e8f7a11"

type enrollCreated struct {
	Enrollment struct {
		ID        int64  `json:"id"`
		TokenName string `json:"token_name"`
		State     string `json:"state"`
	} `json:"enrollment"`
	Token         string `json:"token"`
	Command       string `json:"command"`
	OfflineScript string `json:"offline_script"`
}

// enrollReq sends a public enroll request with an optional Authorization header.
func enrollReq(t *testing.T, app *testutil.App, method, path, auth string, body []byte) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, app.URL+path, r)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return app.Do(req)
}

// wireReport is the testdata report for tokenName, with pve1 pinned to fp.
func wireReport(t *testing.T, tokenName, fp string) []byte {
	t.Helper()
	b, err := os.ReadFile("../enroll/testdata/report-pve9.json")
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	m["token"].(map[string]any)["full-tokenid"] = "nebla@pve!" + tokenName
	m["fingerprints"].(map[string]any)["pve1"] = fp
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return out
}

func createEnrollment(t *testing.T, app *testutil.App, admin *testutil.Client, name string) enrollCreated {
	t.Helper()
	res := admin.Send(http.MethodPost, "/v1/admin/location_enrollments", map[string]any{"name": name})
	body := app.Body(res)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var c enrollCreated
	require.NoError(t, json.Unmarshal([]byte(body), &c))
	return c
}

func TestEnrollScriptPublic(t *testing.T) {
	app := testutil.NewApp(t)
	res := app.Get("/v1/enroll.sh")
	body := app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "text/plain; charset=utf-8", res.Header.Get("Content-Type"))
	require.Contains(t, body, "PANEL_URL='"+app.URL+"'")
	// The script's own token regex mentions nbe_; no real token may appear.
	require.NotRegexp(t, `nbe_[A-Za-z0-9_-]{43}`, body)
}

func TestEnrollAdminOnly(t *testing.T) {
	app := testutil.NewApp(t)
	app.CreateUser("u@example.com", false)
	cl := app.Login("u@example.com")
	res := cl.Send(http.MethodPost, "/v1/admin/location_enrollments", map[string]any{"name": "home"})
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	require.Equal(t, "forbidden", errCode(t, app, res))
}

func TestEnrollCreateInputValidation(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	for _, b := range []map[string]any{{}, {"name": "home", "location_id": 1}} {
		res := admin.Send(http.MethodPost, "/v1/admin/location_enrollments", b)
		require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
		require.Equal(t, "invalid_input", errCode(t, app, res))
	}
}

func enrollFlow(t *testing.T, app *testutil.App) enrollCreated {
	t.Helper()
	app.SeedPVE()
	admin := app.LoginAdmin()
	c := createEnrollment(t, app, admin, "home")
	require.Regexp(t, `^nbe_[A-Za-z0-9_-]{43}$`, c.Token)
	require.Contains(t, c.Command, app.URL)
	require.NotEmpty(t, c.OfflineScript)
	require.Equal(t, "waiting", c.Enrollment.State)
	auth := "Enroll " + c.Token

	res := enrollReq(t, app, http.MethodGet, "/v1/enroll/check", auth, nil)
	body := app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	var chk struct {
		TokenName string `json:"token_name"`
		Location  string `json:"location"`
		ExpiresAt string `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &chk))
	require.Equal(t, c.Enrollment.TokenName, chk.TokenName)
	require.Equal(t, "home", chk.Location)
	require.NotEmpty(t, chk.ExpiresAt)

	res = enrollReq(t, app, http.MethodPost, "/v1/enroll/report", auth, wireReport(t, c.Enrollment.TokenName, app.PVE.Conn().CAFingerprint))
	require.Equal(t, http.StatusNoContent, res.StatusCode, app.Body(res))

	res = enrollReq(t, app, http.MethodGet, "/v1/enroll/check", auth, nil)
	require.Equal(t, http.StatusConflict, res.StatusCode)
	require.Equal(t, "enrollment_used", errCode(t, app, res))

	id := itoa(c.Enrollment.ID)
	res = admin.Get("/v1/admin/location_enrollments/" + id)
	body = app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	require.NotContains(t, body, `"value"`)
	require.NotContains(t, body, enrollSecret)
	require.NotContains(t, body, "token_secret")
	var got struct {
		State  string `json:"state"`
		Report struct {
			TokenID  string `json:"token_id"`
			Nodes    []any  `json:"nodes"`
			Storages []any  `json:"storages"`
		} `json:"report"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	require.Equal(t, "reported", got.State)
	require.Equal(t, "nebla@pve!"+c.Enrollment.TokenName, got.Report.TokenID)
	require.Len(t, got.Report.Nodes, 2)
	require.Len(t, got.Report.Storages, 3)

	res = admin.Get("/v1/admin/location_enrollments")
	body = app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	require.Contains(t, body, `"enrollments"`)
	require.NotContains(t, body, enrollSecret)

	app.PVE.AddToken("nebla@pve!"+c.Enrollment.TokenName, enrollSecret)
	res = admin.Send(http.MethodPost, "/v1/admin/location_enrollments/"+id+"/finish", map[string]any{
		"url": app.PVE.Conn().BaseURL, "disk_storage": "local-lvm", "iso_storage": "local"})
	body = app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	require.NotContains(t, body, enrollSecret)
	var fin struct {
		Location struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			PVE  struct {
				TokenID   string `json:"token_id"`
				HasSecret bool   `json:"has_secret"`
			} `json:"pve"`
			Nodes []any `json:"nodes"`
		} `json:"location"`
		Cleanup []string `json:"cleanup"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &fin))
	require.Equal(t, "home", fin.Location.Name)
	require.Equal(t, "nebla@pve!"+c.Enrollment.TokenName, fin.Location.PVE.TokenID)
	require.True(t, fin.Location.PVE.HasSecret)
	require.Len(t, fin.Location.Nodes, 2)
	require.NotNil(t, fin.Cleanup)

	res = admin.Get("/v1/admin/location_enrollments/" + id)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
	return c
}

func TestEnrollFlowHTTP(t *testing.T) {
	enrollFlow(t, testutil.NewApp(t))
}

func TestEnrollTokenAndDelete(t *testing.T) {
	app := testutil.NewApp(t)
	admin := app.LoginAdmin()
	c := createEnrollment(t, app, admin, "home")
	id := itoa(c.Enrollment.ID)

	res := admin.Send(http.MethodPost, "/v1/admin/location_enrollments/"+id+"/token", nil)
	body := app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	var n enrollCreated
	require.NoError(t, json.Unmarshal([]byte(body), &n))
	require.NotEqual(t, c.Token, n.Token)

	res = enrollReq(t, app, http.MethodGet, "/v1/enroll/check", "Enroll "+c.Token, nil)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.Equal(t, "enrollment_invalid", errCode(t, app, res))

	res = admin.Send(http.MethodDelete, "/v1/admin/location_enrollments/"+id, nil)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res.Body.Close()
	res = admin.Send(http.MethodDelete, "/v1/admin/location_enrollments/"+id, nil)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	res.Body.Close()
}

func TestEnrollReportBlobHTTP(t *testing.T) {
	app := testutil.NewApp(t)
	app.SeedPVE()
	admin := app.LoginAdmin()
	a := createEnrollment(t, app, admin, "home")
	b := createEnrollment(t, app, admin, "away")
	blob, err := enroll.EncodeBlob(a.Token, wireReport(t, a.Enrollment.TokenName, app.PVE.Conn().CAFingerprint))
	require.NoError(t, err)

	res := admin.Send(http.MethodPost, "/v1/admin/location_enrollments/"+itoa(b.Enrollment.ID)+"/report", map[string]any{"blob": blob})
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.Equal(t, "enrollment_invalid", errCode(t, app, res))

	res = admin.Send(http.MethodPost, "/v1/admin/location_enrollments/"+itoa(a.Enrollment.ID)+"/report", map[string]any{"blob": blob})
	body := app.Body(res)
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	require.Contains(t, body, `"state":"reported"`)
	require.NotContains(t, body, enrollSecret)
}

func TestEnrollReportNoHeader(t *testing.T) {
	app := testutil.NewApp(t)
	res := enrollReq(t, app, http.MethodPost, "/v1/enroll/report", "", []byte(`{}`))
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.Equal(t, "enrollment_invalid", errCode(t, app, res))
	res = enrollReq(t, app, http.MethodGet, "/v1/enroll/check", "", nil)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.Equal(t, "enrollment_invalid", errCode(t, app, res))
}

func TestEnrollReportBearerIsNotEnroll(t *testing.T) {
	app := testutil.NewApp(t)
	for _, h := range []string{"Bearer x", "Enroll ", "Enroll nbe_" + strings.Repeat("A", 43)} {
		res := enrollReq(t, app, http.MethodPost, "/v1/enroll/report", h, []byte(`{}`))
		require.Equal(t, http.StatusUnauthorized, res.StatusCode, h)
		require.Equal(t, "enrollment_invalid", errCode(t, app, res), h)
		res = enrollReq(t, app, http.MethodGet, "/v1/enroll/check", h, nil)
		require.Equal(t, http.StatusUnauthorized, res.StatusCode, h)
		require.Equal(t, "enrollment_invalid", errCode(t, app, res), h)
	}
}

func TestEnrollRateLimit(t *testing.T) {
	app := testutil.NewApp(t)
	auth := "Enroll nbe_" + strings.Repeat("A", 43)
	for i := range 10 {
		res := enrollReq(t, app, http.MethodGet, "/v1/enroll/check", auth, nil)
		require.Equal(t, http.StatusUnauthorized, res.StatusCode, i)
		res.Body.Close()
	}
	res := enrollReq(t, app, http.MethodGet, "/v1/enroll/check", auth, nil)
	require.Equal(t, http.StatusTooManyRequests, res.StatusCode)
	require.NotEmpty(t, res.Header.Get("Retry-After"))
	require.Equal(t, "rate_limit_exceeded", errCode(t, app, res))
	res = enrollReq(t, app, http.MethodGet, "/v1/enroll.sh", "", nil)
	require.Equal(t, http.StatusTooManyRequests, res.StatusCode)
	res.Body.Close()
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestEnrollSecretsNotLogged(t *testing.T) {
	var buf syncBuffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	app := testutil.NewApp(t, func(d *httpapi.Deps) { d.Logger = log })
	c := enrollFlow(t, app)
	out := buf.String()
	require.False(t, strings.Contains(out, c.Token), "log contains the enrollment token")
	require.False(t, strings.Contains(out, enrollSecret), "log contains the token secret")
}
