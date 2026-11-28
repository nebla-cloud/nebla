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

package snapshots

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/image"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/labels"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/protection"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/riverqueue/river"
)

const (
	// ResourceType is the actions resource type for snapshots.
	ResourceType = "snapshot"

	KindCreateSnapshot = "create_snapshot"
	KindDeleteSnapshot = "delete_snapshot"

	MaxLabels = 64
)

// CreateSnapshotArgs is the create_snapshot job.
type CreateSnapshotArgs struct {
	SnapshotID int64 `json:"snapshot_id"`
	ActionID   int64 `json:"action_id"`
}

func (CreateSnapshotArgs) Kind() string                 { return KindCreateSnapshot }
func (CreateSnapshotArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

// DeleteSnapshotArgs is the delete_snapshot job; PrevStatus is the status a failed delete restores.
type DeleteSnapshotArgs struct {
	SnapshotID int64  `json:"snapshot_id"`
	ActionID   int64  `json:"action_id"`
	PrevStatus string `json:"prev_status,omitempty"`
}

func (DeleteSnapshotArgs) Kind() string                 { return KindDeleteSnapshot }
func (DeleteSnapshotArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

type Options struct {
	SQL      *sql.DB
	Ent      *ent.Client
	Catalog  *catalog.Service
	Projects *projects.Service
	// Jobs nil makes every action fail with 503 service_unavailable.
	Jobs   *jobs.Jobs
	Logger *slog.Logger
	// Now is the clock for default names; nil means time.Now.
	Now func() time.Time
}

type Service struct{ o Options }

func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{o: o}
}

// Snapshot is a snapshot as the project sees it.
type Snapshot struct {
	ID          int64
	Name        string
	Description string
	Labels      map[string]string
	SizeGB      int
	LocationID  int64
	ServerID    *int64
	ServerName  string
	ImageID     *int64
	ImageName   string
	OSFlavor    string
	OSVersion   string
	Status      string
	// RestoreKeepsIdentity is nil once the source server is gone.
	RestoreKeepsIdentity *bool
	DeleteProtected      bool
	CreatedAt            time.Time
}

type CreateInput struct {
	// Server is a server id or name.
	Server string
	// Name defaults to <server>-<YYYYMMDD-HHMM> in UTC.
	Name        string
	Description string
	Labels      map[string]string
	ForceStop   bool
	// DeleteProtected refuses Delete until cleared.
	DeleteProtected bool
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name, Description *string
	Labels            *map[string]string
	DeleteProtected   *bool
}

type ListFilter struct {
	Name, LabelSelector, Status, Server, Location string
}

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

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
		if !labels.KeyRE.MatchString(k) {
			return invalid("label key %q must match %s", k, labels.KeyRE)
		}
		if !labels.ValueRE.MatchString(v) {
			return invalid("label value %q must match %s", v, labels.ValueRE)
		}
	}
	return nil
}

// defaultName is <server>-<YYYYMMDD-HHMM>, trimming the server's name to keep it a valid name.
func defaultName(serverName string, now time.Time) string {
	suffix := now.UTC().Format("20060102-1504")
	prefix := strings.TrimRight(serverName[:min(len(serverName), 62-len(suffix))], "-")
	return prefix + "-" + suffix
}

// snapshottable is true for servers a snapshot can be taken of.
func snapshottable(row *ent.Server) bool {
	st := string(row.Status)
	return (st == servers.StatusRunning || st == servers.StatusOff) && row.Vmid != nil && row.Node != ""
}

