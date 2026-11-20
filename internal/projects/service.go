// Package projects manages projects, members, invites, API tokens and quotas.
package projects

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/apitoken"
	"github.com/Hexeption/nebla/internal/ent/invite"
	"github.com/Hexeption/nebla/internal/ent/project"
	"github.com/Hexeption/nebla/internal/ent/projectmember"
	"github.com/Hexeption/nebla/internal/ent/user"
	"github.com/Hexeption/nebla/internal/mail"
)

const (
	RoleOwner    = "owner"
	RoleAdmin    = "admin"
	RoleMember   = "member"
	RoleReadOnly = "read_only"

	InviteTTL = 7 * 24 * time.Hour
)

var roles = []string{RoleOwner, RoleAdmin, RoleMember, RoleReadOnly}

// managers may change members, invites and tokens.
var managers = []string{RoleOwner, RoleAdmin}

var (
	errNotFound  = apierr.New(http.StatusNotFound, "not_found", "not found")
	errForbidden = apierr.New(http.StatusForbidden, "forbidden", "insufficient project role")
	errLastOwner = apierr.New(http.StatusConflict, "last_owner", "a project needs at least one owner")
)

// HasServersFunc reports whether a project still owns servers; the servers package supplies it.
type HasServersFunc func(ctx context.Context, db *ent.Client, projectID int64) (bool, error)

// UsageFunc returns a project's live usage, read through tx so it sees the caller's writes.
type UsageFunc func(ctx context.Context, tx *ent.Client, projectID int64) (Usage, error)

// Hooks let later packages plug server counts into projects; nil fields use the defaults.
type Hooks struct {
	// HasServers defaults to always empty.
	HasServers HasServersFunc
	// Usage defaults to zero usage.
	Usage UsageFunc
}

type Options struct {
	Auth    *auth.Service
	Mail    mail.Sender
	BaseURL string
	Logger  *slog.Logger
	// DefaultQuota is applied to projects created by non-admins; zero fields are unlimited.
	DefaultQuota Quota
	// MaxOwnedProjects caps projects a non-admin owns; 0 is unlimited.
	MaxOwnedProjects int
	Hooks
}

type Service struct {
	db   *ent.Client
	opts Options
}

func New(db *ent.Client, opts Options) *Service {
	if opts.Mail == nil {
		opts.Mail = mail.Noop{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.HasServers == nil {
		opts.HasServers = func(context.Context, *ent.Client, int64) (bool, error) { return false, nil }
	}
	if opts.Usage == nil {
		opts.Usage = func(context.Context, *ent.Client, int64) (Usage, error) { return Usage{}, nil }
	}
	return &Service{db: db, opts: opts}
}

type Project struct {
	ID        int64
	Name      string
	Role      string
	Quota     Quota
	Usage     Usage
	CreatedAt time.Time
}

type Member struct {
	UserID int64
	Email  string
	Name   string
	Role   string
}

type Token struct {
	ID         int64
	Name       string
	Permission string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

func toProject(p *ent.Project, role string) Project {
	return Project{ID: p.ID, Name: p.Name, Role: role, Quota: quotaOf(p), CreatedAt: p.CreatedAt}
}

func toMember(m *ent.ProjectMember) Member {
	out := Member{UserID: m.UserID, Role: m.Role}
	if u := m.Edges.User; u != nil {
		out.Email, out.Name = u.Email, u.Name
	}
	return out
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return "", apierr.New(http.StatusUnprocessableEntity, "invalid_input", "name must be 1-100 characters")
	}
	return name, nil
}

func validRole(r string) error {
	if !slices.Contains(roles, r) {
		return apierr.New(http.StatusUnprocessableEntity, "invalid_input", "role must be owner, admin, member or read_only")
	}
	return nil
}

// withTx opens a tx on db, or runs fn on db directly when db is already tx-bound.
func withTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := db.Tx(ctx)
	if errors.Is(err, ent.ErrTxStarted) {
		return fn(db)
	}
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx.Client()); err != nil {
		return err
	}
	return tx.Commit()
}

// Role returns the user's role in a project, or not_found when they aren't a member.
func (s *Service) Role(ctx context.Context, userID, projectID int64) (string, error) {
	return roleIn(ctx, s.db, userID, projectID)
}

