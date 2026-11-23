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

type IPAddress struct{ ent.Schema }

func (IPAddress) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Int64("location_id").Immutable(),
		field.Int64("allocation_id").Immutable(),
		field.Enum("kind").Values("floating").Default("floating").Immutable(),
		field.Text("family").Immutable(),
		field.String("ip").Immutable().SchemaType(inet()),
		field.Text("name"),
		field.Text("description").Default(""),
		field.JSON("labels", map[string]string{}),
		// server_id is the target; releasing_from_id is the old target until its unassign finishes; previous_server_id feeds rollback.
		field.Int64("server_id").Optional().Nillable(),
		field.Int64("releasing_from_id").Optional().Nillable(),
		field.Int64("previous_server_id").Optional().Nillable(),
		// pending_gen is the generation of the in-flight assign/unassign job.
		field.Int64("pending_gen").Optional().Nillable(),
		field.Bool("deleting").Default(false),
		// dns_ptr is stored for later use and not exposed.
		field.Text("dns_ptr").Optional().Nillable(),
		createdAt(),
	}
}

func (IPAddress) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("allocation", IPAllocation.Type).Field("allocation_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("server", Server.Type).Field("server_id").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("releasing_from", Server.Type).Field("releasing_from_id").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("previous_server", Server.Type).Field("previous_server_id").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (IPAddress) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique().StorageKey("ip_addresses_project_name_key"),
		index.Fields("server_id").StorageKey("ip_addresses_server_id_idx"),
		index.Fields("releasing_from_id").StorageKey("ip_addresses_releasing_from_id_idx"),
	}
}
