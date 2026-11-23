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

type ServerNetwork struct{ ent.Schema }

func (ServerNetwork) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("server_id").Immutable(),
		field.Int64("network_id").Immutable(),
		field.Text("ip"),
		field.Text("mac"),
		field.Int("nic").Min(1).Max(3),
		field.Enum("state").Values("attaching", "attached", "detaching").Default("attaching"),
		createdAt(),
	}
}

func (ServerNetwork) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("server", Server.Type).Field("server_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("network", Network.Type).Field("network_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (ServerNetwork) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("server_id", "network_id").Unique().StorageKey("server_networks_server_network_key"),
		index.Fields("server_id", "nic").Unique().StorageKey("server_networks_server_nic_key"),
		index.Fields("network_id", "ip").Unique().StorageKey("server_networks_network_ip_key"),
	}
}
