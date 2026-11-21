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

type IPAllocation struct{ ent.Schema }

func (IPAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("pool_id").Immutable(),
		field.String("ip").Immutable().SchemaType(inet()),
		// server_id has no FK until the servers table exists.
		field.Int64("server_id"),
		createdAt(),
	}
}

func (IPAllocation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("pool", IPPool.Type).Field("pool_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (IPAllocation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("pool_id", "ip").Unique().StorageKey("ip_allocations_pool_ip_key"),
		index.Fields("server_id").StorageKey("ip_allocations_server_id_idx"),
	}
}
