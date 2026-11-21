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
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type APIToken struct{ ent.Schema }

func (APIToken) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("api_tokens")}
}

func (APIToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		// created_by is null for tokens made before creators were tracked; those never auto-revoke.
		field.Int64("created_by").Optional().Nillable().Immutable(),
		field.Text("name"),
		field.Text("permission").Immutable(),
		field.Bytes("token_hash").Immutable().Sensitive(),
		field.Time("last_used_at").Optional().Nillable(),
		createdAt(),
	}
}

func (APIToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("creator", User.Type).Field("created_by").Unique().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (APIToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique().StorageKey("api_tokens_token_hash_key"),
		index.Fields("project_id").StorageKey("api_tokens_project_id_idx"),
		index.Fields("created_by").StorageKey("api_tokens_created_by_idx"),
	}
}
