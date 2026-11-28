// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

package nebla

import (
	"context"
	"net/url"
	"strconv"

	"github.com/nebla-cloud/nebla/sdk/nebla/internal/wire"
)

// LocationNetworks reports whether a location has a free address of each family.
type LocationNetworks struct {
	IPv4, IPv6 bool
}

// Location is a place servers can run.
type Location struct {
	ID                       int64
	Name                     string
	Description              string
	Networks                 LocationNetworks
	VolumesAvailable         bool
	BackupsAvailable         bool
	PrivateNetworksAvailable bool
	SnapshotsAvailable       bool
}

// LocationRef is the short form of a location embedded in other resources.
type LocationRef struct {
	ID          int64
	Name        string
	Description string
}

// ServerType is a server size.
type ServerType struct {
	ID          int64
	Name        string
	Description string
	Cores       int
	MemoryMB    int
	DiskGB      int
	Locations   []int64
	Deprecated  bool
}

// Image is an operating system image.
type Image struct {
	ID          int64
	Name        string
	Description string
	OSFlavor    string
	OSVersion   string
	Locations   []int64
}

func locationFromWire(w wire.Location) Location {
	return Location{
		ID: w.ID, Name: w.Name, Description: w.Description,
		Networks:                 LocationNetworks{IPv4: w.Networks.Ipv4, IPv6: w.Networks.Ipv6},
		VolumesAvailable:         w.VolumesAvailable,
		BackupsAvailable:         w.BackupsAvailable,
		PrivateNetworksAvailable: w.PrivateNetworksAvailable,
		SnapshotsAvailable:       w.SnapshotsAvailable,
	}
}

func locationRefFromWire(w wire.LocationRef) LocationRef {
	return LocationRef{ID: w.ID, Name: w.Name, Description: w.Description}
}

func serverTypeFromWire(w wire.ServerType) ServerType {
	return ServerType{ID: w.ID, Name: w.Name, Description: w.Description, Cores: w.Cores, MemoryMB: w.MemoryMb, DiskGB: w.DiskGb, Locations: w.Locations, Deprecated: w.Deprecated}
}

func imageFromWire(w wire.Image) Image {
	return Image{ID: w.ID, Name: w.Name, Description: w.Description, OSFlavor: w.OsFlavor, OSVersion: w.OsVersion, Locations: w.Locations}
}

// LocationsService works with locations.
type LocationsService struct{ c *Client }

// List returns one page of locations.
func (s *LocationsService) List(ctx context.Context, opts ListOpts) ([]Location, Meta, error) {
	return listPage(ctx, s.c, "/locations", opts.values(), "locations", locationFromWire)
}

// All returns every location.
func (s *LocationsService) All(ctx context.Context) ([]Location, error) {
	return listAll(ctx, s.c, "/locations", nil, "locations", locationFromWire)
}

// Get returns one location.
func (s *LocationsService) Get(ctx context.Context, id int64) (*Location, error) {
	var w wire.Location
	if err := s.c.do(ctx, "GET", "/locations/"+strconv.FormatInt(id, 10), nil, nil, &w); err != nil {
		return nil, err
	}
	l := locationFromWire(w)
	return &l, nil
}

// GetByName returns the location with exactly this name.
func (s *LocationsService) GetByName(ctx context.Context, name string) (*Location, error) {
	return getByName(ctx, s.All, name, func(l Location) string { return l.Name }, "location")
}

// ServerTypesService works with server types.
type ServerTypesService struct{ c *Client }

// List returns one page of server types.
func (s *ServerTypesService) List(ctx context.Context, opts ListOpts) ([]ServerType, Meta, error) {
	return listPage(ctx, s.c, "/server_types", opts.values(), "server_types", serverTypeFromWire)
}

// All returns every server type.
func (s *ServerTypesService) All(ctx context.Context) ([]ServerType, error) {
	return listAll(ctx, s.c, "/server_types", nil, "server_types", serverTypeFromWire)
}

// Get returns one server type.
func (s *ServerTypesService) Get(ctx context.Context, id int64) (*ServerType, error) {
	var w wire.ServerType
	if err := s.c.do(ctx, "GET", "/server_types/"+strconv.FormatInt(id, 10), nil, nil, &w); err != nil {
		return nil, err
	}
	t := serverTypeFromWire(w)
	return &t, nil
}

// GetByName returns the server type with exactly this name.
func (s *ServerTypesService) GetByName(ctx context.Context, name string) (*ServerType, error) {
	return getByName(ctx, s.All, name, func(t ServerType) string { return t.Name }, "server type")
}

// ImageListOpts filters a list of images.
type ImageListOpts struct {
	ListOpts
	// Location is a location id or name.
	Location string
}

func (o ImageListOpts) values() url.Values {
	q := o.ListOpts.values()
	setIf(q, "location", o.Location)
	return q
}

// ImagesService works with images.
type ImagesService struct{ c *Client }

// List returns one page of images.
func (s *ImagesService) List(ctx context.Context, opts ImageListOpts) ([]Image, Meta, error) {
	return listPage(ctx, s.c, "/images", opts.values(), "images", imageFromWire)
}

// All returns every image matching opts.
func (s *ImagesService) All(ctx context.Context, opts ImageListOpts) ([]Image, error) {
	return listAll(ctx, s.c, "/images", opts.values(), "images", imageFromWire)
}

// Get returns one image.
func (s *ImagesService) Get(ctx context.Context, id int64) (*Image, error) {
	var w wire.Image
	if err := s.c.do(ctx, "GET", "/images/"+strconv.FormatInt(id, 10), nil, nil, &w); err != nil {
		return nil, err
	}
	i := imageFromWire(w)
	return &i, nil
}

// GetByName returns the image with exactly this name.
func (s *ImagesService) GetByName(ctx context.Context, name string) (*Image, error) {
	all := func(ctx context.Context) ([]Image, error) { return s.All(ctx, ImageListOpts{}) }
	return getByName(ctx, all, name, func(i Image) string { return i.Name }, "image")
}
