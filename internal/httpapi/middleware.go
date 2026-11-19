package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/auth"
)

const sessionCookie = "nb_session"

type ctxKey int

const (
	userKey ctxKey = iota
	requestKey
)

// publicOps are the operations that need no authentication.
var publicOps = map[string]bool{
	"GetHealthz":    true,
	"GetUiManifest": true,
	"PostSetup":     true,
	"PostAuthLogin": true,
	"PostSignup":    true,
}

// UserFrom returns the authenticated user set by RequireUser.
func UserFrom(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(userKey).(auth.User)
	return u, ok
}

func requestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey).(*http.Request)
	return r
}

// RequireUser authenticates the session cookie and stores the user on the context.
func (s *server) RequireUser(next apigen.StrictHandlerFunc) apigen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			return nil, apierr.New(http.StatusUnauthorized, "unauthorized", "authentication required")
		}
		u, err := s.auth.UserFromSession(ctx, c.Value)
		if err != nil {
			return nil, err
		}
		return next(context.WithValue(ctx, userKey, u), w, r, req)
	}
}

// RequireAdmin rejects non-admin users; it must run after RequireUser.
func (s *server) RequireAdmin(next apigen.StrictHandlerFunc) apigen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		if u, ok := UserFrom(ctx); !ok || !u.IsAdmin {
			return nil, apierr.New(http.StatusForbidden, "forbidden", "admin access required")
		}
		return next(ctx, w, r, req)
	}
}

// authMiddleware protects every operation except publicOps; /v1/admin/* needs an admin.
func (s *server) authMiddleware(next apigen.StrictHandlerFunc, op string) apigen.StrictHandlerFunc {
	inner := next
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		ctx = context.WithValue(ctx, requestKey, r)
		h := inner
		if !publicOps[op] {
			if strings.HasPrefix(r.URL.Path, "/v1/admin/") {
				h = s.RequireAdmin(h)
			}
			h = s.RequireUser(h)
		}
		return h(ctx, w, r, req)
	}
}
