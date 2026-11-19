// Package httpapi wires the /v1 API and the embedded SPA.
package httpapi

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/config"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/ipam"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/mail"
	"github.com/Hexeption/nebla/internal/projects"
	"github.com/Hexeption/nebla/internal/ratelimit"
	"github.com/Hexeption/nebla/internal/settings"
	"github.com/Hexeption/nebla/internal/sshkeys"
	"github.com/Hexeption/nebla/web"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	// Mail defaults to a no-op sender.
	Mail         mail.Sender
	ProjectHooks projects.Hooks
	// Catalog defaults to a service keyed by Config.SecretKey with CatalogHooks.
	Catalog      *catalog.Service
	CatalogHooks catalog.Hooks
	// Jobs enqueues background work; nil means no job runtime.
	Jobs *jobs.Jobs
	// ScopedRoutes mounts extra non-strict routes under /v1 behind the project scope.
	ScopedRoutes func(r chi.Router)
}

type server struct {
	deps       Deps
	log        *slog.Logger
	auth       *auth.Service
	projects   *projects.Service
	settings   *settings.Store
	catalog    *catalog.Service
	ipam       *ipam.Service
	sshkeys    *sshkeys.Service
	loginLimit *ratelimit.Limiter
}

const maxBodyBytes = 1 << 20

var _ apigen.StrictServerInterface = (*server)(nil)

func (*server) GetHealthz(context.Context, apigen.GetHealthzRequestObject) (apigen.GetHealthzResponseObject, error) {
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
	if deps.Catalog == nil {
		deps.Catalog = catalog.New(deps.SQL, deps.Ent, catalog.Options{SecretKey: deps.Config.SecretKey, Hooks: deps.CatalogHooks})
	}
	s := &server{
		deps: deps,
		log:  log,
		auth: au,
		projects: projects.New(deps.Ent, projects.Options{
			Auth: au, Mail: deps.Mail, BaseURL: deps.Config.BaseURL, Logger: log, Hooks: deps.ProjectHooks,
		}),
		settings:   st,
		catalog:    deps.Catalog,
		ipam:       ipam.New(deps.SQL, deps.Ent),
		sshkeys:    sshkeys.New(deps.Ent),
		loginLimit: ratelimit.New(rate.Every(90*time.Second), 10),
	}
	strict := apigen.NewStrictHandlerWithOptions(s, []apigen.StrictMiddlewareFunc{s.authMiddleware}, apigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestError,
		ResponseErrorHandlerFunc: s.responseError,
	})

	r := chi.NewRouter()
	api := chi.NewRouter()
	api.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
			next.ServeHTTP(w, r)
		})
	})
	api.NotFound(notFound)
	api.MethodNotAllowed(methodNotAllowed)
	apigen.HandlerWithOptions(strict, apigen.ChiServerOptions{
		BaseRouter:       api,
		ErrorHandlerFunc: requestError,
	})
	if deps.ScopedRoutes != nil {
		api.Group(func(g chi.Router) {
			g.Use(s.ScopeHandler)
			deps.ScopedRoutes(g)
		})
	}
	r.Mount("/v1", api)
	r.NotFound(spaHandler(web.Dist).ServeHTTP)
	return r
}

func spaHandler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			notFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if f, err := dist.Open(name); err == nil {
				st, serr := f.Stat()
				f.Close()
				if serr == nil && !st.IsDir() {
					files.ServeHTTP(w, r)
					return
				}
			}
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}
