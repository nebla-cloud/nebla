package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type LocationEnrollment struct{ ent.Schema }

func (LocationEnrollment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Text("description").Default(""),
		field.Int64("location_id").Optional().Nillable(),
		field.Text("token_name"),
		field.Bytes("token_hash").Sensitive(),
		field.Enum("state").Values("waiting", "reported").Default("waiting"),
		field.Time("expires_at"),
		field.Text("token_id").Default(""),
		field.Bytes("token_secret").Optional().Sensitive(),
		field.Bytes("report").Optional(),
		field.Int64("created_by"),
		createdAt(),
	}
}

func (LocationEnrollment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("location", Location.Type).Field("location_id").Unique().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("creator", User.Type).Field("created_by").Unique().Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (LocationEnrollment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique().StorageKey("location_enrollments_token_hash_key"),
		index.Fields("location_id").Unique().StorageKey("location_enrollments_location_id_key"),
		index.Fields("name").Unique().StorageKey("location_enrollments_new_name_key").
			Annotations(entsql.IndexWhere("location_id IS NULL")),
		index.Fields("expires_at").StorageKey("location_enrollments_expires_at_idx"),
	}
}
