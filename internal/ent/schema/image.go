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

type Image struct{ ent.Schema }

func (Image) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Text("description").Default(""),
		field.Text("os_flavor"),
		field.Text("os_version"),
		field.Text("catalog_key").Optional().Nillable(),
		createdAt(),
	}
}

func (Image) Edges() []ent.Edge {
	return []ent.Edge{edge.From("templates", ImageTemplate.Type).Ref("image")}
}

func (Image) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique().StorageKey("images_name_key"),
		index.Fields("catalog_key").Unique().StorageKey("images_catalog_key_key"),
	}
}
