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

type Backup struct{ ent.Schema }

func (Backup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Int64("server_id"),
		field.Int64("location_id").Immutable(),
		// storage and node are where vzdump wrote the archive.
		field.Text("storage"),
		field.Text("node"),
		// volid is the archive's Proxmox volume ID once known.
		field.Text("volid").Optional().Nillable().Unique(),
		field.Enum("kind").Values("scheduled", "manual"),
		field.Enum("status").
			Values("queued", "creating", "available", "restoring", "deleting", "error", "missing").
			Default("queued"),
		// instance_generation is the server's VM generation when the backup was taken.
		field.Int("instance_generation"),
		field.Text("network_fingerprint").Default(""),
		field.Int64("size_bytes").Default(0),
		createdAt(),
		field.Time("completed_at").Optional().Nillable(),
	}
}

func (Backup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("server", Server.Type).Field("server_id").Unique().Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (Backup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("server_id").Unique().StorageKey("backups_server_pending_key").
			Annotations(entsql.IndexWhere("status IN ('queued', 'creating')")),
		index.Fields("server_id", "created_at").StorageKey("backups_server_created_idx"),
		index.Fields("location_id", "node", "status").StorageKey("backups_location_node_status_idx"),
	}
}
