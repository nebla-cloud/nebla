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
		createdAt(),
	}
}
