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

package floatingips

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/ippool"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/labels"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/riverqueue/river"
)

const (
	MaxLabels      = 64
	MaxDescription = 255
)

// Publisher receives domain events; *ext.Bus implements it.
type Publisher interface {
	Publish(ctx context.Context, e ext.Event)
}

type Options struct {
	SQL      *sql.DB
	Ent      *ent.Client
	Catalog  *catalog.Service
	Projects *projects.Service
	// Jobs nil makes every action fail with 503 service_unavailable.
	Jobs   *jobs.Jobs
	Logger *slog.Logger
	// Bus nil drops events.
	Bus Publisher
}

type Service struct{ o Options }

func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{o: o}
}

// FloatingIP is a floating IP as the project sees it.
type FloatingIP struct {
	ID          int64
	Name        string
	Description string
	Labels      map[string]string
	Family      string
	IP          string
	Location    catalog.LocationRef
	ServerID    *int64
	Status      string
	Guest       *GuestConfig
	Created     time.Time
}

// GuestConfig is what to run on the current server and, after a move or unassign, on the previous one.
type GuestConfig struct {
	Server         string
	Apply, Persist string
	PreviousServer string
	Remove         string
}

// CreateInput takes exactly one of Location or Server (an id or name).
type CreateInput struct {
	Name, Description string
	Labels            map[string]string
	Family            string
	Location, Server  string
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name, Description *string
	Labels            *map[string]string
}

type ListFilter struct {
	Name, LabelSelector, Server, Location string
}

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validName(n string) error {
	if !nameRE.MatchString(n) {
		return invalid("name must match %s", nameRE)
	}
	return nil
}

func validDescription(d string) error {
	if utf8.RuneCountInString(d) > MaxDescription {
		return invalid("description must be at most %d characters", MaxDescription)
	}
	return nil
}

func validLabels(l map[string]string) error {
	if len(l) > MaxLabels {
		return invalid("at most %d labels", MaxLabels)
	}
	for k, v := range l {
		if !labels.KeyRE.MatchString(k) {
			return invalid("label key %q must match %s", k, labels.KeyRE)
		}
		if !labels.ValueRE.MatchString(v) {
			return invalid("label value %q must match %s", v, labels.ValueRE)
		}
	}
	return nil
}

// ActionsOf matches the project's actions whose resources include the floating IP.
func ActionsOf(projectID, id int64) predicate.Action {
	doc, _ := json.Marshal([]actions.Resource{{ID: id, Type: ResourceType}})
	return action.And(action.ProjectID(projectID), func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
}

func hasRunningAction(ctx context.Context, c *ent.Client, projectID, id int64) (bool, error) {
	return c.Action.Query().Where(ActionsOf(projectID, id), action.StatusEQ(action.StatusRunning)).Exist(ctx)
}

// withTx runs fn with a 10s lock timeout; a timeout becomes the busy error.
func (s *Service) withTx(ctx context.Context, fn func(tx *sql.Tx, etx *ent.Client) error) error {
	return lockErr(db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '10s'"); err != nil {
			return err
		}
		return fn(tx, etx)
	}))
}

// lockFIP locks the project's floating IP row; a running action on it gives 409 locked.
func lockFIP(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.IPAddress, error) {
	row, err := tx.IPAddress.Query().Where(ipaddress.ID(id), ipaddress.ProjectID(projectID)).ForUpdate().Only(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	busy, err := hasRunningAction(ctx, tx, projectID, id)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errBusy
	}
	return row, nil
}

// lockServers takes NO KEY UPDATE on the project's servers in ascending id order.
func lockServers(ctx context.Context, tx *ent.Client, projectID int64, ids ...int64) (map[int64]*ent.Server, error) {
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	q := tx.Server.Query().Where(server.IDIn(ids...), server.ProjectID(projectID)).Order(server.ByID()).Unique(false)
	q.Modify(func(s *entsql.Selector) { s.For(entsql.LockNoKeyUpdate) })
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*ent.Server, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	for _, id := range ids {
		if out[id] == nil {
			return nil, errServerNotFound
		}
	}
	return out, nil
}

