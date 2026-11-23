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

	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/projects"
)

func apiQuota(q projects.Quota) apigen.Quota {
	return apigen.Quota{MaxServers: q.MaxServers, MaxVcpu: q.MaxVCPU, MaxMemoryMb: q.MaxMemoryMB, MaxDiskGb: q.MaxDiskGB, MaxIps: q.MaxIPs,
		MaxVolumes: q.MaxVolumes, MaxVolumeGb: q.MaxVolumeGB, MaxBackups: q.MaxBackups, MaxNetworks: q.MaxNetworks}
}

func apiProject(p projects.Project) apigen.Project {
	return apigen.Project{Id: p.ID, Name: p.Name, Role: apigen.Role(p.Role), Quota: apiQuota(p.Quota), Created: p.CreatedAt,
		Usage: apiUsage(p.Usage)}
}

func apiMember(m projects.Member) apigen.Member {
	return apigen.Member{UserId: m.UserID, Email: m.Email, Name: m.Name, Role: apigen.Role(m.Role)}
}

func apiToken(t projects.Token) apigen.ApiToken {
	return apigen.ApiToken{Id: t.ID, Name: t.Name, Permission: apigen.TokenPermission(t.Permission), Created: t.CreatedAt, LastUsed: t.LastUsedAt, CreatedBy: t.CreatedBy}
}

func userID(ctx context.Context) int64 {
	u, _ := UserFrom(ctx)
	return u.ID
}

func (s *server) GetProjects(ctx context.Context, _ apigen.GetProjectsRequestObject) (apigen.GetProjectsResponseObject, error) {
	ps, err := s.projects.List(ctx, userID(ctx))
	if err != nil {
		return nil, err
	}
	out := apigen.GetProjects200JSONResponse{Projects: make([]apigen.Project, len(ps))}
	for i, p := range ps {
		out.Projects[i] = apiProject(p)
	}
	return out, nil
}

