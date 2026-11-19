package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SSHKey struct{ ent.Schema }

func (SSHKey) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("ssh_keys")}
}

func (SSHKey) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Text("name"),
		field.Text("public_key").Immutable(),
		field.Text("fingerprint").Immutable(),
		createdAt(),
	}
}

func (SSHKey) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (SSHKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique().StorageKey("ssh_keys_project_name_key"),
		index.Fields("project_id", "fingerprint").Unique().StorageKey("ssh_keys_project_fingerprint_key"),
	}
}
