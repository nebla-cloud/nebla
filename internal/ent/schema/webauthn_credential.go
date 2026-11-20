package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/go-webauthn/webauthn/webauthn"
)

type WebauthnCredential struct{ ent.Schema }

func (WebauthnCredential) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id").Immutable(),
		field.Bytes("credential_id").Unique().Immutable(),
		field.Text("name"),
		field.Bytes("aaguid").Optional().Immutable(),
		field.JSON("data", webauthn.Credential{}),
		createdAt(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

func (WebauthnCredential) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", User.Type).Field("user_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (WebauthnCredential) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").StorageKey("webauthn_credentials_user_id_idx"),
	}
}
