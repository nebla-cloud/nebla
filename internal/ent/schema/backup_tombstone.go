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

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// BackupTombstone is an archive left to delete after its backup row is gone.
type BackupTombstone struct{ ent.Schema }

func (BackupTombstone) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("volid").Unique(),
		field.Text("storage"),
		field.Int64("location_id"),
		createdAt(),
		field.Int("attempts").Default(0),
	}
}
