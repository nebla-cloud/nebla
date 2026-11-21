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

package reconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
)

const volumeResource = "volume"

// poolKey is one place disks are listed: a node-local storage, or a shared one (node "").
type poolKey struct{ node, storage string }

// syncVolumes moves volume rows to match the disks Proxmox lists; it never deletes a disk.
func (r *Reconciler) syncVolumes(ctx context.Context, log *slog.Logger, locationID int64, pc pve.Client, vms view) error {
	rows, err := r.o.Ent.Volume.Query().Where(volume.LocationID(locationID)).All(ctx)
	if err != nil {
		return err
	}
	pools := map[poolKey][]*ent.Volume{}
	for _, v := range rows {
		k := poolKey{v.Node, v.Storage}
		pools[k] = append(pools[k], v)
	}
	onlineNodes := slices.Sorted(maps.Keys(vms.online))
	var errs []error
	for k, vols := range pools {
		node := k.node
		if node == "" {
			node = onlineNodes[0]
		} else if !vms.online[node] {
			continue
		}
		listedAt := time.Now()
		tctx, cancel := context.WithTimeout(ctx, pveTimeout)
		items, err := pc.StorageContent(tctx, node, k.storage, vref.OwnerVMID)
		cancel()
		if err != nil {
			log.Warn("reconcile: list volume disks", "node", node, "storage", k.storage, "err", err)
			continue
		}
		disks := map[string]int64{}
		for _, it := range items {
			disks[it.Volid] = it.Size
		}
		known := map[string]bool{}
		for _, v := range vols {
			// No volid means no disk was ever recorded for the row.
			if v.Volid == "" {
				continue
			}
			known[v.Volid] = true
			size, present := disks[v.Volid]
			if err := r.applyVolume(ctx, log, v.ID, present, size, listedAt); err != nil {
				errs = append(errs, fmt.Errorf("volume %d: %w", v.ID, err))
			}
		}
		for _, it := range items {
			if !known[it.Volid] {
				log.Warn("reconcile: disk without a volume row left alone", "volid", it.Volid)
			}
		}
	}
	r.checkAttached(ctx, log, locationID, pc, rows, vms)
	return errors.Join(errs...)
}

// applyVolume re-reads the row under its lock and writes what its disk shows unless an action touches it.
func (r *Reconciler) applyVolume(ctx context.Context, log *slog.Logger, id int64, present bool, size int64, listedAt time.Time) error {
	return db.WithTx(ctx, r.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := etx.Volume.Query().Where(volume.ID(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		switch row.Status {
		case volume.StatusAvailable, volume.StatusAttached, volume.StatusMissing:
		default:
			return nil
		}
		busy, err := etx.Action.Query().Where(volumeActions(row.ProjectID, row.ID),
			action.Or(action.StatusEQ(action.StatusRunning), action.FinishedGTE(listedAt))).Exist(ctx)
		if err != nil || busy {
			return err
		}
		u := etx.Volume.UpdateOneID(row.ID)
		dirty := false
		switch {
		case !present && row.Status != volume.StatusMissing:
			log.Warn("reconcile: volume disk gone", "volume_id", row.ID)
			u.SetStatus(volume.StatusMissing)
			dirty = true
		case present && row.Status == volume.StatusMissing:
			back := volume.StatusAvailable
			if row.ServerID != nil {
				back = volume.StatusAttached
			}
			log.Info("reconcile: volume disk back", "volume_id", row.ID, "status", back)
			u.SetStatus(back)
			dirty = true
		}
		if present {
			switch gb := int(size >> 30); {
			case size > int64(row.SizeGB)<<30:
				log.Warn("reconcile: volume disk larger than its size", "volume_id", row.ID, "bytes", size, "size_gb", row.SizeGB)
			case gb != row.AppliedGB:
				u.SetAppliedGB(gb)
				dirty = true
			}
		}
		if !dirty {
			return nil
		}
		return u.Exec(ctx)
	})
}

// checkAttached warns, once per pass, about attached rows whose server VM config no longer holds their disk,
// and about Nebla volume keys the DB does not record on that server. Only servers of projects with volumes here are read.
func (r *Reconciler) checkAttached(ctx context.Context, log *slog.Logger, locationID int64, pc pve.Client, rows []*ent.Volume, vms view) {
	if len(rows) == 0 {
		return
	}
	byServer := map[int64][]*ent.Volume{}
	projects := map[int64]bool{}
	for _, v := range rows {
		projects[v.ProjectID] = true
		if v.ServerID != nil {
			byServer[*v.ServerID] = append(byServer[*v.ServerID], v)
		}
	}
	srvs, err := r.o.Ent.Server.Query().Where(server.LocationID(locationID), server.ProjectIDIn(slices.Collect(maps.Keys(projects))...)).
		Order(ent.Asc(server.FieldID)).All(ctx)
	if err != nil {
		log.Warn("reconcile: read servers of volume projects", "err", err)
		return
	}
	if len(srvs) == 0 {
		return
	}
	for _, s := range srvs {
		vm, ok := vmOf(s, vms.vms)
		if !ok {
			continue
		}
		tctx, cancel := context.WithTimeout(ctx, pveTimeout)
		cfg, err := pc.Config(tctx, vm.Node, vm.VMID)
		cancel()
		if err != nil {
			log.Warn("reconcile: read vm config", "server_id", s.ID, "err", err)
			continue
		}
		recorded := map[string]string{}
		for _, v := range byServer[s.ID] {
			recorded[v.Device] = v.Volid
			if v.Status != volume.StatusAttached {
				continue
			}
			if !vref.Matches(cfg[v.Device], v.Volid) {
				log.Warn("reconcile: attached volume not in server config", "volume_id", v.ID, "server_id", s.ID, "device", v.Device)
			}
		}
		for _, k := range vref.Keys() {
			id, ok := vref.Parse(cfg[k])
			if !ok {
				continue
			}
			if !vref.Matches(cfg[k], recorded[k]) {
				log.Warn("reconcile: volume key not recorded on its server", "volume_id", id, "server_id", s.ID, "device", k)
			}
		}
	}
}

// volumeActions matches the project's actions whose resources include the volume.
func volumeActions(projectID, volumeID int64) predicate.Action {
	doc, _ := json.Marshal([]actions.Resource{{ID: volumeID, Type: volumeResource}})
	return action.And(action.ProjectID(projectID), func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(action.FieldResources)).WriteString(" @> ").Arg(string(doc)).WriteString("::jsonb")
		}))
	})
}
