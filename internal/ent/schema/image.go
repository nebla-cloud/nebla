package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Image struct{ ent.Schema }

func (Image) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Text("description").Default(""),
		field.Text("os_flavor"),
		field.Text("os_version"),
		field.Text("catalog_key").Optional().Nillable(),
		createdAt(),
	}
}

func (Image) Edges() []ent.Edge {
	return []ent.Edge{edge.From("templates", ImageTemplate.Type).Ref("image")}
}

func (Image) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique().StorageKey("images_name_key"),
		index.Fields("catalog_key").Unique().StorageKey("images_catalog_key_key"),
	}
}
