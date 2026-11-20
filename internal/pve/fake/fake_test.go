package fake_test

import (
	"context"
	"testing"

	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/pve/fake"
)

func TestFakeAddToken(t *testing.T) {
	s := fake.New(t)
	c := s.Conn()
	c.TokenID, c.Secret = "nebla@pve!nebla-home-abc123", "s2"
	cl := pve.New(c)
	if _, err := cl.Version(context.Background()); err == nil {
		t.Fatal("want error before AddToken")
	}
	s.AddToken(c.TokenID, c.Secret)
	if _, err := cl.Version(context.Background()); err != nil {
		t.Fatal(err)
	}
}
