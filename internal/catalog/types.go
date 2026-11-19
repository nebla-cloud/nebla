package catalog

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/location"
	"github.com/Hexeption/nebla/internal/ent/servertype"
)

type ServerType struct {
	ID          int64
	Name        string
	Description string
	Cores       int
	MemoryMB    int
	DiskGB      int
	// Locations is empty when the type is offered everywhere.
	Locations  []int64
	Deprecated bool
	CreatedAt  time.Time
}

func (t ServerType) AvailableIn(locationID int64) bool {
	return len(t.Locations) == 0 || slices.Contains(t.Locations, locationID)
}

type ServerTypeInput struct {
	Name        string
	Description string
	Cores       int
	MemoryMB    int
	DiskGB      int
	Locations   []int64
	Deprecated  bool
}

func toServerType(t *ent.ServerType) ServerType {
	out := ServerType{ID: t.ID, Name: t.Name, Description: t.Description, Cores: t.Cores, MemoryMB: t.MemoryMB,
		DiskGB: t.DiskGB, Locations: []int64{}, Deprecated: t.Deprecated, CreatedAt: t.CreatedAt}
	for _, l := range t.Edges.Locations {
		out.Locations = append(out.Locations, l.ID)
	}
	slices.Sort(out.Locations)
	return out
}

func withLocationIDs(q *ent.LocationQuery) { q.Select(location.FieldID) }

func (s *Service) validServerType(ctx context.Context, in *ServerTypeInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	switch {
	case !nameRE.MatchString(in.Name):
		return invalid("name must match %s", nameRE)
	case len(in.Description) > 500:
		return invalid("description must be at most 500 characters")
	case in.Cores < 1 || in.Cores > 1024:
		return invalid("cores must be between 1 and 1024")
	case in.MemoryMB < 128:
		return invalid("memory_mb must be at least 128")
	case in.DiskGB < 1:
		return invalid("disk_gb must be at least 1")
	}
	slices.Sort(in.Locations)
	in.Locations = slices.Compact(in.Locations)
	if len(in.Locations) == 0 {
		return nil
	}
	n, err := s.db.Location.Query().Where(location.IDIn(in.Locations...)).Count(ctx)
	if err != nil {
		return err
	}
	if n != len(in.Locations) {
		return invalid("locations contains an unknown location id")
	}
	return nil
}

func (s *Service) CreateServerType(ctx context.Context, in ServerTypeInput) (ServerType, error) {
	if err := s.validServerType(ctx, &in); err != nil {
		return ServerType{}, err
	}
	t, err := s.db.ServerType.Create().SetName(in.Name).SetDescription(in.Description).SetCores(in.Cores).
		SetMemoryMB(in.MemoryMB).SetDiskGB(in.DiskGB).SetDeprecated(in.Deprecated).AddLocationIDs(in.Locations...).Save(ctx)
	if err != nil {
		return ServerType{}, mapWriteErr(err, "server type")
	}
	return s.GetServerType(ctx, t.ID)
}

func (s *Service) UpdateServerType(ctx context.Context, id int64, in ServerTypeInput) (ServerType, error) {
	if err := s.validServerType(ctx, &in); err != nil {
		return ServerType{}, err
	}
	err := s.db.ServerType.UpdateOneID(id).SetName(in.Name).SetDescription(in.Description).SetCores(in.Cores).
		SetMemoryMB(in.MemoryMB).SetDiskGB(in.DiskGB).SetDeprecated(in.Deprecated).
		ClearLocations().AddLocationIDs(in.Locations...).Exec(ctx)
	if err != nil {
		return ServerType{}, mapWriteErr(err, "server type")
	}
	return s.GetServerType(ctx, id)
}

func (s *Service) GetServerType(ctx context.Context, id int64) (ServerType, error) {
	t, err := s.db.ServerType.Query().Where(servertype.ID(id)).WithLocations(withLocationIDs).Only(ctx)
	if err != nil {
		return ServerType{}, notFound(err)
	}
	return toServerType(t), nil
}

// ServerTypeByRef finds a server type by numeric id or by name.
func (s *Service) ServerTypeByRef(ctx context.Context, ref string) (ServerType, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		if t, err := s.GetServerType(ctx, id); err == nil {
			return t, nil
		}
	}
	t, err := s.db.ServerType.Query().Where(servertype.Name(ref)).WithLocations(withLocationIDs).Only(ctx)
	if err != nil {
		return ServerType{}, notFound(err)
	}
	return toServerType(t), nil
}

// ListServerTypes includes deprecated types.
func (s *Service) ListServerTypes(ctx context.Context, page, perPage int) ([]ServerType, int, error) {
	total, err := s.db.ServerType.Query().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	off, lim := pageBounds(page, perPage)
	rows, err := s.db.ServerType.Query().Order(servertype.ByID()).Offset(off).Limit(lim).WithLocations(withLocationIDs).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]ServerType, len(rows))
	for i, r := range rows {
		out[i] = toServerType(r)
	}
	return out, total, nil
}

// DeleteServerType fails with resource_in_use once servers reference the type.
func (s *Service) DeleteServerType(ctx context.Context, id int64) error {
	return mapWriteErr(s.db.ServerType.DeleteOneID(id).Exec(ctx), "server type")
}
