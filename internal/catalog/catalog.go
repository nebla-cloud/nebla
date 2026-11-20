// Package catalog manages locations, nodes, server types and images.
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/crypto"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// InUseFunc reports whether something outside the catalog still references a location.
type InUseFunc func(ctx context.Context, tx *ent.Client, locationID int64) (bool, error)

// Hooks let later packages block catalog deletes; nil fields use the defaults.
type Hooks struct {
	// LocationInUse reports servers or IP pools in a location; defaults to never.
	LocationInUse InUseFunc
	// TemplatesPending reports live jobs still removing catalog templates; defaults to never.
	TemplatesPending InUseFunc
}

type Options struct {
	// SecretKey is PANEL_SECRET_KEY; token secrets are sealed with it.
	SecretKey []byte
	// PollInterval is the task poll period for clients; zero uses the pve default.
	PollInterval time.Duration
	// NewClient builds Proxmox clients; defaults to pve.New.
	NewClient func(pve.Conn) pve.Client
	Hooks
}

type Service struct {
	db     *ent.Client
	sql    *sql.DB
	box    *crypto.Box
	boxErr error
	opts   Options

	mu      sync.Mutex
	clients map[int64]cachedClient
}

type cachedClient struct {
	updated time.Time
	client  pve.Client
}

// New builds the service; sqlDB must be the *sql.DB client was opened on.
func New(sqlDB *sql.DB, client *ent.Client, opts Options) *Service {
	if opts.NewClient == nil {
		opts.NewClient = pve.New
	}
	if opts.LocationInUse == nil {
		opts.LocationInUse = func(context.Context, *ent.Client, int64) (bool, error) { return false, nil }
	}
	if opts.TemplatesPending == nil {
		opts.TemplatesPending = func(context.Context, *ent.Client, int64) (bool, error) { return false, nil }
	}
	box, err := crypto.NewBox(opts.SecretKey)
	return &Service{db: client, sql: sqlDB, box: box, boxErr: err, opts: opts, clients: map[int64]cachedClient{}}
}

const pveTimeout = 20 * time.Second

var errNotFound = apierr.New(http.StatusNotFound, "not_found", "not found")

func invalid(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", fmt.Sprintf(format, a...))
}

func unreachable(err error) error {
	return apierr.New(http.StatusUnprocessableEntity, "pve_unreachable", "cannot reach Proxmox: "+err.Error())
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// mapWriteErr turns unique violations into uniqueness_error and FK violations into resource_in_use.
func mapWriteErr(err error, what string) error {
	switch pgCode(err) {
	case "23505":
		return apierr.New(http.StatusConflict, "uniqueness_error", what+" name is already taken")
	case "23503":
		return apierr.New(http.StatusConflict, "resource_in_use", what+" is still in use")
	}
	return notFound(err)
}

func (s *Service) withTx(ctx context.Context, fn func(tx *ent.Client) error) error {
	return db.WithTx(ctx, s.sql, func(_ *sql.Tx, etx *ent.Client) error { return fn(etx) })
}

func pageBounds(page, perPage int) (offset, limit int) {
	return (max(page, 1) - 1) * max(perPage, 1), max(perPage, 1)
}
