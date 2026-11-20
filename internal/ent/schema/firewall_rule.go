package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Direction has a Go type of its own because a generated DirectionIn constant would clash with the DirectionIn predicate.
type Direction string

const (
	DirectionIn  Direction = "in"
	DirectionOut Direction = "out"
)

func (Direction) Values() []string { return []string{string(DirectionIn), string(DirectionOut)} }

type FirewallRule struct{ ent.Schema }

func (FirewallRule) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("firewall_id").Immutable(),
		field.Int("position"),
		field.Enum("direction").GoType(Direction("")),
		field.Enum("protocol").Values("tcp", "udp", "icmp", "esp", "gre"),
		field.Text("port").Optional().Nillable(),
		field.JSON("ips", []string{}),
		field.Text("description").Default(""),
	}
}

func (FirewallRule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("firewall", Firewall.Type).Field("firewall_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (FirewallRule) Indexes() []ent.Index {
	return []ent.Index{index.Fields("firewall_id", "position").StorageKey("firewall_rules_firewall_position_idx")}
}