// ActionsOf matches the project's actions whose resources include the snapshot.
func ActionsOf(projectID, snapshotID int64) predicate.Action {
	doc, _ := json.Marshal([]actions.Resource{{ID: snapshotID, Type: ResourceType}})
	return action.And(action.ProjectID(projectID), func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
}

func hasRunningAction(ctx context.Context, tx *ent.Client, projectID, id int64) (bool, error) {
	return tx.Action.Query().Where(ActionsOf(projectID, id), action.StatusEQ(action.StatusRunning)).Exist(ctx)
}

// lockSnapshot locks the project's snapshot row in tx.
func lockSnapshot(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.Snapshot, error) {
	row, err := tx.Snapshot.Query().Where(snapshot.ID(id), snapshot.ProjectID(projectID)).ForUpdate().Only(ctx)
	return row, notFound(err)
}

// CheckDeletable is the one protection function for snapshot delete; the row must be locked in etx.
func CheckDeletable(ctx context.Context, etx *ent.Client, row *ent.Snapshot) error {
	busy, err := hasRunningAction(ctx, etx, row.ProjectID, row.ID)
	if err != nil {
		return err
	}
	if busy {
		return errLocked
	}
	building, err := etx.Server.Query().Where(server.Or(
		server.And(server.SnapshotID(row.ID), server.StatusEQ(servers.StatusInitializing)),
		server.RebuildSnapshotID(row.ID))).Exist(ctx)
	if err != nil {
		return err
	}
	if building {
		return errInUse
	}
	switch row.Status {
	case snapshot.StatusAvailable, snapshot.StatusMissing, snapshot.StatusError:
		return nil
	}
	return errLocked
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

// bootSizeGB reads the server's boot disk size from its VM config, rounded up to whole GB.
func (s *Service) bootSizeGB(ctx context.Context, srv *ent.Server) (int, error) {
	pc, err := s.o.Catalog.ProxmoxFor(ctx, srv.LocationID)
	if err != nil {
		return 0, err
	}
	cfg, err := pc.Config(ctx, srv.Node, *srv.Vmid)
	switch {
	case errors.Is(err, pve.ErrNotFound):
		return 0, servers.ErrMissing
	case err != nil:
		s.o.Logger.WarnContext(ctx, "snapshots: read server config", "server_id", srv.ID, "err", err)
		return 0, errUnreachable
	}
	key := catalog.BootDisk(cfg)
	b, ok := servers.DiskBytes(cfg[key])
	if key == "" || !ok || b <= 0 {
		return 0, errNoBootDisk
	}
	const gb = int64(1) << 30
	return int((b + gb - 1) / gb), nil
}

// Create reserves quota for the server's boot disk and queues create_snapshot; the server is not touched here.
func (s *Service) Create(ctx context.Context, projectID int64, in CreateInput) (Snapshot, actions.Action, error) {
	if s.o.Jobs == nil {
		return Snapshot{}, actions.Action{}, errNoJobs
	}
	if in.Name != "" {
		if err := validName(in.Name); err != nil {
			return Snapshot{}, actions.Action{}, err
		}
	}
	if err := validLabels(in.Labels); err != nil {
		return Snapshot{}, actions.Action{}, err
	}
	labels := in.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	srv, err := serverByRef(ctx, s.o.Ent, projectID, in.Server)
	if err != nil {
		return Snapshot{}, actions.Action{}, err
	}
	if !snapshottable(srv) {
		return Snapshot{}, actions.Action{}, errNotReady
	}
	ok, err := s.Available(ctx, srv.LocationID)
	if err != nil {
		return Snapshot{}, actions.Action{}, err
	}
	if !ok {
		return Snapshot{}, actions.Action{}, errUnavailable
	}
	name := in.Name
	if name == "" {
		name = defaultName(srv.Name, s.o.Now())
	}
	sizeGB, err := s.bootSizeGB(ctx, srv)
	if err != nil {
		return Snapshot{}, actions.Action{}, err
	}

	var id int64
	var act actions.Action
	err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, projectID); err != nil {
			return err
		}
		row, err := servers.LockServer(ctx, etx, projectID, srv.ID)
		if err != nil {
			return err
		}
		if !snapshottable(row) {
			return errNotReady
		}
		if row.Node != srv.Node || *row.Vmid != *srv.Vmid {
			return errServerChanged
		}
		if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, projects.Usage{Snapshots: 1, SnapshotGB: sizeGB}); err != nil {
			return err
		}
		fp, err := servers.NetworkFingerprint(ctx, etx, row)
		if err != nil {
			return err
		}
		// With an instance bump pending the guest may run an older network under this instance-id.
		if row.InstanceBumpPending {
			fp = ""
		}
		flavor, version, err := servers.OSFacts(ctx, etx, row)
		if err != nil {
			return err
		}
		snap, err := etx.Snapshot.Create().SetProjectID(projectID).SetLocationID(row.LocationID).SetName(name).
			SetDescription(in.Description).SetLabels(labels).SetServerID(row.ID).SetServerName(row.Name).
			SetNillableImageID(row.ImageID).SetOsFlavor(flavor).SetOsVersion(version).SetSizeGB(sizeGB).
			SetInstanceGeneration(servers.InstanceGeneration(row)).SetNetworkFingerprint(fp).
			SetForceStop(in.ForceStop).SetDeleteProtected(in.DeleteProtected).SetStatus(snapshot.StatusCreating).Save(ctx)
		if err != nil {
			return writeErr(err)
		}
		id = snap.ID
		res := []actions.Resource{{ID: row.ID, Type: servers.ResourceType}, {ID: snap.ID, Type: ResourceType}}
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindCreateSnapshot, res, func(actionID int64) river.JobArgs {
			return CreateSnapshotArgs{SnapshotID: snap.ID, ActionID: actionID}
		}, nil)
		return err
	})
	if err != nil {
		return Snapshot{}, actions.Action{}, err
	}
	out, err := s.Get(ctx, projectID, id)
	return out, act, err
}

