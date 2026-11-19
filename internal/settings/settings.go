// Package settings stores admin-editable key/value settings.
package settings

import (
	"context"
	"strconv"

	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/setting"
)

type Store struct{ db *ent.Client }

func New(db *ent.Client) *Store { return &Store{db: db} }

// Bool implements ext.SettingsReader.
func (s *Store) Bool(ctx context.Context, key string, def bool) bool {
	row, err := s.db.Setting.Get(ctx, key)
	if err != nil {
		return def
	}
	b, err := strconv.ParseBool(row.Value)
	if err != nil {
		return def
	}
	return b
}

func (s *Store) SetBool(ctx context.Context, key string, v bool) error {
	return s.db.Setting.Create().SetID(key).SetValue(strconv.FormatBool(v)).
		OnConflictColumns(setting.FieldID).UpdateValue().Exec(ctx)
}
