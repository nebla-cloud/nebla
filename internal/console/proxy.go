// Package console issues one-time VNC console tokens and proxies the websocket to Proxmox.
package console

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/apitoken"
	"github.com/nebla-cloud/nebla/internal/ent/consoletoken"
	"github.com/nebla-cloud/nebla/internal/ent/projectmember"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/session"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
)

const (
	// TokenTTL matches the ~10s vncproxy listen window, so a late redeem is a 404, not a 502.
	TokenTTL = 10 * time.Second
	// MaxSession and IdleTimeout are the defaults for Options.
	MaxSession  = 8 * time.Hour
	IdleTimeout = 30 * time.Minute
	// Recheck is how often an open console re-checks that its principal still has access.
	Recheck = 30 * time.Second
	// MaxBrowserMessage caps frames from the browser; MaxUpstreamMessage caps frames from Proxmox.
	MaxBrowserMessage  = 1 << 20
	MaxUpstreamMessage = 8 << 20

	tokenBytes   = 32
	dialTimeout  = 15 * time.Second
	subprotocol  = "binary"
	closeTimeout = 5 * time.Second
)

var (
	errNotFound   = apierr.New(http.StatusNotFound, "not_found", "not found")
	errNotRunning = apierr.New(http.StatusConflict, "server_not_running", "the server must be running")
	errUpstream   = apierr.New(http.StatusBadGateway, "pve_unreachable", "could not reach the Proxmox console")
	errOrigin     = apierr.New(http.StatusForbidden, "forbidden", "origin not allowed")
	errHandshake  = apierr.New(http.StatusBadRequest, "invalid_input", "a websocket upgrade request is required")
)

type Options struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// BaseURL is the panel's public URL; it builds wss_url and is the allowed Origin.
	BaseURL string
	Logger  *slog.Logger
	// WriteError writes an error envelope before the upgrade.
	WriteError func(w http.ResponseWriter, err error)
	// MaxSession and IdleTimeout default to the package constants.
	MaxSession, IdleTimeout time.Duration
	// Recheck defaults to the package constant.
	Recheck time.Duration
}

type Service struct {
	o      Options
	wsBase string
	// baseOrigin is scheme://host[:port] of BaseURL; it is the only allowed browser Origin.
	baseOrigin string
}

func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.MaxSession <= 0 {
		o.MaxSession = MaxSession
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = IdleTimeout
	}
	if o.Recheck <= 0 {
		o.Recheck = Recheck
	}
	s := &Service{o: o}
	base := strings.TrimRight(o.BaseURL, "/")
	s.wsBase = base
	if u, err := url.Parse(base); err == nil && u.Scheme != "" && u.Host != "" {
		s.baseOrigin = strings.ToLower(u.Scheme + "://" + u.Host)
		switch strings.ToLower(u.Scheme) {
		case "https":
			u.Scheme = "wss"
		case "http":
			u.Scheme = "ws"
		}
		s.wsBase = u.String()
	}
	return s
}

// Grant is what the browser needs to open the console.
type Grant struct {
	URL      string
	Password string
}

// Principal is who asked for a console: an API token, or a user with their session hash.
type Principal struct {
	UserID      int64
	SessionHash []byte
	TokenID     int64
}

