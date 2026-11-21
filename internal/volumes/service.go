// Package volumes owns block volumes: records, placement, quota and the volume actions.
package volumes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/labels"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/riverqueue/river"
)

const (
	// ResourceType is the actions resource type for volumes.
	ResourceType = "volume"

	KindCreateVolume = "create_volume"
	KindAttachVolume = "attach_volume"
	KindDetachVolume = "detach_volume"
	KindResizeVolume = "resize_volume"
	KindDeleteVolume = "delete_volume"

	MinSizeGB = 10
	MaxSizeGB = 10240
	MaxLabels = 64
)

// CreateVolumeArgs is the create_volume job; ServerID set attaches it in the same action.
// File is set when the storage keeps disks as files.
type CreateVolumeArgs struct {
	VolumeID int64  `json:"volume_id"`
	ActionID int64  `json:"action_id"`
	ServerID *int64 `json:"server_id,omitempty"`
	File     bool   `json:"file,omitempty"`
}

func (CreateVolumeArgs) Kind() string                 { return KindCreateVolume }
func (CreateVolumeArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

type AttachVolumeArgs struct {
	VolumeID int64 `json:"volume_id"`
	ServerID int64 `json:"server_id"`
	ActionID int64 `json:"action_id"`
}

func (AttachVolumeArgs) Kind() string                 { return KindAttachVolume }
func (AttachVolumeArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

type DetachVolumeArgs struct {
	VolumeID int64 `json:"volume_id"`
	ServerID int64 `json:"server_id"`
	ActionID int64 `json:"action_id"`
}

func (DetachVolumeArgs) Kind() string                 { return KindDetachVolume }
func (DetachVolumeArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

// ResizeVolumeArgs is the resize_volume job; PrevSizeGB is restored if it fails.
type ResizeVolumeArgs struct {
	VolumeID   int64 `json:"volume_id"`
	ActionID   int64 `json:"action_id"`
	SizeGB     int   `json:"size_gb"`
	PrevSizeGB int   `json:"prev_size_gb"`
}

func (ResizeVolumeArgs) Kind() string                 { return KindResizeVolume }
func (ResizeVolumeArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

type DeleteVolumeArgs struct {
	VolumeID int64 `json:"volume_id"`
	ActionID int64 `json:"action_id"`
}

func (DeleteVolumeArgs) Kind() string                 { return KindDeleteVolume }
func (DeleteVolumeArgs) InsertOpts() river.InsertOpts { return servers.InsertOpts() }

type Options struct {
	SQL      *sql.DB
	Ent      *ent.Client
	Catalog  *catalog.Service
	Projects *projects.Service
	// Jobs nil makes every action fail with 503 service_unavailable.
	Jobs   *jobs.Jobs
	Logger *slog.Logger
}

type Service struct{ o Options }

func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{o: o}
}

// Volume is a volume as the project sees it.
type Volume struct {
	ID           int64
	Name         string
	Labels       map[string]string
	SizeGB       int
	LocationID   int64
	LocationName string
	// LocationDescription completes the API's location reference.
	LocationDescription string
	ServerID            *int64
	Status              string
	LinuxDevice         string
	Created             time.Time
}

type CreateInput struct {
	Name   string
	SizeGB int
	// Location may be empty when Server is set.
	Location string
	// Server is a server id or name, "" for none.
	Server string
	Labels map[string]string
}

// UpdateInput changes only the non-nil fields.
type UpdateInput struct {
	Name   *string
	Labels *map[string]string
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

func validSize(gb int) error {
	if gb < MinSizeGB || gb > MaxSizeGB {
		return invalid("size must be between %d and %d GB", MinSizeGB, MaxSizeGB)
	}
	return nil
}

// serverBusy is true while the server is mid-create, rebuild or delete.
func serverBusy(row *ent.Server) bool {
	switch string(row.Status) {
	case servers.StatusInitializing, servers.StatusRebuilding, servers.StatusDeleting:
		return true
	}
	return false
}

// usable refuses volumes another action holds, and error or missing ones.
func usable(v *ent.Volume) error {
	switch v.Status {
	case volume.StatusCreating, volume.StatusDeleting:
		return errLocked
	case volume.StatusError:
		return errNotReady
	case volume.StatusMissing:
		return errMissing
	}
	return nil
}

// ActionsOf matches the project's actions whose resources include the volume.
func ActionsOf(projectID, volumeID int64) predicate.Action {
	doc, _ := json.Marshal([]actions.Resource{{ID: volumeID, Type: ResourceType}})
	return action.And(action.ProjectID(projectID), func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
}

// lockVolume locks the project's volume row in tx; another running action on it gives 409 locked.
func lockVolume(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.Volume, error) {
	row, err := tx.Volume.Query().Where(volume.ID(id), volume.ProjectID(projectID)).ForUpdate().Only(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	busy, err := tx.Action.Query().Where(ActionsOf(projectID, id), action.StatusEQ(action.StatusRunning)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errLocked
	}
	return row, nil
}

// lockServer is servers.LockServer that also refuses servers mid-create, rebuild or delete.
func lockServer(ctx context.Context, tx *ent.Client, projectID, id int64) (*ent.Server, error) {
	row, err := servers.LockServer(ctx, tx, projectID, id)
	if err != nil {
		return nil, err
	}
	if serverBusy(row) {
		return nil, errServerLocked
	}
	return row, nil
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

func (s *Service) get(ctx context.Context, projectID, id int64) (*ent.Volume, error) {
	row, err := s.o.Ent.Volume.Query().Where(volume.ID(id), volume.ProjectID(projectID)).Only(ctx)
	return row, notFound(err)
}

// attachedCount is how many volumes the server holds.
func attachedCount(ctx context.Context, tx *ent.Client, serverID int64) (int, error) {
	return tx.Volume.Query().Where(volume.ServerID(serverID)).Count(ctx)
}

// Create validates in, places the volume and commits it with its quota and create_volume action.
func (s *Service) Create(ctx context.Context, projectID int64, in CreateInput) (Volume, actions.Action, error) {
	if s.o.Jobs == nil {
		return Volume{}, actions.Action{}, errNoJobs
	}
	if err := validName(in.Name); err != nil {
		return Volume{}, actions.Action{}, err
	}
	if err := validSize(in.SizeGB); err != nil {
		return Volume{}, actions.Action{}, err
	}
	if err := validLabels(in.Labels); err != nil {
		return Volume{}, actions.Action{}, err
	}
	labels := in.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	var srv *ent.Server
	var locationID int64
	if in.Server != "" {
		var err error
		if srv, err = serverByRef(ctx, s.o.Ent, projectID, in.Server); err != nil {
			return Volume{}, actions.Action{}, err
		}
		locationID = srv.LocationID
	}
	if in.Location != "" {
		loc, err := s.o.Catalog.LocationByRef(ctx, in.Location)
		if err != nil {
			return Volume{}, actions.Action{}, err
		}
		if srv != nil && loc.ID != srv.LocationID {
			return Volume{}, actions.Action{}, errOtherLoc
		}
		locationID = loc.ID
	}
	if locationID == 0 {
		return Volume{}, actions.Action{}, invalid("location or server is required")
	}
	pin := ""
	if srv != nil {
		if serverBusy(srv) || srv.Node == "" {
			return Volume{}, actions.Action{}, errServerLocked
		}
		pin = srv.Node
	}
	c, err := s.place(ctx, locationID, pin, in.SizeGB)
	if err != nil {
		return Volume{}, actions.Action{}, err
	}

	var id int64
	var act actions.Action
	err = db.WithTx(ctx, s.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if err := s.o.Projects.ReserveQuota(ctx, etx, projectID, projects.Usage{Volumes: 1, VolumeGB: in.SizeGB}); err != nil {
			return err
		}
		var res []actions.Resource
		var serverID *int64
		if srv != nil {
			row, err := lockServer(ctx, etx, projectID, srv.ID)
			if err != nil {
				return err
			}
			if row.Node != srv.Node {
				return errServerLocked
			}
			n, err := attachedCount(ctx, etx, row.ID)
			if err != nil {
				return err
			}
			if n >= vref.MaxPerServer {
				return errLimit
			}
			serverID = &row.ID
			res = append(res, actions.Resource{ID: row.ID, Type: servers.ResourceType})
		}
		row, err := etx.Volume.Create().SetProjectID(projectID).SetLocationID(locationID).SetName(in.Name).SetLabels(labels).
			SetSizeGB(in.SizeGB).SetNode(c.Node).SetStorage(c.Storage).SetStatus(volume.StatusCreating).Save(ctx)
		if err != nil {
			return writeErr(err)
		}
		id = row.ID
		res = append(res, actions.Resource{ID: row.ID, Type: ResourceType})
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindCreateVolume, res, func(actionID int64) river.JobArgs {
			return CreateVolumeArgs{VolumeID: row.ID, ActionID: actionID, ServerID: serverID, File: c.File}
		}, nil)
		return err
	})
	if err != nil {
		return Volume{}, actions.Action{}, err
	}
	v, err := s.Get(ctx, projectID, id)
	return v, act, err
}

// prepareFunc checks the locked rows and returns the job args; srv is nil for a detached volume.
type prepareFunc func(etx *ent.Client, vol *ent.Volume, srv *ent.Server) (func(actionID int64) river.JobArgs, error)

// queue locks the server (when serverID is set) and then the volume, and starts the action prepare returns args for.
// before runs first in the transaction, for quota.
func (s *Service) queue(ctx context.Context, projectID, id int64, serverID *int64, command string,
	before func(etx *ent.Client) error, prepare prepareFunc) (actions.Action, error) {
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
		var srv *ent.Server
		var res []actions.Resource
		if serverID != nil {
			var err error
			if srv, err = lockServer(ctx, etx, projectID, *serverID); err != nil {
				return err
			}
			res = append(res, actions.Resource{ID: srv.ID, Type: servers.ResourceType})
		}
		vol, err := lockVolume(ctx, etx, projectID, id)
		if err != nil {
			return err
		}
		args, err := prepare(etx, vol, srv)
		if err != nil {
			return err
		}
		res = append(res, actions.Resource{ID: id, Type: ResourceType})
		act, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, command, res, args, nil)
		return err
	})
	return act, err
}

// sameServer reports whether the locked volume is still attached where the request saw it.
func sameServer(vol *ent.Volume, serverID *int64) bool {
	if vol.ServerID == nil || serverID == nil {
		return vol.ServerID == nil && serverID == nil
	}
	return *vol.ServerID == *serverID
}

// Attach queues attach_volume to the project's server ref.
func (s *Service) Attach(ctx context.Context, projectID, id int64, ref string) (actions.Action, error) {
	if _, err := s.get(ctx, projectID, id); err != nil {
		return actions.Action{}, err
	}
	target, err := serverByRef(ctx, s.o.Ent, projectID, ref)
	if err != nil {
		return actions.Action{}, err
	}
	return s.queue(ctx, projectID, id, &target.ID, KindAttachVolume, nil,
		func(etx *ent.Client, vol *ent.Volume, srv *ent.Server) (func(int64) river.JobArgs, error) {
			if err := usable(vol); err != nil {
				return nil, err
			}
			if vol.ServerID != nil {
				return nil, errAttached
			}
			switch {
			case vol.LocationID != srv.LocationID:
				return nil, errOtherLoc
			case vol.Node != "" && vol.Node != srv.Node:
				return nil, errOtherHost
			}
			n, err := attachedCount(ctx, etx, srv.ID)
			if err != nil {
				return nil, err
			}
			if n >= vref.MaxPerServer {
				return nil, errLimit
			}
			return func(actionID int64) river.JobArgs {
				return AttachVolumeArgs{VolumeID: id, ServerID: srv.ID, ActionID: actionID}
			}, nil
		})
}

// Detach queues detach_volume from the volume's server.
func (s *Service) Detach(ctx context.Context, projectID, id int64) (actions.Action, error) {
	cur, err := s.get(ctx, projectID, id)
	if err != nil {
		return actions.Action{}, err
	}
	if cur.ServerID == nil {
		return actions.Action{}, errNotAttached
	}
	return s.queue(ctx, projectID, id, cur.ServerID, KindDetachVolume, nil,
		func(_ *ent.Client, vol *ent.Volume, srv *ent.Server) (func(int64) river.JobArgs, error) {
			if !sameServer(vol, cur.ServerID) {
				return nil, errLocked
			}
			// A missing volume may still be detached so its key can be cleared.
			if vol.Status != volume.StatusMissing {
				if err := usable(vol); err != nil {
					return nil, err
				}
			}
			return func(actionID int64) river.JobArgs {
				return DetachVolumeArgs{VolumeID: id, ServerID: srv.ID, ActionID: actionID}
			}, nil
		})
}

// Resize grows the volume to sizeGB, reserving the extra quota; an attached volume locks its server first.
func (s *Service) Resize(ctx context.Context, projectID, id int64, sizeGB int) (actions.Action, error) {
	if err := validSize(sizeGB); err != nil {
		return actions.Action{}, err
	}
	cur, err := s.get(ctx, projectID, id)
	if err != nil {
		return actions.Action{}, err
	}
	if err := usable(cur); err != nil {
		return actions.Action{}, err
	}
	switch {
	case sizeGB < cur.SizeGB:
		return actions.Action{}, errShrink
	case sizeGB == cur.SizeGB:
		return actions.Action{}, invalid("size must be larger than the current %d GB", cur.SizeGB)
	}
	reserve := func(etx *ent.Client) error {
		return s.o.Projects.ReserveQuota(ctx, etx, projectID, projects.Usage{VolumeGB: sizeGB - cur.SizeGB})
	}
	return s.queue(ctx, projectID, id, cur.ServerID, KindResizeVolume, reserve,
		func(etx *ent.Client, vol *ent.Volume, _ *ent.Server) (func(int64) river.JobArgs, error) {
			if !sameServer(vol, cur.ServerID) || vol.SizeGB != cur.SizeGB {
				return nil, errLocked
			}
			if err := usable(vol); err != nil {
				return nil, err
			}
			if err := etx.Volume.UpdateOneID(id).SetSizeGB(sizeGB).Exec(ctx); err != nil {
				return nil, err
			}
			return func(actionID int64) river.JobArgs {
				return ResizeVolumeArgs{VolumeID: id, ActionID: actionID, SizeGB: sizeGB, PrevSizeGB: vol.SizeGB}
			}, nil
		})
}

// Delete marks a detached volume deleting and queues delete_volume.
func (s *Service) Delete(ctx context.Context, projectID, id int64) (actions.Action, error) {
	return s.queue(ctx, projectID, id, nil, KindDeleteVolume, nil,
		func(etx *ent.Client, vol *ent.Volume, _ *ent.Server) (func(int64) river.JobArgs, error) {
			if vol.Status == volume.StatusCreating || vol.Status == volume.StatusDeleting {
				return nil, errLocked
			}
			if vol.ServerID != nil {
				return nil, errAttached
			}
			if err := etx.Volume.UpdateOneID(id).SetStatus(volume.StatusDeleting).Exec(ctx); err != nil {
				return nil, err
			}
			return func(actionID int64) river.JobArgs { return DeleteVolumeArgs{VolumeID: id, ActionID: actionID} }, nil
		})
}

func (s *Service) Get(ctx context.Context, projectID, id int64) (Volume, error) {
	row, err := s.get(ctx, projectID, id)
	if err != nil {
		return Volume{}, err
	}
	out, err := s.hydrate(ctx, []*ent.Volume{row})
	if err != nil {
		return Volume{}, err
	}
	return out[0], nil
}

// ValidStatus reports whether st is a volume status.
func ValidStatus(st string) bool { return volume.StatusValidator(volume.Status(st)) == nil }

// List filters the project's volumes; an unknown server or location matches nothing.
func (s *Service) List(ctx context.Context, projectID int64, f ListFilter, page, perPage int) ([]Volume, int, error) {
	preds, err := LabelSelector(f.LabelSelector)
	if err != nil {
		return nil, 0, err
	}
	q := s.o.Ent.Volume.Query().Where(volume.ProjectID(projectID)).Where(preds...)
	if f.Name != "" {
		q = q.Where(volume.Name(f.Name))
	}
	if f.Status != "" {
		if !ValidStatus(f.Status) {
			return nil, 0, invalid("unknown status %q", f.Status)
		}
		q = q.Where(volume.StatusEQ(volume.Status(f.Status)))
	}
	if f.Server != "" {
		srv, err := serverByRef(ctx, s.o.Ent, projectID, f.Server)
		if err != nil && err != errServerNotFound {
			return nil, 0, err
		}
		if srv == nil {
			return []Volume{}, 0, nil
		}
		q = q.Where(volume.ServerID(srv.ID))
	}
	if f.Location != "" {
		loc, err := s.o.Catalog.LocationByRef(ctx, f.Location)
		var ae apierr.E
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return []Volume{}, 0, nil
		}
		if err != nil {
			return nil, 0, err
		}
		q = q.Where(volume.LocationID(loc.ID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, perPage = max(page, 1), max(perPage, 1)
	rows, err := q.Order(volume.ByID()).Offset((page - 1) * perPage).Limit(perPage).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out, err := s.hydrate(ctx, rows)
	return out, total, err
}

func (s *Service) hydrate(ctx context.Context, rows []*ent.Volume) ([]Volume, error) {
	locs := map[int64]catalog.Location{}
	out := make([]Volume, len(rows))
	for i, r := range rows {
		loc, ok := locs[r.LocationID]
		if !ok {
			var err error
			if loc, err = s.o.Catalog.GetLocation(ctx, r.LocationID); err != nil {
				return nil, err
			}
			locs[r.LocationID] = loc
		}
		labels := r.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		out[i] = Volume{ID: r.ID, Name: r.Name, Labels: labels, SizeGB: r.SizeGB, LocationID: r.LocationID, LocationName: loc.Name,
			LocationDescription: loc.Description, ServerID: r.ServerID, Status: string(r.Status), LinuxDevice: vref.LinuxDevice(r.ID), Created: r.CreatedAt}
	}
	return out, nil
}

// Update changes the name and labels; a running action on the volume gives 409 locked.
func (s *Service) Update(ctx context.Context, projectID, id int64, in UpdateInput) (Volume, error) {
	if in.Name != nil {
		if err := validName(*in.Name); err != nil {
			return Volume{}, err
		}
	}
	var labels map[string]string
	if in.Labels != nil {
		if labels = *in.Labels; labels == nil {
			labels = map[string]string{}
		}
		if err := validLabels(labels); err != nil {
			return Volume{}, err
		}
	}
	err := db.WithTx(ctx, s.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		if _, err := lockVolume(ctx, etx, projectID, id); err != nil {
			return err
		}
		u := etx.Volume.UpdateOneID(id)
		if in.Name != nil {
			u.SetName(*in.Name)
		}
		if labels != nil {
			u.SetLabels(labels)
		}
		return writeErr(u.Exec(ctx))
	})
	if err != nil {
		return Volume{}, err
	}
	return s.Get(ctx, projectID, id)
}

// Actions lists the volume's actions; the volume must exist in the project.
func (s *Service) Actions(ctx context.Context, projectID, id int64, f actions.ListFilter, page, perPage int) ([]actions.Action, int, error) {
	if _, err := s.get(ctx, projectID, id); err != nil {
		return nil, 0, err
	}
	f.Where = append(f.Where, ActionsOf(projectID, id))
	return actions.List(ctx, s.o.Ent, projectID, f, page, perPage)
}

// ServerVolumes maps each given server to its attached volume ids, in id order.
func (s *Service) ServerVolumes(ctx context.Context, serverIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(serverIDs) == 0 {
		return out, nil
	}
	rows, err := s.o.Ent.Volume.Query().Where(volume.ServerIDIn(serverIDs...)).Order(volume.ByID()).
		Select(volume.FieldID, volume.FieldServerID).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[*r.ServerID] = append(out[*r.ServerID], r.ID)
	}
	return out, nil
}

// HasVolumesWith reports next's answer, or whether the project still owns volumes.
func HasVolumesWith(next projects.HasServersFunc) projects.HasServersFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (bool, error) {
		busy, err := next(ctx, tx, projectID)
		if err != nil || busy {
			return busy, err
		}
		return tx.Volume.Query().Where(volume.ProjectID(projectID)).Exist(ctx)
	}
}

// UsageWith adds the project's volumes and their requested GB to next's usage.
func UsageWith(next projects.UsageFunc) projects.UsageFunc {
	return func(ctx context.Context, tx *ent.Client, projectID int64) (projects.Usage, error) {
		u, err := next(ctx, tx, projectID)
		if err != nil {
			return projects.Usage{}, err
		}
		rows, err := tx.Volume.Query().Where(volume.ProjectID(projectID)).Select(volume.FieldSizeGB).All(ctx)
		if err != nil {
			return projects.Usage{}, err
		}
		u.Volumes = len(rows)
		for _, r := range rows {
			u.VolumeGB += r.SizeGB
		}
		return u, nil
	}
}

func labelsContain(k, v string) predicate.Volume {
	doc, _ := json.Marshal(map[string]string{k: v})
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(volume.FieldLabels)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	}
}

func labelsHave(k string) predicate.Volume {
	return func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString("jsonb_exists(").Ident(s.C(volume.FieldLabels)).Comma().Arg(k).WriteString(")")
		}))
	}
}

// LabelSelector converts the shared selector terms into volume predicates.
func LabelSelector(sel string) ([]predicate.Volume, error) {
	if strings.TrimSpace(sel) == "" {
		return nil, nil
	}
	terms, err := labels.Parse(sel)
	if err != nil {
		return nil, err
	}
	out := make([]predicate.Volume, 0, len(terms))
	for _, term := range terms {
		switch term.Op {
		case labels.Has:
			out = append(out, labelsHave(term.Key))
		case labels.Lacks:
			out = append(out, volume.Not(labelsHave(term.Key)))
		case labels.Eq:
			out = append(out, labelsContain(term.Key, term.Value))
		case labels.Ne:
			out = append(out, volume.Not(labelsContain(term.Key, term.Value)))
		}
	}
	return out, nil
}
