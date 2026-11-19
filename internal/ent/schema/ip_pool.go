package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type IPPool struct{ ent.Schema }

func inet() map[string]string { return map[string]string{dialect.Postgres: "inet"} }

func (IPPool) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("location_id").Immutable(),
		field.Text("family"),
		field.Text("bridge"),
		field.Int("vlan_tag").Optional().Nillable(),
		field.String("cidr").SchemaType(map[string]string{dialect.Postgres: "cidr"}),
		field.String("gateway").SchemaType(inet()),
		field.Strings("dns"),
		field.String("range_start").SchemaType(inet()),
		field.String("range_end").SchemaType(inet()),
		createdAt(),
	}
}

func (IPPool) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.From("allocations", IPAllocation.Type).Ref("pool"),
	}
}

func (IPPool) Indexes() []ent.Index {
	return []ent.Index{index.Fields("location_id").StorageKey("ip_pools_location_id_idx")}
}
