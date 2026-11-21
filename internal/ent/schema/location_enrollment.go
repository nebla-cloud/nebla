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

type LocationEnrollment struct{ ent.Schema }

func (LocationEnrollment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Text("description").Default(""),
		field.Int64("location_id").Optional().Nillable(),
		field.Text("token_name"),
		field.Bytes("token_hash").Sensitive(),
		field.Enum("state").Values("waiting", "reported").Default("waiting"),
		field.Time("expires_at"),
		field.Text("token_id").Default(""),
		field.Bytes("token_secret").Optional().Sensitive(),
		field.Bytes("report").Optional(),
		field.Int64("created_by"),
		createdAt(),
	}
}

func (LocationEnrollment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("location", Location.Type).Field("location_id").Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("creator", User.Type).Field("created_by").Unique().Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (LocationEnrollment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique().StorageKey("location_enrollments_token_hash_key"),
		index.Fields("location_id").Unique().StorageKey("location_enrollments_location_id_key"),
		index.Fields("name").Unique().StorageKey("location_enrollments_new_name_key").
			Annotations(entsql.IndexWhere("location_id IS NULL")),
		index.Fields("expires_at").StorageKey("location_enrollments_expires_at_idx"),
	}
}
