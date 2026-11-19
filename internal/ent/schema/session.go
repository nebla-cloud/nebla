package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Session struct{ ent.Schema }

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("id").StorageKey("token_hash").Immutable().Sensitive(),
		field.Int64("user_id").Immutable(),
		field.Time("expires_at"),
		createdAt(),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		// One-way edge: a bytes ID can't be a map key in User's generated mutation.
		edge.To("user", User.Type).Field("user_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").StorageKey("sessions_user_id_idx"),
	}
}
