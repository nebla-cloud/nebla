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

package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/realip"
)

type Config struct {
	DatabaseURL string
	Listen      string
	BaseURL     string
	SecretKey   []byte
	SMTPURL     string
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is believed.
	TrustedProxies []netip.Prefix
	// DefaultQuota is applied to projects created by non-admins; 0 in a field means unlimited.
	DefaultQuota projects.Quota
	// MaxProjectsPerUser caps projects a non-admin owns; 0 means unlimited.
	MaxProjectsPerUser int
}

var defaultQuota = projects.Quota{MaxServers: 5, MaxVCPU: 8, MaxMemoryMB: 16384, MaxDiskGB: 200, MaxIPs: 5, MaxVolumes: 5, MaxVolumeGB: 100, MaxBackups: 35}

func Load() (Config, error) {
	c := Config{
		DatabaseURL: os.Getenv("PANEL_DATABASE_URL"),
		Listen:      os.Getenv("PANEL_LISTEN"),
		BaseURL:     os.Getenv("PANEL_BASE_URL"),
		SecretKey:   []byte(os.Getenv("PANEL_SECRET_KEY")),
		SMTPURL:     os.Getenv("PANEL_SMTP_URL"),
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("PANEL_DATABASE_URL is required"))
	}
	if err := checkBaseURL(c.BaseURL); err != nil {
		errs = append(errs, err)
	}
	if len(c.SecretKey) < 32 {
		errs = append(errs, fmt.Errorf("PANEL_SECRET_KEY must be at least 32 bytes, got %d", len(c.SecretKey)))
	}
	tp, err := realip.ParseCIDRs(os.Getenv("PANEL_TRUSTED_PROXIES"))
	if err != nil {
		errs = append(errs, fmt.Errorf("PANEL_TRUSTED_PROXIES: %w", err))
	}
	c.TrustedProxies = tp
	if c.DefaultQuota, err = parseQuota(os.Getenv("PANEL_DEFAULT_QUOTA")); err != nil {
		errs = append(errs, fmt.Errorf("PANEL_DEFAULT_QUOTA: %w", err))
	}
	c.MaxProjectsPerUser = 3
	if v := os.Getenv("PANEL_MAX_PROJECTS_PER_USER"); v != "" {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("PANEL_MAX_PROJECTS_PER_USER must be a non-negative integer, got %q", v))
		}
		c.MaxProjectsPerUser = n
	}
	return c, errors.Join(errs...)
}

// parseQuota overlays the keys set in spec on the default quota.
func parseQuota(spec string) (projects.Quota, error) {
	q := defaultQuota
	if strings.TrimSpace(spec) == "" {
		return q, nil
	}
	fields := map[string]*int{"servers": &q.MaxServers, "vcpu": &q.MaxVCPU, "memory_mb": &q.MaxMemoryMB, "disk_gb": &q.MaxDiskGB, "ips": &q.MaxIPs,
		"volumes": &q.MaxVolumes, "volume_gb": &q.MaxVolumeGB, "backups": &q.MaxBackups}
	seen := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		dst := fields[k]
		if !ok || dst == nil {
			return q, fmt.Errorf("invalid entry %q, want key=value with keys servers, vcpu, memory_mb, disk_gb, ips, volumes, volume_gb, backups", part)
		}
		if seen[k] {
			return q, fmt.Errorf("duplicate key %q", k)
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return q, fmt.Errorf("%s must be a non-negative integer, got %q", k, v)
		}
		seen[k], *dst = true, n
	}
	return q, nil
}

func checkBaseURL(raw string) error {
	if raw == "" {
		return errors.New("PANEL_BASE_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("PANEL_BASE_URL: %w", err)
	}
	if s := strings.ToLower(u.Scheme); (s != "http" && s != "https") || u.Host == "" {
		return fmt.Errorf("PANEL_BASE_URL must be an http or https URL with a host, got %q", raw)
	}
	return nil
}

// HTTPS reports whether the panel is served over https, so cookies are Secure.
func (c Config) HTTPS() bool {
	u, err := url.Parse(c.BaseURL)
	return err == nil && strings.EqualFold(u.Scheme, "https")
}

// PasskeyOrigin is BaseURL's scheme://host[:port] when passkeys can work there, else "".
func (c Config) PasskeyOrigin() string {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname())
	if scheme != "http" && scheme != "https" {
		return ""
	}
	_, ipErr := netip.ParseAddr(host)
	if host != "localhost" && (scheme != "https" || ipErr == nil) {
		return ""
	}
	host = strings.ToLower(u.Host)
	if port := u.Port(); (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return scheme + "://" + host
}
