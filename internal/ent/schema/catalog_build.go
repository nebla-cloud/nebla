package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CatalogBuild struct{ ent.Schema }

func (CatalogBuild) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("catalog_key").Immutable(),
		field.Int64("location_id").Immutable(),
		field.Text("node").Default("").Immutable(),
		field.Enum("status").Values("building", "ready", "failed").Default("building"),
		field.Int("step").Default(0),
		field.Int("vmid").Optional().Nillable(),
		field.Int("new_vmid").Optional().Nillable(),
		field.Text("nonce").Optional().Nillable(),
		field.Text("new_nonce").Optional().Nillable(),
		field.Text("upid").Optional().Nillable(),
		field.Text("file").Optional().Nillable(),
		field.Text("checksum").Optional().Nillable(),
		field.Text("new_file").Optional().Nillable(),
		field.Text("new_checksum").Optional().Nillable(),
		field.Time("built_at").Optional().Nillable(),
		field.Text("error_code").Optional().Nillable(),
		field.Text("error_message").Optional().Nillable(),
		field.Int64("river_job_id").Optional().Nillable(),
		createdAt(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).
			Annotations(entsql.DefaultExpr("now()")),
	}
}

func (CatalogBuild) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (CatalogBuild) Indexes() []ent.Index {
	return []ent.Index{index.Fields("catalog_key", "location_id", "node").Unique().StorageKey("catalog_builds_key_location_node_key")}
}
