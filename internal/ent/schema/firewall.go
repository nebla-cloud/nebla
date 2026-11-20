package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Firewall struct{ ent.Schema }

func (Firewall) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Text("name"),
		field.JSON("labels", map[string]string{}),
		createdAt(),
	}
}

func (Firewall) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.From("rules", FirewallRule.Type).Ref("firewall"),
		edge.From("resources", FirewallResource.Type).Ref("firewall"),
	}
}

func (Firewall) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique().StorageKey("firewalls_project_name_key"),
	}
}