// needsRestart reports whether a delete must start the snapshot's server first: a failed create left it off.
func needsRestart(row *ent.Snapshot) bool { return row.WasRunning && row.ServerID != nil }

// Delete marks the snapshot deleting and queues delete_snapshot, holding the server too when the job must start it.
func (s *Service) Delete(ctx context.Context, projectID, id int64) (actions.Action, error) {
	if s.o.Jobs == nil {
		return actions.Action{}, errNoJobs
	}
	pre, err := s.get(ctx, projectID, id)
	if err != nil {
		return actions.Action{}, err
	}
	if pre.DeleteProtected {
		return actions.Action{}, protection.ErrDelete
	}
	var act actions.Action
	err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		res := []actions.Resource{{ID: id, Type: ResourceType}}
		gone := false
		// Lock order project -> server -> snapshot, so the server is locked before the snapshot row.
		if needsRestart(pre) {
			_, err := servers.LockServer(ctx, etx, projectID, *pre.ServerID)
			var ae apierr.E
			switch {
			case errors.As(err, &ae) && ae.Status == http.StatusNotFound:
				gone = true
			case err != nil:
				return err
			default:
				res = []actions.Resource{{ID: *pre.ServerID, Type: servers.ResourceType}, {ID: id, Type: ResourceType}}
			}
		}
		row, err := lockSnapshot(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		if row.DeleteProtected {
			return protection.ErrDelete
		}
		if needsRestart(row) != needsRestart(pre) || (needsRestart(row) && *row.ServerID != *pre.ServerID) {
			return errLocked
		}
		if err := CheckDeletable(ctx, etx, row); err != nil {
			return err
		}
		u := etx.Snapshot.UpdateOneID(id).SetStatus(snapshot.StatusDeleting)
		if gone {
			// The server is gone, so there is nothing to start.
			u.SetWasRunning(false)
		}
		if err := u.Exec(ctx); err != nil {
			return err
		}
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindDeleteSnapshot, res, func(actionID int64) river.JobArgs {
			return DeleteSnapshotArgs{SnapshotID: id, ActionID: actionID, PrevStatus: string(row.Status)}
		}, nil)
		return err
	})
	return act, err
}

