package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ActionResource is one resource an action touches.
type ActionResource struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Action struct{ ent.Schema }

func (Action) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Text("command").Immutable(),
		field.Enum("status").Values("running", "success", "error").Default("running"),
		field.Int("progress").Default(0).Min(0).Max(100),
		field.JSON("resources", []ActionResource{}),
		field.Text("error_code").Optional().Nillable(),
		field.Text("error_message").Optional().Nillable(),
		field.Time("started").Default(time.Now).Immutable().Annotations(entsql.DefaultExpr("now()")),
		field.Time("finished").Optional().Nillable(),
		// river_job_id has no FK; River owns its table and prunes finished jobs.
		field.Int64("river_job_id").Optional().Nillable(),
	}
}

func (Action) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Action) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "id").StorageKey("actions_project_id_idx"),
		index.Fields("status").StorageKey("actions_status_idx"),
		index.Fields("resources").StorageKey("actions_resources_idx").Annotations(entsql.IndexType("GIN")),
	}
}
