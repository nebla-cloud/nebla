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

type Snapshot struct{ ent.Schema }

func (Snapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Int64("location_id").Immutable(),
		field.Text("name"),
		field.Text("description").Default(""),
		field.JSON("labels", map[string]string{}),
		// server_id is the source server; it goes null when that server is deleted.
		field.Int64("server_id").Optional().Nillable(),
		// server_name is the source server's name when the snapshot was taken.
		field.Text("server_name").Default(""),
		field.Int64("image_id").Optional().Nillable(),
		field.Text("os_flavor").Default(""),
		field.Text("os_version").Default(""),
		// size_gb is the boot disk size, rounded up.
		field.Int("size_gb"),
		field.Int("instance_generation").Default(0),
		field.Text("network_fingerprint").Default(""),
		field.Int("vmid").Optional().Nillable(),
		// node is "" for shared storage, else the node holding the template.
		field.Text("node").Default(""),
		field.Text("storage").Default(""),
		field.Text("boot_key").Default(""),
		// step is the last finished job step, so retries resume.
		field.Int("step").Default(0),
		field.Text("nonce").Optional().Nillable(),
		field.Text("upid").Optional().Nillable(),
		field.Bool("was_running").Default(false),
		field.Bool("force_stop").Default(false),
		field.Enum("status").
			Values("creating", "available", "deleting", "missing", "error").
			Default("creating"),
		createdAt(),
	}
}

func (Snapshot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("server", Server.Type).Field("server_id").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("image", Image.Type).Field("image_id").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (Snapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique().StorageKey("snapshots_project_name_key"),
		index.Fields("location_id", "node").StorageKey("snapshots_location_node_idx"),
		index.Fields("server_id").StorageKey("snapshots_server_id_idx"),
		index.Fields("location_id", "vmid").Unique().StorageKey("snapshots_location_vmid_key").
			Annotations(entsql.IndexWhere("vmid IS NOT NULL")),
	}
}
