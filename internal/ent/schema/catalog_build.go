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
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CatalogBuild struct{ ent.Schema }

func (CatalogBuild) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("catalog_key").Immutable(),
		field.Int64("location_id").Immutable(),
		field.Text("node").Default("").Immutable(),
		field.Enum("status").Values("building", "ready", "failed").Default("building"),
		field.Int("step").Default(0),
		field.Int("vmid").Optional().Nillable(),
		field.Int("new_vmid").Optional().Nillable(),
		field.Text("nonce").Optional().Nillable(),
		field.Text("new_nonce").Optional().Nillable(),
		field.Text("upid").Optional().Nillable(),
		field.Text("build_node").Optional().Nillable(),
		field.Text("file").Optional().Nillable(),
		field.Text("checksum").Optional().Nillable(),
		field.Text("new_file").Optional().Nillable(),
		field.Text("new_checksum").Optional().Nillable(),
		field.Time("built_at").Optional().Nillable(),
		field.Text("error_code").Optional().Nillable(),
		field.Text("error_message").Optional().Nillable(),
		field.Int64("river_job_id").Optional().Nillable(),
		createdAt(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).
			Annotations(entsql.DefaultExpr("now()")),
	}
}

func (CatalogBuild) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (CatalogBuild) Indexes() []ent.Index {
	return []ent.Index{index.Fields("catalog_key", "location_id", "node").Unique().StorageKey("catalog_builds_key_location_node_key")}
}