// Request asks Proxmox for a VNC ticket and stores a one-time token for the project's running server.
func (s *Service) Request(ctx context.Context, projectID, serverID int64, who Principal) (Grant, error) {
	if who.TokenID == 0 && (who.UserID == 0 || len(who.SessionHash) == 0) {
		return Grant{}, errNotFound
	}
	row, err := s.o.Ent.Server.Query().Where(server.ID(serverID), server.ProjectID(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return Grant{}, errNotFound
	}
	if err != nil {
		return Grant{}, err
	}
	if row.Vmid == nil || row.Node == "" || row.Status != server.StatusRunning {
		return Grant{}, errNotRunning
	}
	pc, err := s.o.Catalog.ProxmoxFor(ctx, row.LocationID)
	if err != nil {
		return Grant{}, err
	}
	v, err := servers.OwnedVM(ctx, pc, row.ID, *row.Vmid)
	if errors.Is(err, servers.ErrMissing) {
		return Grant{}, err
	}
	if err != nil {
		s.o.Logger.Warn("console: list vms", "server_id", serverID, "err", err)
		return Grant{}, errUpstream
	}
	t, err := pc.VNCProxy(ctx, v.Node, v.VMID)
	if errors.Is(err, pve.ErrNotFound) {
		return Grant{}, errNotRunning
	}
	if err != nil {
		s.o.Logger.Warn("console: vncproxy", "server_id", serverID, "err", err)
		return Grant{}, errUpstream
	}
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Grant{}, err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	// Expired tokens are swept here, so the table stays small without a job.
	if _, err := s.o.Ent.ConsoleToken.Delete().Where(consoletoken.ExpiresAtLT(now)).Exec(ctx); err != nil {
		return Grant{}, err
	}
	err = s.o.Ent.ConsoleToken.Create().SetID(auth.HashSecret(tok)).SetServerID(serverID).SetProjectID(projectID).
		SetVmid(*row.Vmid).SetTicket(t.Ticket).SetPort(t.Port).SetExpiresAt(now.Add(TokenTTL)).
		SetNillableUserID(nonZero(who.UserID)).SetSessionHash(who.SessionHash).SetNillableAPITokenID(nonZero(who.TokenID)).Exec(ctx)
	if err != nil {
		return Grant{}, err
	}
	return Grant{URL: s.wsBase + "/v1/console/" + tok, Password: t.Ticket}, nil
}

type redeemed struct {
	serverID, projectID int64
	vmid, port          int
	ticket              string
	who                 Principal
}

func nonZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// consume deletes a live token and returns it; a second use finds no row.
func (s *Service) consume(ctx context.Context, tok string) (redeemed, bool, error) {
	if len(tok) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return redeemed{}, false, nil
	}
	var t redeemed
	var userID, tokenID sql.NullInt64
	err := s.o.SQL.QueryRowContext(ctx, `delete from console_tokens where token_hash = $1 and expires_at > $2
		returning server_id, project_id, vmid, port, ticket, user_id, session_hash, api_token_id`, auth.HashSecret(tok), time.Now()).
		Scan(&t.serverID, &t.projectID, &t.vmid, &t.port, &t.ticket, &userID, &t.who.SessionHash, &tokenID)
	if errors.Is(err, sql.ErrNoRows) {
		return redeemed{}, false, nil
	}
	t.who.UserID, t.who.TokenID = userID.Int64, tokenID.Int64
	return t, err == nil, err
}

// allowed reports whether who may still use a console in projectID; it fails open on database errors.
func (s *Service) allowed(ctx context.Context, projectID int64, who Principal) bool {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	var ok bool
	var err error
	if who.TokenID != 0 {
		ok, err = s.o.Ent.APIToken.Query().Where(apitoken.ID(who.TokenID), apitoken.ProjectID(projectID),
			apitoken.Permission(auth.PermReadWrite)).Exist(ctx)
	} else {
		ok, err = s.o.Ent.ProjectMember.Query().Where(projectmember.ProjectID(projectID), projectmember.UserID(who.UserID),
			projectmember.RoleNEQ(projects.RoleReadOnly)).Exist(ctx)
		if err == nil && ok {
			ok, err = s.o.Ent.Session.Query().Where(session.ID(who.SessionHash), session.UserID(who.UserID),
				session.ExpiresAtGT(time.Now())).Exist(ctx)
		}
	}
	if err != nil {
		s.o.Logger.Warn("console: recheck access", "err", err)
		return true
	}
	return ok
}

