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

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/projects"
)

// projectScopedOps take a token or a session plus Project-ID; a test keeps it equal to the spec.
var projectScopedOps = map[string]bool{
	"GetLocations":        true,
	"GetLocation":         true,
	"GetServerTypes":      true,
	"GetServerType":       true,
	"GetImages":           true,
	"GetImage":            true,
	"GetSshKeys":          true,
	"PostSshKeys":         true,
	"GetSshKey":           true,
	"PutSshKey":           true,
	"DeleteSshKey":        true,
	"GetActions":          true,
	"GetAction":           true,
	"GetServers":          true,
	"PostServers":         true,
	"GetServer":           true,
	"PutServer":           true,
	"DeleteServer":        true,
	"GetServerActions":    true,
	"PoweronServer":       true,
	"PoweroffServer":      true,
	"ShutdownServer":      true,
	"RebootServer":        true,
	"ResetServer":         true,
	"RebuildServer":       true,
	"ChangeServerType":    true,
	"GetServerBackups":    true,
	"PostServerBackups":   true,
	"GetServerBackup":     true,
	"DeleteServerBackup":  true,
	"RestoreServerBackup": true,
	"GetVolumes":          true,
	"PostVolumes":         true,
	"GetVolume":           true,
	"PutVolume":           true,
	"DeleteVolume":        true,
	"GetVolumeActions":    true,
	"AttachVolume":        true,
	"DetachVolume":        true,
	"ResizeVolume":        true,

	"GetFloatingIps":       true,
	"PostFloatingIps":      true,
	"GetFloatingIp":        true,
	"PutFloatingIp":        true,
	"DeleteFloatingIp":     true,
	"GetFloatingIpActions": true,
	"AssignFloatingIp":     true,
	"UnassignFloatingIp":   true,

	"GetNetworks":             true,
	"PostNetworks":            true,
	"GetNetwork":              true,
	"PutNetwork":              true,
	"DeleteNetwork":           true,
	"GetNetworkActions":       true,
	"AttachServerToNetwork":   true,
	"DetachServerFromNetwork": true,

	"GetFirewalls":                true,
	"PostFirewalls":               true,
	"GetFirewall":                 true,
	"PutFirewall":                 true,
	"DeleteFirewall":              true,
	"GetFirewallActions":          true,
	"SetFirewallRules":            true,
	"ApplyFirewallToResources":    true,
	"RemoveFirewallFromResources": true,

	"RequestServerConsole": true,
	"GetServerMetrics":     true,
}

// RoleAPIToken is Scope.Role for requests authenticated by an API token.
const RoleAPIToken = "api_token"

// ProjectHeader selects the project for session-authenticated project requests.
const ProjectHeader = "Project-ID"

// Scope is the project a request acts on.
type Scope struct {
	ProjectID int64
	Role      string
	CanWrite  bool
	// Who the scope belongs to: TokenID for an API token, else UserID and SessionHash.
	TokenID     int64
	UserID      int64
	SessionHash []byte
}

func ScopeFrom(ctx context.Context) (Scope, bool) {
	sc, ok := ctx.Value(scopeKey).(Scope)
	return sc, ok
}

var errScopeNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")

// mustScope fails closed with not_found when a handler runs without a resolved project.
func mustScope(ctx context.Context) (Scope, error) {
	sc, ok := ScopeFrom(ctx)
	if !ok || sc.ProjectID == 0 {
		return Scope{}, errScopeNotFound
	}
	return sc, nil
}

// scope authenticates r and resolves its project, enforcing read-only access.
func (s *server) scope(ctx context.Context, r *http.Request) (context.Context, error) {
	p, err := s.authenticate(ctx, r)
	if err != nil {
		return ctx, err
	}
	var sc Scope
	if p.token != nil {
		sc = Scope{ProjectID: p.token.ProjectID, Role: RoleAPIToken, CanWrite: p.token.Permission == auth.PermReadWrite,
			TokenID: p.token.ID}
	} else {
		id, err := strconv.ParseInt(r.Header.Get(ProjectHeader), 10, 64)
		if err != nil {
			return ctx, errScopeNotFound
		}
		role, err := s.projects.Role(ctx, p.user.ID, id)
		if err != nil {
			return ctx, err
		}
		sc = Scope{ProjectID: id, Role: role, CanWrite: role != projects.RoleReadOnly, UserID: p.user.ID, SessionHash: p.session}
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
