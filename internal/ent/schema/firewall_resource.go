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

type FirewallResource struct{ ent.Schema }

func (FirewallResource) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("firewall_id").Immutable(),
		field.Enum("type").Values("server", "label_selector").Immutable(),
		field.Int64("server_id").Optional().Nillable().Immutable(),
		field.Text("selector").Optional().Nillable().Immutable(),
	}
}

func (FirewallResource) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("firewall", Firewall.Type).Field("firewall_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("server", Server.Type).Field("server_id").Unique().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (FirewallResource) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("firewall_id", "server_id").Unique().StorageKey("firewall_resources_server_key"),
		index.Fields("firewall_id", "selector").Unique().StorageKey("firewall_resources_selector_key"),
		index.Fields("server_id").StorageKey("firewall_resources_server_id_idx"),
	}
}
