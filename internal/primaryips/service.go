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

package primaryips

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/ent/ipallocation"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/serverlock"
	"github.com/riverqueue/river"
)

type Options struct {
	SQL      *sql.DB
	Ent      *ent.Client
	Catalog  *catalog.Service
	Projects *projects.Service
	// Jobs nil makes every action fail with 503 service_unavailable.
	Jobs *jobs.Jobs
	// Bus nil drops events.
	Bus floatingips.Publisher
}

type Service struct{ o Options }

func New(o Options) *Service { return &Service{o: o} }

// Ref names a server.
type Ref struct {
	ID   int64
	Name string
}

// PrimaryIP is a primary IP as the project sees it.
type PrimaryIP struct {
	ID          int64
	Name        string
	Description string
	Labels      map[string]string
	Type        string
	IP          string
	Status      string
	Location    catalog.LocationRef
	Server      *Ref
	AutoDelete  bool
	Created     time.Time
}

type CreateInput struct {
	Type, Location, Name, Description string
	Labels                            map[string]string
	AutoDelete                        *bool
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name, Description *string
	Labels            *map[string]string
	AutoDelete        *bool
}

type ListFilter struct {
	Name, LabelSelector, Server, Location, Type string
}

// primary keeps a query to primary IP rows; floating IPs share the table.
var primary = ipaddress.KindEQ(ipaddress.KindPrimary)

// ByRef finds the project's primary IP by id, then by name.
func ByRef(ctx context.Context, c *ent.Client, projectID int64, ref string) (*ent.IPAddress, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		row, err := c.IPAddress.Query().Where(ipaddress.ID(id), ipaddress.ProjectID(projectID), primary).Only(ctx)
		if err == nil {
			return row, nil
		}
		if !ent.IsNotFound(err) {
			return nil, err
		}
	}
	row, err := c.IPAddress.Query().Where(ipaddress.Name(ref), ipaddress.ProjectID(projectID), primary).Only(ctx)
	return row, notFound(err)
}

func (s *Service) withTx(ctx context.Context, fn func(tx *sql.Tx, etx *ent.Client) error) error {
	return floatingips.WithTx(ctx, s.o.SQL, errBusy, fn)
}

// lockPIP locks the project's primary IP row; a running action on it gives 409 locked.
func lockPIP(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.IPAddress, error) {
	row, err := tx.IPAddress.Query().Where(ipaddress.ID(id), ipaddress.ProjectID(projectID), primary).ForUpdate().Only(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	busy, err := tx.Action.Query().Where(floatingips.ActionsOfType(projectID, id, ResourceType), action.StatusEQ(action.StatusRunning)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errBusy
	}
	return row, nil
}

// Create allocates a primary IP in a location, unassigned.
func (s *Service) Create(ctx context.Context, projectID int64, in CreateInput) (PrimaryIP, error) {
	if in.Name != "" {
		if err := floatingips.ValidName(in.Name); err != nil {
			return PrimaryIP{}, err
		}
	}
	if err := floatingips.ValidDescription(in.Description); err != nil {
		return PrimaryIP{}, err
	}
	if err := floatingips.ValidLabels(in.Labels); err != nil {
		return PrimaryIP{}, err
	}
	if in.Type != ipam.FamilyIPv4 && in.Type != ipam.FamilyIPv6 {
		return PrimaryIP{}, errBadType
	}
	if in.Location == "" {
		return PrimaryIP{}, errNoLocation
	}
	loc, err := s.o.Catalog.LocationByRef(ctx, in.Location)
	if err != nil {
		return PrimaryIP{}, err
	}
	lbls := in.Labels
	if lbls == nil {
		lbls = map[string]string{}
	}
	autoDelete := in.AutoDelete == nil || *in.AutoDelete

	var id int64
	err = s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, projects.Usage{IPs: 1}); err != nil {
			return err
		}
		a, allocID, err := ipam.AllocatePrimary(ctx, etx, loc.ID, in.Type, nil)
		if err != nil {
			return err
		}
		name := in.Name
		if name == "" {
			// Not a valid name, so it can't collide; replaced once the id is known.
			name = "_pending-" + strconv.FormatInt(allocID, 10)
		}
		row, err := etx.IPAddress.Create().SetKind(ipaddress.KindPrimary).SetProjectID(projectID).SetLocationID(loc.ID).
			SetAllocationID(allocID).SetFamily(in.Type).SetIP(a.IP.String()).SetName(name).SetDescription(in.Description).
			SetLabels(lbls).SetAutoDelete(autoDelete).Save(ctx)
		if err != nil {
			return writeErr(err)
		}
		id = row.ID
		if in.Name == "" {
			return writeErr(etx.IPAddress.UpdateOneID(id).SetName(fmt.Sprintf("ip-%s-%d", in.Type, id)).Exec(ctx))
		}
		return nil
	})
	if err != nil {
		return PrimaryIP{}, err
	}
	p, err := s.Get(ctx, projectID, id)
	if err != nil {
		return PrimaryIP{}, err
	}
	s.publish(ctx, ext.PrimaryIPCreated, projectID, p.ID, p.IP, p.Type, p.Location.ID)
	return p, nil
}

