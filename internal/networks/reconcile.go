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

package networks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ent/vnetslot"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
)

// Reconcile runs one location's pass: discovery, cooling slots, then stuck attachments. Every step runs; errors are joined.
func (s *Service) Reconcile(ctx context.Context, pc pve.Client, locationID int64) error {
	var errs []error
	if err := s.Discover(ctx, pc, locationID); err != nil {
		errs = append(errs, fmt.Errorf("discover: %w", err))
	}
	if err := s.thawCooling(ctx, pc, locationID); err != nil {
		errs = append(errs, fmt.Errorf("cooling: %w", err))
	}
	if err := s.resumeStuck(ctx, locationID); err != nil {
		errs = append(errs, fmt.Errorf("resume: %w", err))
	}
	return errors.Join(errs...)
}

// thawCooling frees cooling slots whose VNet no guest config names; one listing error leaves all of them as they are.
func (s *Service) thawCooling(ctx context.Context, pc pve.Client, locationID int64) error {
	slots, err := s.o.Ent.VNetSlot.Query().Where(vnetslot.LocationID(locationID), vnetslot.StateEQ(vnetslot.StateCooling)).
		Order(vnetslot.ByID()).All(ctx)
	if err != nil || len(slots) == 0 {
		return err
	}
	guests, err := pc.Guests(ctx)
	if err != nil {
		return err
	}
	slices.SortFunc(guests, func(a, b pve.GuestRef) int { return a.VMID - b.VMID })
	used := map[string]string{}
	for _, g := range guests {
		refs, err := pc.GuestNetRefs(ctx, g)
		if err != nil {
			return err
		}
		for _, r := range refs {
			if _, ok := used[r.Bridge]; !ok {
				used[r.Bridge] = fmt.Sprintf("still used by VM %d (%s)", g.VMID, r.Where)
			}
		}
	}
	for _, sl := range slots {
		reason, busy := used[sl.Vnet]
		up := s.o.Ent.VNetSlot.Update().Where(vnetslot.ID(sl.ID), vnetslot.StateEQ(vnetslot.StateCooling))
		if busy {
			if sl.Reason == reason {
				continue
			}
			up.SetReason(reason)
		} else {
			up.SetState(vnetslot.StateFree).SetReason("")
		}
		if err := up.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// resumeStuck starts an action for each attaching or detaching row in the location that has none running.
func (s *Service) resumeStuck(ctx context.Context, locationID int64) error {
	if s.o.Jobs == nil {
		return errNoJobs
	}
	rows, err := s.o.Ent.ServerNetwork.Query().Where(servernetwork.StateIn(servernetwork.StateAttaching, servernetwork.StateDetaching),
		servernetwork.HasServerWith(server.LocationID(locationID))).WithServer().Order(servernetwork.ByID()).All(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range rows {
		err := s.withTx(ctx, func(tx *sql.Tx, etx *ent.Client) error {
			projectID := r.Edges.Server.ProjectID
			if err := projects.LockProject(ctx, etx, projectID); err != nil {
				return notFound(err)
			}
			if _, err := lockNetworks(ctx, etx, projectID, r.NetworkID); err != nil {
				return err
			}
			if _, err := lockServer(ctx, etx, projectID, r.ServerID); err != nil {
				return err
			}
			row, err := etx.ServerNetwork.Get(ctx, r.ID)
			if err != nil {
				return notFound(err)
			}
			res := attachResources(row.ServerID, row.NetworkID)
			switch row.State {
			case servernetwork.StateAttaching:
				_, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindAttach, res, func(id int64) river.JobArgs {
					return AttachArgs{ActionID: id, ServerID: row.ServerID, NetworkID: row.NetworkID}
				}, nil)
			case servernetwork.StateDetaching:
				_, err = s.o.Jobs.StartAction(ctx, tx, etx, projectID, KindDetach, res, func(id int64) river.JobArgs {
					return DetachArgs{ActionID: id, ServerID: row.ServerID, NetworkID: row.NetworkID}
				}, nil)
			}
			return err
		})
		if errors.Is(err, errLocked) || errors.Is(err, errBusy) || errors.Is(err, errNotFound) || errors.Is(err, errServerNotFound) {
			continue
		}
		if err != nil {
			s.o.Logger.Error("resume network attachment", "server_network", r.ID, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
