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

package servers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/snapshot"
)

var (
	errSnapshotNotReady = apierr.New(http.StatusConflict, "snapshot_not_ready", "This snapshot isn't ready")
	errSnapshotMismatch = apierr.New(http.StatusUnprocessableEntity, "snapshot_server_mismatch", "This snapshot is on a different host in this location")
	errSnapshotHostFull = apierr.New(http.StatusUnprocessableEntity, "resource_unavailable", "The host that holds this snapshot is full")
)

func errSnapshotTooLarge(gb int) error {
	return apierr.New(http.StatusUnprocessableEntity, "snapshot_too_large",
		"This snapshot needs a server type with at least "+strconv.Itoa(gb)+" GB of disk")
}

// readSnapshot finds the project's available snapshot by id or name, share-locking its row when lock is set.
func readSnapshot(ctx context.Context, etx *ent.Client, projectID int64, ref string, lock bool) (*ent.Snapshot, error) {
	find := func(p ...func(*ent.SnapshotQuery)) (*ent.Snapshot, error) {
		q := etx.Snapshot.Query()
		for _, f := range p {
			f(q)
		}
		if lock {
			q.ForShare()
		}
		return q.Only(ctx)
	}
	var row *ent.Snapshot
	var err error
	if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
		row, err = find(func(q *ent.SnapshotQuery) { q.Where(snapshot.ID(id), snapshot.ProjectID(projectID)) })
	}
	if row == nil {
		row, err = find(func(q *ent.SnapshotQuery) { q.Where(snapshot.Name(ref), snapshot.ProjectID(projectID)) })
	}
	if err != nil {
		return nil, notFound(err)
	}
	if row.Status != snapshot.StatusAvailable || row.Vmid == nil {
		return nil, errSnapshotNotReady
	}
	return row, nil
}

// lockSnapshot share-locks the project's available snapshot, so a delete can't start while the server is built from it.
func lockSnapshot(ctx context.Context, etx *ent.Client, projectID int64, ref string) (*ent.Snapshot, error) {
	return readSnapshot(ctx, etx, projectID, ref, true)
}

// snapshotLocation is the location a snapshot lives in.
func (s *Service) snapshotLocation(ctx context.Context, snap *ent.Snapshot) (catalog.Location, error) {
	return s.o.Catalog.GetLocation(ctx, snap.LocationID)
}

// sourceFacts is what a new server row records about what it was built from.
type sourceFacts struct {
	imageID, snapshotID *int64
	flavor, version     string
}
