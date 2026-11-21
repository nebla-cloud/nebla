package volumes

import (
	"context"
	"slices"
)

// candidate is a node's resolved volume storage as Proxmox reports it now.
type candidate struct {
	Node, Storage string
	Shared        bool
	Avail         int64
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
				out = append(out, candidate{Node: n.Name, Storage: st.ID, Shared: st.Shared, Avail: st.Avail})
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
func (s *Service) place(ctx context.Context, locationID int64, pin string, sizeGB int) (nodeName, storage string, err error) {
	cands, pveErr, err := s.candidates(ctx, locationID, pin)
	switch {
	case err != nil:
		return "", "", err
	case len(cands) == 0 && pveErr != nil:
		return "", "", pveErr
	case len(cands) == 0:
		return "", "", errUnavailable
	}
	c, err := pick(cands, sizeGB)
	if err != nil {
		return "", "", err
	}
	if c.Shared {
		return "", c.Storage, nil
	}
	return c.Node, c.Storage, nil
}

// Available reports whether an enabled node has a resolved volume storage with images content.
func (s *Service) Available(ctx context.Context, locationID int64) (bool, error) {
	cands, pveErr, err := s.candidates(ctx, locationID, "")
	if err != nil {
		return false, err
	}
	if len(cands) == 0 && pveErr != nil && s.o.Logger != nil {
		s.o.Logger.Warn("volumes: cannot list storages", "location", locationID, "err", pveErr)
	}
	return len(cands) > 0, nil
}
