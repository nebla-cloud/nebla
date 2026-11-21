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
