// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// SnapshotStatus is the state of a snapshot.
type SnapshotStatus string

// Snapshot statuses.
const (
	SnapshotStatusCreating  SnapshotStatus = "creating"
	SnapshotStatusAvailable SnapshotStatus = "available"
	SnapshotStatusDeleting  SnapshotStatus = "deleting"
	SnapshotStatusMissing   SnapshotStatus = "missing"
	SnapshotStatusError     SnapshotStatus = "error"
)

// SnapshotImage identifies the image a snapshot's source server used.
type SnapshotImage struct {
	ID   int64
	Name string
}

// Snapshot is a saved copy of a server's disk.
type Snapshot struct {
	ID          int64
	Name        string
	Description string
	Labels      map[string]string
	Size        int
	Location    LocationRef
	// Server is the source server's id, nil once it is deleted.
	Server *int64
	// ServerName is the source server's name when the snapshot was taken.
	ServerName string
	Image      *SnapshotImage
	OSFlavor   string
	OSVersion  string
	Status     SnapshotStatus
	// RestoreKeepsIdentity is nil when the source server is gone.
	RestoreKeepsIdentity *bool
	Protection           Protection
	Created              time.Time
}

func snapshotFromWire(w wire.Snapshot) Snapshot {
	s := Snapshot{
		ID: w.ID, Name: w.Name, Description: w.Description, Labels: map[string]string(w.Labels), Size: w.Size,
		Location: locationRefFromWire(w.Location), Server: w.Server, ServerName: w.ServerName,
		OSFlavor: w.OsFlavor, OSVersion: w.OsVersion, Status: SnapshotStatus(w.Status),
		RestoreKeepsIdentity: w.RestoreKeepsIdentity, Protection: Protection{Delete: w.Protection.Delete}, Created: w.Created,
	}
	if w.Image != nil {
		s.Image = &SnapshotImage{ID: w.Image.ID, Name: w.Image.Name}
	}
	return s
}

// SnapshotCreateOpts snapshots a server; Name defaults to <server>-<YYYYMMDD-HHMM> in UTC.
type SnapshotCreateOpts struct {
	// Server is a server id or name.
	Server      string
	Name        string
	Description string
	Labels      map[string]string
	// ForceStop powers the server off if it does not shut down in time.
	ForceStop  bool
	Protection *ProtectionOpts
}

// SnapshotCreateResult is the outcome of creating a snapshot.
type SnapshotCreateResult struct {
	Snapshot Snapshot
	Action   Action
}

// SnapshotUpdateOpts changes a snapshot; nil fields are not sent, and a non-nil empty Labels clears them.
type SnapshotUpdateOpts struct {
	Name        *string
	Description *string
	Labels      *map[string]string
	Protection  *ProtectionOpts
}

// SnapshotListOpts filters a list of snapshots.
type SnapshotListOpts struct {
	ListOpts
	Name          string
	LabelSelector string
	Status        SnapshotStatus
	// Server is a server id or name.
	Server string
	// Location is a location id or name.
	Location string
}

func (o SnapshotListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "name", o.Name)
	setIf(q, "label_selector", o.LabelSelector)
	setIf(q, "status", string(o.Status))
	setIf(q, "server", o.Server)
	setIf(q, "location", o.Location)
	return q
}

type snapshotCreateBody struct {
	Server      string            `json:"server"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	ForceStop   bool              `json:"force_stop,omitempty"`
	Protection  *ProtectionOpts   `json:"protection,omitempty"`
}

type snapshotUpdateBody struct {
	Name        *string            `json:"name,omitempty"`
	Description *string            `json:"description,omitempty"`
	Labels      *map[string]string `json:"labels,omitempty"`
	Protection  *ProtectionOpts    `json:"protection,omitempty"`
}

// SnapshotsService works with snapshots.
type SnapshotsService struct{ c *Client }

func snapshotPath(id int64, suffix string) string {
	return "/snapshots/" + strconv.FormatInt(id, 10) + suffix
}

// List returns one page of snapshots.
func (s *SnapshotsService) List(ctx context.Context, opts SnapshotListOpts) ([]Snapshot, Meta, error) {
	return listPage(ctx, s.c, "/snapshots", opts.values(), "snapshots", snapshotFromWire)
}

// All returns every snapshot matching opts.
func (s *SnapshotsService) All(ctx context.Context, opts SnapshotListOpts) ([]Snapshot, error) {
	return listAll(ctx, s.c, "/snapshots", opts.values(), "snapshots", snapshotFromWire)
}

// Get returns one snapshot.
func (s *SnapshotsService) Get(ctx context.Context, id int64) (*Snapshot, error) {
	var w wire.Snapshot
	if err := s.c.do(ctx, "GET", snapshotPath(id, ""), nil, nil, &w); err != nil {
		return nil, err
	}
	v := snapshotFromWire(w)
	return &v, nil
}

// GetByName returns the snapshot with exactly this name.
func (s *SnapshotsService) GetByName(ctx context.Context, name string) (*Snapshot, error) {
	all := func(ctx context.Context) ([]Snapshot, error) { return s.All(ctx, SnapshotListOpts{Name: name}) }
	return getByName(ctx, all, name, func(v Snapshot) string { return v.Name }, "snapshot")
}

// Create snapshots a server; the returned action runs in the background.
func (s *SnapshotsService) Create(ctx context.Context, opts SnapshotCreateOpts) (SnapshotCreateResult, error) {
	var w struct {
		Snapshot wire.Snapshot `json:"snapshot"`
		Action   wire.Action   `json:"action"`
	}
	if err := s.c.do(ctx, "POST", "/snapshots", nil, snapshotCreateBody(opts), &w); err != nil {
		return SnapshotCreateResult{}, err
	}
	return SnapshotCreateResult{Snapshot: snapshotFromWire(w.Snapshot), Action: actionFromWire(w.Action)}, nil
}

// Update renames a snapshot, changes its description or protection, or replaces its labels.
func (s *SnapshotsService) Update(ctx context.Context, id int64, opts SnapshotUpdateOpts) (*Snapshot, error) {
	opts.Labels = clearable(opts.Labels)
	var w wire.Snapshot
	if err := s.c.do(ctx, "PUT", snapshotPath(id, ""), nil, snapshotUpdateBody(opts), &w); err != nil {
		return nil, err
	}
	v := snapshotFromWire(w)
	return &v, nil
}

// Delete deletes a snapshot and returns the delete action.
func (s *SnapshotsService) Delete(ctx context.Context, id int64) (*Action, error) {
	return actionReply(ctx, s.c, "DELETE", snapshotPath(id, ""), nil)
}

// Actions returns one page of a snapshot's actions.
func (s *SnapshotsService) Actions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, snapshotPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// AllActions returns every action of a snapshot matching opts.
func (s *SnapshotsService) AllActions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, snapshotPath(id, "/actions"), opts.values(), "actions", actionFromWire)
}
