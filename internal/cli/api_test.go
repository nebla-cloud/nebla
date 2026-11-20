package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
	"golang.org/x/crypto/ssh"
)

func TestMain(m *testing.M) {
	retryBaseDelay = time.Millisecond
	pollBase = time.Millisecond
	pollMax = 5 * time.Millisecond
	m.Run()
}

const locationsBody = `{"locations":[],"meta":{"pagination":{"page":1,"per_page":50,"total_entries":0}}}`

func stubAPI(t *testing.T, h http.HandlerFunc) (*API, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	a, err := newAPI(Env{HTTPClient: srv.Client()}, Target{Endpoint: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return a, &n
}

func TestRetryGETOn429ThenOK(t *testing.T) {
	var calls atomic.Int32
	a, n := stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || !strings.HasPrefix(r.Header.Get("User-Agent"), "nebla-cli/") {
			t.Errorf("headers=%v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"code":"rate_limited","message":"slow down"}}`)
			return
		}
		fmt.Fprint(w, locationsBody)
	})
	if _, err := a.Locations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("requests=%d", n.Load())
	}
}

func TestRetryGETOn503ThenOK(t *testing.T) {
	var calls atomic.Int32
	a, n := stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, locationsBody)
	})
	if _, err := a.Locations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("requests=%d", n.Load())
	}
}

func TestNoRetryOnPOST(t *testing.T) {
	a, n := stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	res, err := a.C.PostSshKeysWithResponse(context.Background(), apiclient.PostSshKeysJSONRequestBody{Name: "k", PublicKey: "x"})
	if err != nil {
		t.Fatal(err)
	}
	err = apiErr(res.StatusCode(), res.Body)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 503 {
		t.Fatalf("err=%v", err)
	}
	if n.Load() != 1 {
		t.Fatalf("requests=%d", n.Load())
	}
}

func TestRetryGivesUpAfterFive(t *testing.T) {
	a, n := stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := a.Locations(context.Background())
	if err == nil || err.Error() != "unexpected status 503" {
		t.Fatalf("err=%v", err)
	}
	if n.Load() != 5 {
		t.Fatalf("requests=%d", n.Load())
	}
}

func TestAPIErrorDecode(t *testing.T) {
	err := apiErr(409, []byte(`{"error":{"code":"conflict","message":"busy"}}`))
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 409 || err.Error() != "busy (conflict)" {
		t.Fatalf("err=%v", err)
	}
	if err := apiErr(500, []byte("oops")); err.Error() != "unexpected status 500" {
		t.Fatalf("err=%v", err)
	}
}

func TestBaseURLNoDoubleSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, locationsBody)
	}))
	defer srv.Close()
	a, err := newAPI(Env{}, Target{Endpoint: srv.URL + "/", Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Locations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/locations" {
		t.Fatalf("path=%q", path)
	}
}

func (h *harness) api() *API {
	h.t.Helper()
	a, err := newAPI(Env{HTTPClient: http.DefaultClient}, Target{Endpoint: h.URL, Token: h.Token})
	if err != nil {
		h.t.Fatal(err)
	}
	return a
}

func TestResolveServerNumericName(t *testing.T) {
	h := newHarnessQueueOnly(t)
	s := h.Env.CreateServer("123", nil)
	if s.ServerID == 123 {
		t.Fatal("server id collides with its name")
	}
	got, err := h.api().Server(context.Background(), "123")
	if err != nil || got.Id != s.ServerID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	byID, err := h.api().Server(context.Background(), fmt.Sprint(s.ServerID))
	if err != nil || byID.Name != "123" {
		t.Fatalf("byID=%+v err=%v", byID, err)
	}
	if _, err := h.api().Server(context.Background(), "ghost"); err == nil || err.Error() != `server "ghost" not found` {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveNotFound(t *testing.T) {
	h := newHarnessQueueOnly(t)
	a := h.api()
	ctx := context.Background()
	if _, err := a.Image(ctx, "nope"); err == nil || err.Error() != `image "nope" not found` {
		t.Fatalf("err=%v", err)
	}
	if _, err := a.SSHKey(ctx, "999"); err == nil || err.Error() != `SSH key "999" not found` {
		t.Fatalf("err=%v", err)
	}
	if img, err := a.Image(ctx, "ubuntu"); err != nil || img.Id != h.Env.Image {
		t.Fatalf("img=%+v err=%v", img, err)
	}
	if st, err := a.ServerType(ctx, fmt.Sprint(h.Env.Type)); err != nil || st.Name != "small" {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if l, err := a.Location(ctx, "eu-1"); err != nil || l.Id != h.Env.Location {
		t.Fatalf("l=%+v err=%v", l, err)
	}
	if _, err := a.ServerType(ctx, "huge"); err == nil || err.Error() != `server type "huge" not found` {
		t.Fatalf("err=%v", err)
	}
	if _, err := a.Location(ctx, "mars"); err == nil || err.Error() != `location "mars" not found` {
		t.Fatalf("err=%v", err)
	}
}

func TestListAllPages(t *testing.T) {
	h := newHarnessQueueOnly(t)
	for i := range 55 {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pk, err := ssh.NewPublicKey(pub)
		if err != nil {
			t.Fatal(err)
		}
		key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk)))
		res := h.Env.C.Send(http.MethodPost, "/v1/ssh_keys", map[string]string{"name": fmt.Sprintf("k%d", i), "public_key": key})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("status=%d", res.StatusCode)
		}
		res.Body.Close()
	}
	keys, err := h.api().SSHKeys(context.Background())
	if err != nil || len(keys) != 55 {
		t.Fatalf("len=%d err=%v", len(keys), err)
	}
	k, err := h.api().SSHKey(context.Background(), "k54")
	if err != nil || k.Name != "k54" {
		t.Fatalf("k=%+v err=%v", k, err)
	}
}
