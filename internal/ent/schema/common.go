package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

func createdAt() ent.Field {
	return field.Time("created_at").Default(time.Now).Immutable().
		Annotations(entsql.DefaultExpr("now()"))
}
