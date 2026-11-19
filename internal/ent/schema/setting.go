package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Setting struct{ ent.Schema }

func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.Text("id").StorageKey("key").Immutable(),
		field.Text("value"),
	}
}
