// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package servers owns server records, placement and the server actions.
package servers

import (
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/cloudinit"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/backup"
	"github.com/nebla-cloud/nebla/internal/ent/firewall"
	"github.com/nebla-cloud/nebla/internal/ent/firewallresource"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/project"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/ent/sshkey"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/labels"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/primaryips"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/serverlock"
	"github.com/riverqueue/river"
)

const (
	StatusInitializing = "initializing"
	StatusStarting     = "starting"
	StatusRunning      = "running"
	StatusStopping     = "stopping"
	StatusOff          = "off"
	StatusRebuilding   = "rebuilding"
	StatusDeleting     = "deleting"
	StatusMissing      = "missing"
	StatusError        = "error"
)

const (
	KindCreateServer = "create_server"
	KindDeleteServer = "delete_server"
	// KindCreateBackup is the action command of a running backup, which locks the server.
	KindCreateBackup = serverlock.KindCreateBackup
	// ResourceType is the actions resource type for servers.
	ResourceType = jobs.ServerResource
	// MaxUserData is the largest accepted user_data in bytes.
	MaxUserData = 32 << 10
	MaxLabels   = 64
)

// CreateServerArgs is the create_server job, worked by CreateServerWorker.
type CreateServerArgs struct {
	ServerID   int64 `json:"server_id"`
	ActionID   int64 `json:"action_id"`
	StartAfter bool  `json:"start_after"`
	// KeptIPs are the existing primary IPs this create attached; a failed create gives them back.
	KeptIPs []int64 `json:"kept_ips,omitempty"`
}

func (CreateServerArgs) Kind() string { return KindCreateServer }

// DeleteServerArgs is the delete_server job.
type DeleteServerArgs struct {
	ServerID int64 `json:"server_id"`
	ActionID int64 `json:"action_id"`
}

func (DeleteServerArgs) Kind() string { return KindDeleteServer }

type Options struct {
	SQL      *sql.DB
	Ent      *ent.Client
	Catalog  *catalog.Service
	Projects *projects.Service
	// Jobs nil makes Create and Delete fail with 503 service_unavailable.
	Jobs *jobs.Jobs
	// BackupInfo fills Server.Backups' count and times; nil leaves them zero.
	BackupInfo func(ctx context.Context, ids []int64) (map[int64]ServerBackups, error)
	// Networks nil disables private networks: a create that names one fails with 422.
	Networks *networks.Service
	// Bus nil drops events.
	Bus Publisher
}

type Service struct{ o Options }

func New(o Options) *Service { return &Service{o: o} }

// Server is a server as the project sees it; IPv4/IPv6 are invalid when not assigned.
type Server struct {
	ID, ProjectID int64
	Name, Status  string
	Location      catalog.Location
	Type          catalog.ServerType
	// Image is nil when the server was built from a snapshot whose image is gone.
	Image *catalog.Image
	// Snapshot is the snapshot the server was built from, if it still exists.
	Snapshot   *SnapshotRef
	OSFlavor   string
	OSVersion  string
	Node       string
	Labels     map[string]string
	DiskGB     int
	IPv4, IPv6 netip.Addr
	// IPv4ID and IPv6ID are the primary IP ids, zero when unassigned.
	IPv4ID, IPv6ID int64
	CreatedAt      time.Time
	Firewalls      []firewalls.Applied
	Backups        ServerBackups
	// IPChangeRerunsCloudInit is true for servers created before IP changes were supported.
	IPChangeRerunsCloudInit bool
}

// ServerBackups is a server's backup settings and state.
type ServerBackups struct {
	Enabled bool
	Keep    int
	Count   int
	// LastBackupAt and NextBackupAt are nil when there is none.
	LastBackupAt, NextBackupAt *time.Time
}

// BackupSettings is a server's backup toggle and how many backups it keeps.
type BackupSettings struct {
	Enabled bool
	Keep    int
}

const (
	MinBackupKeep = 1
	MaxBackupKeep = 14
	// backupSlotPrime spreads servers over the day: slot = id * prime mod 1440.
	backupSlotPrime = 887
)

// SnapshotRef names a snapshot.
type SnapshotRef struct {
	ID   int64
	Name string
}

type CreateInput struct {
	Name, ServerType, Image, Snapshot, Location string
	// SSHKeys are the project's key ids or names.
	SSHKeys    []string
	UserData   string
	IPv4, IPv6 IPChoice
	Labels     map[string]string
	StartAfter bool
	Firewalls  []int64
	// Backups nil leaves backups off.
	Backups  *BackupSettings
	Networks []networks.CreateRef
}

// IPChoice is a new address, an existing primary IP (id or name), or neither.
type IPChoice struct {
	New      bool
	Existing string
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name    *string
	Labels  *map[string]string
	Backups *BackupSettings
}

