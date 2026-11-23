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

type Network struct{ ent.Schema }

func (Network) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Int64("location_id").Immutable(),
		field.Text("name"),
		field.JSON("labels", map[string]string{}),
		// ip_range is the canonical CIDR prefix string.
		field.Text("ip_range").Immutable(),
		createdAt(),
	}
}

func (Network) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (Network) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique().StorageKey("networks_project_name_key"),
	}
}
