// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package settings stores admin-editable key/value settings.
package settings

import (
	"context"
	"strconv"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/setting"
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
