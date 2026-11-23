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
	"slices"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// ResolveStorage is the storage snapshots on n live in: the location's snapshot storage, else n's disk storage.
func ResolveStorage(loc catalog.Location, n catalog.Node) string {
	if loc.SnapshotStorage != "" {
		return loc.SnapshotStorage
	}
	return n.DiskStorage(loc)
}

// Available reports whether an enabled online node's snapshot storage holds images, as Proxmox says now.
// An unreachable location counts as unavailable.
func (s *Service) Available(ctx context.Context, locationID int64) (bool, error) {
	loc, err := s.o.Catalog.GetLocation(ctx, locationID)
	if err != nil {
		return false, err
	}
	pc, err := s.o.Catalog.ProxmoxFor(ctx, locationID)
	if err != nil {
		s.o.Logger.WarnContext(ctx, "snapshots: connect for availability", "location_id", locationID, "err", err)
		return false, nil
	}
	for _, n := range loc.Nodes {
		if !n.Enabled || n.Status != "online" {
			continue
		}
		list, err := pc.Storages(ctx, n.Name)
		if err != nil {
			s.o.Logger.WarnContext(ctx, "snapshots: list storages for availability", "location_id", locationID, "node", n.Name, "err", err)
			continue
		}
		want := ResolveStorage(loc, n)
		if slices.ContainsFunc(list, func(st pve.Storage) bool { return st.ID == want && slices.Contains(st.Content, "images") }) {
			return true, nil
		}
	}
	return false, nil
}
