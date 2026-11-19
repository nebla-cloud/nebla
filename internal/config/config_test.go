package config_test

import (
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