func (s *server) PostProjects(ctx context.Context, req apigen.PostProjectsRequestObject) (apigen.PostProjectsResponseObject, error) {
	u, _ := UserFrom(ctx)
	p, err := s.projects.Create(ctx, u.ID, u.IsAdmin, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return apigen.PostProjects201JSONResponse(apiProject(p)), nil
}

func (s *server) GetProject(ctx context.Context, req apigen.GetProjectRequestObject) (apigen.GetProjectResponseObject, error) {
	p, err := s.projects.Get(ctx, userID(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetProject200JSONResponse(apiProject(p)), nil
}

func (s *server) PutProject(ctx context.Context, req apigen.PutProjectRequestObject) (apigen.PutProjectResponseObject, error) {
	p, err := s.projects.Rename(ctx, userID(ctx), req.Id, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return apigen.PutProject200JSONResponse(apiProject(p)), nil
}

func (s *server) DeleteProject(ctx context.Context, req apigen.DeleteProjectRequestObject) (apigen.DeleteProjectResponseObject, error) {
	if err := s.projects.Delete(ctx, userID(ctx), req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteProject204Response{}, nil
}

func (s *server) GetProjectMembers(ctx context.Context, req apigen.GetProjectMembersRequestObject) (apigen.GetProjectMembersResponseObject, error) {
	ms, err := s.projects.Members(ctx, userID(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := apigen.GetProjectMembers200JSONResponse{Members: make([]apigen.Member, len(ms))}
	for i, m := range ms {
		out.Members[i] = apiMember(m)
	}
	return out, nil
}

func (s *server) GetProjectMember(ctx context.Context, req apigen.GetProjectMemberRequestObject) (apigen.GetProjectMemberResponseObject, error) {
	m, err := s.projects.Member(ctx, userID(ctx), req.Id, req.UserId)
	if err != nil {
		return nil, err
	}
	return apigen.GetProjectMember200JSONResponse(apiMember(m)), nil
}

func (s *server) PutProjectMember(ctx context.Context, req apigen.PutProjectMemberRequestObject) (apigen.PutProjectMemberResponseObject, error) {
	m, err := s.projects.SetRole(ctx, userID(ctx), req.Id, req.UserId, string(req.Body.Role))
	if err != nil {
		return nil, err
	}
	return apigen.PutProjectMember200JSONResponse(apiMember(m)), nil
}

func (s *server) DeleteProjectMember(ctx context.Context, req apigen.DeleteProjectMemberRequestObject) (apigen.DeleteProjectMemberResponseObject, error) {
	if err := s.projects.RemoveMember(ctx, userID(ctx), req.Id, req.UserId); err != nil {
		return nil, err
	}
	return apigen.DeleteProjectMember204Response{}, nil
}

func (s *server) PostProjectInvites(ctx context.Context, req apigen.PostProjectInvitesRequestObject) (apigen.PostProjectInvitesResponseObject, error) {
	url, err := s.projects.Invite(ctx, userID(ctx), req.Id, req.Body.Email, string(req.Body.Role))
	if err != nil {
		return nil, err
	}
	return apigen.PostProjectInvites201JSONResponse{InviteUrl: url}, nil
}

func (s *server) GetProjectInvites(ctx context.Context, req apigen.GetProjectInvitesRequestObject) (apigen.GetProjectInvitesResponseObject, error) {
	invs, err := s.projects.Invites(ctx, userID(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := apigen.GetProjectInvites200JSONResponse{Invites: make([]apigen.Invite, len(invs))}
	for i, v := range invs {
		out.Invites[i] = apigen.Invite{Id: v.ID, Email: v.Email, Role: apigen.Role(v.Role), CreatedBy: v.CreatedBy, Expires: v.ExpiresAt}
	}
	return out, nil
}

func (s *server) DeleteProjectInvite(ctx context.Context, req apigen.DeleteProjectInviteRequestObject) (apigen.DeleteProjectInviteResponseObject, error) {
	if err := s.projects.RevokeInvite(ctx, userID(ctx), req.Id, req.InviteId); err != nil {
		return nil, err
	}
	return apigen.DeleteProjectInvite204Response{}, nil
}

func (s *server) GetInvite(ctx context.Context, req apigen.GetInviteRequestObject) (apigen.GetInviteResponseObject, error) {
	pv, err := s.projects.PreviewInvite(ctx, req.Token, s.deps.Ext.SelfSignupEnabled(ctx))
	if err != nil {
		return nil, err
	}
	return apigen.GetInvite200JSONResponse{
		ProjectName: pv.ProjectName, Email: pv.Email, Role: apigen.Role(pv.Role), InviterName: pv.InviterName,
		Expires: pv.ExpiresAt, AccountExists: pv.AccountExists, CanCreateAccount: pv.CanCreateAccount,
	}, nil
}

func (s *server) PostInviteAccept(ctx context.Context, req apigen.PostInviteAcceptRequestObject) (apigen.PostInviteAcceptResponseObject, error) {
	var me *auth.User
	if r := requestFrom(ctx); r != nil {
		if c, err := r.Cookie(sessionCookie); err == nil {
			if u, err := s.auth.UserFromSession(ctx, c.Value); err == nil {
				me = &u
			}
		}
	}
	var name, pw string
	if b := req.Body; b != nil {
		if b.Name != nil {
			name = *b.Name
		}
		if b.Password != nil {
			pw = *b.Password
		}
	}
	acc, err := s.projects.AcceptInvite(ctx, req.Token, me, name, pw, me == nil && s.deps.Ext.SelfSignupEnabled(ctx))
	if err != nil {
		return nil, err
	}
	out := apigen.PostInviteAccept200JSONResponse{Body: apiProject(acc.Project)}
	if acc.NewUser != nil {
		tok, err := s.auth.NewSession(ctx, acc.NewUser.ID)
		if err != nil {
			return nil, err
		}
		out.Headers.SetCookie = new(s.sessionCookie(tok).String())
	}
	return out, nil
}

func (s *server) GetProjectTokens(ctx context.Context, req apigen.GetProjectTokensRequestObject) (apigen.GetProjectTokensResponseObject, error) {
	ts, err := s.projects.Tokens(ctx, userID(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := apigen.GetProjectTokens200JSONResponse{Tokens: make([]apigen.ApiToken, len(ts))}
	for i, t := range ts {
		out.Tokens[i] = apiToken(t)
	}
	return out, nil
}

func (s *server) PostProjectTokens(ctx context.Context, req apigen.PostProjectTokensRequestObject) (apigen.PostProjectTokensResponseObject, error) {
	t, plain, err := s.projects.CreateToken(ctx, userID(ctx), req.Id, req.Body.Name, string(req.Body.Permission))
	if err != nil {
		return nil, err
	}
	a := apiToken(t)
	return apigen.PostProjectTokens201JSONResponse{
		Id: a.Id, Name: a.Name, Permission: a.Permission, Created: a.Created, LastUsed: a.LastUsed, CreatedBy: a.CreatedBy, Token: plain,
	}, nil
}

func (s *server) DeleteProjectToken(ctx context.Context, req apigen.DeleteProjectTokenRequestObject) (apigen.DeleteProjectTokenResponseObject, error) {
	if err := s.projects.RevokeToken(ctx, userID(ctx), req.Id, req.TokenId); err != nil {
		return nil, err
	}
	return apigen.DeleteProjectToken204Response{}, nil
}

func (s *server) GetAdminUsers(ctx context.Context, req apigen.GetAdminUsersRequestObject) (apigen.GetAdminUsersResponseObject, error) {
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	us, total, err := s.auth.ListAdminUsers(ctx, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminUsers200JSONResponse{Users: make([]apigen.AdminUser, len(us))}
	for i, u := range us {
		out.Users[i] = apiAdminUser(u)
	}
	out.Meta = meta(page, perPage, total)
	return out, nil
}

func (s *server) PostAdminUsers(ctx context.Context, req apigen.PostAdminUsersRequestObject) (apigen.PostAdminUsersResponseObject, error) {
	b := req.Body
	u, err := s.auth.CreateUser(ctx, b.Email, b.Password, b.Name, b.IsAdmin != nil && *b.IsAdmin)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminUsers201JSONResponse(apiAdminUser(auth.AdminUser{User: u})), nil
}

func apiAdminUser(u auth.AdminUser) apigen.AdminUser {
	return apigen.AdminUser{Id: u.ID, Email: u.Email, Name: u.Name, IsAdmin: u.IsAdmin, TotpEnabled: u.TOTPEnabled,
		PasskeyCount: u.PasskeyCount}
}

func (s *server) PutAdminUser(ctx context.Context, req apigen.PutAdminUserRequestObject) (apigen.PutAdminUserResponseObject, error) {
	b := req.Body
	u, err := s.auth.AdminUpdateUser(ctx, userID(ctx), req.Id, b.Name, b.Email, b.IsAdmin, sessionToken(ctx))
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminUser200JSONResponse(apiAdminUser(u)), nil
}

func (s *server) DeleteAdminUser(ctx context.Context, req apigen.DeleteAdminUserRequestObject) (apigen.DeleteAdminUserResponseObject, error) {
	takeOver := req.Params.TakeOver != nil && *req.Params.TakeOver
	if err := s.projects.DeleteUser(ctx, userID(ctx), req.Id, takeOver); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminUser204Response{}, nil
}

func (s *server) PostAdminUserPassword(ctx context.Context, req apigen.PostAdminUserPasswordRequestObject) (apigen.PostAdminUserPasswordResponseObject, error) {
	if err := s.auth.AdminSetPassword(ctx, userID(ctx), req.Id, req.Body.Password, req.Body.RemovePasskeys); err != nil {
		return nil, err
	}
	return apigen.PostAdminUserPassword204Response{}, nil
}

func (s *server) DeleteAdminUserTotp(ctx context.Context, req apigen.DeleteAdminUserTotpRequestObject) (apigen.DeleteAdminUserTotpResponseObject, error) {
	if err := s.auth.AdminResetTOTP(ctx, userID(ctx), req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminUserTotp204Response{}, nil
}

func (s *server) DeleteAdminUserPasskeys(ctx context.Context, req apigen.DeleteAdminUserPasskeysRequestObject) (apigen.DeleteAdminUserPasskeysResponseObject, error) {
	if err := s.auth.AdminRemovePasskeys(ctx, userID(ctx), req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminUserPasskeys204Response{}, nil
}

func (s *server) GetAdminProjects(ctx context.Context, req apigen.GetAdminProjectsRequestObject) (apigen.GetAdminProjectsResponseObject, error) {
	page, perPage := pageParams(req.Params.Page, req.Params.PerPage)
	ps, total, err := s.projects.ListAll(ctx, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminProjects200JSONResponse{Projects: make([]apigen.AdminProject, len(ps))}
	for i, p := range ps {
		out.Projects[i] = apiAdminProject(p)
	}
	out.Meta = meta(page, perPage, total)
	return out, nil
}

func apiUsage(u projects.Usage) apigen.ProjectUsage {
	return apigen.ProjectUsage{Servers: u.Servers, Vcpu: u.VCPU, MemoryMb: u.MemoryMB, DiskGb: u.DiskGB, Ips: u.IPs,
		Volumes: u.Volumes, VolumeGb: u.VolumeGB, Backups: u.Backups, BackupsReserved: u.BackupsReserved, Networks: u.Networks}
}

func apiAdminProject(p projects.AdminProject) apigen.AdminProject {
	return apigen.AdminProject{Id: p.ID, Name: p.Name, Quota: apiQuota(p.Quota), OwnerEmail: p.OwnerEmail,
		Usage: apiUsage(p.Usage), MemberCount: p.MemberCount, Created: p.Created}
}

func (s *server) GetAdminProject(ctx context.Context, req apigen.GetAdminProjectRequestObject) (apigen.GetAdminProjectResponseObject, error) {
	p, err := s.projects.AdminGet(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	a := apiAdminProject(p.AdminProject)
	out := apigen.GetAdminProject200JSONResponse{Id: a.Id, Name: a.Name, Quota: a.Quota, OwnerEmail: a.OwnerEmail,
		Usage: a.Usage, MemberCount: a.MemberCount, Created: a.Created, Members: make([]apigen.Member, len(p.Members))}
	for i, m := range p.Members {
		out.Members[i] = apiMember(m)
	}
	return out, nil
}

func (s *server) DeleteAdminProject(ctx context.Context, req apigen.DeleteAdminProjectRequestObject) (apigen.DeleteAdminProjectResponseObject, error) {
	if err := s.projects.AdminDelete(ctx, req.Id); err != nil {
		return nil, err
	}
	return apigen.DeleteAdminProject204Response{}, nil
}

func (s *server) PutAdminProjectQuota(ctx context.Context, req apigen.PutAdminProjectQuotaRequestObject) (apigen.PutAdminProjectQuotaResponseObject, error) {
	b := req.Body
	q := projects.Quota{MaxServers: b.MaxServers, MaxVCPU: b.MaxVcpu, MaxMemoryMB: b.MaxMemoryMb, MaxDiskGB: b.MaxDiskGb, MaxIPs: b.MaxIps}
	q, err := s.projects.UpdateQuota(ctx, req.Id, q, b.MaxVolumes, b.MaxVolumeGb, b.MaxBackups, b.MaxNetworks, nil, nil)
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminProjectQuota200JSONResponse(apiQuota(q)), nil
}
