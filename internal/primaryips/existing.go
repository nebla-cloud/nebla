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

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/ipaddress"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/ipam"
)

// LockForCreate locks the project's primary IP id and checks a new server in location can take it as its family address.
func LockForCreate(ctx context.Context, etx *ent.Client, projectID, id, locationID int64, family string) (*ent.IPAddress, error) {
	row, err := etx.IPAddress.Query().Where(ipaddress.ID(id), ipaddress.ProjectID(projectID), primary).ForUpdate().Only(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	switch {
	case row.Family != family:
		return nil, errWrongFamily(family)
	case row.LocationID != locationID:
		return nil, errLocation
	case row.ReleasingFromID != nil || row.Deleting:
		return nil, errReleasing
	case row.ServerID != nil:
		cur, err := etx.Server.Get(ctx, *row.ServerID)
		if err != nil {
			return nil, err
		}
		return nil, errInUseOn(cur.Name)
	}
	busy, err := etx.Action.Query().Where(floatingips.ActionsOfType(projectID, id, ResourceType), action.StatusEQ(action.StatusRunning)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, errBusy
	}
	return row, nil
}

// ErrSegment is the 422 for two IPs on different networks.
var ErrSegment = errSegment

// SegmentOf returns the network of a primary IP's allocation.
func SegmentOf(ctx context.Context, etx *ent.Client, allocationID int64) (ipam.Segment, error) {
	return segmentOf(ctx, etx, allocationID)
}
