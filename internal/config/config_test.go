package config_test

import (
	"net/netip"
	"testing"

	"github.com/Hexeption/nebla/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoadRequiresSecretKeyLength(t *testing.T) {
	t.Setenv("PANEL_DATABASE_URL", "postgres://x")
	t.Setenv("PANEL_BASE_URL", "https://panel.test")
	t.Setenv("PANEL_SECRET_KEY", "short")
	_, err := config.Load()
	require.ErrorContains(t, err, "PANEL_SECRET_KEY")
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PANEL_DATABASE_URL", "postgres://x")
	t.Setenv("PANEL_BASE_URL", "https://panel.test")
	t.Setenv("PANEL_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.Listen)
	require.Empty(t, cfg.SMTPURL)
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("PANEL_DATABASE_URL", "")
	t.Setenv("PANEL_BASE_URL", "https://panel.test")
	t.Setenv("PANEL_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	_, err := config.Load()
	require.ErrorContains(t, err, "PANEL_DATABASE_URL")
}

func TestLoadTrustedProxies(t *testing.T) {
	t.Setenv("PANEL_DATABASE_URL", "postgres://x")
	t.Setenv("PANEL_BASE_URL", "https://panel.test")
	t.Setenv("PANEL_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("PANEL_TRUSTED_PROXIES", "")
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Empty(t, cfg.TrustedProxies)

	t.Setenv("PANEL_TRUSTED_PROXIES", "172.16.0.0/12, 10.0.0.1")
	cfg, err = config.Load()
	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("10.0.0.1/32")}, cfg.TrustedProxies)

	t.Setenv("PANEL_TRUSTED_PROXIES", "bogus")
	_, err = config.Load()
	require.ErrorContains(t, err, "PANEL_TRUSTED_PROXIES")
}

func TestLoadValidatesBaseURL(t *testing.T) {
	t.Setenv("PANEL_DATABASE_URL", "postgres://x")
	t.Setenv("PANEL_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	for _, tc := range []struct {
		url   string
		ok    bool
		https bool
	}{
		{"https://panel.test", true, true},
		{"HTTPS://panel.test/", true, true},
		{"http://localhost:8080", true, false},
		{"panel.test", false, false},
		{"ftp://panel.test", false, false},
		{"https://", false, false},
		{"://bad", false, false},
	} {
		t.Setenv("PANEL_BASE_URL", tc.url)
		cfg, err := config.Load()
		if !tc.ok {
			require.ErrorContains(t, err, "PANEL_BASE_URL", tc.url)
			continue
		}
		require.NoError(t, err, tc.url)
		require.Equal(t, tc.https, cfg.HTTPS(), tc.url)
	}
}
