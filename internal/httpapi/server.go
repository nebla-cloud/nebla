// Package httpapi wires the /v1 API and the embedded SPA.
package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/apigen"
	"github.com/nebla-cloud/nebla/internal/auth"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/config"
	"github.com/nebla-cloud/nebla/internal/console"
	"github.com/nebla-cloud/nebla/internal/enroll"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/mail"
	"github.com/nebla-cloud/nebla/internal/osimages"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/ratelimit"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/settings"
	"github.com/nebla-cloud/nebla/internal/sshkeys"
	"github.com/nebla-cloud/nebla/web"
	"golang.org/x/time/rate"
)

type Deps struct {
	Pool *pgxpool.Pool
	// SQL is the *sql.DB shared by Ent and db.WithTx.
	SQL    *sql.DB
	Ent    *ent.Client
	Config config.Config
	Logger *slog.Logger
	Ext    ext.Extensions
	// Bus carries domain events to Ext.BillingSink; workers publish on it.
	Bus *ext.Bus
	// Mail defaults to a no-op sender.
	Mail         mail.Sender
	ProjectHooks projects.Hooks
	// Catalog defaults to a service keyed by Config.SecretKey with CatalogHooks.
	Catalog      *catalog.Service
	CatalogHooks catalog.Hooks
	// OSImages serves the built-in OS image catalog; defaults to one over Catalog with no job runtime.
	OSImages *osimages.Service
	// Enroll defaults to a service over Catalog keyed by Config.SecretKey.
	Enroll *enroll.Service
	// Jobs enqueues background work; nil makes server create and delete return 503 service_unavailable.
	Jobs *jobs.Jobs
	// ScopedRoutes mounts extra non-strict routes under /v1 behind the project scope.
	ScopedRoutes func(r chi.Router)
	// APILimiter overrides the 3600/hour, burst 100 API limiter (tests).
	APILimiter *ratelimit.Limiter
}

type server struct {
	deps        Deps
	log         *slog.Logger
	auth        *auth.Service
	projects    *projects.Service
	settings    *settings.Store
	catalog     *catalog.Service
	osimages    *osimages.Service
	ipam        *ipam.Service
	sshkeys     *sshkeys.Service
	servers     *servers.Service
	console     *console.Service
	enroll      *enroll.Service
	loginLimit  *ratelimit.Limiter
	apiLimit    *ratelimit.Limiter
	enrollLimit *ratelimit.Limiter
}

const maxBodyBytes = 1 << 20

var _ apigen.StrictServerInterface = (*server)(nil)

const healthzTimeout = 2 * time.Second

func (s *server) GetHealthz(ctx context.Context, _ apigen.GetHealthzRequestObject) (apigen.GetHealthzResponseObject, error) {
	if s.deps.Pool != nil {
		pctx, cancel := context.WithTimeout(ctx, healthzTimeout)
		defer cancel()
		if err := s.deps.Pool.Ping(pctx); err != nil {
			s.log.Warn("healthz: database ping", "err", err)
			return apigen.GetHealthz503JSONResponse{Status: apigen.Unavailable}, nil
		}
	}
	return apigen.GetHealthz200JSONResponse{Status: apigen.Ok}, nil
}