func roleIn(ctx context.Context, db *ent.Client, userID, projectID int64) (string, error) {
	m, err := db.ProjectMember.Query().
		Where(projectmember.ProjectID(projectID), projectmember.UserID(userID)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	return m.Role, nil
}

func (s *Service) require(ctx context.Context, userID, projectID int64, allowed ...string) (string, error) {
	r, err := s.Role(ctx, userID, projectID)
	if err != nil {
		return "", err
	}
	if !slices.Contains(allowed, r) {
		return "", errForbidden
	}
	return r, nil
}

// Create makes a project owned by userID; non-admins get DefaultQuota and a cap on projects they created.
func (s *Service) Create(ctx context.Context, userID int64, isAdmin bool, name string) (Project, error) {
	name, err := validName(name)
	if err != nil {
		return Project{}, err
	}
	var p *ent.Project
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		c := tx.Project.Create().SetName(name).SetCreatedBy(userID)
		if !isAdmin {
			if s.opts.MaxOwnedProjects > 0 {
				// Lock the user row so concurrent creates cannot pass the cap together.
				if _, err := tx.User.Query().Where(user.ID(userID)).ForUpdate().Only(ctx); err != nil {
					return err
				}
				n, err := tx.Project.Query().Where(project.CreatedBy(userID)).Count(ctx)
				if err != nil {
					return err
				}
				if n >= s.opts.MaxOwnedProjects {
					return apierr.New(http.StatusUnprocessableEntity, "quota_exceeded", "project limit reached")
				}
			}
			q := s.opts.DefaultQuota
			c.SetMaxServers(q.MaxServers).SetMaxVcpu(q.MaxVCPU).SetMaxMemoryMB(q.MaxMemoryMB).SetMaxDiskGB(q.MaxDiskGB).SetMaxIps(q.MaxIPs)
		}
		if p, err = c.Save(ctx); err != nil {
			return err
		}
		return tx.ProjectMember.Create().SetProjectID(p.ID).SetUserID(userID).SetRole(RoleOwner).Exec(ctx)
	})
	if err != nil {
		return Project{}, err
	}
	return toProject(p, RoleOwner), nil
}

// AdminProject is a project with its first owner's email, for the admin list.
type AdminProject struct {
	ID         int64
	Name       string
	Quota      Quota
	OwnerEmail string
}

