package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/go-webauthn/webauthn/webauthn"
)

type WebauthnChallenge struct{ ent.Schema }

func (WebauthnChallenge) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("id").StorageKey("token_hash").Immutable().Sensitive(),
		field.Enum("purpose").Values("register", "login").Immutable(),
		field.Int64("user_id").Optional().Nillable().Immutable(),
		field.JSON("session_data", webauthn.SessionData{}).Sensitive(),
		field.Time("expires_at"),
	}
}

func (WebauthnChallenge) Edges() []ent.Edge {
	return []ent.Edge{
		// One-way edge: a bytes ID can't be a map key in User's generated mutation.
		edge.To("user", User.Type).Field("user_id").Unique().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (WebauthnChallenge) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at").StorageKey("webauthn_challenges_expires_at_idx"),
	}
}