var (
	nameRE       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	labelKeyRE   = labels.KeyRE
	labelValueRE = labels.ValueRE

	errNotFound  = apierr.New(http.StatusNotFound, "not_found", "not found")
	errNoJobs    = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	errLocked    = serverlock.ErrLocked
	errNoBackups = apierr.New(http.StatusUnprocessableEntity, "backups_unavailable", "Backups are not available in this location")
	ErrMissing   = apierr.New(http.StatusConflict, "server_missing", "the server's VM is missing in Proxmox")
	errNotReady  = apierr.New(http.StatusConflict, "server_not_ready", "the server has no usable VM")
	errNotOff    = apierr.New(http.StatusConflict, "server_not_stopped", "the server must be stopped")
	errShrink    = apierr.New(http.StatusUnprocessableEntity, "disk_shrink_not_allowed", "disks cannot shrink")
	errNameTaken = apierr.New(http.StatusConflict, "uniqueness_error", "server name is already taken")
	errMACTaken  = errors.New("mac collision")
)

func invalid(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", fmt.Sprintf(format, a...))
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

func validName(n string) error {
	if !nameRE.MatchString(n) {
		return invalid("name must match %s", nameRE)
	}
	return nil
}

func validLabels(l map[string]string) error {
	if len(l) > MaxLabels {
		return invalid("at most %d labels", MaxLabels)
	}
	for k, v := range l {
		if !labelKeyRE.MatchString(k) {
			return invalid("label key %q must match %s", k, labelKeyRE)
		}
		if !labelValueRE.MatchString(v) {
			return invalid("label value %q must match %s", v, labelValueRE)
		}
	}
	return nil
}

// writeErr maps constraint violations on servers rows.
func writeErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch {
		case pe.ConstraintName == "servers_project_name_key":
			return errNameTaken
		case pe.ConstraintName == "ip_addresses_project_name_key":
			return primaryips.ErrNameTaken
		case pe.ConstraintName == "servers_mac_key":
			return errMACTaken
		case pe.Code == "23503":
			return invalid("the server type, image or location no longer exists")
		}
	}
	return notFound(err)
}