// serverByRef finds the project's server by id, then by name.
func serverByRef(ctx context.Context, c *ent.Client, projectID int64, ref string) (*ent.Server, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		row, err := c.Server.Query().Where(server.ID(id), server.ProjectID(projectID)).Only(ctx)
		if err == nil {
			return row, nil
		}
		if !ent.IsNotFound(err) {
			return nil, err
		}
	}
	row, err := c.Server.Query().Where(server.Name(ref), server.ProjectID(projectID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, errServerNotFound
	}
	return row, err
}

// serverSegment is the segment of the server's primary address of family; errSegment when it has none.
func serverSegment(ctx context.Context, c *ent.Client, serverID int64, family string) (ipam.Segment, error) {
	a, err := c.IPAllocation.Query().Where(ipallocation.ServerID(serverID), ipallocation.OwnerEQ(ipallocation.OwnerServer),
		ipallocation.HasPoolWith(ippool.Family(family))).WithPool().First(ctx)
	if ent.IsNotFound(err) {
		return ipam.Segment{}, errSegment
	}
	if err != nil {
		return ipam.Segment{}, err
	}
	return ipam.Segment{Bridge: a.Edges.Pool.Bridge, VLAN: a.Edges.Pool.VlanTag}, nil
}

func sameSegment(a, b ipam.Segment) bool {
	if a.Bridge != b.Bridge || (a.VLAN == nil) != (b.VLAN == nil) {
		return false
	}
	return a.VLAN == nil || *a.VLAN == *b.VLAN
}

// checkTarget refuses a target that is deleting, elsewhere, on another segment or full.
func checkTarget(ctx context.Context, tx *ent.Client, target *ent.Server, locationID int64, family string, seg *ipam.Segment) (ipam.Segment, error) {
	if target.Status == server.StatusDeleting {
		return ipam.Segment{}, errServerDeleting
	}
	if target.LocationID != locationID {
		return ipam.Segment{}, errSegment
	}
	got, err := serverSegment(ctx, tx, target.ID, family)
	if err != nil {
		return ipam.Segment{}, err
	}
	if seg != nil && !sameSegment(*seg, got) {
		return ipam.Segment{}, errSegment
	}
	n, err := tx.IPAddress.Query().Where(ipaddress.ServerID(target.ID)).Count(ctx)
	if err != nil {
		return ipam.Segment{}, err
	}
	if n >= MaxPerServer {
		return ipam.Segment{}, errLimit
	}
	return got, nil
}

// fipSegment is the segment of the floating IP's own allocation.
func fipSegment(ctx context.Context, tx *ent.Client, row *ent.IPAddress) (ipam.Segment, error) {
	a, err := tx.IPAllocation.Query().Where(ipallocation.ID(row.AllocationID)).WithPool().Only(ctx)
	if err != nil {
		return ipam.Segment{}, err
	}
	return ipam.Segment{Bridge: a.Edges.Pool.Bridge, VLAN: a.Edges.Pool.VlanTag}, nil
}

func resources(id int64, serverIDs ...*int64) []actions.Resource {
	var ids []int64
	for _, s := range serverIDs {
		if s != nil {
			ids = append(ids, *s)
		}
	}
	res := []actions.Resource{{ID: id, Type: ResourceType}}
	for _, s := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		res = append(res, actions.Resource{ID: s, Type: jobs.ServerResource})
	}
	return res
}

func (s *Service) start(ctx context.Context, tx *sql.Tx, etx *ent.Client, projectID int64, kind string, id int64, res []actions.Resource) (actions.Action, error) {
	return s.o.Jobs.StartAction(ctx, tx, etx, projectID, kind, res, func(actionID int64) river.JobArgs {
		return ChangeArgs{ActionID: actionID, FloatingIPID: id}
	}, nil)
}

