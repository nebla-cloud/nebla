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

type Project struct{ ent.Schema }

func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Int("max_servers").Default(0),
		field.Int("max_vcpu").Default(0),
		field.Int("max_memory_mb").Default(0),
		field.Int("max_disk_gb").Default(0),
		field.Int("max_ips").Default(0),
		field.Int("max_volumes").Default(0),
		field.Int("max_volume_gb").Default(0),
		// created_by is nil for projects made before it existed, or whose creator was deleted.
		field.Int64("created_by").Optional().Nillable(),
		createdAt(),
	}
}

func (Project) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("creator", User.Type).Field("created_by").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.From("members", ProjectMember.Type).Ref("project"),
	}
}

func (Project) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_by").StorageKey("projects_created_by_idx"),
	}
}
