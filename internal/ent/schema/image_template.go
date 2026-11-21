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
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ImageTemplate struct{ ent.Schema }

func (ImageTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("image_id").Immutable(),
		field.Int64("location_id").Immutable(),
		// node is empty when the template's storage is shared across the location.
		field.Text("node").Default(""),
		field.Int("vmid"),
	}
}

func (ImageTemplate) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("image", Image.Type).Field("image_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (ImageTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("image_id", "location_id", "node").Unique().StorageKey("image_templates_image_location_node_key"),
		index.Fields("location_id").StorageKey("image_templates_location_id_idx"),
	}
}
