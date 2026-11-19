// Package settings stores admin-editable key/value settings.
package settings

import (
	"context"
	"strconv"

	"github.com/Hexeption/nebla/internal/db/gen"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ q *gen.Queries }

func New(pool *pgxpool.Pool) *Store { return &Store{q: gen.New(pool)} }

// Bool implements ext.SettingsReader.
func (s *Store) Bool(ctx context.Context, key string, def bool) bool {
	v, err := s.q.GetSetting(ctx, key)
	if err != nil {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func (s *Store) SetBool(ctx context.Context, key string, v bool) error {
	return s.q.UpsertSetting(ctx, gen.UpsertSettingParams{Key: key, Value: strconv.FormatBool(v)})
}