// Create validates in, places the server and commits it with its IPs, quota and create_server action.
func (s *Service) Create(ctx context.Context, projectID int64, in CreateInput) (Server, actions.Action, error) {
	if s.o.Jobs == nil {
		return Server{}, actions.Action{}, errNoJobs
	}
	if err := validName(in.Name); err != nil {
		return Server{}, actions.Action{}, err
	}
	if err := validLabels(in.Labels); err != nil {
		return Server{}, actions.Action{}, err
	}
	if len(in.UserData) > MaxUserData {
		return Server{}, actions.Action{}, invalid("user_data must be at most %d bytes", MaxUserData)
	}
	if err := cloudinit.ValidateUserData(in.UserData); err != nil {
		return Server{}, actions.Action{}, invalid("user_data: %s", strings.TrimPrefix(err.Error(), "cloudinit: "))
	}
	typ, err := s.o.Catalog.ServerTypeByRef(ctx, in.ServerType)
	if err != nil {
		return Server{}, actions.Action{}, err
	}
	if (in.Image == "") == (in.Snapshot == "") {
		return Server{}, actions.Action{}, invalid("give either image or snapshot")
	}
	var img catalog.Image
	var pre *ent.Snapshot
	var loc catalog.Location
	if in.Snapshot != "" {
		if pre, err = readSnapshot(ctx, s.o.Ent, projectID, in.Snapshot, false); err != nil {
			return Server{}, actions.Action{}, err
		}
		if loc, err = s.snapshotLocation(ctx, pre); err != nil {
			return Server{}, actions.Action{}, err
		}
		if in.Location != "" {
			want, err := s.o.Catalog.LocationByRef(ctx, in.Location)
			if err != nil {
				return Server{}, actions.Action{}, err
			}
			if want.ID != loc.ID {
				return Server{}, actions.Action{}, invalid("This snapshot is in location %s", loc.Name)
			}
		}
	} else {
		if img, err = s.o.Catalog.ImageByRef(ctx, in.Image); err != nil {
			return Server{}, actions.Action{}, err
		}
		if loc, err = s.o.Catalog.LocationByRef(ctx, in.Location); err != nil {
			return Server{}, actions.Action{}, err
		}
	}
	switch {
	case typ.Deprecated:
		return Server{}, actions.Action{}, invalid("server type %s is deprecated", typ.Name)
	case !typ.AvailableIn(loc.ID):
		return Server{}, actions.Action{}, invalid("server type %s is not offered in location %s", typ.Name, loc.Name)
	case pre != nil && typ.DiskGB < pre.SizeGB:
		return Server{}, actions.Action{}, errSnapshotTooLarge(pre.SizeGB)
	}
	var tmpls []catalog.Template
	if pre == nil {
		if tmpls, err = s.o.Catalog.TemplatesFor(ctx, img.ID, loc.ID); err != nil {
			return Server{}, actions.Action{}, err
		}
		if len(tmpls) == 0 {
			return Server{}, actions.Action{}, invalid("image %s is not available in location %s", img.Name, loc.Name)
		}
	}
	keys, err := s.resolveKeys(ctx, projectID, in.SSHKeys)
	if err != nil {
		return Server{}, actions.Action{}, err
	}
	var newFamilies []string
	var kept []keptIP
	for _, c := range []struct {
		family string
		choice IPChoice
	}{{ipam.FamilyIPv4, in.IPv4}, {ipam.FamilyIPv6, in.IPv6}} {
		switch {
		case c.choice.New && c.choice.Existing != "":
			return Server{}, actions.Action{}, invalid("choose a new %s address or an existing primary IP, not both", c.family)
		case c.choice.New:
			newFamilies = append(newFamilies, c.family)
		case c.choice.Existing != "":
			row, err := primaryips.ByRef(ctx, s.o.Ent, projectID, c.choice.Existing)
			if err != nil {
				return Server{}, actions.Action{}, err
			}
			kept = append(kept, keptIP{id: row.ID, family: c.family})
		}
	}
	slices.SortFunc(kept, func(a, b keptIP) int { return cmp.Compare(a.id, b.id) })
	labels := in.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	if len(in.Networks) > 0 && s.o.Networks == nil {
		return Server{}, actions.Action{}, invalid("Private networks aren't available in this location.")
	}
	fwIDs := slices.Compact(slices.Sorted(slices.Values(in.Firewalls)))
	delta := projects.Usage{Servers: 1, VCPU: typ.Cores, MemoryMB: typ.MemoryMB, DiskGB: typ.DiskGB, IPs: len(newFamilies)}
	if in.Backups != nil {
		if err := s.checkBackups(*in.Backups, loc, false); err != nil {
			return Server{}, actions.Action{}, err
		}
		if in.Backups.Enabled {
			delta.BackupsReserved = in.Backups.Keep
		}
	}

	var id int64
	var act actions.Action
	var created []*ent.IPAddress
	for range 3 {
		err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
			if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, delta); err != nil {
				return err
			}
			seg, err := lockKept(ctx, etx, projectID, loc.ID, kept)
			if err != nil {
				return err
			}
			if err := lockFirewalls(ctx, etx, projectID, fwIDs); err != nil {
				return err
			}
			loads, err := lockNodes(ctx, etx, loc.ID)
			if err != nil {
				return err
			}
			src := sourceFacts{imageID: &img.ID, flavor: img.OSFlavor, version: img.OSVersion}
			if pre != nil {
				snap, err := lockSnapshot(ctx, etx, projectID, strconv.FormatInt(pre.ID, 10))
				if err != nil {
					return err
				}
				tmpls = []catalog.Template{{Node: snap.Node, VMID: *snap.Vmid}}
				src = sourceFacts{imageID: snap.ImageID, flavor: snap.OsFlavor, version: snap.OsVersion, snapshotID: &snap.ID}
			}
			nodeName, vmid, err := Place(loads, tmpls, typ)
			if errors.Is(err, errNoCapacity) && pre != nil && tmpls[0].Node != "" {
				err = errSnapshotHostFull
			}
			if err != nil {
				return err
			}
			mac, err := newMAC(ctx, etx)
			if err != nil {
				return err
			}
			create := etx.Server.Create().SetProjectID(projectID).SetName(in.Name).SetLocationID(loc.ID).SetNode(nodeName).
				SetTemplateVmid(vmid).SetServerTypeID(typ.ID).SetNillableImageID(src.imageID).SetNillableSnapshotID(src.snapshotID).
				SetOsFlavor(src.flavor).SetOsVersion(src.version).SetMAC(mac).SetLabels(labels).
				SetCores(typ.Cores).SetMemoryMB(typ.MemoryMB).SetDiskGB(typ.DiskGB).SetUserData(in.UserData).SetSSHKeys(keys)
			if in.Backups != nil {
				create.SetBackupsEnabled(in.Backups.Enabled).SetBackupKeep(in.Backups.Keep)
				if in.Backups.Enabled {
					create.SetBackupsEnabledAt(time.Now())
				}
			}
			row, err := create.Save(ctx)
			if err != nil {
				return writeErr(err)
			}
			// The id is known only now.
			if err := etx.Server.UpdateOneID(row.ID).SetBackupSlot(int(row.ID * backupSlotPrime % 1440)).Exec(ctx); err != nil {
				return err
			}
			created = nil
			for _, k := range kept {
				if err := etx.IPAddress.UpdateOneID(k.id).SetServerID(row.ID).Exec(ctx); err != nil {
					return err
				}
			}
			for _, f := range newFamilies {
				r, err := attachNewPrimary(ctx, etx, projectID, row, f, seg)
				if err != nil {
					return err
				}
				created = append(created, r)
			}
			if err := attachFirewalls(ctx, etx, projectID, row.ID, labels, fwIDs); err != nil {
				return err
			}
			if len(in.Networks) > 0 {
				if err := s.o.Networks.ReserveForCreate(ctx, etx, projectID, loc.ID, row.ID, in.Networks); err != nil {
					return err
				}
			}
			act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindCreateServer,
				[]actions.Resource{{ID: row.ID, Type: ResourceType}},
				func(actionID int64) river.JobArgs {
					return CreateServerArgs{ServerID: row.ID, ActionID: actionID, StartAfter: in.StartAfter, KeptIPs: keptIDs(kept)}
				}, nil)
			id = row.ID
			return err
		})
		if !errors.Is(err, errMACTaken) {
			break
		}
	}
	if err != nil {
		return Server{}, actions.Action{}, err
	}
	for _, r := range created {
		s.publishPrimary(ctx, ext.PrimaryIPCreated, r)
	}
	srv, err := s.Get(ctx, projectID, id)
	return srv, act, err
}

