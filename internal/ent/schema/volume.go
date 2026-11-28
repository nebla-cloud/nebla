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

type Volume struct{ ent.Schema }

func (Volume) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Int64("location_id").Immutable(),
		field.Text("name"),
		field.JSON("labels", map[string]string{}),
		// size_gb is the requested size; applied_gb is the size last confirmed on storage.
		field.Int("size_gb"),
		field.Int("applied_gb").Default(0),
		// node is "" for shared storage, else the pinned node.
		field.Text("node").Default(""),
		// storage is the storage ID at create time.
		field.Text("storage").Immutable(),
		// volid is the disk's Proxmox volume ID once allocated, else "".
		field.Text("volid").Default(""),
		field.Int64("server_id").Optional().Nillable(),
		// device is the virtioN key while attached, else "".
		field.Text("device").Default(""),
		field.Enum("status").
			Values("creating", "available", "attached", "deleting", "missing", "error").
			Default("creating"),
		deleteProtected(),
		createdAt(),
	}
}

func (Volume) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("server", Server.Type).Field("server_id").Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (Volume) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique().StorageKey("volumes_project_name_key"),
		index.Fields("server_id", "device").Unique().StorageKey("volumes_server_device_key").
			Annotations(entsql.IndexWhere("server_id IS NOT NULL")),
		index.Fields("location_id", "node").StorageKey("volumes_location_node_idx"),
		index.Fields("server_id").StorageKey("volumes_server_id_idx"),
	}
}
