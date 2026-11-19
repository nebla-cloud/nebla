package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Node struct{ ent.Schema }

func (Node) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("location_id").Immutable(),
		field.Text("name").Immutable(),
		field.Bool("enabled").Default(true),
		field.Text("disk_storage_override").Default(""),
		field.Text("status").Default(""),
		field.Int("maxcpu").Default(0),
		field.Int64("maxmem").Default(0),
		field.Float("cpu").Default(0),
		field.Int64("mem").Default(0),
		createdAt(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).
			Annotations(entsql.DefaultExpr("now()")),
	}
}

func (Node) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Node) Indexes() []ent.Index {
	return []ent.Index{index.Fields("location_id", "name").Unique().StorageKey("nodes_location_name_key")}
}
