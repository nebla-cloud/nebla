// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
