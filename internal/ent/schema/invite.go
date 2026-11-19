package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Invite struct{ ent.Schema }

func (Invite) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Text("email").Immutable(),
		field.Text("role").Immutable(),
		field.Bytes("token_hash").Immutable().Sensitive(),
		field.Time("expires_at").Immutable(),
		field.Time("accepted_at").Optional().Nillable(),
		createdAt(),
	}
}

func (Invite) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Invite) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique().StorageKey("invites_token_hash_key"),
		index.Fields("project_id").StorageKey("invites_project_id_idx"),
	}
}