// assignFresh points an unassigned row at target, bumps its firewall and records the generation to wait for.
func (s *Service) assignFresh(ctx context.Context, tx *sql.Tx, etx *ent.Client, projectID, id, target int64) error {
	if err := etx.IPAddress.UpdateOneID(id).SetServerID(target).ClearPreviousServerID().Exec(ctx); err != nil {
		return err
	}
	if _, err := firewalls.Bump(ctx, tx, etx, s.o.Jobs, projectID, []int64{target}, nil); err != nil {
		return err
	}
	srv, err := etx.Server.Get(ctx, target)
	if err != nil {
		return err
	}
	return etx.IPAddress.UpdateOneID(id).SetPendingGen(srv.FirewallGenDesired).Exec(ctx)
}

// Create allocates a floating IP in a location, or on a server's segment and assigned to it.
func (s *Service) Create(ctx context.Context, projectID int64, in CreateInput) (FloatingIP, *actions.Action, error) {
	if in.Name != "" {
		if err := validName(in.Name); err != nil {
			return FloatingIP{}, nil, err
		}
	}
	if err := validDescription(in.Description); err != nil {
		return FloatingIP{}, nil, err
	}
	if err := validLabels(in.Labels); err != nil {
		return FloatingIP{}, nil, err
	}
	if in.Family != ipam.FamilyIPv4 && in.Family != ipam.FamilyIPv6 {
		return FloatingIP{}, nil, invalid("type must be ipv4 or ipv6")
	}
	lbls := in.Labels
	if lbls == nil {
		lbls = map[string]string{}
	}
	var target *ent.Server
	var locationID int64
	switch {
	case in.Location != "" && in.Server != "":
		return FloatingIP{}, nil, invalid("give either location or server, not both")
	case in.Server != "":
		if s.o.Jobs == nil {
			return FloatingIP{}, nil, errNoJobs
		}
		var err error
		if target, err = serverByRef(ctx, s.o.Ent, projectID, in.Server); err != nil {
			return FloatingIP{}, nil, err
		}
		locationID = target.LocationID
	case in.Location != "":
		loc, err := s.o.Catalog.LocationByRef(ctx, in.Location)
		if err != nil {
			return FloatingIP{}, nil, err
		}
		locationID = loc.ID
	default:
		return FloatingIP{}, nil, invalid("location or server is required")
	}

	var id int64
	var act *actions.Action
	err := s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
		if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, projects.Usage{IPs: 1}); err != nil {
			return err
		}
		var seg *ipam.Segment
		if target != nil {
			srvs, err := lockServers(ctx, etx, projectID, target.ID)
			if err != nil {
				return err
			}
			got, err := checkTarget(ctx, etx, srvs[target.ID], locationID, in.Family, nil)
			if err != nil {
				return err
			}
			seg = &got
		}
		a, allocID, err := ipam.AllocateFloating(ctx, etx, locationID, in.Family, seg)
		if err != nil {
			return err
		}
		row, err := etx.IPAddress.Create().SetProjectID(projectID).SetLocationID(locationID).SetAllocationID(allocID).
			SetFamily(in.Family).SetIP(a.IP.String()).SetName(in.Name).SetDescription(in.Description).SetLabels(lbls).Save(ctx)
		if err != nil {
			return writeErr(err)
		}
		id = row.ID
		if in.Name == "" {
			if err := etx.IPAddress.UpdateOneID(id).SetName(fmt.Sprintf("fip-%d", id)).Exec(ctx); err != nil {
				return writeErr(err)
			}
		}
		if target == nil {
			return nil
		}
		if err := s.assignFresh(ctx, tx, etx, projectID, id, target.ID); err != nil {
			return err
		}
		started, err := s.start(ctx, tx, etx, projectID, KindAssign, id, resources(id, &target.ID))
		act = &started
		return err
	})
	if err != nil {
		return FloatingIP{}, nil, err
	}
	f, err := s.Get(ctx, projectID, id)
	if err != nil {
		return FloatingIP{}, nil, err
	}
	if s.o.Bus != nil {
		s.o.Bus.Publish(ctx, ext.Event{Kind: ext.FloatingIPCreated, ProjectID: projectID, ResourceType: ResourceType, ResourceID: id,
			At: time.Now(), Data: map[string]any{"ip": f.IP, "family": f.Family, "location_id": f.Location.ID}})
	}
	return f, act, nil
}