type keptIP struct {
	id     int64
	family string
}

func keptIDs(k []keptIP) []int64 {
	ids := make([]int64, len(k))
	for i, x := range k {
		ids[i] = x.id
	}
	return ids
}

// lockKept locks the existing primary IPs in id order and returns their shared network, if any.
func lockKept(ctx context.Context, etx *ent.Client, projectID, locationID int64, kept []keptIP) (*ipam.Segment, error) {
	var seg *ipam.Segment
	for _, k := range kept {
		row, err := primaryips.LockForCreate(ctx, etx, projectID, k.id, locationID, k.family)
		if err != nil {
			return nil, err
		}
		got, err := primaryips.SegmentOf(ctx, etx, row.AllocationID)
		if err != nil {
			return nil, err
		}
		if seg != nil && !floatingips.SameSegment(*seg, got) {
			return nil, primaryips.ErrSegment
		}
		seg = &got
	}
	return seg, nil
}

// publishPrimary sends a billing event for a primary IP row.
func (s *Service) publishPrimary(ctx context.Context, kind ext.EventKind, r *ent.IPAddress) {
	publishPrimary(ctx, s.o.Bus, kind, r)
}

func publishPrimary(ctx context.Context, bus Publisher, kind ext.EventKind, r *ent.IPAddress) {
	if bus == nil {
		return
	}
	bus.Publish(ctx, ext.Event{Kind: kind, ProjectID: r.ProjectID, ResourceType: primaryips.ResourceType, ResourceID: r.ID,
		At: time.Now(), Data: map[string]any{"ip": r.IP, "family": r.Family, "location_id": r.LocationID}})
}

// attachNewPrimary allocates a new address of family (on seg when set) and inserts its primary IP on srv.
func attachNewPrimary(ctx context.Context, etx *ent.Client, projectID int64, srv *ent.Server, family string, seg *ipam.Segment) (*ent.IPAddress, error) {
	a, allocID, err := ipam.AllocatePrimary(ctx, etx, srv.LocationID, family, seg)
	if err != nil {
		return nil, err
	}
	var qerr error
	name := primaryips.DefaultName(srv.Name, family, allocID, func(n string) bool {
		taken, err := etx.IPAddress.Query().Where(ipaddress.ProjectID(projectID), ipaddress.Name(n)).Exist(ctx)
		qerr = errors.Join(qerr, err)
		return taken
	})
	if qerr != nil {
		return nil, qerr
	}
	row, err := etx.IPAddress.Create().SetKind(ipaddress.KindPrimary).SetProjectID(projectID).SetLocationID(srv.LocationID).
		SetAllocationID(allocID).SetFamily(family).SetIP(a.IP.String()).SetName(name).SetLabels(map[string]string{}).
		SetServerID(srv.ID).Save(ctx)
	if err != nil {
		return nil, writeErr(err)
	}
	return row, nil
}

// releasePrimaries deletes the server's auto_delete primary IPs and frees their addresses, and unassigns the rest.
func releasePrimaries(ctx context.Context, etx *ent.Client, serverID int64) (freed []*ent.IPAddress, err error) {
	rows, err := etx.IPAddress.Query().Where(ipaddress.KindEQ(ipaddress.KindPrimary), ipaddress.ServerID(serverID)).
		Order(ipaddress.ByID()).ForUpdate().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if !r.AutoDelete {
			if err := etx.IPAddress.UpdateOneID(r.ID).ClearServerID().ClearPendingGen().Exec(ctx); err != nil {
				return nil, err
			}
			continue
		}
		if err := etx.IPAddress.DeleteOneID(r.ID).Exec(ctx); err != nil {
			return nil, err
		}
		if err := ipam.FreeAllocation(ctx, etx, r.AllocationID); err != nil {
			return nil, err
		}
		freed = append(freed, r)
	}
	return freed, nil
}

