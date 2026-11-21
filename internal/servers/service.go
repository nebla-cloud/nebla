// Package servers owns server records, placement and the server actions.
package servers

import (
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
	"github.com/nebla-cloud/nebla/internal/ent/firewall"
	"github.com/nebla-cloud/nebla/internal/ent/firewallresource"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/sshkey"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/labels"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
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
}

type Service struct{ o Options }

func New(o Options) *Service { return &Service{o: o} }

// Server is a server as the project sees it; IPv4/IPv6 are invalid when not assigned.
type Server struct {
	ID, ProjectID int64
	Name, Status  string
	Location      catalog.Location
	Type          catalog.ServerType
	Image         catalog.Image
	Node          string
	Labels        map[string]string
	DiskGB        int
	IPv4, IPv6    netip.Addr
	CreatedAt     time.Time
	Firewalls     []firewalls.Applied
}

type CreateInput struct {
	Name, ServerType, Image, Location string
	// SSHKeys are the project's key ids or names.
	SSHKeys    []string
	UserData   string
	IPv4, IPv6 bool
	Labels     map[string]string
	StartAfter bool
	Firewalls  []int64
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name   *string
	Labels *map[string]string
}

var (
	nameRE       = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	labelKeyRE   = labels.KeyRE
	labelValueRE = labels.ValueRE

	errNotFound  = apierr.New(http.StatusNotFound, "not_found", "not found")
	errNoJobs    = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	errLocked    = apierr.New(http.StatusConflict, "locked", "the server has a running action")
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
	img, err := s.o.Catalog.ImageByRef(ctx, in.Image)
	if err != nil {
		return Server{}, actions.Action{}, err
	}
	loc, err := s.o.Catalog.LocationByRef(ctx, in.Location)
	if err != nil {
		return Server{}, actions.Action{}, err
	}
	switch {
	case typ.Deprecated:
		return Server{}, actions.Action{}, invalid("server type %s is deprecated", typ.Name)
	case !typ.AvailableIn(loc.ID):
		return Server{}, actions.Action{}, invalid("server type %s is not offered in location %s", typ.Name, loc.Name)
	}
	tmpls, err := s.o.Catalog.TemplatesFor(ctx, img.ID, loc.ID)
	if err != nil {
		return Server{}, actions.Action{}, err
	}
	if len(tmpls) == 0 {
		return Server{}, actions.Action{}, invalid("image %s is not available in location %s", img.Name, loc.Name)
	}
	keys, err := s.resolveKeys(ctx, projectID, in.SSHKeys)
	if err != nil {
		return Server{}, actions.Action{}, err
	}
	var families []string
	if in.IPv4 {
		families = append(families, "ipv4")
	}
	if in.IPv6 {
		families = append(families, "ipv6")
	}
	labels := in.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	fwIDs := slices.Compact(slices.Sorted(slices.Values(in.Firewalls)))
	delta := projects.Usage{Servers: 1, VCPU: typ.Cores, MemoryMB: typ.MemoryMB, DiskGB: typ.DiskGB, IPs: len(families)}

	var id int64
	var act actions.Action
	for range 3 {
		err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
			if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, delta); err != nil {
				return err
			}
			if err := lockFirewalls(ctx, etx, projectID, fwIDs); err != nil {
				return err
			}
			loads, err := lockNodes(ctx, etx, loc.ID)
			if err != nil {
				return err
			}
			nodeName, vmid, err := Place(loads, tmpls, typ)
			if err != nil {
				return err
			}
			mac, err := newMAC(ctx, etx)
			if err != nil {
				return err
			}
			row, err := etx.Server.Create().SetProjectID(projectID).SetName(in.Name).SetLocationID(loc.ID).SetNode(nodeName).
				SetTemplateVmid(vmid).SetServerTypeID(typ.ID).SetImageID(img.ID).SetMAC(mac).SetLabels(labels).
				SetCores(typ.Cores).SetMemoryMB(typ.MemoryMB).SetDiskGB(typ.DiskGB).SetUserData(in.UserData).SetSSHKeys(keys).Save(ctx)
			if err != nil {
				return writeErr(err)
			}
			for _, f := range families {
				if _, err := ipam.Allocate(ctx, etx, loc.ID, f, row.ID); err != nil {
					return err
				}
			}
			if err := attachFirewalls(ctx, etx, projectID, row.ID, labels, fwIDs); err != nil {
				return err
			}
			act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindCreateServer,
				[]actions.Resource{{ID: row.ID, Type: ResourceType}},
				func(actionID int64) river.JobArgs {
					return CreateServerArgs{ServerID: row.ID, ActionID: actionID, StartAfter: in.StartAfter}
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
	srv, err := s.Get(ctx, projectID, id)
	return srv, act, err
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

// attachFirewalls applies firewalls ids directly to a new server; its build renders them, so no apply is queued.
func attachFirewalls(ctx context.Context, etx *ent.Client, projectID, serverID int64, labels map[string]string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := firewalls.CheckLimit(ctx, etx, projectID, serverID, labels, ids); err != nil {
		return err
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
	allocs, err := s.o.Ent.IPAllocation.Query().Where(ipallocation.ServerIDIn(ids...)).WithPool().All(ctx)
	if err != nil {
		return nil, err
	}
	type ips struct{ v4, v6 netip.Addr }
	byServer := map[int64]*ips{}
	for _, a := range allocs {
		ip, err := netip.ParseAddr(a.IP)
		if err != nil {
			return nil, err
		}
		e := byServer[a.ServerID]
		if e == nil {
			e = &ips{}
			byServer[a.ServerID] = e
		}
		if a.Edges.Pool.Family == "ipv6" {
			e.v6 = ip
		} else {
			e.v4 = ip
		}
	}
	atts := map[int64]firewalls.Attachments{}
	types := map[int64]catalog.ServerType{}
	imgs := map[int64]catalog.Image{}
	locs := map[int64]catalog.Location{}
	out := make([]Server, len(rows))
	for i, r := range rows {
		t, ok := types[r.ServerTypeID]
		if !ok {
			if t, err = s.o.Catalog.GetServerType(ctx, r.ServerTypeID); err != nil {
				return nil, err
			}
			types[r.ServerTypeID] = t
		}
		img, ok := imgs[r.ImageID]
		if !ok {
			if img, err = s.o.Catalog.GetImage(ctx, r.ImageID); err != nil {
				return nil, err
			}
			imgs[r.ImageID] = img
		}
		loc, ok := locs[r.LocationID]
		if !ok {
			if loc, err = s.o.Catalog.GetLocation(ctx, r.LocationID); err != nil {
				return nil, err
			}
			locs[r.LocationID] = loc
		}
		out[i] = Server{ID: r.ID, ProjectID: r.ProjectID, Name: r.Name, Status: string(r.Status), Location: loc, Type: t, Image: img,
			Node: r.Node, Labels: r.Labels, DiskGB: r.DiskGB, CreatedAt: r.CreatedAt}
		if out[i].Labels == nil {
			out[i].Labels = map[string]string{}
		}
		if e := byServer[r.ID]; e != nil {
			out[i].IPv4, out[i].IPv6 = e.v4, e.v6
		}
		att, ok := atts[r.ProjectID]
		if !ok {
			if att, err = firewalls.LoadAttachments(ctx, s.o.Ent, r.ProjectID); err != nil {
				return nil, err
			}
			atts[r.ProjectID] = att
		}
		out[i].Firewalls = att.Effective(r.ID, out[i].Labels)
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
	err := db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
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
	return c.Action.Query().Where(ActionsOf(projectID, serverID), actionRunning).Exist(ctx)
}

// LockServer locks the project's server row in tx; another running action on it gives 409 locked.
func LockServer(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.Server, error) {
	row, err := tx.Server.Query().Where(server.ID(id), server.ProjectID(projectID)).ForUpdate().Only(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	busy, err := HasRunningAction(ctx, tx, projectID, id)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errLocked
	}
	return row, nil
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

// Delete marks the server deleting and queues delete_server; a running action on it gives 409 locked.
func (s *Service) Delete(ctx context.Context, projectID, id int64) (actions.Action, error) {
	return s.queue(ctx, projectID, id, KindDeleteServer, func(etx *ent.Client, _ *ent.Server) (func(int64) river.JobArgs, error) {
		if err := etx.Server.UpdateOneID(id).SetStatus(StatusDeleting).Exec(ctx); err != nil {
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
