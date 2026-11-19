package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/Hexeption/nebla/internal/realip"
)

type Config struct {
	DatabaseURL string
	Listen      string
	BaseURL     string
	SecretKey   []byte
	SMTPURL     string
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is believed.
	TrustedProxies []netip.Prefix
}

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
	return c, errors.Join(errs...)
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
