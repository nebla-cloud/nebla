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
	"entgo.io/ent/schema/field"
)

type User struct{ ent.Schema }

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("email").Unique(),
		field.Text("name"),
		field.Text("password_hash").Sensitive(),
		field.Bool("is_admin").Default(false),
		field.Text("totp_secret").Optional().Nillable().Sensitive(),
		field.Text("totp_pending_secret").Optional().Nillable().Sensitive(),
		field.Bool("totp_enabled").Default(false),
		field.Bytes("webauthn_handle").Optional().Nillable().Unique().Sensitive(),
		createdAt(),
	}
}
