package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Server struct{ ent.Schema }

func (Server) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("project_id").Immutable(),
		field.Text("name"),
		field.Int64("location_id").Immutable(),
		// node is "" until placed.
		field.Text("node").Default(""),
		field.Int("vmid").Optional().Nillable(),
		field.Int("template_vmid"),
		field.Int64("server_type_id"),
		field.Int64("image_id"),
		field.Enum("status").
			Values("initializing", "starting", "running", "stopping", "off", "rebuilding", "deleting", "missing", "error").
			Default("initializing"),
		field.Text("mac").Immutable(),
		field.JSON("labels", map[string]string{}),
		field.Text("iso_volid").Optional().Nillable(),
		// iso_generation numbers the cloud-init ISO; rebuild increments it.
		field.Int("iso_generation").Default(1),
		// clone_upid is the clone task, saved before waiting; it proves an untagged VM at vmid is ours.
		field.Text("clone_upid").Optional().Nillable(),
		// clone_nonce is sent in the clone description before the request; a match proves the VM is ours.
		field.Text("clone_nonce").Optional().Nillable(),
		// create_step is the last finished create-job step, so retries resume.
		field.Int("create_step").Default(0),
		// rebuild_* is the rebuild's other VM: the new one before the swap, the old one after it.
		field.Int("rebuild_vmid").Optional().Nillable(),
		field.Text("rebuild_clone_upid").Optional().Nillable(),
		field.Text("rebuild_clone_nonce").Optional().Nillable(),
		field.Int("rebuild_step").Default(0),
		// cores, memory_mb and disk_gb are snapshots of the type; change-type updates them.
		field.Int("cores"),
		field.Int("memory_mb"),
		field.Int("disk_gb"),
		// user_data and ssh_keys feed cloud-init and are never returned by the API.
		field.Text("user_data").Default("").Sensitive(),
		field.JSON("ssh_keys", []string{}).Sensitive(),
		// firewall_gen_* track the desired and applied firewall generation; the lease marks a worker apply in flight.
		field.Int64("firewall_gen_desired").Default(0),
		field.Int64("firewall_gen_applied").Default(0),
		field.Time("firewall_lease_until").Optional().Nillable(),
		createdAt(),
	}
}

func (Server) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("project", Project.Type).Field("project_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("location", Location.Type).Field("location_id").Unique().Required().Immutable().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("server_type", ServerType.Type).Field("server_type_id").Unique().Required().
			Annotations(entsql.OnDelete(entsql.NoAction)),
		edge.To("image", Image.Type).Field("image_id").Unique().Required().
			Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (Server) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique().StorageKey("servers_project_name_key"),
		index.Fields("mac").Unique().StorageKey("servers_mac_key"),
		index.Fields("location_id", "node").StorageKey("servers_location_node_idx"),
		index.Fields("server_type_id").StorageKey("servers_server_type_id_idx"),
		index.Fields("image_id").StorageKey("servers_image_id_idx"),
	}
}
