// Package httpapi wires the /v1 API and the embedded SPA.
package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/apigen"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/config"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/ratelimit"
	"github.com/Hexeption/nebla/internal/settings"
	"github.com/Hexeption/nebla/web"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"
)

type Deps struct {
	Pool   *pgxpool.Pool
	Config config.Config
	Logger *slog.Logger
	Ext    ext.Extensions
}

type server struct {
	deps       Deps
	log        *slog.Logger
	auth       *auth.Service
	settings   *settings.Store
	loginLimit *ratelimit.Limiter
}

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
	st := settings.New(deps.Pool)
	deps.Ext = deps.Ext.WithDefaults(st)
	s := &server{
		deps:       deps,
		log:        log,
		auth:       auth.New(deps.Pool, deps.Ext.Registration),
		settings:   st,
		loginLimit: ratelimit.New(rate.Every(90*time.Second), 10),
	}
	strict := apigen.NewStrictHandlerWithOptions(s, []apigen.StrictMiddlewareFunc{s.authMiddleware}, apigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestError,
		ResponseErrorHandlerFunc: s.responseError,
	})

	r := chi.NewRouter()
	api := chi.NewRouter()
	api.NotFound(notFound)
	api.MethodNotAllowed(methodNotAllowed)
	apigen.HandlerWithOptions(strict, apigen.ChiServerOptions{
		BaseRouter:       api,
		ErrorHandlerFunc: requestError,
	})
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
