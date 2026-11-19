package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ImageTemplate struct{ ent.Schema }

func (ImageTemplate) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("image_id").Immutable(),
		field.Int64("location_id").Immutable(),
		// node is empty when the template's storage is shared across the location.
		field.Text("node").Default(""),
		field.Int("vmid"),
	}
}

func (ImageTemplate) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("image", Image.Type).Field("image_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (ImageTemplate) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("image_id", "location_id", "node").Unique().StorageKey("image_templates_image_location_node_key"),
		index.Fields("location_id").StorageKey("image_templates_location_id_idx"),
	}
}