// lockFirewalls share-locks the project's firewalls ids, before any server row; one outside the project is not found.
func lockFirewalls(ctx context.Context, etx *ent.Client, projectID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	got, err := etx.Firewall.Query().Where(firewall.IDIn(ids...), firewall.ProjectID(projectID)).ForShare().IDs(ctx)
	if err != nil {
		return err
	}
	if len(got) != len(ids) {
		return errNotFound
	}
	return nil
}

// attachFirewalls checks the new server's firewall limit and applies ids directly; its build renders them, so no apply is queued.
func attachFirewalls(ctx context.Context, etx *ent.Client, projectID, serverID int64, labels map[string]string, ids []int64) error {
	if err := firewalls.CheckLimit(ctx, etx, projectID, serverID, labels, ids); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	b := make([]*ent.FirewallResourceCreate, len(ids))
	for i, id := range ids {
		b[i] = etx.FirewallResource.Create().SetFirewallID(id).SetType(firewallresource.TypeServer).SetServerID(serverID)
	}
	return etx.FirewallResource.CreateBulk(b...).Exec(ctx)
}

// resolveKeys returns the public keys for refs (ids first, then names) in the project, deduplicated.
func (s *Service) resolveKeys(ctx context.Context, projectID int64, refs []string) ([]string, error) {
	out := []string{}
	seen := map[int64]bool{}
	for _, ref := range refs {
		var k *ent.SSHKey
		var err error
		if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
			k, err = s.o.Ent.SSHKey.Query().Where(sshkey.ProjectID(projectID), sshkey.ID(id)).Only(ctx)
		}
		if k == nil {
			k, err = s.o.Ent.SSHKey.Query().Where(sshkey.ProjectID(projectID), sshkey.Name(ref)).Only(ctx)
		}
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, apierr.New(http.StatusNotFound, "not_found", fmt.Sprintf("ssh key %q not found", ref))
			}
			return nil, err
		}
		if !seen[k.ID] {
			seen[k.ID] = true
			out = append(out, k.PublicKey)
		}
	}
	return out, nil
}

// lockNodes locks the location's nodes in name order, as syncNodes does, and returns their loads.
func lockNodes(ctx context.Context, tx *ent.Client, locationID int64) ([]NodeLoad, error) {
	if _, err := tx.Location.Query().Where(location.ID(locationID)).ForShare().Only(ctx); err != nil {
		return nil, notFound(err)
	}
	nodes, err := tx.Node.Query().Where(node.LocationID(locationID)).Order(node.ByName()).ForUpdate().All(ctx)
	if err != nil {
		return nil, err
	}
	// Running servers are already in the node's mem; missing and error ones hold nothing.
	placed, err := tx.Server.Query().
		Where(server.LocationID(locationID), server.NodeNEQ(""), server.StatusNotIn(StatusRunning, StatusMissing, StatusError)).
		Select(server.FieldNode, server.FieldMemoryMB).All(ctx)
	if err != nil {
		return nil, err
	}
	reserved := map[string]int64{}
	for _, p := range placed {
		reserved[p.Node] += int64(p.MemoryMB) << 20
	}
	out := make([]NodeLoad, len(nodes))
	for i, n := range nodes {
		out[i] = NodeLoad{Name: n.Name, Enabled: n.Enabled, Status: n.Status, MaxMem: n.Maxmem, Mem: n.Mem, Reserved: reserved[n.Name]}
	}
	return out, nil
}

