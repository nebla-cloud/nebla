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

// VolumeStatus is the state of a volume.
type VolumeStatus string

// Volume statuses.
const (
	VolumeStatusCreating  VolumeStatus = "creating"
	VolumeStatusAvailable VolumeStatus = "available"
	VolumeStatusAttached  VolumeStatus = "attached"
	VolumeStatusDeleting  VolumeStatus = "deleting"
	VolumeStatusMissing   VolumeStatus = "missing"
	VolumeStatusError     VolumeStatus = "error"
)

// Volume is a block storage volume.
type Volume struct {
	ID       int64
	Name     string
	Labels   map[string]string
	Size     int
	Location LocationRef
	// Server is the id of the attached server, nil when detached.
	Server *int64
	Status VolumeStatus
	// LinuxDevice is the stable device path inside the server.
	LinuxDevice string
	Protection  Protection
	Created     time.Time
}

func volumeFromWire(w wire.Volume) Volume {
	return Volume{
		ID: w.ID, Name: w.Name, Labels: map[string]string(w.Labels), Size: w.Size,
		Location: locationRefFromWire(w.Location), Server: w.Server, Status: VolumeStatus(w.Status),
		LinuxDevice: w.LinuxDevice, Protection: Protection{Delete: w.Protection.Delete}, Created: w.Created,
	}
}

// VolumeCreateOpts creates a volume; give Location, Server or both.
type VolumeCreateOpts struct {
	Name string
	// Size is in GB.
	Size     int
	Location string
	// Server attaches the new volume to a server, by id or name.
	Server     string
	Labels     map[string]string
	Protection *ProtectionOpts
}

// VolumeCreateResult is the outcome of creating a volume.
type VolumeCreateResult struct {
	Volume Volume
	Action Action
}

// VolumeUpdateOpts changes a volume; nil fields are not sent, and a non-nil empty Labels clears them.
type VolumeUpdateOpts struct {
	Name       *string
	Labels     *map[string]string
	Protection *ProtectionOpts
}

// VolumeAttachOpts attaches a volume to a server.
type VolumeAttachOpts struct {
	// Server is a server id or name.
	Server string
}

// VolumeListOpts filters a list of volumes.
type VolumeListOpts struct {
	ListOpts
	Name          string
	LabelSelector string
	Status        VolumeStatus
	// Server is a server id or name.
	Server string
	// Location is a location id or name.
	Location string
}

func (o VolumeListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "name", o.Name)
	setIf(q, "label_selector", o.LabelSelector)
	setIf(q, "status", string(o.Status))
	setIf(q, "server", o.Server)
	setIf(q, "location", o.Location)
	return q
}

type volumeCreateBody struct {
	Name       string            `json:"name"`
	Size       int               `json:"size"`
	Location   string            `json:"location,omitempty"`
	Server     string            `json:"server,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Protection *ProtectionOpts   `json:"protection,omitempty"`
}

type volumeUpdateBody struct {
	Name       *string            `json:"name,omitempty"`
	Labels     *map[string]string `json:"labels,omitempty"`
	Protection *ProtectionOpts    `json:"protection,omitempty"`
}

// VolumesService works with volumes.
type VolumesService struct{ c *Client }

func volumePath(id int64, suffix string) string {
	return "/volumes/" + strconv.FormatInt(id, 10) + suffix
}

// List returns one page of volumes.
func (s *VolumesService) List(ctx context.Context, opts VolumeListOpts) ([]Volume, Meta, error) {
	return listPage(ctx, s.c, "/volumes", opts.values(), "volumes", volumeFromWire)
}

// All returns every volume matching opts.
func (s *VolumesService) All(ctx context.Context, opts VolumeListOpts) ([]Volume, error) {
	return listAll(ctx, s.c, "/volumes", opts.values(), "volumes", volumeFromWire)
}

// Get returns one volume.
func (s *VolumesService) Get(ctx context.Context, id int64) (*Volume, error) {
	var w wire.Volume
	if err := s.c.do(ctx, "GET", volumePath(id, ""), nil, nil, &w); err != nil {
		return nil, err
	}
	v := volumeFromWire(w)
	return &v, nil
}

// GetByName returns the volume with exactly this name.
func (s *VolumesService) GetByName(ctx context.Context, name string) (*Volume, error) {
	all := func(ctx context.Context) ([]Volume, error) { return s.All(ctx, VolumeListOpts{Name: name}) }
	return getByName(ctx, all, name, func(v Volume) string { return v.Name }, "volume")
}

// Create creates a volume; the returned action runs in the background.
func (s *VolumesService) Create(ctx context.Context, opts VolumeCreateOpts) (VolumeCreateResult, error) {
	var w struct {
		Volume wire.Volume `json:"volume"`
		Action wire.Action `json:"action"`
	}
	body := volumeCreateBody{Name: opts.Name, Size: opts.Size, Location: opts.Location, Server: opts.Server, Labels: opts.Labels, Protection: opts.Protection}
	if err := s.c.do(ctx, "POST", "/volumes", nil, body, &w); err != nil {
		return VolumeCreateResult{}, err
	}
	return VolumeCreateResult{Volume: volumeFromWire(w.Volume), Action: actionFromWire(w.Action)}, nil
}

// Update renames a volume, replaces its labels or changes its protection.
func (s *VolumesService) Update(ctx context.Context, id int64, opts VolumeUpdateOpts) (*Volume, error) {
	opts.Labels = clearable(opts.Labels)
	var w wire.Volume
	if err := s.c.do(ctx, "PUT", volumePath(id, ""), nil, volumeUpdateBody(opts), &w); err != nil {
		return nil, err
	}
	v := volumeFromWire(w)
	return &v, nil
}

// Delete deletes a detached volume and returns the delete action.
func (s *VolumesService) Delete(ctx context.Context, id int64) (*Action, error) {
	return actionReply(ctx, s.c, "DELETE", volumePath(id, ""), nil)
}

// Actions returns one page of a volume's actions.
func (s *VolumesService) Actions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, Meta, error) {
	return listPage(ctx, s.c, volumePath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// AllActions returns every action of a volume matching opts.
func (s *VolumesService) AllActions(ctx context.Context, id int64, opts ActionListOpts) ([]Action, error) {
	return listAll(ctx, s.c, volumePath(id, "/actions"), opts.values(), "actions", actionFromWire)
}

// Attach attaches a volume to a server in its location.
func (s *VolumesService) Attach(ctx context.Context, id int64, opts VolumeAttachOpts) (*Action, error) {
	return actionReply(ctx, s.c, "POST", volumePath(id, "/actions/attach"), wire.VolumeAttachInput{Server: opts.Server})
}

// Detach detaches a volume from its server.
func (s *VolumesService) Detach(ctx context.Context, id int64) (*Action, error) {
	return actionReply(ctx, s.c, "POST", volumePath(id, "/actions/detach"), nil)
}

// VolumeResizeOpts is the new size of a volume in GB.
type VolumeResizeOpts struct {
	Size int
}

// Resize grows a volume to opts.Size GB.
func (s *VolumesService) Resize(ctx context.Context, id int64, opts VolumeResizeOpts) (*Action, error) {
	return actionReply(ctx, s.c, "POST", volumePath(id, "/actions/resize"), wire.VolumeResizeInput{Size: opts.Size})
}