// mutate locks the project and the floating IP, then runs fn and starts the action it names.
func (s *Service) mutate(ctx context.Context, projectID, id int64,
	fn func(tx *sql.Tx, etx *ent.Client, row *ent.IPAddress) (kind string, res []actions.Resource, err error)) (actions.Action, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, errNoJobs
	}
	var act actions.Action
	err := s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		row, err := lockFIP(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		kind, res, err := fn(tx, etx, row)
		if err != nil {
			return err
		}
		act, err = s.start(ctx, tx, etx, projectID, kind, id, res)
		return err
	})
	return act, err
}

// release records that the row's current server must drop the address before anything else happens.
func release(ctx context.Context, etx *ent.Client, row *ent.IPAddress, to *int64) error {
	u := etx.IPAddress.UpdateOneID(row.ID).SetReleasingFromID(*row.ServerID).SetPreviousServerID(*row.ServerID).ClearPendingGen()
	if to == nil {
		u.ClearServerID()
	} else {
		u.SetServerID(*to)
	}
	return u.Exec(ctx)
}

// Assign assigns the floating IP to the project's server ref, moving it when it is assigned elsewhere.
func (s *Service) Assign(ctx context.Context, projectID, id int64, serverRef string) (actions.Action, error) {
	return s.mutate(ctx, projectID, id, func(tx *sql.Tx, etx *ent.Client, row *ent.IPAddress) (string, []actions.Resource, error) {
		target, err := serverByRef(ctx, etx, projectID, serverRef)
		if err != nil {
			return "", nil, err
		}
		switch {
		case row.ServerID != nil && *row.ServerID == target.ID:
			return "", nil, errSameServer
		case row.ReleasingFromID != nil:
			return "", nil, errReleasing
		case row.Deleting:
			return "", nil, errBusy
		}
		ids := []int64{target.ID}
		if row.ServerID != nil {
			ids = append(ids, *row.ServerID)
		}
		srvs, err := lockServers(ctx, etx, projectID, ids...)
		if err != nil {
			return "", nil, err
		}
		if row.ServerID != nil && srvs[*row.ServerID].Status == server.StatusDeleting {
			return "", nil, errServerDeleting
		}
		seg, err := fipSegment(ctx, etx, row)
		if err != nil {
			return "", nil, err
		}
		if _, err := checkTarget(ctx, etx, srvs[target.ID], row.LocationID, row.Family, &seg); err != nil {
			return "", nil, err
		}
		if row.ServerID == nil {
			err = s.assignFresh(ctx, tx, etx, projectID, id, target.ID)
		} else {
			err = release(ctx, etx, row, &target.ID)
		}
		return KindAssign, resources(id, &target.ID, row.ServerID), err
	})
}

// Unassign takes the floating IP off its server.
func (s *Service) Unassign(ctx context.Context, projectID, id int64) (actions.Action, error) {
	return s.mutate(ctx, projectID, id, func(_ *sql.Tx, etx *ent.Client, row *ent.IPAddress) (string, []actions.Resource, error) {
		switch {
		case row.Deleting:
			return "", nil, errBusy
		case row.ServerID == nil:
			return "", nil, errNotAssigned
		case row.ReleasingFromID != nil:
			return "", nil, errReleasing
		}
		srvs, err := lockServers(ctx, etx, projectID, *row.ServerID)
		if err != nil {
			return "", nil, err
		}
		if srvs[*row.ServerID].Status == server.StatusDeleting {
			return "", nil, errServerDeleting
		}
		return KindUnassign, resources(id, row.ServerID), release(ctx, etx, row, nil)
	})
}