// newMAC returns a free BC:24:11 address; a concurrent duplicate surfaces as errMACTaken on insert.
func newMAC(ctx context.Context, tx *ent.Client) (string, error) {
	for range 16 {
		b := make([]byte, 3)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		mac := fmt.Sprintf("BC:24:11:%02X:%02X:%02X", b[0], b[1], b[2])
		taken, err := tx.Server.Query().Where(server.MAC(mac)).Exist(ctx)
		if err != nil {
			return "", err
		}
		if !taken {
			taken, err = tx.ServerNetwork.Query().Where(servernetwork.MACEqualFold(mac)).Exist(ctx)
			if err != nil {
				return "", err
			}
		}
		if !taken {
			return mac, nil
		}
	}
	return "", errMACTaken
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (Server, error) {
	row, err := s.o.Ent.Server.Query().Where(server.ID(id), server.ProjectID(projectID)).Only(ctx)
	if err != nil {
		return Server{}, notFound(err)
	}
	out, err := s.hydrate(ctx, []*ent.Server{row})
	if err != nil {
		return Server{}, err
	}
	return out[0], nil
}

type ListFilter struct {
	Name          string
	Statuses      []string
	LabelSelector string
}

// ValidStatus reports whether st is a server status.
func ValidStatus(st string) bool { return server.StatusValidator(server.Status(st)) == nil }

func (s *Service) List(ctx context.Context, projectID int64, f ListFilter, page, perPage int) ([]Server, int, error) {
	preds, err := LabelSelector(f.LabelSelector)
	if err != nil {
		return nil, 0, err
	}
	q := s.o.Ent.Server.Query().Where(server.ProjectID(projectID)).Where(preds...)
	if f.Name != "" {
		q = q.Where(server.Name(f.Name))
	}
	if len(f.Statuses) > 0 {
		sts := make([]server.Status, len(f.Statuses))
		for i, st := range f.Statuses {
			if !ValidStatus(st) {
				return nil, 0, invalid("unknown status %q", st)
			}
			sts[i] = server.Status(st)
		}
		q = q.Where(server.StatusIn(sts...))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, perPage = max(page, 1), max(perPage, 1)
	rows, err := q.Order(server.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.hydrate(ctx, rows)
	return out, total, err
}

// hydrate attaches catalog objects and IPs to rows.
func (s *Service) hydrate(ctx context.Context, rows []*ent.Server) ([]Server, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	prims, err := s.o.Ent.IPAddress.Query().Where(ipaddress.KindEQ(ipaddress.KindPrimary), ipaddress.ServerIDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	type ips struct {
		v4, v6   netip.Addr
		id4, id6 int64
	}
	byServer := map[int64]*ips{}
	for _, a := range prims {
		ip, err := netip.ParseAddr(a.IP)
		if err != nil {
			return nil, err
		}
		e := byServer[*a.ServerID]
		if e == nil {
			e = &ips{}
			byServer[*a.ServerID] = e
		}
		if a.Family == ipam.FamilyIPv6 {
			e.v6, e.id6 = ip, a.ID
		} else {
			e.v4, e.id4 = ip, a.ID
		}
	}
	atts := map[int64]firewalls.Attachments{}
	types := map[int64]catalog.ServerType{}
	imgs := map[int64]catalog.Image{}
	locs := map[int64]catalog.Location{}
	var snapIDs []int64
	for _, r := range rows {
		if r.SnapshotID != nil {
			snapIDs = append(snapIDs, *r.SnapshotID)
		}
	}
	snaps := map[int64]string{}
	if len(snapIDs) > 0 {
		ss, err := s.o.Ent.Snapshot.Query().Where(snapshot.IDIn(snapIDs...)).Select(snapshot.FieldID, snapshot.FieldName).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, sn := range ss {
			snaps[sn.ID] = sn.Name
		}
	}
	out := make([]Server, len(rows))
	for i, r := range rows {
		t, ok := types[r.ServerTypeID]
		if !ok {
			if t, err = s.o.Catalog.GetServerType(ctx, r.ServerTypeID); err != nil {
				return nil, err
			}
			types[r.ServerTypeID] = t
		}
		var img *catalog.Image
		if r.ImageID != nil {
			cached, ok := imgs[*r.ImageID]
			if !ok {
				if cached, err = s.o.Catalog.GetImage(ctx, *r.ImageID); err != nil {
					return nil, err
				}
				imgs[*r.ImageID] = cached
			}
			img = &cached
		}
		flavor, version, err := catalog.ServerOSFacts(ctx, s.o.Ent, r)
		if err != nil {
			return nil, err
		}
		loc, ok := locs[r.LocationID]
		if !ok {
			if loc, err = s.o.Catalog.GetLocation(ctx, r.LocationID); err != nil {
				return nil, err
			}
			locs[r.LocationID] = loc
		}
		out[i] = Server{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Status: string(r.Status), Location: loc, Type: t, Image: img, OSFlavor: flavor, OSVersion: version,
			Node: r.Node, Labels: r.Labels, DiskGB: r.DiskGB, CreatedAt: r.CreatedAt, IPChangeRerunsCloudInit: !r.NetRefresh}
		if out[i].Labels == nil {
			out[i].Labels = map[string]string{}
		}
		if r.SnapshotID != nil {
			if name, ok := snaps[*r.SnapshotID]; ok {
				out[i].Snapshot = &SnapshotRef{ID: *r.SnapshotID, Name: name}
			}
		}
		if e := byServer[r.ID]; e != nil {
			out[i].IPv4, out[i].IPv6, out[i].IPv4ID, out[i].IPv6ID = e.v4, e.v6, e.id4, e.id6
		}
		att, ok := atts[r.ProjectID]
		if !ok {
			if att, err = firewalls.LoadAttachments(ctx, s.o.Ent, r.ProjectID); err != nil {
				return nil, err
			}
			atts[r.ProjectID] = att
		}
		out[i].Firewalls = att.Effective(r.ID, out[i].Labels)
		out[i].Backups = ServerBackups{Enabled: r.BackupsEnabled, Keep: r.BackupKeep}
	}
	if s.o.BackupInfo != nil {
		info, err := s.o.BackupInfo(ctx, ids)
		if err != nil {
			return nil, err
		}
		for i := range out {
			b := info[out[i].ID]
			out[i].Backups.Count, out[i].Backups.LastBackupAt, out[i].Backups.NextBackupAt = b.Count, b.LastBackupAt, b.NextBackupAt
		}
	}
	return out, nil
}

func (s *Service) Update(ctx context.Context, projectID, id int64, in UpdateInput) (Server, error) {
	if in.Name != nil {
		if err := validName(*in.Name); err != nil {
			return Server{}, err
		}
	}
	var labels map[string]string
	if in.Labels != nil {
		if labels = *in.Labels; labels == nil {
			labels = map[string]string{}
		}
		if err := validLabels(labels); err != nil {
			return Server{}, err
		}
	}
	if in.Backups != nil {
		if err := checkKeep(in.Backups.Keep); err != nil {
			return Server{}, err
		}
	}
	err := db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		// The project lock comes first, before the shared lock lockProjectFirewalls would upgrade.
		if in.Backups != nil {
			if err := projects.LockProject(ctx, etx, projectID); err != nil {
				return err
			}
		}
		if labels != nil {
			if err := lockProjectFirewalls(ctx, etx, projectID); err != nil {
				return err
			}
		}
		row, err := LockServer(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		if labels != nil {
			if err := s.relabel(ctx, tx, etx, row, labels); err != nil {
				return err
			}
		}
		u := etx.Server.UpdateOneID(id)
		if in.Backups != nil {
			if err := s.applyBackups(ctx, etx, row, *in.Backups, u); err != nil {
				return err
			}
		}
		if in.Name != nil {
			u.SetName(*in.Name)
		}
		if labels != nil {
			u.SetLabels(labels)
		}
		return writeErr(u.Exec(ctx))
	})
	if err != nil {
		return Server{}, err
	}
	return s.Get(ctx, projectID, id)
}

func checkKeep(keep int) error {
	if keep < MinBackupKeep || keep > MaxBackupKeep {
		return invalid("backups keep must be between %d and %d", MinBackupKeep, MaxBackupKeep)
	}
	return nil
}

// checkBackups validates b; wasOn is whether the server already has backups on, which skips the storage check.
func (s *Service) checkBackups(b BackupSettings, loc catalog.Location, wasOn bool) error {
	if err := checkKeep(b.Keep); err != nil {
		return err
	}
	if b.Enabled && !wasOn && !s.o.Catalog.BackupsAvailable(loc) {
		return errNoBackups
	}
	return nil
}

// StoredBackups matches the backups that count against a project's backup quota.
var StoredBackups = backup.StatusNotIn(backup.StatusQueued, backup.StatusError)

// BackupReservation is what a server reserves of its project's backup quota: its stored backups, and at least its keep when on.
func BackupReservation(enabled bool, keep, stored int) int {
	if enabled {
		return max(keep, stored)
	}
	return stored
}

// applyBackups reserves the change in keep under the project lock and sets the backup columns on u.
func (s *Service) applyBackups(ctx context.Context, etx *ent.Client, row *ent.Server, b BackupSettings, u *ent.ServerUpdateOne) error {
	loc, err := s.o.Catalog.GetLocation(ctx, row.LocationID)
	if err != nil {
		return err
	}
	if err := s.checkBackups(b, loc, row.BackupsEnabled); err != nil {
		return err
	}
	stored, err := etx.Backup.Query().Where(backup.ServerID(row.ID), StoredBackups).Count(ctx)
	if err != nil {
		return err
	}
	old, next := BackupReservation(row.BackupsEnabled, row.BackupKeep, stored), BackupReservation(b.Enabled, b.Keep, stored)
	if next > old {
		if err := s.o.Projects.ReserveQuota(ctx, etx, row.ProjectID, projects.Usage{BackupsReserved: next - old}); err != nil {
			return err
		}
	}
	if !b.Enabled {
		// A queued backup would land after this with no quota check.
		if err := dropQueuedBackups(ctx, etx, row.ID); err != nil {
			return err
		}
	}
	u.SetBackupsEnabled(b.Enabled).SetBackupKeep(b.Keep)
	if b.Enabled && !row.BackupsEnabled {
		u.SetBackupsEnabledAt(time.Now())
	}
	return nil
}

// lockProjectFirewalls serializes a label change with firewall changes, taking their locks before the server's.
func lockProjectFirewalls(ctx context.Context, etx *ent.Client, projectID int64) error {
	// SHARE, not KEY SHARE: it must wait for firewalls.Create's NO KEY UPDATE.
	pq := etx.Project.Query().Where(project.ID(projectID)).ForShare()
	if _, err := pq.Only(ctx); err != nil {
		return notFound(err)
	}
	_, err := etx.Firewall.Query().Where(firewall.ProjectID(projectID)).Order(firewall.ByID()).ForShare().IDs(ctx)
	return err
}

// relabel checks the firewall limit and queues an apply when new labels change the server's effective firewalls.
func (s *Service) relabel(ctx context.Context, tx *sql.Tx, etx *ent.Client, row *ent.Server, labels map[string]string) error {
	att, err := firewalls.LoadAttachments(ctx, etx, row.ProjectID)
	if err != nil {
		return err
	}
	ids := func(l map[string]string) []int64 {
		var out []int64
		for _, a := range att.Effective(row.ID, l) {
			out = append(out, a.FirewallID)
		}
		return out
	}
	before, after := ids(row.Labels), ids(labels)
	if slices.Equal(before, after) {
		return nil
	}
	if err := firewalls.CheckLimit(ctx, etx, row.ProjectID, row.ID, labels, nil); err != nil {
		return err
	}
	var causes []int64
	for _, id := range append(slices.Clone(before), after...) {
		if slices.Contains(before, id) != slices.Contains(after, id) {
			causes = append(causes, id)
		}
	}
	_, err = firewalls.Bump(ctx, tx, etx, s.o.Jobs, row.ProjectID, []int64{row.ID}, causes)
	return err
}

// HasRunningAction reports whether any running action other than a firewall apply lists the server.
func HasRunningAction(ctx context.Context, c *ent.Client, projectID, serverID int64) (bool, error) {
	return serverlock.HasRunningAction(ctx, c, projectID, serverID)
}

// LockServer locks the project's server row in tx; another running action on it gives 409 locked, or backup_in_progress for a backup.
func LockServer(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.Server, error) {
	return serverlock.Lock(ctx, tx, projectID, id, errNotFound)
}

// queue runs prepare under the server lock and starts the job it returns args for.
func (s *Service) queue(ctx context.Context, projectID, id int64, command string,
	prepare func(etx *ent.Client, row *ent.Server) (func(actionID int64) river.JobArgs, error)) (actions.Action, error) {
	return s.queueAfter(ctx, projectID, id, command, nil, prepare)
}

// queueAfter is queue with before run ahead of the server lock, for parent rows.
func (s *Service) queueAfter(ctx context.Context, projectID, id int64, command string, before func(etx *ent.Client) error,
	prepare func(etx *ent.Client, row *ent.Server) (func(actionID int64) river.JobArgs, error)) (actions.Action, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, errNoJobs
	}
	var act actions.Action
	err := db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if before != nil {
			if err := before(etx); err != nil {
				return err
			}
		}
		row, err := LockServer(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		args, err := prepare(etx, row)
		if err != nil {
			return err
		}
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, command, []actions.Resource{{ID: id, Type: ResourceType}}, args, nil)
		return err
	})
	return act, err
}

