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

package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/auth"
)

const sessionCookie = "nb_session"

type ctxKey int

const (
	userKey ctxKey = iota
	requestKey
	scopeKey
)

var errUnauthorized = apierr.New(http.StatusUnauthorized, "unauthorized", "authentication required")

// publicOps are the operations that need no authentication.
var publicOps = map[string]bool{
	"GetHealthz":            true,
	"GetUiManifest":         true,
	"PostSetup":             true,
	"PostAuthLogin":         true,
	"PostAuthPasskeyBegin":  true,
	"PostAuthPasskeyFinish": true,
	"PostSignup":            true,
	"PostInviteAccept":      true,
	"GetInvite":             true,
	"GetEnrollScript":       true,
	"GetEnrollCheck":        true,
	"PostEnrollReport":      true,
}

// adminOps need a panel admin; TestAdminOpsMatchSpec keeps it equal to the spec's /admin/ paths.
var adminOps = map[string]bool{
	"GetAdminSettings":     true,
	"PutAdminSettings":     true,
	"GetAdminUsers":        true,
	"PostAdminUsers":       true,
	"PutAdminProjectQuota": true,
	"GetAdminProjects":     true,
	"GetAdminActions":      true,
	"GetAdminAction":       true,

	"PutAdminUser":            true,
	"DeleteAdminUser":         true,
	"PostAdminUserPassword":   true,
	"DeleteAdminUserTotp":     true,
	"DeleteAdminUserPasskeys": true,
	"GetAdminProject":         true,
	"DeleteAdminProject":      true,

	"GetAdminLocations":          true,
	"PostAdminLocations":         true,
	"GetAdminLocation":           true,
	"PutAdminLocation":           true,
	"DeleteAdminLocation":        true,
	"PostAdminLocationSyncNodes": true,
	"PutAdminLocationNode":       true,
	"GetAdminServerTypes":        true,
	"PostAdminServerTypes":       true,
	"GetAdminServerType":         true,
	"PutAdminServerType":         true,
	"DeleteAdminServerType":      true,
	"GetAdminImages":             true,
	"PostAdminImages":            true,
	"GetAdminImage":              true,
	"PutAdminImage":              true,
	"DeleteAdminImage":           true,
	"GetAdminIpPools":            true,
	"PostAdminIpPools":           true,
	"GetAdminIpPool":             true,
	"PutAdminIpPool":             true,
	"DeleteAdminIpPool":          true,
	"GetAdminCatalog":            true,
	"PostAdminCatalogEnable":     true,
	"PostAdminCatalogRefresh":    true,
	"PostAdminCatalogDisable":    true,

	"GetAdminLocationEnrollments":       true,
	"PostAdminLocationEnrollments":      true,
	"GetAdminLocationEnrollment":        true,
	"DeleteAdminLocationEnrollment":     true,
	"PostAdminLocationEnrollmentToken":  true,
	"PostAdminLocationEnrollmentReport": true,
	"PostAdminLocationEnrollmentFinish": true,
}

// UserFrom returns the authenticated user set by RequireUser or a session scope.
func UserFrom(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(userKey).(auth.User)
	return u, ok
}

func requestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey).(*http.Request)
	return r
}

// principal is who a request authenticates as: a session user or a project API token.
type principal struct {
	user  *auth.User
	token *auth.APIToken
	// session is the SHA-256 of the session cookie when user is set.
	session []byte
}

// authenticate prefers an Authorization bearer token and falls back to the session cookie.
func (s *server) authenticate(ctx context.Context, r *http.Request) (principal, error) {
	p, err := s.authenticateRaw(ctx, r)
	st := rlFrom(ctx)
	if err != nil {
		// Failed auth is limited by IP so bad credentials can't be tried freely.
		if lerr := st.byIP(); lerr != nil {
			return principal{}, lerr
		}
		return principal{}, err
	}
	key := "token:"
	if p.token != nil {
		key += strconv.FormatInt(p.token.ID, 10)
	} else {
		key = "user:" + strconv.FormatInt(p.user.ID, 10)
	}
	if err := st.consume(key); err != nil {
		return principal{}, err
	}
	return p, nil
}

func (s *server) authenticateRaw(ctx context.Context, r *http.Request) (principal, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return principal{}, errUnauthorized
		}
		t, err := s.auth.AuthenticateToken(ctx, strings.TrimSpace(tok))
		if err != nil {
			return principal{}, err
		}
		return principal{token: &t}, nil
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return principal{}, errUnauthorized
	}
	u, err := s.auth.UserFromSession(ctx, c.Value)
	if err != nil {
		return principal{}, err
	}
	return principal{user: &u, session: auth.HashSecret(c.Value)}, nil
}

// RequireUser needs a session; API tokens are rejected as unauthorized.
func (s *server) RequireUser(next apigen.StrictHandlerFunc) apigen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		p, err := s.authenticate(ctx, r)
		if err != nil {
			return nil, err
		}
		if p.user == nil {
			return nil, apierr.New(http.StatusUnauthorized, "unauthorized", "this endpoint needs a user session")
		}
		return next(context.WithValue(ctx, userKey, *p.user), w, r, req)
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

// authMiddleware: publicOps are open, projectScopedOps go through Scope, admin ops need an admin, the rest a session.
func (s *server) authMiddleware(next apigen.StrictHandlerFunc, op string) apigen.StrictHandlerFunc {
	h := next
	switch {
	case publicOps[op]:
		h = func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if err := rlFrom(ctx).byIP(); err != nil {
				return nil, err
			}
			return next(ctx, w, r, req)
		}
	case projectScopedOps[op]:
		h = s.RequireScope(h)
	case adminOps[op]:
		h = s.RequireUser(s.RequireAdmin(h))
	default:
		h = s.RequireUser(h)
	}
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		return h(context.WithValue(ctx, requestKey, r), w, r, req)
	}
}
