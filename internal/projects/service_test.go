package projects_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Hexeption/nebla/internal/apierr"
	"github.com/Hexeption/nebla/internal/auth"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ext"
	"github.com/Hexeption/nebla/internal/projects"
	"github.com/Hexeption/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T, opts projects.Options) (*projects.Service, *ent.Client, *sql.DB, auth.User) {
	t.Helper()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	au := auth.New(client, ext.Defaults(nil).Registration)
	u, err := au.CreateUser(context.Background(), "owner@example.com", testutil.Password, "Owner", false)
	require.NoError(t, err)
	opts.Auth = au
	return projects.New(client, opts), client, sqlDB, u
}

func TestCreatorIsOwner(t *testing.T) {
	ctx := context.Background()
	svc, _, _, u := setup(t, projects.Options{})
	p, err := svc.Create(ctx, u.ID, "Web")
	require.NoError(t, err)
	require.Equal(t, projects.RoleOwner, p.Role)

	got, err := svc.Get(ctx, u.ID, p.ID)
	require.NoError(t, err)
	require.Equal(t, "Web", got.Name)
	require.Equal(t, projects.RoleOwner, got.Role)

	ms, err := svc.Members(ctx, u.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, ms, 1)
	require.Equal(t, u.ID, ms[0].UserID)
	require.Equal(t, projects.RoleOwner, ms[0].Role)
}

func TestQuotaExceeded(t *testing.T) {
	ctx := context.Background()
	existing := projects.Usage{Servers: 1, VCPU: 2}
	svc, _, sqlDB, u := setup(t, projects.Options{Hooks: projects.Hooks{
		Usage: func(ctx context.Context, tx *ent.Client, projectID int64) (projects.Usage, error) {
			return existing, nil
		},
	}})
	p, err := svc.Create(ctx, u.ID, "Web")
	require.NoError(t, err)
	_, err = svc.SetQuota(ctx, p.ID, projects.Quota{MaxServers: 1})
	require.NoError(t, err)

	reserve := func(d projects.Usage) error {
		return db.WithTx(ctx, sqlDB, func(_ *sql.Tx, etx *ent.Client) error {
			return svc.ReserveQuota(ctx, etx, p.ID, d)
		})
	}
	err = reserve(projects.Usage{Servers: 1, VCPU: 2})
	var e apierr.E
	require.True(t, errors.As(err, &e), "got %v", err)
	require.Equal(t, 422, e.Status)
	require.Equal(t, "quota_exceeded", e.Code)
	require.Contains(t, e.Msg, "max_servers")

	existing = projects.Usage{}
	require.NoError(t, reserve(projects.Usage{Servers: 1, VCPU: 64}), "0 limits are unlimited")
}