// Delete marks the floating IP deleting, unassigning it first; the job frees the address once it is released.
func (s *Service) Delete(ctx context.Context, projectID, id int64) (actions.Action, error) {
	return s.mutate(ctx, projectID, id, func(_ *sql.Tx, etx *ent.Client, row *ent.IPAddress) (string, []actions.Resource, error) {
		if row.Deleting {
			return "", nil, errBusy
		}
		res := resources(id, row.ServerID, row.ReleasingFromID)
		u := etx.IPAddress.UpdateOneID(id).SetDeleting(true).ClearPendingGen()
		if row.ServerID != nil {
			srvs, err := lockServers(ctx, etx, projectID, *row.ServerID)
			if err != nil {
				return "", nil, err
			}
			if srvs[*row.ServerID].Status == server.StatusDeleting {
				return "", nil, errServerDeleting
			}
			u.ClearServerID()
			if row.ReleasingFromID == nil {
				u.SetReleasingFromID(*row.ServerID).SetPreviousServerID(*row.ServerID)
			}
		}
		return KindDelete, res, u.Exec(ctx)
	})
}

// ResumeStuck starts an action for each floating IP left mid-change with no running action, backing off after release_pending failures.
func (s *Service) ResumeStuck(ctx context.Context) error {
	if s.o.Jobs == nil {
		return errNoJobs
	}
	rows, err := s.o.Ent.IPAddress.Query().Where(ipaddress.Or(ipaddress.ReleasingFromIDNotNil(), ipaddress.Deleting(true),
		ipaddress.PendingGenNotNil())).Order(ipaddress.ByID()).All(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range rows {
		err := s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
			if err := projects.LockProject(ctx, etx, r.ProjectID); err != nil {
				return err
			}
			row, err := lockFIP(ctx, etx, r.ProjectID, r.ID)
			if err != nil {
				return err
			}
			if cool, err := coolingDown(ctx, etx, row); err != nil || cool {
				return err
			}
			var kind string
			switch {
			case row.Deleting:
				kind = KindDelete
			case row.ReleasingFromID == nil && row.PendingGen == nil:
				return nil
			case row.ServerID != nil:
				kind = KindAssign
			default:
				kind = KindUnassign
			}
			_, err = s.start(ctx, tx, etx, row.ProjectID, kind, row.ID, resources(row.ID, row.ServerID, row.ReleasingFromID))
			return err
		})
		if errors.Is(err, errBusy) || errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			s.o.Logger.Error("resume floating IP", "floating_ip", r.ID, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// resumeCooldown is the wait after the nth release_pending failure in a row; the last entry caps it.
var resumeCooldown = []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour}

// coolingDown reports a row whose latest action failed with release_pending within the cooldown for its failure streak.
func coolingDown(ctx context.Context, etx *ent.Client, row *ent.IPAddress) (bool, error) {
	acts, err := etx.Action.Query().Where(ActionsOf(row.ProjectID, row.ID)).Order(ent.Desc(action.FieldID)).
		Limit(len(resumeCooldown)).All(ctx)
	if err != nil {
		return false, err
	}
	n := 0
	for _, a := range acts {
		if a.Status != action.StatusError || a.ErrorCode == nil || *a.ErrorCode != actions.CodeReleasePending || a.Finished == nil {
			break
		}
		n++
	}
	if n == 0 {
		return false, nil
	}
	return time.Since(*acts[0].Finished) < resumeCooldown[n-1], nil
}

func (s *Service) get(ctx context.Context, projectID, id int64) (*ent.IPAddress, error) {
	row, err := s.o.Ent.IPAddress.Query().Where(ipaddress.ID(id), ipaddress.ProjectID(projectID)).Only(ctx)
	return row, notFound(err)
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (FloatingIP, error) {
	row, err := s.get(ctx, projectID, id)
	if err != nil {
		return FloatingIP{}, err
	}
	out, err := s.hydrate(ctx, []*ent.IPAddress{row})
	if err != nil {
		return FloatingIP{}, err
	}
	return out[0], nil
}

// List filters the project's floating IPs; an unknown server or location matches nothing.
func (s *Service) List(ctx context.Context, projectID int64, f ListFilter, page, perPage int) ([]FloatingIP, int, error) {
	preds, err := LabelSelector(f.LabelSelector)
	if err != nil {
		return nil, 0, err
	}
	q := s.o.Ent.IPAddress.Query().Where(ipaddress.ProjectID(projectID)).Where(preds...)
	if f.Name != "" {
		q = q.Where(ipaddress.Name(f.Name))
	}
	if f.Server != "" {
		srv, err := serverByRef(ctx, s.o.Ent, projectID, f.Server)
		if errors.Is(err, errServerNotFound) {
			return []FloatingIP{}, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		q = q.Where(ipaddress.ServerID(srv.ID))
	}
	if f.Location != "" {
		loc, err := s.o.Catalog.LocationByRef(ctx, f.Location)
		var ae apierr.E
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return []FloatingIP{}, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		q = q.Where(ipaddress.LocationID(loc.ID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, perPage = max(page, 1), max(perPage, 1)
	rows, err := q.Order(ipaddress.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.hydrate(ctx, rows)
	return out, total, err
}

func (s *Service) hydrate(ctx context.Context, rows []*ent.IPAddress) ([]FloatingIP, error) {
	var ids []int64
	for _, r := range rows {
		for _, p := range []*int64{r.ServerID, r.PreviousServerID} {
			if p != nil {
				ids = append(ids, *p)
			}
		}
	}
	srvs := map[int64]*ent.Server{}
	if len(ids) > 0 {
		list, err := s.o.Ent.Server.Query().Where(server.IDIn(ids...)).WithImage().All(ctx)
		if err != nil {
			return nil, err
		}
		for _, sv := range list {
			srvs[sv.ID] = sv
		}
	}
	locs := map[int64]catalog.Location{}
	out := make([]FloatingIP, len(rows))
	for i, r := range rows {
		loc, ok := locs[r.LocationID]
		if !ok {
			var err error
			if loc, err = s.o.Catalog.GetLocation(ctx, r.LocationID); err != nil {
				return nil, err
			}
			locs[r.LocationID] = loc
		}
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			return nil, fmt.Errorf("floating IP %d: %w", r.ID, err)
		}
		lbls := r.Labels
		if lbls == nil {
			lbls = map[string]string{}
		}
		out[i] = FloatingIP{ID: r.ID, Name: r.Name, Description: r.Description, Labels: lbls, Family: r.Family, IP: ip.String(),
			Location: loc.Ref(), ServerID: r.ServerID, Status: Status(r), Guest: guestConfig(r, ip, srvs), Created: r.CreatedAt}
	}
	return out, nil
}

func flavor(sv *ent.Server) string {
	if sv.Edges.Image == nil {
		return ""
	}
	return sv.Edges.Image.OsFlavor
}

func guestConfig(r *ent.IPAddress, ip netip.Addr, srvs map[int64]*ent.Server) *GuestConfig {
	var g GuestConfig
	cur, prev := (*ent.Server)(nil), (*ent.Server)(nil)
	if r.ServerID != nil {
		cur = srvs[*r.ServerID]
	}
	if r.PreviousServerID != nil {
		prev = srvs[*r.PreviousServerID]
	}
	if cur == nil && prev == nil {
		return nil
	}
	if cur != nil {
		cmds := Guest(flavor(cur), ip)
		g.Server, g.Apply, g.Persist = cur.Name, cmds.Apply, cmds.Persist
	}
	if prev != nil {
		g.PreviousServer, g.Remove = prev.Name, Removal(flavor(prev), ip)
	}
	return &g
}

// Update changes the name, description and labels; a running action on the floating IP gives 409 locked.
func (s *Service) Update(ctx context.Context, projectID, id int64, in UpdateInput) (FloatingIP, error) {
	if in.Name != nil {
		if err := validName(*in.Name); err != nil {
			return FloatingIP{}, err
		}
	}
	if in.Description != nil {
		if err := validDescription(*in.Description); err != nil {
			return FloatingIP{}, err
		}
	}
	var lbls map[string]string
	if in.Labels != nil {
		if lbls = *in.Labels; lbls == nil {
			lbls = map[string]string{}
		}
		if err := validLabels(lbls); err != nil {
			return FloatingIP{}, err
		}
	}
	err := s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		if _, err := lockFIP(ctx, etx, projectID, id); err != nil {
			return err
		}
		u := etx.IPAddress.UpdateOneID(id)
		if in.Name != nil {
			u.SetName(*in.Name)
		}
		if in.Description != nil {
			u.SetDescription(*in.Description)
		}
		if lbls != nil {
			u.SetLabels(lbls)
		}
		return writeErr(u.Exec(ctx))
	})
	if err != nil {
		return FloatingIP{}, err
	}
	return s.Get(ctx, projectID, id)
}

// Actions lists the floating IP's actions; it must exist in the project.
func (s *Service) Actions(ctx context.Context, projectID, id int64, f actions.ListFilter, page, perPage int) ([]actions.Action, int, error) {
	if _, err := s.get(ctx, projectID, id); err != nil {
		return nil, 0, err
	}
	f.Where = append(f.Where, ActionsOf(projectID, id))
	return actions.List(ctx, s.o.Ent, projectID, f, page, perPage)
}

// HasWith reports next's answer, or whether the project still owns floating IPs.
func HasWith(next projects.HasServersFunc) projects.HasServersFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (bool, error) {
		busy, err := next(ctx, tx, projectID)
		if err != nil || busy {
			return busy, err
		}
		return tx.IPAddress.Query().Where(ipaddress.ProjectID(projectID)).Exist(ctx)
	}
}

// UsageWith adds the project's floating IPs, assigned or not, to next's IPs.
func UsageWith(next projects.UsageFunc) projects.UsageFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (projects.Usage, error) {
		u, err := next(ctx, tx, projectID)
		if err != nil {
			return projects.Usage{}, err
		}
		n, err := tx.IPAddress.Query().Where(ipaddress.ProjectID(projectID)).Count(ctx)
		if err != nil {
			return projects.Usage{}, err
		}
		u.IPs += n
		return u, nil
	}
}

func labelsContain(k, v string) predicate.IPAddress {
	doc, _ := json.Marshal(map[string]string{k: v})
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(ipaddress.FieldLabels)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	}
}

func labelsHave(k string) predicate.IPAddress {
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("jsonb_exists(").Ident(s.C(ipaddress.FieldLabels)).Comma().Arg(k).WriteString(")")
		}))
	}
}

// LabelSelector converts the shared selector terms into floating IP predicates.
func LabelSelector(sel string) ([]predicate.IPAddress, error) {
	if strings.TrimSpace(sel) == "" {
		return nil, nil
	}
	terms, err := labels.Parse(sel)
	if err != nil {
		return nil, err
	}
	out := make([]predicate.IPAddress, 0, len(terms))
	for _, term := range terms {
		switch term.Op {
		case labels.Has:
			out = append(out, labelsHave(term.Key))
		case labels.Lacks:
			out = append(out, ipaddress.Not(labelsHave(term.Key)))
		case labels.Eq:
			out = append(out, labelsContain(term.Key, term.Value))
		case labels.Ne:
			out = append(out, ipaddress.Not(labelsContain(term.Key, term.Value)))
		}
	}
	return out, nil
}
