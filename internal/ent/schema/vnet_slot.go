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

type VNetSlot struct{ ent.Schema }

func (VNetSlot) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("location_id").Immutable(),
		field.Text("vnet").Immutable(),
		field.Text("zone"),
		field.Text("zone_type"),
		field.Int("mtu"),
		field.Enum("state").Values("free", "in_use", "cooling", "unusable", "gone").Default("free"),
		field.Enum("health").Values("ok", "degraded").Default("ok"),
		field.Text("reason").Default(""),
		field.Int64("network_id").Optional().Nillable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).
			Annotations(entsql.DefaultExpr("now()")),
	}
}

func (VNetSlot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("network", Network.Type).Field("network_id").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (VNetSlot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("location_id", "vnet").Unique().StorageKey("vnet_slots_location_vnet_key"),
		index.Fields("network_id").Unique().StorageKey("vnet_slots_network_id_key"),
	}
}