func (s *Service) get(ctx context.Context, projectID, id int64) (*ent.Snapshot, error) {
	row, err := s.o.Ent.Snapshot.Query().Where(snapshot.ID(id), snapshot.ProjectID(projectID)).Only(ctx)
	return row, notFound(err)
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (Snapshot, error) {
	row, err := s.get(ctx, projectID, id)
	if err != nil {
		return Snapshot{}, err
	}
	out, err := s.hydrate(ctx, []*ent.Snapshot{row})
	if err != nil {
		return Snapshot{}, err
	}
	return out[0], nil
}

// ValidStatus reports whether st is a snapshot status.
func ValidStatus(st string) bool { return snapshot.StatusValidator(snapshot.Status(st)) == nil }

// List filters the project's snapshots; an unknown server or location matches nothing.
func (s *Service) List(ctx context.Context, projectID int64, f ListFilter, page, perPage int) ([]Snapshot, int, error) {
	preds, err := LabelSelector(f.LabelSelector)
	if err != nil {
		return nil, 0, err
	}
	q := s.o.Ent.Snapshot.Query().Where(snapshot.ProjectID(projectID)).Where(preds...)
	if f.Name != "" {
		q = q.Where(snapshot.Name(f.Name))
	}
	if f.Status != "" {
		if !ValidStatus(f.Status) {
			return nil, 0, invalid("unknown status %q", f.Status)
		}
		q = q.Where(snapshot.StatusEQ(snapshot.Status(f.Status)))
	}
	if f.Server != "" {
		srv, err := serverByRef(ctx, s.o.Ent, projectID, f.Server)
		if err != nil && !errors.Is(err, errServerNotFound) {
			return nil, 0, err
		}
		if srv == nil {
			return []Snapshot{}, 0, nil
		}
		q = q.Where(snapshot.ServerID(srv.ID))
	}
	if f.Location != "" {
		loc, err := s.o.Catalog.LocationByRef(ctx, f.Location)
		var ae apierr.E
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return []Snapshot{}, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		q = q.Where(snapshot.LocationID(loc.ID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, perPage = max(page, 1), max(perPage, 1)
	rows, err := q.Order(snapshot.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.hydrate(ctx, rows)
	return out, total, err
}

func (s *Service) hydrate(ctx context.Context, rows []*ent.Snapshot) ([]Snapshot, error) {
	var imageIDs, serverIDs []int64
	for _, r := range rows {
		if r.ImageID != nil {
			imageIDs = append(imageIDs, *r.ImageID)
		}
		if r.ServerID != nil {
			serverIDs = append(serverIDs, *r.ServerID)
		}
	}
	imageNames := map[int64]string{}
	if len(imageIDs) > 0 {
		imgs, err := s.o.Ent.Image.Query().Where(image.IDIn(imageIDs...)).Select(image.FieldID, image.FieldName).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, im := range imgs {
			imageNames[im.ID] = im.Name
		}
	}
	srvs := map[int64]*ent.Server{}
	if len(serverIDs) > 0 {
		list, err := s.o.Ent.Server.Query().Where(server.IDIn(serverIDs...)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, sv := range list {
			srvs[sv.ID] = sv
		}
	}
	out := make([]Snapshot, len(rows))
	for i, r := range rows {
		labels := r.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		o := Snapshot{ID: r.ID, Name: r.Name, Description: r.Description, Labels: labels, SizeGB: r.SizeGB, LocationID: r.LocationID,
			ServerID: r.ServerID, ServerName: r.ServerName, ImageID: r.ImageID, OSFlavor: r.OsFlavor, OSVersion: r.OsVersion,
			Status: string(r.Status), DeleteProtected: r.DeleteProtected, CreatedAt: r.CreatedAt}
		if r.ImageID != nil {
			o.ImageName = imageNames[*r.ImageID]
		}
		if r.ServerID != nil {
			if sv, ok := srvs[*r.ServerID]; ok {
				fp, err := servers.NetworkFingerprint(ctx, s.o.Ent, sv)
				if err != nil {
					return nil, fmt.Errorf("network fingerprint of server %d: %w", sv.ID, err)
				}
				keeps := fp == r.NetworkFingerprint
				o.RestoreKeepsIdentity = &keeps
			}
		}
		out[i] = o
	}
	return out, nil
}

// Update changes name, description, labels and protection; a running action gives 409 locked unless only protection changes.
func (s *Service) Update(ctx context.Context, projectID, id int64, in UpdateInput) (Snapshot, error) {
	if in.Name != nil {
		if err := validName(*in.Name); err != nil {
			return Snapshot{}, err
		}
	}
	var labels map[string]string
	if in.Labels != nil {
		if labels = *in.Labels; labels == nil {
			labels = map[string]string{}
		}
		if err := validLabels(labels); err != nil {
			return Snapshot{}, err
		}
	}
	err := db.WithTx(ctx, s.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := lockSnapshot(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		// Protection alone may change while an action runs.
		if in.Name != nil || in.Description != nil || in.Labels != nil || in.DeleteProtected == nil {
			busy, err := hasRunningAction(ctx, etx, projectID, row.ID)
			if err != nil {
				return err
			}
			if busy {
				return errLocked
			}
		}
		u := etx.Snapshot.UpdateOneID(id)
		if in.DeleteProtected != nil {
			u.SetDeleteProtected(*in.DeleteProtected)
		}
		if in.Name != nil {
			u.SetName(*in.Name)
		}
		if in.Description != nil {
			u.SetDescription(*in.Description)
		}
		if labels != nil {
			u.SetLabels(labels)
		}
		return writeErr(u.Exec(ctx))
	})
	if err != nil {
		return Snapshot{}, err
	}
	return s.Get(ctx, projectID, id)
}

// Actions lists the snapshot's actions; the snapshot must exist in the project.
func (s *Service) Actions(ctx context.Context, projectID, id int64, f actions.ListFilter, page, perPage int) ([]actions.Action, int, error) {
	if _, err := s.get(ctx, projectID, id); err != nil {
		return nil, 0, err
	}
	f.Where = append(f.Where, ActionsOf(projectID, id))
	return actions.List(ctx, s.o.Ent, projectID, f, page, perPage)
}

func labelsContain(k, v string) predicate.Snapshot {
	doc, _ := json.Marshal(map[string]string{k: v})
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(snapshot.FieldLabels)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	}
}

func labelsHave(k string) predicate.Snapshot {
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("jsonb_exists(").Ident(s.C(snapshot.FieldLabels)).Comma().Arg(k).WriteString(")")
		}))
	}
}

// LabelSelector converts the shared selector terms into snapshot predicates.
func LabelSelector(sel string) ([]predicate.Snapshot, error) {
	if strings.TrimSpace(sel) == "" {
		return nil, nil
	}
	terms, err := labels.Parse(sel)
	if err != nil {
		return nil, err
	}
	out := make([]predicate.Snapshot, 0, len(terms))
	for _, term := range terms {
		switch term.Op {
		case labels.Has:
			out = append(out, labelsHave(term.Key))
		case labels.Lacks:
			out = append(out, snapshot.Not(labelsHave(term.Key)))
		case labels.Eq:
			out = append(out, labelsContain(term.Key, term.Value))
		case labels.Ne:
			out = append(out, snapshot.Not(labelsContain(term.Key, term.Value)))
		}
	}
	return out, nil
}
