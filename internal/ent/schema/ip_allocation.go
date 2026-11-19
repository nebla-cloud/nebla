package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type IPAllocation struct{ ent.Schema }

func (IPAllocation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("pool_id").Immutable(),
		field.String("ip").Immutable().SchemaType(inet()),
		// server_id has no FK until the servers table exists.
		field.Int64("server_id"),
		createdAt(),
	}
}

func (IPAllocation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("pool", IPPool.Type).Field("pool_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (IPAllocation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("pool_id", "ip").Unique().StorageKey("ip_allocations_pool_ip_key"),
		index.Fields("server_id").StorageKey("ip_allocations_server_id_idx"),
	}
}
