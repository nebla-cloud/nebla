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

type Location struct{ ent.Schema }

func (Location) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Text("description").Default(""),
		field.Text("pve_url"),
		field.Text("token_id"),
		field.Bytes("token_secret").Sensitive(),
		field.Text("ca_fingerprint").Default(""),
		field.Bool("insecure_tls").Default(false),
		field.Text("disk_storage"),
		field.Text("iso_storage"),
		field.Text("import_storage").Default(""),
		// firewall_enabled is nil until the datacenter firewall state is known.
		field.Bool("firewall_enabled").Optional().Nillable(),
		// volume_storage "" means each node's effective disk storage.
		field.Text("volume_storage").Default(""),
		field.Bool("volume_storage_ok").Default(false),
		createdAt(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).
			Annotations(entsql.DefaultExpr("now()")),
	}
}

func (Location) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("nodes", Node.Type).Ref("location"),
		edge.From("server_types", ServerType.Type).Ref("locations"),
		edge.From("image_templates", ImageTemplate.Type).Ref("location"),
		edge.From("catalog_builds", CatalogBuild.Type).Ref("location"),
	}
}

func (Location) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name").Unique().StorageKey("locations_name_key")}
}