// New builds the root handler: /v1 API plus the SPA.
func New(deps Deps) http.Handler {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	st := settings.New(deps.Ent)
	deps.Ext = deps.Ext.WithDefaults(st)
	if deps.Mail == nil {
		deps.Mail = mail.Noop{}
	}
	au := auth.New(deps.Ent, deps.Ext.Registration)
	deps.ProjectHooks, deps.CatalogHooks = ServerHooks(deps.ProjectHooks, deps.CatalogHooks)
	if deps.Catalog == nil {
		deps.Catalog = catalog.New(deps.SQL, deps.Ent, catalog.Options{SecretKey: deps.Config.SecretKey, Hooks: deps.CatalogHooks})
	}
	if deps.OSImages == nil {
		deps.OSImages = osimages.New(osimages.Options{SQL: deps.SQL, Ent: deps.Ent, Catalog: deps.Catalog, Jobs: deps.Jobs, Logger: log})
	}
	if deps.Enroll == nil {
		deps.Enroll = enroll.New(deps.SQL, deps.Ent, enroll.Options{SecretKey: deps.Config.SecretKey, BaseURL: deps.Config.BaseURL,
			Catalog: deps.Catalog, Logger: log})
	}
	proj := projects.New(deps.Ent, projects.Options{
		Auth: au, Mail: deps.Mail, BaseURL: deps.Config.BaseURL, Logger: log, Hooks: deps.ProjectHooks,
		DefaultQuota: deps.Config.DefaultQuota, MaxOwnedProjects: deps.Config.MaxProjectsPerUser,
	})
	s := &server{
		deps:        deps,
		log:         log,
		auth:        au,
		projects:    proj,
		settings:    st,
		catalog:     deps.Catalog,
		osimages:    deps.OSImages,
		ipam:        ipam.New(deps.SQL, deps.Ent),
		sshkeys:     sshkeys.New(deps.Ent),
		servers:     servers.New(servers.Options{SQL: deps.SQL, Ent: deps.Ent, Catalog: deps.Catalog, Projects: proj, Jobs: deps.Jobs}),
		enroll:      deps.Enroll,
		loginLimit:  ratelimit.New(rate.Every(90*time.Second), 10),
		apiLimit:    newAPILimiter(deps.APILimiter),
		enrollLimit: ratelimit.New(rate.Every(6*time.Second), 10),
	}
	s.console = console.New(console.Options{SQL: deps.SQL, Ent: deps.Ent, Catalog: deps.Catalog, BaseURL: deps.Config.BaseURL,
		Logger: log, WriteError: func(w http.ResponseWriter, err error) { s.responseError(w, nil, err) }})
	strict := apigen.NewStrictHandlerWithOptions(s, []apigen.StrictMiddlewareFunc{s.authMiddleware}, apigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestError,
		ResponseErrorHandlerFunc: s.responseError,
	})

	r := chi.NewRouter()
	api := chi.NewRouter()
	cop := http.NewCrossOriginProtection()
	// Behind a host-rewriting proxy, the configured panel origin is still trusted.
	if u, err := url.Parse(deps.Config.BaseURL); err == nil && u.Host != "" {
		if err := cop.AddTrustedOrigin(u.Scheme + "://" + u.Host); err != nil {
			log.Error("trust panel origin", "base_url", deps.Config.BaseURL, "err", err)
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.responseError(w, r, apierr.New(http.StatusForbidden, "forbidden", "cross-origin request rejected"))
	}))
	api.Use(cop.Handler)
	api.Use(s.rateLimitMiddleware)
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
			next.ServeHTTP(w, r)
		})
	})
	limited := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if err := rlFrom(r.Context()).byIP(); err != nil {
				s.responseError(w, r, err)
				return
			}
			h(w, r)
		}
	}
	api.NotFound(limited(notFound))
	api.MethodNotAllowed(limited(methodNotAllowed))
	apigen.HandlerWithOptions(strict, apigen.ChiServerOptions{
		BaseRouter:       api,
		ErrorHandlerFunc: requestError,
	})
	// The console websocket is outside the spec and auth: the one-time token in the path is its only credential.
	api.Get("/console/{token}", func(w http.ResponseWriter, r *http.Request) {
		if err := rlFrom(r.Context()).byIP(); err != nil {
			s.responseError(w, r, err)
			return
		}
		s.console.Serve(w, r, chi.URLParam(r, "token"))
	})
	if deps.ScopedRoutes != nil {
		api.Group(func(g chi.Router) {
			g.Use(s.ScopeHandler)
			deps.ScopedRoutes(g)
		})
	}
	r.Mount("/v1", api)
	r.NotFound(spaHandler(web.Dist, web.Placeholder).ServeHTTP)
	return r
}

// ServerHooks fills unset project and catalog hooks with the servers implementations.
func ServerHooks(p projects.Hooks, c catalog.Hooks) (projects.Hooks, catalog.Hooks) {
	if p.HasServers == nil {
		p.HasServers = servers.HasServers
	}
	if p.Usage == nil {
		p.Usage = servers.Usage
	}
	if c.LocationInUse == nil {
		c.LocationInUse = servers.LocationInUse
	}
	if c.TemplatesPending == nil {
		c.TemplatesPending = osimages.TemplatesPending
	}
	return p, c
}

func spaHandler(dist fs.FS, placeholder []byte) http.Handler {
	files := http.FileServerFS(dist)
	_, err := fs.Stat(dist, "index.html")
	built := err == nil
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			notFound(w, r)
			return
		}
		if !built {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(placeholder))
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" && name != "index.html" {
			if f, err := dist.Open(name); err == nil {
				st, serr := f.Stat()
				f.Close()
				if serr == nil && !st.IsDir() {
					if strings.HasPrefix(name, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					files.ServeHTTP(w, r)
					return
				}
			}
		}
		if strings.HasPrefix(name, "assets/") {
			// A stale hashed chunk must not be answered with the index.
			notFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}
