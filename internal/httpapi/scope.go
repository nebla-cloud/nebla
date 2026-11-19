package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/projects"
)

// projectScopedOps are operations on project resources; they accept a token or a session plus Project-ID.
var projectScopedOps = map[string]bool{}

// RoleAPIToken is Scope.Role for requests authenticated by an API token.
const RoleAPIToken = "api_token"

// ProjectHeader selects the project for session-authenticated project requests.
const ProjectHeader = "Project-ID"

// Scope is the project a request acts on.
type Scope struct {
	ProjectID int64
	Role      string
	CanWrite  bool
}

func ScopeFrom(ctx context.Context) (Scope, bool) {
	sc, ok := ctx.Value(scopeKey).(Scope)
	return sc, ok
}

var errScopeNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")

// scope authenticates r and resolves its project, enforcing read-only access.
func (s *server) scope(ctx context.Context, r *http.Request) (context.Context, error) {
	p, err := s.authenticate(ctx, r)
	if err != nil {
		return ctx, err
	}
	var sc Scope
	if p.token != nil {
		sc = Scope{ProjectID: p.token.ProjectID, Role: RoleAPIToken, CanWrite: p.token.Permission == auth.PermReadWrite}
	} else {
		id, err := strconv.ParseInt(r.Header.Get(ProjectHeader), 10, 64)
		if err != nil {
			return ctx, errScopeNotFound
		}
		role, err := s.projects.Role(ctx, p.user.ID, id)
		if err != nil {
			return ctx, err
		}
		sc = Scope{ProjectID: id, Role: role, CanWrite: role != projects.RoleReadOnly}
		ctx = context.WithValue(ctx, userKey, *p.user)
	}
	if !sc.CanWrite && r.Method != http.MethodGet && r.Method != http.MethodHead {
		return ctx, apierr.New(http.StatusForbidden, "forbidden", "read-only access")
	}
	return context.WithValue(ctx, scopeKey, sc), nil
}

// RequireScope is the strict-middleware form of the project scope.
func (s *server) RequireScope(next apigen.StrictHandlerFunc) apigen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		ctx, err := s.scope(ctx, r)
		if err != nil {
			return nil, err
		}
		return next(ctx, w, r, req)
	}
}

// ScopeHandler is the plain-chi form of the project scope for non-strict routes.
func (s *server) ScopeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := s.scope(r.Context(), r)
		if err != nil {
			s.responseError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, requestKey, r)))
	})
}