func (s *Service) publish(ctx context.Context, kind ext.EventKind, projectID, id int64, ip, family string, locationID int64) {
	if s.o.Bus == nil {
		return
	}
	s.o.Bus.Publish(ctx, ext.Event{Kind: kind, ProjectID: projectID, ResourceType: ResourceType, ResourceID: id,
		At: time.Now(), Data: map[string]any{"ip": ip, "family": family, "location_id": locationID}})
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (PrimaryIP, error) {
	row, err := s.get(ctx, projectID, id)
	if err != nil {
		return PrimaryIP{}, err
	}
	out, err := s.hydrate(ctx, []*ent.IPAddress{row})
	if err != nil {
		return PrimaryIP{}, err
	}
	return out[0], nil
}

func (s *Service) get(ctx context.Context, projectID, id int64) (*ent.IPAddress, error) {
	row, err := s.o.Ent.IPAddress.Query().Where(ipaddress.ID(id), ipaddress.ProjectID(projectID), primary).Only(ctx)
	return row, notFound(err)
}

// List filters the project's primary IPs; an unknown server or location matches nothing.
func (s *Service) List(ctx context.Context, projectID int64, f ListFilter, page, perPage int) ([]PrimaryIP, int, error) {
	preds, err := floatingips.LabelSelector(f.LabelSelector)
	if err != nil {
		return nil, 0, err
	}
	q := s.o.Ent.IPAddress.Query().Where(ipaddress.ProjectID(projectID), primary).Where(preds...)
	if f.Name != "" {
		q = q.Where(ipaddress.Name(f.Name))
	}
	if f.Type != "" {
		q = q.Where(ipaddress.Family(f.Type))
	}
	if f.Server != "" {
		srv, err := floatingips.ServerByRef(ctx, s.o.Ent, projectID, f.Server)
		if errors.Is(err, floatingips.ErrServerNotFound) {
			return []PrimaryIP{}, 0, nil
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
			return []PrimaryIP{}, 0, nil
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

func (s *Service) hydrate(ctx context.Context, rows []*ent.IPAddress) ([]PrimaryIP, error) {
	var ids []int64
	for _, r := range rows {
		if r.ServerID != nil {
			ids = append(ids, *r.ServerID)
		}
	}
	srvs := map[int64]*ent.Server{}
	if len(ids) > 0 {
		list, err := s.o.Ent.Server.Query().Where(server.IDIn(ids...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, sv := range list {
			srvs[sv.ID] = sv
		}
	}
	locs := map[int64]catalog.Location{}
	out := make([]PrimaryIP, len(rows))
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
			return nil, fmt.Errorf("primary IP %d: %w", r.ID, err)
		}
		lbls := r.Labels
		if lbls == nil {
			lbls = map[string]string{}
		}
		var ref *Ref
		if r.ServerID != nil {
			if sv := srvs[*r.ServerID]; sv != nil {
				ref = &Ref{ID: sv.ID, Name: sv.Name}
			}
		}
		out[i] = PrimaryIP{ID: r.ID, Name: r.Name, Description: r.Description, Labels: lbls, Type: r.Family, IP: ip.String(),
			Status: Status(r), Location: loc.Ref(), Server: ref, AutoDelete: r.AutoDelete, Created: r.CreatedAt}
	}
	return out, nil
}

// Update changes the name, description, labels and auto_delete; a running action gives 409 locked.
func (s *Service) Update(ctx context.Context, projectID, id int64, in UpdateInput) (PrimaryIP, error) {
	if in.Name != nil {
		if err := floatingips.ValidName(*in.Name); err != nil {
			return PrimaryIP{}, err
		}
	}
	if in.Description != nil {
		if err := floatingips.ValidDescription(*in.Description); err != nil {
			return PrimaryIP{}, err
		}
	}
	var lbls map[string]string
	if in.Labels != nil {
		if lbls = *in.Labels; lbls == nil {
			lbls = map[string]string{}
		}
		if err := floatingips.ValidLabels(lbls); err != nil {
			return PrimaryIP{}, err
		}
	}
	err := s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		if _, err := lockPIP(ctx, etx, projectID, id); err != nil {
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
		if in.AutoDelete != nil {
			u.SetAutoDelete(*in.AutoDelete)
		}
		return writeErr(u.Exec(ctx))
	})
	if err != nil {
		return PrimaryIP{}, err
	}
	return s.Get(ctx, projectID, id)
}

// Delete frees an unassigned primary IP.
func (s *Service) Delete(ctx context.Context, projectID, id int64) error {
	var gone *ent.IPAddress
	err := s.withTx(ctx, func(_ *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		row, err := lockPIP(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		switch {
		case row.ServerID != nil:
			return errInUse
		case row.ReleasingFromID != nil:
			return errReleasing
		}
		if err := etx.IPAddress.DeleteOneID(id).Exec(ctx); err != nil {
			return err
		}
		if err := ipam.FreeAllocation(ctx, etx, row.AllocationID); err != nil {
			return err
		}
		gone = row
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, ext.PrimaryIPDeleted, projectID, gone.ID, gone.IP, gone.Family, gone.LocationID)
	return nil
}

// Actions lists the primary IP's actions; it must exist in the project.
func (s *Service) Actions(ctx context.Context, projectID, id int64, f actions.ListFilter, page, perPage int) ([]actions.Action, int, error) {
	if _, err := s.get(ctx, projectID, id); err != nil {
		return nil, 0, err
	}
	f.Where = append(f.Where, floatingips.ActionsOfType(projectID, id, ResourceType))
	return actions.List(ctx, s.o.Ent, projectID, f, page, perPage)
}

func lockServer(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.Server, error) {
	return serverlock.Lock(ctx, tx, projectID, id, floatingips.ErrServerNotFound)
}

func segmentOf(ctx context.Context, c *ent.Client, allocationID int64) (ipam.Segment, error) {
	a, err := c.IPAllocation.Query().Where(ipallocation.ID(allocationID)).WithPool().Only(ctx)
	if err != nil {
		return ipam.Segment{}, err
	}
	return ipam.Segment{Bridge: a.Edges.Pool.Bridge, VLAN: a.Edges.Pool.VlanTag}, nil
}

// oldServer marks a server without boot-time network refresh for an instance bump and returns the member warning.
func oldServer(ctx context.Context, etx *ent.Client, srv *ent.Server) ([]string, error) {
	if srv.NetRefresh {
		return nil, nil
	}
	return []string{OldServerWarning}, etx.Server.UpdateOneID(srv.ID).SetInstanceBumpPending(true).Exec(ctx)
}

// start begins kind on the primary IP and the server; the action holds the server so it can't be started meanwhile.
func (s *Service) start(ctx context.Context, tx *sql.Tx, etx *ent.Client, projectID int64, kind string, id, serverID int64) (actions.Action, error) {
	res := []actions.Resource{{ID: id, Type: ResourceType}, {ID: serverID, Type: jobs.ServerResource}}
	return s.o.Jobs.StartAction(ctx, tx, etx, projectID, kind, res, func(actionID int64) river.JobArgs {
		return floatingips.ChangeArgs{ActionID: actionID, FloatingIPID: id}
	}, nil)
}

// Assign assigns an unassigned primary IP to a stopped server; the warnings are for the member.
func (s *Service) Assign(ctx context.Context, projectID, id int64, serverRef string) (actions.Action, []string, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, nil, errNoJobs
	}
	var act actions.Action
	var warns []string
	err := s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		row, err := lockPIP(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		if row.ReleasingFromID != nil {
			return errReleasing
		}
		if row.ServerID != nil {
			cur, err := etx.Server.Get(ctx, *row.ServerID)
			if err != nil {
				return err
			}
			return errInUseOn(cur.Name)
		}
		ref, err := floatingips.ServerByRef(ctx, etx, projectID, serverRef)
		if err != nil {
			return err
		}
		srv, err := lockServer(ctx, etx, projectID, ref.ID)
		if err != nil {
			return err
		}
		if srv.LocationID != row.LocationID {
			return errLocation
		}
		if srv.Status != server.StatusOff {
			return errNotStopped
		}
		others, err := etx.IPAddress.Query().Where(primary, ipaddress.ServerID(srv.ID)).All(ctx)
		if err != nil {
			return err
		}
		for _, o := range others {
			if o.Family == row.Family {
				return errHasIP(row.Family)
			}
		}
		if len(others) > 0 {
			want, err := segmentOf(ctx, etx, row.AllocationID)
			if err != nil {
				return err
			}
			got, err := segmentOf(ctx, etx, others[0].AllocationID)
			if err != nil {
				return err
			}
			if !floatingips.SameSegment(want, got) {
				return errSegment
			}
		}
		if err := floatingips.AssignFresh(ctx, tx, etx, s.o.Jobs, projectID, id, srv.ID); err != nil {
			return err
		}
		if warns, err = oldServer(ctx, etx, srv); err != nil {
			return err
		}
		act, err = s.start(ctx, tx, etx, projectID, KindAssign, id, srv.ID)
		return err
	})
	if err != nil {
		return actions.Action{}, nil, err
	}
	return act, warns, nil
}

// Unassign takes the primary IP off its stopped server; it stays releasing until every VM of the server dropped it.
func (s *Service) Unassign(ctx context.Context, projectID, id int64) (actions.Action, []string, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, nil, errNoJobs
	}
	var act actions.Action
	var warns []string
	err := s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return notFound(err)
		}
		row, err := lockPIP(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		if row.ReleasingFromID != nil {
			return errReleasing
		}
		if row.ServerID == nil {
			return errNotAssigned
		}
		srv, err := lockServer(ctx, etx, projectID, *row.ServerID)
		if err != nil {
			return err
		}
		if srv.Status != server.StatusOff {
			return errNotStopped
		}
		attached, err := etx.IPAddress.Query().Where(ipaddress.KindEQ(ipaddress.KindFloating), ipaddress.ServerID(srv.ID),
			ipaddress.Family(row.Family)).Exist(ctx)
		if err != nil {
			return err
		}
		if attached {
			return errFloating(row.Family)
		}
		if err := floatingips.Release(ctx, etx, row, nil); err != nil {
			return err
		}
		if warns, err = oldServer(ctx, etx, srv); err != nil {
			return err
		}
		act, err = s.start(ctx, tx, etx, projectID, KindUnassign, id, srv.ID)
		return err
	})
	if err != nil {
		return actions.Action{}, nil, err
	}
	return act, warns, nil
}

