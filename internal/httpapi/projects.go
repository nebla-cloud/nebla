package httpapi

import (
	"context"

	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/projects"
)

func apiQuota(q projects.Quota) apigen.Quota {
	return apigen.Quota{MaxServers: q.MaxServers, MaxVcpu: q.MaxVCPU, MaxMemoryMb: q.MaxMemoryMB, MaxDiskGb: q.MaxDiskGB, MaxIps: q.MaxIPs}
}

func apiProject(p projects.Project) apigen.Project {
	return apigen.Project{Id: p.ID, Name: p.Name, Role: apigen.Role(p.Role), Quota: apiQuota(p.Quota), Created: p.CreatedAt}
}

func apiMember(m projects.Member) apigen.Member {
	return apigen.Member{UserId: m.UserID, Email: m.Email, Name: m.Name, Role: apigen.Role(m.Role)}
}

func apiToken(t projects.Token) apigen.ApiToken {
	return apigen.ApiToken{Id: t.ID, Name: t.Name, Permission: apigen.TokenPermission(t.Permission), Created: t.CreatedAt, LastUsed: t.LastUsedAt}
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
	p, err := s.projects.Create(ctx, userID(ctx), req.Body.Name)
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

func (s *server) PostInviteAccept(ctx context.Context, req apigen.PostInviteAcceptRequestObject) (apigen.PostInviteAcceptResponseObject, error) {
	var uid int64
	if r := requestFrom(ctx); r != nil {
		if c, err := r.Cookie(sessionCookie); err == nil {
			if u, err := s.auth.UserFromSession(ctx, c.Value); err == nil {
				uid = u.ID
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
	acc, err := s.projects.AcceptInvite(ctx, req.Token, uid, name, pw)
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
		Id: a.Id, Name: a.Name, Permission: a.Permission, Created: a.Created, LastUsed: a.LastUsed, Token: plain,
	}, nil
}

func (s *server) DeleteProjectToken(ctx context.Context, req apigen.DeleteProjectTokenRequestObject) (apigen.DeleteProjectTokenResponseObject, error) {
	if err := s.projects.RevokeToken(ctx, userID(ctx), req.Id, req.TokenId); err != nil {
		return nil, err
	}
	return apigen.DeleteProjectToken204Response{}, nil
}

func (s *server) GetAdminUsers(ctx context.Context, req apigen.GetAdminUsersRequestObject) (apigen.GetAdminUsersResponseObject, error) {
	page, perPage := 1, 25
	if p := req.Params.Page; p != nil && *p > 0 {
		page = min(*p, 1_000_000)
	}
	if p := req.Params.PerPage; p != nil && *p > 0 {
		perPage = min(*p, 50)
	}
	us, total, err := s.auth.ListUsers(ctx, page, perPage)
	if err != nil {
		return nil, err
	}
	out := apigen.GetAdminUsers200JSONResponse{Users: make([]apigen.User, len(us))}
	for i, u := range us {
		out.Users[i] = apiUser(u)
	}
	out.Meta.Pagination.Page, out.Meta.Pagination.PerPage, out.Meta.Pagination.TotalEntries = page, perPage, total
	return out, nil
}

func (s *server) PostAdminUsers(ctx context.Context, req apigen.PostAdminUsersRequestObject) (apigen.PostAdminUsersResponseObject, error) {
	b := req.Body
	u, err := s.auth.CreateUser(ctx, b.Email, b.Password, b.Name, b.IsAdmin != nil && *b.IsAdmin)
	if err != nil {
		return nil, err
	}
	return apigen.PostAdminUsers201JSONResponse(apiUser(u)), nil
}

func (s *server) PutAdminProjectQuota(ctx context.Context, req apigen.PutAdminProjectQuotaRequestObject) (apigen.PutAdminProjectQuotaResponseObject, error) {
	b := req.Body
	q, err := s.projects.SetQuota(ctx, req.Id, projects.Quota{
		MaxServers: b.MaxServers, MaxVCPU: b.MaxVcpu, MaxMemoryMB: b.MaxMemoryMb, MaxDiskGB: b.MaxDiskGb, MaxIPs: b.MaxIps,
	})
	if err != nil {
		return nil, err
	}
	return apigen.PutAdminProjectQuota200JSONResponse(apiQuota(q)), nil
}
