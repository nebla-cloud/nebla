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
)

// ConsoleToken is a one-time console websocket token; only its SHA-256 is stored.
type ConsoleToken struct{ ent.Schema }

func (ConsoleToken) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("id").StorageKey("token_hash").Immutable().Sensitive(),
		field.Int64("server_id").Immutable(),
		field.Int64("project_id").Immutable(),
		field.Int("vmid").Immutable(),
		field.Text("ticket").Immutable().Sensitive(),
		// port is an int, which ent cannot mark Sensitive; it is useless without the ticket.
		field.Int("port").Immutable(),
		field.Time("expires_at").Immutable(),
		// Who asked for the console: a session user or an API token. Live sessions re-check it.
		field.Int64("user_id").Optional().Nillable().Immutable(),
		field.Bytes("session_hash").Optional().Nillable().Immutable().Sensitive(),
		field.Int64("api_token_id").Optional().Nillable().Immutable(),
		createdAt(),
	}
}

func (ConsoleToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("server", Server.Type).Field("server_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (ConsoleToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at").StorageKey("console_tokens_expires_at_idx"),
		index.Fields("server_id").StorageKey("console_tokens_server_id_idx"),
	}
}
