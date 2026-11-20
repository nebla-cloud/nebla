package auth_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

// A login that read the old hash before a password change commits must not get a session.
func TestLoginLosesRaceWithPasswordChange(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	s := auth.New(c, ext.Defaults(nil).Registration)
	u, err := s.Setup(ctx, "victim@example.com", pw, "V")
	require.NoError(t, err)
	keep, err := s.Login(ctx, "victim@example.com", pw, "")
	require.NoError(t, err)

	var armed atomic.Bool
	// The change commits right after Login's unlocked read of the user.
	c.User.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			v, err := next.Query(ctx, q)
			if err == nil && armed.CompareAndSwap(true, false) {
				require.NoError(t, s.ChangePassword(context.Background(), u.ID, pw, "another-long-password", keep))
			}
			return v, err
		})
	}))

	armed.Store(true)
	tok, err := s.Login(ctx, "victim@example.com", pw, "")
	if err == nil {
		_, err = s.UserFromSession(ctx, tok)
	}
	requireCode(t, err, http.StatusUnauthorized, "invalid_credentials")
	_, err = s.Login(ctx, "victim@example.com", "another-long-password", "")
	require.NoError(t, err)
}