// ListAll returns one page of every project ordered by id, plus the total count.
func (s *Service) ListAll(ctx context.Context, page, perPage int) ([]AdminProject, int, error) {
	total, err := s.db.Project.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Project.Query().Order(project.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	owners, err := s.db.ProjectMember.Query().Where(projectmember.ProjectIDIn(ids...), projectmember.Role(RoleOwner)).
		WithUser().Order(projectmember.ByUserID(sql.OrderDesc())).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	email := map[int64]string{}
	for _, m := range owners {
		if m.Edges.User != nil {
			email[m.ProjectID] = m.Edges.User.Email // descending order leaves the lowest user id last
		}
	}
	out := make([]AdminProject, len(rows))
	for i, r := range rows {
		out[i] = AdminProject{ID: r.ID, Name: r.Name, Quota: quotaOf(r), OwnerEmail: email[r.ID]}
	}
	return out, total, nil
}

func (s *Service) List(ctx context.Context, userID int64) ([]Project, error) {
	ms, err := s.db.ProjectMember.Query().Where(projectmember.UserID(userID)).
		WithProject().Order(projectmember.ByProjectID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Project, len(ms))
	for i, m := range ms {
		out[i] = toProject(m.Edges.Project, m.Role)
		if out[i].Usage, err = s.opts.Usage(ctx, s.db, m.ProjectID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, userID, projectID int64) (Project, error) {
	r, err := s.Role(ctx, userID, projectID)
	if err != nil {
		return Project{}, err
	}
	p, err := s.db.Project.Get(ctx, projectID)
	if err != nil {
		return Project{}, notFound(err)
	}
	out := toProject(p, r)
	if out.Usage, err = s.opts.Usage(ctx, s.db, projectID); err != nil {
		return Project{}, err
	}
	return out, nil
}

func (s *Service) Rename(ctx context.Context, userID, projectID int64, name string) (Project, error) {
	r, err := s.require(ctx, userID, projectID, managers...)
	if err != nil {
		return Project{}, err
	}
	if name, err = validName(name); err != nil {
		return Project{}, err
	}
	p, err := s.db.Project.UpdateOneID(projectID).SetName(name).Save(ctx)
	if err != nil {
		return Project{}, notFound(err)
	}
	return toProject(p, r), nil
}

// Delete removes a project; it fails with project_not_empty while servers exist.
func (s *Service) Delete(ctx context.Context, userID, projectID int64) error {
	if _, err := s.require(ctx, userID, projectID, RoleOwner); err != nil {
		return err
	}
	return withTx(ctx, s.db, func(tx *ent.Client) error {
		if _, err := tx.Project.Query().Where(project.ID(projectID)).ForUpdate().Only(ctx); err != nil {
			return notFound(err)
		}
		busy, err := s.opts.HasServers(ctx, tx, projectID)
		if err != nil {
			return err
		}
		if busy {
			return apierr.New(http.StatusConflict, "project_not_empty", "delete the project's servers first")
		}
		return tx.Project.DeleteOneID(projectID).Exec(ctx)
	})
}

func (s *Service) Members(ctx context.Context, userID, projectID int64) ([]Member, error) {
	if _, err := s.Role(ctx, userID, projectID); err != nil {
		return nil, err
	}
	ms, err := s.db.ProjectMember.Query().Where(projectmember.ProjectID(projectID)).
		WithUser().Order(projectmember.ByUserID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Member, len(ms))
	for i, m := range ms {
		out[i] = toMember(m)
	}
	return out, nil
}

func (s *Service) Member(ctx context.Context, userID, projectID, memberID int64) (Member, error) {
	if _, err := s.Role(ctx, userID, projectID); err != nil {
		return Member{}, err
	}
	return member(ctx, s.db, projectID, memberID, false)
}

func member(ctx context.Context, db *ent.Client, projectID, memberID int64, lock bool) (Member, error) {
	q := db.ProjectMember.Query().
		Where(projectmember.ProjectID(projectID), projectmember.UserID(memberID)).WithUser()
	if lock {
		q = q.ForUpdate()
	}
	m, err := q.Only(ctx)
	if err != nil {
		return Member{}, notFound(err)
	}
	return toMember(m), nil
}

// changeMember locks the project and applies fn to the target member, guarding owner rules.
func (s *Service) changeMember(ctx context.Context, userID, projectID, memberID int64, newRole string, fn func(tx *ent.Client, m Member) error) (Member, error) {
	callerRole, err := s.require(ctx, userID, projectID, managers...)
	if err != nil {
		return Member{}, err
	}
	var out Member
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		if _, err := tx.Project.Query().Where(project.ID(projectID)).ForUpdate().Only(ctx); err != nil {
			return notFound(err)
		}
		m, err := member(ctx, tx, projectID, memberID, true)
		if err != nil {
			return err
		}
		if (m.Role == RoleOwner || newRole == RoleOwner) && callerRole != RoleOwner {
			return errForbidden
		}
		if m.Role == RoleOwner && newRole != RoleOwner {
			n, err := tx.ProjectMember.Query().
				Where(projectmember.ProjectID(projectID), projectmember.Role(RoleOwner)).Count(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return errLastOwner
			}
		}
		if err := fn(tx, m); err != nil {
			return err
		}
		m.Role = newRole
		out = m
		return nil
	})
	return out, err
}

func (s *Service) SetRole(ctx context.Context, userID, projectID, memberID int64, role string) (Member, error) {
	if err := validRole(role); err != nil {
		return Member{}, err
	}
	return s.changeMember(ctx, userID, projectID, memberID, role, func(tx *ent.Client, m Member) error {
		_, err := tx.ProjectMember.Update().
			Where(projectmember.ProjectID(projectID), projectmember.UserID(memberID)).SetRole(role).Save(ctx)
		if err != nil {
			return err
		}
		if !slices.Contains(managers, role) {
			return deleteInvitesBy(ctx, tx, projectID, memberID)
		}
		if m.Role == RoleOwner && role != RoleOwner {
			// Owner invites need an owner creator, so drop them on demotion.
			_, err = tx.Invite.Delete().Where(invite.ProjectID(projectID), invite.CreatedBy(memberID),
				invite.AcceptedAtIsNil(), invite.Role(RoleOwner)).Exec(ctx)
		}
		return err
	})
}

func (s *Service) RemoveMember(ctx context.Context, userID, projectID, memberID int64) error {
	_, err := s.changeMember(ctx, userID, projectID, memberID, "", func(tx *ent.Client, _ Member) error {
		_, err := tx.ProjectMember.Delete().
			Where(projectmember.ProjectID(projectID), projectmember.UserID(memberID)).Exec(ctx)
		if err != nil {
			return err
		}
		return deleteInvitesBy(ctx, tx, projectID, memberID)
	})
	return err
}

// deleteInvitesBy drops pending invites a user created once they can no longer manage the project.
func deleteInvitesBy(ctx context.Context, tx *ent.Client, projectID, creatorID int64) error {
	_, err := tx.Invite.Delete().
		Where(invite.ProjectID(projectID), invite.CreatedBy(creatorID), invite.AcceptedAtIsNil()).Exec(ctx)
	return err
}

// PendingInvite is an unaccepted, unexpired invite.
type PendingInvite struct {
	ID        int64
	Email     string
	Role      string
	CreatedBy int64
	ExpiresAt time.Time
}

func (s *Service) Invites(ctx context.Context, userID, projectID int64) ([]PendingInvite, error) {
	if _, err := s.require(ctx, userID, projectID, managers...); err != nil {
		return nil, err
	}
	rows, err := s.db.Invite.Query().
		Where(invite.ProjectID(projectID), invite.AcceptedAtIsNil(), invite.ExpiresAtGT(time.Now())).
		Order(invite.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PendingInvite, len(rows))
	for i, r := range rows {
		out[i] = PendingInvite{ID: r.ID, Email: r.Email, Role: r.Role, CreatedBy: r.CreatedBy, ExpiresAt: r.ExpiresAt}
	}
	return out, nil
}

func (s *Service) RevokeInvite(ctx context.Context, userID, projectID, inviteID int64) error {
	if _, err := s.require(ctx, userID, projectID, managers...); err != nil {
		return err
	}
	n, err := s.db.Invite.Delete().
		Where(invite.ID(inviteID), invite.ProjectID(projectID), invite.AcceptedAtIsNil()).Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

// Invite stores a single-use invite, emails it, and returns its URL.
func (s *Service) Invite(ctx context.Context, userID, projectID int64, email, role string) (string, error) {
	callerRole, err := s.require(ctx, userID, projectID, managers...)
	if err != nil {
		return "", err
	}
	if err := validRole(role); err != nil {
		return "", err
	}
	if role == RoleOwner && callerRole != RoleOwner {
		return "", errForbidden
	}
	email = auth.NormalizeEmail(email)
	if !strings.Contains(email, "@") || strings.ContainsAny(email, "\r\n ") {
		return "", apierr.New(http.StatusUnprocessableEntity, "invalid_input", "a valid email is required")
	}
	p, err := s.db.Project.Get(ctx, projectID)
	if err != nil {
		return "", notFound(err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	err = s.db.Invite.Create().SetProjectID(projectID).SetCreatedBy(userID).SetEmail(email).SetRole(role).
		SetTokenHash(auth.HashSecret(tok)).SetExpiresAt(time.Now().Add(InviteTTL)).Exec(ctx)
	if err != nil {
		return "", err
	}
	link := strings.TrimRight(s.opts.BaseURL, "/") + "/invite/" + tok
	body := fmt.Sprintf("You have been invited to the project %q.\n\nAccept the invite within 7 days:\n%s\n", p.Name, link)
	if err := s.opts.Mail.Send(ctx, email, "You're invited to "+oneLine(p.Name), body); err != nil {
		s.opts.Logger.Warn("send invite email", "err", err, "project_id", projectID)
	}
	return link, nil
}

func oneLine(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }

// Accepted is the outcome of AcceptInvite; NewUser is set when an account was created.
type Accepted struct {
	Project Project
	NewUser *auth.User
}

var errInviteMismatch = apierr.New(http.StatusForbidden, "invite_email_mismatch", "this invite was sent to a different email address")

var errLoginRequired = apierr.New(http.StatusUnauthorized, "login_required", "log in to accept this invite")

// AcceptInvite adds u, or a new account if selfSignup or an admin made the invite, to the invite's project.
func (s *Service) AcceptInvite(ctx context.Context, token string, u *auth.User, name, password string, selfSignup bool) (Accepted, error) {
	hash := auth.HashSecret(token)
	inv, err := s.liveInvite(ctx, s.db, hash, false)
	if err != nil {
		return Accepted{}, err
	}
	if u != nil && auth.NormalizeEmail(u.Email) != inv.Email {
		return Accepted{}, errInviteMismatch
	}
	if u == nil && !selfSignup {
		admin, err := s.db.User.Query().Where(user.ID(inv.CreatedBy), user.IsAdmin(true)).Exist(ctx)
		if err != nil {
			return Accepted{}, err
		}
		if !admin {
			return Accepted{}, errLoginRequired
		}
	}
	var out Accepted
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		var uid int64
		if u != nil {
			uid = u.ID
		} else {
			taken, err := tx.User.Query().Where(user.Email(inv.Email)).Exist(ctx)
			if err != nil {
				return err
			}
			if taken {
				return apierr.New(http.StatusConflict, "email_taken", "an account with this email exists; log in to accept the invite")
			}
			u, err := s.opts.Auth.CreateUserTx(ctx, tx, inv.Email, password, name, false)
			if err != nil {
				return err
			}
			uid, out.NewUser = u.ID, &u
		}
		inv, err := s.liveInvite(ctx, tx, hash, true)
		if err != nil {
			return err
		}
		// The creator must still manage the project (and own it, for owner invites).
		if r, err := roleIn(ctx, tx, inv.CreatedBy, inv.ProjectID); err != nil || !slices.Contains(managers, r) ||
			(inv.Role == RoleOwner && r != RoleOwner) {
			return errNotFound
		}
		if _, err := roleIn(ctx, tx, uid, inv.ProjectID); err == nil {
			return apierr.New(http.StatusConflict, "already_member", "you are already a member of this project")
		}
		if err := tx.ProjectMember.Create().SetProjectID(inv.ProjectID).SetUserID(uid).SetRole(inv.Role).Exec(ctx); err != nil {
			return err
		}
		if err := tx.Invite.UpdateOneID(inv.ID).SetAcceptedAt(time.Now()).Exec(ctx); err != nil {
			return err
		}
		p, err := tx.Project.Get(ctx, inv.ProjectID)
		if err != nil {
			return err
		}
		out.Project = toProject(p, inv.Role)
		return nil
	})
	if err != nil {
		return Accepted{}, err
	}
	if out.NewUser != nil {
		if err := s.opts.Auth.Registered(ctx, out.NewUser.ID); err != nil {
			return Accepted{}, err
		}
	}
	return out, nil
}

func (s *Service) liveInvite(ctx context.Context, db *ent.Client, hash []byte, lock bool) (*ent.Invite, error) {
	q := db.Invite.Query().Where(invite.TokenHash(hash), invite.AcceptedAtIsNil(), invite.ExpiresAtGT(time.Now()))
	if lock {
		q = q.ForUpdate()
	}
	inv, err := q.Only(ctx)
	return inv, notFound(err)
}

func (s *Service) Tokens(ctx context.Context, userID, projectID int64) ([]Token, error) {
	if _, err := s.require(ctx, userID, projectID, managers...); err != nil {
		return nil, err
	}
	rows, err := s.db.APIToken.Query().Where(apitoken.ProjectID(projectID)).Order(apitoken.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Token, len(rows))
	for i, r := range rows {
		out[i] = toToken(r)
	}
	return out, nil
}

func toToken(r *ent.APIToken) Token {
	return Token{ID: r.ID, Name: r.Name, Permission: r.Permission, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt}
}

// CreateToken returns the token row and its plaintext, which is never stored.
func (s *Service) CreateToken(ctx context.Context, userID, projectID int64, name, perm string) (Token, string, error) {
	if _, err := s.require(ctx, userID, projectID, managers...); err != nil {
		return Token{}, "", err
	}
	name, err := validName(name)
	if err != nil {
		return Token{}, "", err
	}
	if perm != auth.PermRead && perm != auth.PermReadWrite {
		return Token{}, "", apierr.New(http.StatusUnprocessableEntity, "invalid_input", "permission must be read or read_write")
	}
	plain, hash, err := auth.NewAPIToken()
	if err != nil {
		return Token{}, "", err
	}
	row, err := s.db.APIToken.Create().SetProjectID(projectID).SetName(name).SetPermission(perm).SetTokenHash(hash).Save(ctx)
	if err != nil {
		return Token{}, "", err
	}
	return toToken(row), plain, nil
}

func (s *Service) RevokeToken(ctx context.Context, userID, projectID, tokenID int64) error {
	if _, err := s.require(ctx, userID, projectID, managers...); err != nil {
		return err
	}
	n, err := s.db.APIToken.Delete().Where(apitoken.ID(tokenID), apitoken.ProjectID(projectID)).Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	var e apierr.E
	if errors.As(err, &e) {
		return e
	}
	return err
}
