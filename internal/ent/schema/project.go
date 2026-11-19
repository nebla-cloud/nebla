package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Project struct{ ent.Schema }

func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Text("name"),
		field.Int("max_servers").Default(0),
		field.Int("max_vcpu").Default(0),
		field.Int("max_memory_mb").Default(0),
		field.Int("max_disk_gb").Default(0),
		field.Int("max_ips").Default(0),
		createdAt(),
	}
}
