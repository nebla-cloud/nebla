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
}

// Usage is an amount of project resources, current or requested.
type Usage struct {
	Servers, VCPU, MemoryMB, DiskGB, IPs int
}

func quotaOf(p *ent.Project) Quota {
	return Quota{MaxServers: p.MaxServers, MaxVCPU: p.MaxVcpu, MaxMemoryMB: p.MaxMemoryMB, MaxDiskGB: p.MaxDiskGB, MaxIPs: p.MaxIps}
}

func (s *Service) SetQuota(ctx context.Context, projectID int64, q Quota) (Quota, error) {
	for _, v := range []int{q.MaxServers, q.MaxVCPU, q.MaxMemoryMB, q.MaxDiskGB, q.MaxIPs} {
		if v < 0 {
			return Quota{}, apierr.New(http.StatusUnprocessableEntity, "invalid_input", "quota values must be 0 or more")
		}
	}
	p, err := s.db.Project.UpdateOneID(projectID).
		SetMaxServers(q.MaxServers).SetMaxVcpu(q.MaxVCPU).SetMaxMemoryMB(q.MaxMemoryMB).
		SetMaxDiskGB(q.MaxDiskGB).SetMaxIps(q.MaxIPs).Save(ctx)
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
