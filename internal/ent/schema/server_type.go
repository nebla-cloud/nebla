package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ServerType struct{ ent.Schema }

func (ServerType) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Text("description").Default(""),
		field.Int("cores"),
		field.Int("memory_mb"),
		field.Int("disk_gb"),
		field.Bool("deprecated").Default(false),
		createdAt(),
	}
}

// Edges: an empty locations set means the type is offered everywhere.
func (ServerType) Edges() []ent.Edge {
	return []ent.Edge{edge.To("locations", Location.Type)}
}

func (ServerType) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name").Unique().StorageKey("server_types_name_key")}
}
