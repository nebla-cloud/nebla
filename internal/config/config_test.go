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

package config_test

import (
	"net/netip"
	"testing"

	"github.com/nebla-cloud/nebla/internal/config"
	"github.com/nebla-cloud/nebla/internal/projects"
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

func TestLoadDefaultQuota(t *testing.T) {
	t.Setenv("PANEL_DATABASE_URL", "postgres://x")
	t.Setenv("PANEL_BASE_URL", "https://panel.test")
	t.Setenv("PANEL_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, projects.Quota{MaxServers: 5, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxIPs: 5, MaxVolumes: 5, MaxVolumeGB: 100}, cfg.DefaultQuota)
	require.Equal(t, 3, cfg.MaxProjectsPerUser)

	t.Setenv("PANEL_DEFAULT_QUOTA", "servers=2, vcpu=0,memory_mb=1024,disk_gb=10,ips=1")
	t.Setenv("PANEL_MAX_PROJECTS_PER_USER", "0")
	cfg, err = config.Load()
	require.NoError(t, err)
	require.Equal(t, projects.Quota{MaxServers: 2, MaxMemoryMB: 1024, MaxDiskGB: 10, MaxIPs: 1, MaxVolumes: 5, MaxVolumeGB: 100}, cfg.DefaultQuota)
	require.Zero(t, cfg.MaxProjectsPerUser)

	t.Setenv("PANEL_DEFAULT_QUOTA", "servers=10")
	cfg, err = config.Load()
	require.NoError(t, err)
	require.Equal(t, projects.Quota{MaxServers: 10, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxIPs: 5, MaxVolumes: 5, MaxVolumeGB: 100}, cfg.DefaultQuota, "unset keys keep the defaults")

	t.Setenv("PANEL_DEFAULT_QUOTA", "ips=0")
	cfg, err = config.Load()
	require.NoError(t, err)
	require.Equal(t, projects.Quota{MaxServers: 5, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxVolumes: 5, MaxVolumeGB: 100}, cfg.DefaultQuota, "0 is unlimited")

	t.Setenv("PANEL_DEFAULT_QUOTA", "volumes=2,volume_gb=20")
	cfg, err = config.Load()
	require.NoError(t, err)
	require.Equal(t, projects.Quota{MaxServers: 5, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxIPs: 5, MaxVolumes: 2, MaxVolumeGB: 20}, cfg.DefaultQuota)

	for _, bad := range []string{"servers", "servers=x", "servers=-1", "bogus=1", "servers=1,servers=2"} {
		t.Setenv("PANEL_DEFAULT_QUOTA", bad)
		_, err = config.Load()
		require.ErrorContains(t, err, "PANEL_DEFAULT_QUOTA", bad)
	}
	t.Setenv("PANEL_DEFAULT_QUOTA", "")
	t.Setenv("PANEL_MAX_PROJECTS_PER_USER", "-2")
	_, err = config.Load()
	require.ErrorContains(t, err, "PANEL_MAX_PROJECTS_PER_USER")
}

func TestPasskeyOrigin(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8080":            "http://localhost:8080",
		"https://panel.example.com":        "https://panel.example.com",
		"https://panel.example.com:8443/x": "https://panel.example.com:8443",
		"https://10.0.0.5":                 "",
		"https://[::1]:8443":               "",
		"http://panel.example.com":         "",
		"http://127.0.0.1:8080":            "",
		"https://panel.example.com:443":    "https://panel.example.com",
		"http://localhost:80":              "http://localhost",
	}
	for in, want := range cases {
		require.Equal(t, want, config.Config{BaseURL: in}.PasskeyOrigin(), in)
	}
}
