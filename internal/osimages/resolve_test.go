package osimages_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Hexeption/nebla/internal/osimages"
	"github.com/stretchr/testify/require"
)

var keyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func TestParseChecksums(t *testing.T) {
	body := "-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA256\n\n# comment\n" +
		strings.Repeat("a", 64) + "  gnu.img\n" +
		strings.Repeat("B", 64) + " *star.img\n" +
		"SHA256 (bsd.qcow2) = " + strings.Repeat("c", 64) + "\n" +
		"-----BEGIN PGP SIGNATURE-----\n\nabcDEF==\n-----END PGP SIGNATURE-----\n"
	got := osimages.ParseChecksums([]byte(body))
	require.Equal(t, map[string]string{
		"gnu.img":   strings.Repeat("a", 64),
		"star.img":  strings.Repeat("b", 64),
		"bsd.qcow2": strings.Repeat("c", 64),
	}, got)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	files := map[string]string{
		"debian": "debian-SHA512SUMS",
		"ubuntu": "ubuntu-SHA256SUMS",
		"rocky":  "rocky-CHECKSUM",
		"alma":   "alma-CHECKSUM",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		family := strings.SplitN(parts[0], "-", 2)[0]
		last := parts[len(parts)-1]
		switch {
		case family == "fedora" && len(parts) == 1:
			_, _ = w.Write(fixture(t, "fedora-index.html"))
		case family == "fedora" && strings.HasSuffix(last, "CHECKSUM"):
			_, _ = w.Write(fixture(t, "fedora-CHECKSUM"))
		case files[family] != "" && len(parts) == 2:
			_, _ = w.Write(fixture(t, files[family]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestResolveEachEntry(t *testing.T) {
	srv := fixtureServer(t)
	lens := map[string]int{"sha256": 64, "sha512": 128}
	for _, e := range osimages.Entries() {
		t.Run(e.Key, func(t *testing.T) {
			dir := srv.URL + "/" + e.Key + "/"
			e.Dirs = []string{dir}
			got, err := osimages.Resolve(context.Background(), srv.Client(), e)
			require.NoError(t, err)
			require.True(t, e.File.MatchString(got.File), got.File)
			require.Regexp(t, `\.(qcow2|img)$`, got.File)
			require.Equal(t, dir+got.File, got.URL)
			require.Equal(t, e.Algorithm, got.Algorithm)
			require.Regexp(t, `^[0-9a-f]+$`, got.Checksum)
			require.Len(t, got.Checksum, lens[e.Algorithm])
			require.NotContains(t, got.File, "latest")
		})
	}
}

func TestResolvePrefersDatedOverLatest(t *testing.T) {
	h := strings.Repeat("d", 64)
	body := h + "  X-latest.qcow2\n" + h + "  X-20261101.qcow2\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	e := osimages.Entry{Dirs: []string{srv.URL + "/"}, ChecksumName: "SUMS", File: regexp.MustCompile(`^X-.*\.qcow2$`), Algorithm: "sha256", Format: "qcow2"}
	got, err := osimages.Resolve(context.Background(), srv.Client(), e)
	require.NoError(t, err)
	require.Equal(t, "X-20261101.qcow2", got.File)
}

func TestResolveFallsBackToArchive(t *testing.T) {
	h := strings.Repeat("e", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/archive/") {
			_, _ = w.Write([]byte(h + "  X-1.qcow2\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	e := osimages.Entry{Dirs: []string{srv.URL + "/current/", srv.URL + "/archive/"}, ChecksumName: "SUMS", File: regexp.MustCompile(`^X-`), Algorithm: "sha256", Format: "qcow2"}
	got, err := osimages.Resolve(context.Background(), srv.Client(), e)
	require.NoError(t, err)
	require.Equal(t, srv.URL+"/archive/X-1.qcow2", got.URL)
}

func TestResolveBodyCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 2<<20)))
	}))
	defer srv.Close()
	e := osimages.Entry{Dirs: []string{srv.URL + "/"}, ChecksumName: "SUMS", File: regexp.MustCompile(`.`), Algorithm: "sha256", Format: "qcow2"}
	_, err := osimages.Resolve(context.Background(), srv.Client(), e)
	require.True(t, errors.Is(err, osimages.ErrUpstream))
}

func TestEntriesValid(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range osimages.Entries() {
		require.Regexp(t, keyRe, e.Key)
		require.False(t, seen[e.Key], e.Key)
		seen[e.Key] = true
		require.Contains(t, []string{"qcow2", "raw"}, e.Format)
		require.Contains(t, []string{"sha256", "sha512"}, e.Algorithm)
		require.NotEmpty(t, e.Dirs)
		got, ok := osimages.Lookup(osimages.Entries(), e.Key)
		require.True(t, ok)
		require.Equal(t, e.Key, got.Key)
	}
	_, ok := osimages.Lookup(osimages.Entries(), "nope")
	require.False(t, ok)
}