// originOK allows no Origin (non-browser clients) or exactly the panel's own origin.
func (s *Service) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" || s.baseOrigin == "" {
		return false
	}
	return strings.EqualFold(u.Scheme+"://"+u.Host, s.baseOrigin)
}

// isUpgrade reports whether r is a websocket version 13 handshake.
func isUpgrade(r *http.Request) bool {
	return headerHas(r.Header, "Upgrade", "websocket") && headerHas(r.Header, "Connection", "upgrade") &&
		r.Header.Get("Sec-WebSocket-Version") == "13" && r.Header.Get("Sec-WebSocket-Key") != ""
}

func headerHas(h http.Header, key, token string) bool {
	for _, v := range h.Values(key) {
		for p := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), token) {
				return true
			}
		}
	}
	return false
}

func (s *Service) fail(w http.ResponseWriter, err error) {
	if s.o.WriteError != nil {
		s.o.WriteError(w, err)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// Serve redeems tok and proxies the browser's websocket to the server's Proxmox VNC websocket.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, tok string) {
	if !s.originOK(r) {
		s.fail(w, errOrigin)
		return
	}
	if !isUpgrade(r) {
		s.fail(w, errHandshake)
		return
	}
	ctx := r.Context()
	t, ok, err := s.consume(ctx, tok)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		s.fail(w, errNotFound)
		return
	}
	row, err := s.o.Ent.Server.Query().Where(server.ID(t.serverID), server.ProjectID(t.projectID),
		server.StatusEQ(server.StatusRunning), server.VmidEQ(t.vmid)).Only(ctx)
	if ent.IsNotFound(err) {
		s.fail(w, errNotFound)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	node, err := s.ownedNode(ctx, row)
	if errors.Is(err, servers.ErrMissing) {
		s.fail(w, errNotFound)
		return
	}
	if err != nil {
		s.o.Logger.Warn("console: dial proxmox", "server_id", row.ID, "err", err)
		s.fail(w, errUpstream)
		return
	}
	up, err := s.dial(ctx, row, node, t)
	if err != nil {
		s.o.Logger.Warn("console: dial proxmox", "server_id", row.ID, "err", err)
		s.fail(w, errUpstream)
		return
	}
	opts := &websocket.AcceptOptions{Subprotocols: []string{subprotocol}}
	if s.baseOrigin != "" {
		opts.OriginPatterns = []string{escapePattern(s.baseOrigin)}
	}
	browser, err := websocket.Accept(w, r, opts)
	if err != nil {
		up.CloseNow()
		return
	}
	s.pipe(browser, up, func(ctx context.Context) bool { return s.allowed(ctx, t.projectID, t.who) })
}

// ownedNode is the node of row's VM if it still carries row's tags.
func (s *Service) ownedNode(ctx context.Context, row *ent.Server) (string, error) {
	pc, err := s.o.Catalog.ProxmoxFor(ctx, row.LocationID)
	if err != nil {
		return "", err
	}
	v, err := servers.OwnedVM(ctx, pc, row.ID, *row.Vmid)
	return v.Node, err
}

func (s *Service) dial(ctx context.Context, row *ent.Server, node string, t redeemed) (*websocket.Conn, error) {
	conn, err := s.o.Catalog.ConnFor(ctx, row.LocationID)
	if err != nil {
		return nil, err
	}
	pc, err := s.o.Catalog.ProxmoxFor(ctx, row.LocationID)
	if err != nil {
		return nil, err
	}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	hc := &http.Client{Transport: &http.Transport{TLSClientConfig: pve.TLSConfig(conn)}}
	c, _, err := websocket.Dial(dctx, pc.VNCWebsocketURL(node, t.vmid, pve.VNCTicket{Port: t.port, Ticket: t.ticket}),
		&websocket.DialOptions{HTTPClient: hc, HTTPHeader: pc.AuthHeader(), Subprotocols: []string{subprotocol}})
	if err != nil {
		return nil, scrub(err, t.ticket)
	}
	c.SetReadLimit(MaxUpstreamMessage)
	return c, nil
}

// copyFrames copies src to dst; it reports src's read error, or nil when dst failed.
func copyFrames(ctx context.Context, dst, src *websocket.Conn, last *atomic.Int64, out chan<- error) {
	for {
		typ, b, err := src.Read(ctx)
		if err != nil {
			out <- err
			return
		}
		last.Store(time.Now().UnixNano())
		if err := dst.Write(ctx, typ, b); err != nil {
			out <- nil
			return
		}
	}
}

// pipe copies frames both ways until a side closes, the session limit, the idle timeout or allowed turns false.
func (s *Service) pipe(browser, up *websocket.Conn, allowed func(context.Context) bool) {
	browser.SetReadLimit(MaxBrowserMessage)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	done := make(chan error, 2)
	go copyFrames(ctx, up, browser, &last, done)
	go copyFrames(ctx, browser, up, &last, done)

	limit := time.NewTimer(s.o.MaxSession)
	defer limit.Stop()
	tick := time.NewTicker(min(s.o.IdleTimeout/4, time.Minute))
	defer tick.Stop()
	recheck := time.NewTicker(s.o.Recheck)
	defer recheck.Stop()
	for {
		select {
		case err := <-done:
			code, reason := forwardStatus(err)
			closeBoth(browser, up, code, reason)
			<-done
			return
		case <-limit.C:
			closeBoth(browser, up, websocket.StatusPolicyViolation, "session limit reached")
			<-done
			<-done
			return
		case <-recheck.C:
			if allowed != nil && !allowed(ctx) {
				closeBoth(browser, up, websocket.StatusPolicyViolation, "access revoked")
				<-done
				<-done
				return
			}
		case <-tick.C:
			if time.Since(time.Unix(0, last.Load())) >= s.o.IdleTimeout {
				closeBoth(browser, up, websocket.StatusPolicyViolation, "idle timeout")
				<-done
				<-done
				return
			}
		}
	}
}

// forwardStatus maps a read error to the close status to pass on.
func forwardStatus(err error) (websocket.StatusCode, string) {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Code {
		case websocket.StatusNoStatusRcvd, websocket.StatusAbnormalClosure, websocket.StatusTLSHandshake:
			return websocket.StatusNormalClosure, ""
		}
		return ce.Code, ce.Reason
	}
	return websocket.StatusGoingAway, "peer went away"
}

