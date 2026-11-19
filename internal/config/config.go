package config

import (
	"errors"
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	Listen      string
	BaseURL     string
	SecretKey   []byte
	SMTPURL     string
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
	if c.BaseURL == "" {
		errs = append(errs, errors.New("PANEL_BASE_URL is required"))
	}
	if len(c.SecretKey) < 32 {
		errs = append(errs, fmt.Errorf("PANEL_SECRET_KEY must be at least 32 bytes, got %d", len(c.SecretKey)))
	}
	return c, errors.Join(errs...)
}
