package pve_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
)

func TestLeafFingerprint(t *testing.T) {
	conn := fake.New(t).Conn()
	got, err := pve.LeafFingerprint(context.Background(), conn.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if want := pve.NormFP(conn.CAFingerprint); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestLeafFingerprintBadURL(t *testing.T) {
	if _, err := pve.LeafFingerprint(context.Background(), "https://127.0.0.1:1"); err == nil {
		t.Fatal("want error")
	}
}
