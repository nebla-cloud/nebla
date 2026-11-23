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

package projects

import (
	"context"
	"fmt"
	"net/http"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/project"
)

// Quota limits a project's resources; 0 means unlimited.
type Quota struct {
	MaxServers, MaxVCPU, MaxMemoryMB, MaxDiskGB, MaxIPs int
	MaxVolumes, MaxVolumeGB                             int
	MaxBackups                                          int
}

// Usage is an amount of project resources, current or requested.
type Usage struct {
	Servers, VCPU, MemoryMB, DiskGB, IPs int
	Volumes, VolumeGB                    int
	Backups, BackupsReserved             int
}

func quotaOf(p *ent.Project) Quota {
	return Quota{MaxServers: p.MaxServers, MaxVCPU: p.MaxVcpu, MaxMemoryMB: p.MaxMemoryMB, MaxDiskGB: p.MaxDiskGB, MaxIPs: p.MaxIps,
		MaxVolumes: p.MaxVolumes, MaxVolumeGB: p.MaxVolumeGB, MaxBackups: p.MaxBackups}
}

func (s *Service) SetQuota(ctx context.Context, projectID int64, q Quota) (Quota, error) {
	return s.UpdateQuota(ctx, projectID, q, &q.MaxVolumes, &q.MaxVolumeGB, &q.MaxBackups)
}

// UpdateQuota sets the server quota fields from q and each volume and backup field that is not nil; q's own are ignored.
func (s *Service) UpdateQuota(ctx context.Context, projectID int64, q Quota, maxVolumes, maxVolumeGB, maxBackups *int) (Quota, error) {
	for _, v := range []*int{&q.MaxServers, &q.MaxVCPU, &q.MaxMemoryMB, &q.MaxDiskGB, &q.MaxIPs, maxVolumes, maxVolumeGB, maxBackups} {
		if v != nil && *v < 0 {
			return Quota{}, apierr.New(http.StatusUnprocessableEntity, "invalid_input", "quota values must be 0 or more")
		}
	}
	p, err := s.db.Project.UpdateOneID(projectID).
		SetMaxServers(q.MaxServers).SetMaxVcpu(q.MaxVCPU).SetMaxMemoryMB(q.MaxMemoryMB).
		SetMaxDiskGB(q.MaxDiskGB).SetMaxIps(q.MaxIPs).
		SetNillableMaxVolumes(maxVolumes).SetNillableMaxVolumeGB(maxVolumeGB).SetNillableMaxBackups(maxBackups).Save(ctx)
	if err != nil {
		return Quota{}, notFound(err)
	}
	return quotaOf(p), nil
}

// LockProject locks the project row in tx; take it before any server or volume row.
func LockProject(ctx context.Context, tx *ent.Client, projectID int64) error {
	_, err := lockProject(ctx, tx, projectID)
	return err
}

// lockProject takes FOR NO KEY UPDATE so inserts referencing the project (actions, volumes) are not blocked.
func lockProject(ctx context.Context, tx *ent.Client, projectID int64) (*ent.Project, error) {
	p, err := tx.Project.Query().Where(project.ID(projectID)).
		Modify(func(s *entsql.Selector) { s.For(entsql.LockNoKeyUpdate) }).Only(ctx)
	return p, notFound(err)
}

// ReserveQuota locks the project row in tx and fails with quota_exceeded if usage plus delta passes a limit.
func (s *Service) ReserveQuota(ctx context.Context, tx *ent.Client, projectID int64, delta Usage) error {
	p, err := lockProject(ctx, tx, projectID)
	if err != nil {
		return err
	}
	cur, err := s.opts.Usage(ctx, tx, projectID)
	if err != nil {
		return err
	}
	q := quotaOf(p)
	checks := []struct {
		name              string
		limit, add, total int
	}{
		{"max_servers", q.MaxServers, delta.Servers, cur.Servers + delta.Servers},
		{"max_vcpu", q.MaxVCPU, delta.VCPU, cur.VCPU + delta.VCPU},
		{"max_memory_mb", q.MaxMemoryMB, delta.MemoryMB, cur.MemoryMB + delta.MemoryMB},
		{"max_disk_gb", q.MaxDiskGB, delta.DiskGB, cur.DiskGB + delta.DiskGB},
		{"max_ips", q.MaxIPs, delta.IPs, cur.IPs + delta.IPs},
		{"max_volumes", q.MaxVolumes, delta.Volumes, cur.Volumes + delta.Volumes},
		{"max_volume_gb", q.MaxVolumeGB, delta.VolumeGB, cur.VolumeGB + delta.VolumeGB},
		{"max_backups", q.MaxBackups, delta.BackupsReserved, cur.BackupsReserved + delta.BackupsReserved},
	}
	// Only growing dimensions are checked, so shrinking an over-quota project still works.
	for _, c := range checks {
		if c.limit > 0 && c.add > 0 && c.total > c.limit {
			return apierr.New(http.StatusUnprocessableEntity, "quota_exceeded",
				fmt.Sprintf("quota exceeded: %s is %d, this would use %d", c.name, c.limit, c.total))
		}
	}
	return nil
}