// dropQueuedBackups deletes the server's queued backups; their jobs find no row and stop.
func dropQueuedBackups(ctx context.Context, etx *ent.Client, serverID int64) error {
	_, err := etx.Backup.Delete().Where(backup.ServerID(serverID), backup.StatusEQ(backup.StatusQueued)).Exec(ctx)
	return err
}

// Delete marks the server deleting and queues delete_server; a running action on it gives 409 locked.
func (s *Service) Delete(ctx context.Context, projectID, id int64) (actions.Action, error) {
	return s.queue(ctx, projectID, id, KindDeleteServer, func(etx *ent.Client, _ *ent.Server) (func(int64) river.JobArgs, error) {
		if err := etx.Server.UpdateOneID(id).SetStatus(StatusDeleting).Exec(ctx); err != nil {
			return nil, err
		}
		if err := dropQueuedBackups(ctx, etx, id); err != nil {
			return nil, err
		}
		return func(actionID int64) river.JobArgs { return DeleteServerArgs{ServerID: id, ActionID: actionID} }, nil
	})
}

// Actions lists the server's actions; the server must exist in the project.
func (s *Service) Actions(ctx context.Context, projectID, id int64, f actions.ListFilter, page, perPage int) ([]actions.Action, int, error) {
	ok, err := s.o.Ent.Server.Query().Where(server.ID(id), server.ProjectID(projectID)).Exist(ctx)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, errNotFound
	}
	f.Where = append(f.Where, ActionsOf(projectID, id))
	return actions.List(ctx, s.o.Ent, projectID, f, page, perPage)
}

// VMRef is where a server's VM lives.
type VMRef struct {
	LocationID int64
	Node       string
	VMID       int
}

// VM returns the project's server VM and its Proxmox client; 409 server_not_ready without one, server_missing unless tagged ours.
func (s *Service) VM(ctx context.Context, projectID, id int64) (VMRef, pve.Client, error) {
	row, err := s.o.Ent.Server.Query().Where(server.ID(id), server.ProjectID(projectID)).Only(ctx)
	if err != nil {
		return VMRef{}, nil, notFound(err)
	}
	if row.Vmid == nil || row.Node == "" {
		return VMRef{}, nil, errNotReady
	}
	pc, err := s.o.Catalog.ProxmoxFor(ctx, row.LocationID)
	if err != nil {
		return VMRef{}, nil, err
	}
	v, err := OwnedVM(ctx, pc, row.ID, *row.Vmid)
	if err != nil {
		return VMRef{}, nil, err
	}
	return VMRef{LocationID: row.LocationID, Node: v.Node, VMID: v.VMID}, pc, nil
}
