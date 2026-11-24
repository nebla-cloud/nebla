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

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent/node"
)

// ResolveStorage is the storage snapshots on n live in: the location's snapshot storage, else n's disk storage.
func ResolveStorage(loc catalog.Location, n catalog.Node) string {
	if loc.SnapshotStorage != "" {
		return loc.SnapshotStorage
	}
	return n.DiskStorage(loc)
}

// Available reports, from the DB only, whether the location has an enabled online node.
func (s *Service) Available(ctx context.Context, locationID int64) (bool, error) {
	if _, err := s.o.Catalog.GetLocation(ctx, locationID); err != nil {
		return false, err
	}
	return s.o.Ent.Node.Query().Where(node.LocationID(locationID), node.Enabled(true), node.Status("online")).Exist(ctx)
}