// closeBoth closes both sides with the status in parallel; the side that closed first just finishes.
func closeBoth(a, b *websocket.Conn, code websocket.StatusCode, reason string) {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		if a.Close(code, reason) != nil {
			a.CloseNow()
		}
	}()
	if b.Close(code, reason) != nil {
		b.CloseNow()
	}
	select {
	case <-ch:
	case <-time.After(2 * closeTimeout):
		a.CloseNow()
	}
}

// escapePattern quotes path.Match metacharacters in a host.
func escapePattern(h string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`).Replace(h)
}

// scrubbed is a dial error with the VNC websocket URL's query and the ticket removed.
type scrubbed struct {
	msg   string
	inner error
}

func (e *scrubbed) Error() string { return e.msg }
func (e *scrubbed) Unwrap() error { return e.inner }

// scrub strips the query (port and vncticket) from any URL in err and redacts the ticket itself.
func scrub(err error, ticket string) error {
	msg := err.Error()
	var inner error
	var ue *url.Error
	if errors.As(err, &ue) {
		bare := ue.URL
		if i := strings.IndexByte(bare, '?'); i >= 0 {
			bare = bare[:i]
		}
		msg = strings.ReplaceAll(msg, ue.URL, bare)
		inner = ue.Err
	}
	if ticket != "" {
		msg = strings.NewReplacer(ticket, "[redacted]", url.QueryEscape(ticket), "[redacted]").Replace(msg)
	}
	return &scrubbed{msg: msg, inner: inner}
}
