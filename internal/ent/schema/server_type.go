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
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ServerType struct{ ent.Schema }

func (ServerType) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Text("description").Default(""),
		field.Int("cores"),
		field.Int("memory_mb"),
		field.Int("disk_gb"),
		field.Bool("deprecated").Default(false),
		createdAt(),
	}
}

// Edges: an empty locations set means the type is offered everywhere.
func (ServerType) Edges() []ent.Edge {
	return []ent.Edge{edge.To("locations", Location.Type)}
}

func (ServerType) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name").Unique().StorageKey("server_types_name_key")}
}