// ResumeStuck starts an action for each primary IP left mid-change with no running action, backing off after release_pending failures.
func (s *Service) ResumeStuck(ctx context.Context) error {
	if s.o.Jobs == nil {
		return errNoJobs
	}
	rows, err := s.o.Ent.IPAddress.Query().Where(primary, ipaddress.Or(ipaddress.ReleasingFromIDNotNil(), ipaddress.PendingGenNotNil())).
		Order(ipaddress.ByID()).All(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range rows {
		err := s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
			if err := projects.LockProject(ctx, etx, r.ProjectID); err != nil {
				return err
			}
			row, err := lockPIP(ctx, etx, r.ProjectID, r.ID)
			if err != nil {
				return err
			}
			if row.ReleasingFromID == nil && row.PendingGen != nil {
				streak, err := floatingips.FailureStreak(ctx, etx, row, ResourceType, MaxAssignFailures)
				if err != nil {
					return err
				}
				if len(streak) >= MaxAssignFailures {
					// The IP stays assigned; the failed actions are the record.
					return etx.IPAddress.UpdateOneID(row.ID).ClearPendingGen().Exec(ctx)
				}
			}
			if cool, err := floatingips.CoolingDown(ctx, etx, row, ResourceType); err != nil || cool {
				return err
			}
			if row.ReleasingFromID != nil {
				_, err := s.start(ctx, tx, etx, row.ProjectID, KindUnassign, row.ID, *row.ReleasingFromID)
				return err
			}
			if row.PendingGen == nil || row.ServerID == nil {
				return nil
			}
			// An assign changes the server's network, so it waits until the server is stopped and idle.
			srv, err := lockServer(ctx, etx, row.ProjectID, *row.ServerID)
			if err != nil {
				return err
			}
			if srv.Status != server.StatusOff {
				return nil
			}
			_, err = s.start(ctx, tx, etx, row.ProjectID, KindAssign, row.ID, *row.ServerID)
			return err
		})
		if errors.Is(err, errBusy) || errors.Is(err, errNotFound) || errors.Is(err, serverlock.ErrLocked) ||
			errors.Is(err, serverlock.ErrBackingUp) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("resume primary IP %d: %w", r.ID, err))
		}
	}
	return errors.Join(errs...)
}
