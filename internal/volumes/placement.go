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

package volumes

import (
	"context"
	"slices"

	"github.com/nebla-cloud/nebla/internal/ent/node"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
)

// candidate is a node's resolved volume storage as Proxmox reports it now.
type candidate struct {
	Node, Storage string
	Shared        bool
	// File storages keep disks as files, which need a format extension.
	File  bool
	Avail int64
}

// candidates lists the location's usable volume storages, one per node; pin limits it to that node, enabled or not.
// pveErr is the last Proxmox error, which only matters when nothing is listed.
func (s *Service) candidates(ctx context.Context, locationID int64, pin string) (_ []candidate, pveErr, err error) {
	row, err := s.o.Ent.Location.Get(ctx, locationID)
	if err != nil {
		return nil, nil, notFound(err)
	}
	if !row.VolumeStorageOk {
		return nil, nil, nil
	}
	loc, err := s.o.Catalog.GetLocation(ctx, locationID)
	if err != nil {
		return nil, nil, err
	}
	pc, err := s.o.Catalog.ProxmoxFor(ctx, locationID)
	if err != nil {
		return nil, nil, err
	}
	var out []candidate
	for _, n := range loc.Nodes {
		if (pin != "" && n.Name != pin) || (pin == "" && (!n.Enabled || n.Status != "online")) {
			continue
		}
		want := row.VolumeStorage
		if want == "" {
			want = n.DiskStorage(loc)
		}
		list, lerr := pc.Storages(ctx, n.Name)
		if lerr != nil {
			pveErr = lerr
			continue
		}
		for _, st := range list {
			if st.ID == want && slices.Contains(st.Content, "images") {
				out = append(out, candidate{Node: n.Name, Storage: st.ID, Shared: st.Shared, File: vref.IsFileStorage(st.Type), Avail: st.Avail})
			}
		}
	}
	return out, pveErr, nil
}

// pick returns the candidate with the most free space that fits sizeGB, ties by node name.
func pick(cands []candidate, sizeGB int) (candidate, error) {
	need := int64(sizeGB) << 30
	var best *candidate
	for i := range cands {
		c := &cands[i]
		if c.Avail < need {
			continue
		}
		if best == nil || c.Avail > best.Avail || c.Avail == best.Avail && c.Node < best.Node {
			best = c
		}
	}
	if best == nil {
		return candidate{}, errNoCapacity
	}
	return *best, nil
}

// place picks the node and storage for a new volume; pin is the server's node, or "" for standalone.
// The candidate's Node is "" on shared storage.
func (s *Service) place(ctx context.Context, locationID int64, pin string, sizeGB int) (candidate, error) {
	cands, pveErr, err := s.candidates(ctx, locationID, pin)
	switch {
	case err != nil:
		return candidate{}, err
	case len(cands) == 0 && pveErr != nil:
		s.o.Logger.WarnContext(ctx, "volumes: list storages for placement", "location_id", locationID, "err", pveErr)
		return candidate{}, errUnreachable
	case len(cands) == 0:
		return candidate{}, errUnavailable
	}
	c, err := pick(cands, sizeGB)
	if err != nil {
		return candidate{}, err
	}
	if c.Shared {
		c.Node = ""
	}
	return c, nil
}

// Available reports, from the DB only, whether the location's volume storage is checked and an enabled node is online.
func (s *Service) Available(ctx context.Context, locationID int64) (bool, error) {
	row, err := s.o.Ent.Location.Get(ctx, locationID)
	if err != nil {
		return false, notFound(err)
	}
	if !row.VolumeStorageOk {
		return false, nil
	}
	return s.o.Ent.Node.Query().Where(node.LocationID(locationID), node.Enabled(true), node.Status("online")).Exist(ctx)
}
