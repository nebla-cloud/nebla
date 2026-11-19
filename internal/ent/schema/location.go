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
	}
}

func (Location) Indexes() []ent.Index {
	return []ent.Index{index.Fields("name").Unique().StorageKey("locations_name_key")}
}
