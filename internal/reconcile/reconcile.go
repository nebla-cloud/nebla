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

// Package reconcile keeps server rows in line with what Proxmox reports.
package reconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/location"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/snapshots"
	"github.com/nebla-cloud/nebla/internal/volumes"
)

// ErrProxmoxState means Proxmox answered with a state too implausible to act on.
var ErrProxmoxState = errors.New("reconcile: implausible proxmox state")

const pveTimeout = 30 * time.Second

// networkPoolTimeout bounds the pool pass, which reads every guest config on the cluster.
const networkPoolTimeout = 5 * time.Minute

type Options struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// Jobs starts drift repair applies; without it drift is not repaired.
	Jobs   *jobs.Jobs
	Logger *slog.Logger
	// VolumeRescue repairs orphaned volume actions; it runs at the end of every pass.
	VolumeRescue func(ctx context.Context) error
	// FloatingIPResume restarts floating and primary IP changes left without an action; it runs once per pass.
	FloatingIPResume func(ctx context.Context) error
	// NetworkPool discovers private network slots, thaws cooling ones and resumes stuck attachments for one location; errors are logged.
	NetworkPool func(ctx context.Context, pc pve.Client, locationID int64) error
}

type Reconciler struct {
	o       Options
	jobs    atomic.Pointer[jobs.Jobs]
	noJobs  sync.Once
	mu      sync.Mutex
	orphans map[[2]int64]bool
	// driftAt is each location's last firewall drift check.
	driftAt    map[int64]time.Time
	driftEvery time.Duration
	nicWarned  map[int64]time.Time
	// afterList runs once the VM list is read; tests use it to race a job.
	afterList func()
	// rescueHook runs inside each rescue transaction; an error fails it.
	rescueHook func(actionID int64) error
}

func New(o Options) *Reconciler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	r := &Reconciler{o: o, orphans: map[[2]int64]bool{}, driftAt: map[int64]time.Time{}, driftEvery: DriftInterval,
		nicWarned: map[int64]time.Time{}}
	r.jobs.Store(o.Jobs)
	return r
}

// SetJobs wires the job runtime, which is built after the workers are registered.
func (r *Reconciler) SetJobs(j *jobs.Jobs) { r.jobs.Store(j) }

// Run reconciles one location: stuck actions from the DB alone, then node load, server status and node.
func (r *Reconciler) Run(ctx context.Context, locationID int64) error {
	log := r.o.Logger.With("location_id", locationID)
	reverts, errs, err := r.rescue(ctx, log, locationID)
	if err != nil {
		errs = append(errs, err)
	}
	if err := r.sync(ctx, log, locationID, reverts); err != nil {
		errs = append(errs, err)
	}
	if r.o.VolumeRescue != nil {
		if err := r.o.VolumeRescue(ctx); err != nil {
			log.Error("reconcile: volume rescue", "err", err)
		}
	}
	return errors.Join(errs...)
}

// ResumeFloatingIPs runs FloatingIPResume; the fan-out calls it once per pass.
func (r *Reconciler) ResumeFloatingIPs(ctx context.Context) {
	if r.o.FloatingIPResume == nil {
		return
	}
	if err := r.o.FloatingIPResume(ctx); err != nil {
		r.o.Logger.Error("reconcile: floating IP resume", "err", err)
	}
}

// RescueAll fails orphaned server actions in every location without Proxmox; failed rows are logged and left to Run.
func (r *Reconciler) RescueAll(ctx context.Context) error {
	ids, err := r.o.Ent.Location.Query().IDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		log := r.o.Logger.With("location_id", id)
		reverts, _, err := r.rescue(ctx, log, id)
		if err != nil {
			return err
		}
		unrestored(log, reverts)
	}
	return nil
}

func unrestored(log *slog.Logger, reverts []revert) {
	for _, rv := range reverts {
		log.Warn("reconcile: vm config not restored, proxmox not read", "server_id", rv.row.ID)
	}
}

// sync reads Proxmox and moves server rows to match it.
func (r *Reconciler) sync(ctx context.Context, log *slog.Logger, locationID int64, reverts []revert) error {
	restored := false
	defer func() {
		if !restored {
			unrestored(log, reverts)
		}
	}()
	// SyncNodes refreshes node load and fails when Proxmox is unreachable or lists no nodes.
	loc, err := r.o.Catalog.SyncNodes(ctx, locationID)
	if err != nil {
		log.Warn("reconcile: sync nodes", "err", err)
		return fmt.Errorf("sync nodes: %w", err)
	}
	online := map[string]bool{}
	for _, n := range loc.Nodes {
		if n.Status == nodeOnline {
			online[n.Name] = true
		}
	}
	if len(online) == 0 {
		log.Warn("reconcile: no node online, skipping")
		return fmt.Errorf("%w: no node online", ErrProxmoxState)
	}
	pc, err := r.o.Catalog.ProxmoxFor(ctx, locationID)
	if err != nil {
		return err
	}
	// Actions finishing after this saw a newer Proxmox state than the list.
	listedAt := time.Now()
	tctx, cancel := context.WithTimeout(ctx, pveTimeout)
	all, err := pc.VMs(tctx)
	cancel()
	if err != nil {
		log.Warn("reconcile: list vms", "err", err)
		return fmt.Errorf("list vms: %w", err)
	}
	if r.afterList != nil {
		r.afterList()
	}
	rows, err := r.o.Ent.Server.Query().Where(server.LocationID(locationID)).All(ctx)
	if err != nil {
		return err
	}
	if len(all) == 0 && slices.ContainsFunc(rows, func(s *ent.Server) bool { return s.Vmid != nil }) {
		log.Warn("reconcile: proxmox lists no VMs while servers have VMs, skipping")
		return fmt.Errorf("%w: no VMs listed", ErrProxmoxState)
	}
	vms := view{vms: tagged(all), online: online}
	r.restore(ctx, log, pc, reverts, vms)
	restored = true
	if err := tooManyMissing(rows, vms); err != nil {
		log.Error("reconcile: refusing to mark servers missing", "err", err)
		return err
	}
	r.logOrphans(log, locationID, vms.vms, rows)
	var errs []error
	for _, row := range rows {
		if _, _, ok := desired(row, vms); !ok {
			continue
		}
		if err := r.apply(ctx, log, row.ID, vms, listedAt); err != nil {
			errs = append(errs, fmt.Errorf("server %d: %w", row.ID, err))
		}
	}
	if err := r.syncVolumes(ctx, log, locationID, pc, vms); err != nil {
		errs = append(errs, err)
	}
	if err := r.syncBackups(ctx, log, locationID, pc, vms); err != nil {
		errs = append(errs, err)
	}
	if err := r.queueISOSyncs(ctx, rows, vms); err != nil {
		log.Warn("reconcile: seed iso sync", "err", err)
		errs = append(errs, fmt.Errorf("seed iso sync: %w", err))
	}
	fwOn, err := r.recordFirewall(ctx, pc, locationID)
	if err != nil {
		log.Warn("reconcile: datacenter firewall", "err", err)
		errs = append(errs, fmt.Errorf("datacenter firewall: %w", err))
	}
	if r.o.NetworkPool != nil {
		nctx, cancel := context.WithTimeout(ctx, networkPoolTimeout)
		err := r.o.NetworkPool(nctx, pc, locationID)
		cancel()
		if err != nil {
			log.Error("reconcile: network pool", "err", err)
		}
	}
	if !r.driftDue(locationID) {
		return errors.Join(errs...)
	}
	atts := map[int64]firewalls.Attachments{}
	for _, row := range rows {
		if err := r.repairFirewall(ctx, log, pc, row, vms, fwOn, atts); err != nil {
			log.Warn("reconcile: firewall drift", "server_id", row.ID, "err", err)
			errs = append(errs, fmt.Errorf("server %d firewall: %w", row.ID, err))
		}
	}
	return errors.Join(errs...)
}

// recordFirewall stores the datacenter firewall switch on the location; nil when it could not be read.
func (r *Reconciler) recordFirewall(ctx context.Context, pc pve.Client, locationID int64) (*bool, error) {
	tctx, cancel := context.WithTimeout(ctx, pveTimeout)
	on, err := pc.ClusterFirewallEnabled(tctx)
	cancel()
	if err != nil {
		return nil, err
	}
	err = r.o.Ent.Location.Update().Where(location.ID(locationID),
		location.Or(location.FirewallEnabledIsNil(), location.FirewallEnabledNEQ(on))).SetFirewallEnabled(on).Exec(ctx)
	return &on, err
}

// Cooldowns after a server's last apply before the reconciler queues another one.
const (
	repairCooldownError   = 10 * time.Minute
	repairCooldownSuccess = 5 * time.Minute
)

// DriftInterval is how often a location's VM firewalls are compared with the desired state.
const DriftInterval = 5 * time.Minute

// nicWarnCooldown spaces out repeated warnings about one server's NIC firewall flag.
const nicWarnCooldown = time.Hour

// driftDue reports whether the location's drift check is due, and if so marks it run.
func (r *Reconciler) driftDue(locationID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if last, ok := r.driftAt[locationID]; ok && time.Since(last) < r.driftEvery {
		return false
	}
	r.driftAt[locationID] = time.Now()
	return true
}

// nicWarnDue reports whether server id's NIC flag warning is due, and if so marks it logged.
func (r *Reconciler) nicWarnDue(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	maps.DeleteFunc(r.nicWarned, func(_ int64, at time.Time) bool { return time.Since(at) >= nicWarnCooldown })
	if _, ok := r.nicWarned[id]; ok {
		return false
	}
	r.nicWarned[id] = time.Now()
	return true
}

// queueISOSyncs queues a seed ISO sync for listed servers whose ISO is behind their IPs and that run no action.
func (r *Reconciler) queueISOSyncs(ctx context.Context, rows []*ent.Server, w view) error {
	j := r.jobs.Load()
	if j == nil {
		return nil
	}
	var due []int64
	for _, row := range rows {
		if _, ok := vmOf(row, w.vms); !ok {
			continue
		}
		stale, err := servers.ISOSyncDue(ctx, r.o.Ent, row)
		if err != nil {
			return err
		}
		if !stale {
			continue
		}
		busy, err := r.o.Ent.Action.Query().Where(servers.ActionsOf(row.ProjectID, row.ID), action.StatusEQ(action.StatusRunning)).Exist(ctx)
		if err != nil {
			return err
		}
		if !busy {
			due = append(due, row.ID)
		}
	}
	return servers.QueueISOSync(ctx, r.o.SQL, j, due...)
}

// fwRepairable reports a server whose VM firewall the reconciler may compare and repair.
func fwRepairable(row *ent.Server) bool {
	return row.Vmid != nil && (row.Status == server.StatusRunning || row.Status == server.StatusOff) && !servers.Building(row)
}

func applyPending(ctx context.Context, c *ent.Client, row *ent.Server) (bool, error) {
	return c.Action.Query().Where(servers.ActionsOf(row.ProjectID, row.ID), action.CommandEQ(firewalls.KindApply),
		action.StatusEQ(action.StatusRunning)).Exist(ctx)
}

// cooling reports whether the server's newest apply finished within its cooldown, and whether it succeeded.
func cooling(ctx context.Context, c *ent.Client, row *ent.Server) (cool, success bool, err error) {
	last, err := c.Action.Query().Where(servers.ActionsOf(row.ProjectID, row.ID), action.CommandEQ(firewalls.KindApply)).
		Order(action.ByID(entsql.OrderDesc())).First(ctx)
	if ent.IsNotFound(err) || err == nil && last.Finished == nil {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	success = last.Status == action.StatusSuccess
	wait := repairCooldownError
	if success {
		wait = repairCooldownSuccess
	}
	return time.Since(*last.Finished) < wait, success, nil
}

// ipsetDrift describes how the live ipfilter set differs from the server's primary and floating IPs; "" when it matches.
func ipsetDrift(cidrs []string, exists bool, assign []ipam.Assignment, floating []netip.Addr) string {
	if !exists {
		return "ipset " + pve.IPFilterSet + " is missing"
	}
	have := map[netip.Addr]bool{}
	for _, c := range cidrs {
		if a, err := netip.ParseAddr(c); err == nil {
			have[a] = true
		} else if p, err := netip.ParsePrefix(c); err == nil && p.IsSingleIP() {
			have[p.Addr()] = true
		} else {
			return "ipset " + pve.IPFilterSet + " has unexpected entry " + c
		}
	}
	want := map[netip.Addr]bool{}
	ips := make([]netip.Addr, 0, len(assign)+len(floating))
	for _, a := range assign {
		ips = append(ips, a.IP)
	}
	for _, a := range append(ips, floating...) {
		want[a] = true
		if !have[a] {
			return "ipset " + pve.IPFilterSet + " lacks " + a.String()
		}
	}
	for a := range have {
		if !want[a] {
			return "ipset " + pve.IPFilterSet + " has unexpected entry " + a.String()
		}
	}
	return ""
}

// privateSetDrift describes how ipfilter-net1..3 differ from the server's private NICs; "" when they match.
func (r *Reconciler) privateSetDrift(ctx context.Context, pc pve.Client, v pve.VM, serverID int64) (string, error) {
	nics, err := networks.PrivateNICs(ctx, r.o.Ent, serverID)
	if err != nil {
		return "", err
	}
	want := map[int]netip.Addr{}
	for _, n := range nics {
		want[n.Index] = n.IP
	}
	for i := 1; i <= networks.MaxNICs; i++ {
		name := pve.IPFilterSetFor(i)
		cidrs, exists, err := pc.IPSet(ctx, v.Node, v.VMID, name)
		if err != nil {
			return "", fmt.Errorf("read ipset %s: %w", name, err)
		}
		ip, ok := want[i]
		switch {
		case !ok && exists:
			return "ipset " + name + " should not exist", nil
		case ok && !exists:
			return "ipset " + name + " is missing", nil
		case ok && !onlyAddr(cidrs, ip):
			return "ipset " + name + " is " + strings.Join(cidrs, ",") + ", want " + ip.String(), nil
		}
	}
	return "", nil
}

// onlyAddr reports whether an ipset's entries are exactly ip, bare or as a /32.
func onlyAddr(cidrs []string, ip netip.Addr) bool {
	if len(cidrs) != 1 {
		return false
	}
	if a, err := netip.ParseAddr(cidrs[0]); err == nil {
		return a == ip
	}
	p, err := netip.ParsePrefix(cidrs[0])
	return err == nil && p.IsSingleIP() && p.Addr() == ip
}

// nicFirewall reports whether a net0 config value has the firewall flag on.
func nicFirewall(net0 string) bool {
	return slices.Contains(strings.Split(net0, ","), "firewall=1")
}

// repairFirewall queues an apply when the VM's firewall or anti-spoofing differs from the desired state.
func (r *Reconciler) repairFirewall(ctx context.Context, log *slog.Logger, pc pve.Client, row *ent.Server, w view, fwOn *bool,
	atts map[int64]firewalls.Attachments) error {
	j := r.jobs.Load()
	if j == nil {
		r.noJobs.Do(func() { log.Warn("reconcile: no job runtime, firewall drift repair is disabled") })
		return nil
	}
	if !fwRepairable(row) {
		return nil
	}
	v, ok := vmOf(row, w.vms)
	if !ok || !w.online[v.Node] {
		return nil
	}
	if pending, err := applyPending(ctx, r.o.Ent, row); err != nil || pending {
		return err
	}
	cool, lastOK, err := cooling(ctx, r.o.Ent, row)
	if err != nil || cool && !lastOK {
		return err
	}
	att, ok := atts[row.ProjectID]
	if !ok {
		if att, err = firewalls.LoadAttachments(ctx, r.o.Ent, row.ProjectID); err != nil {
			return err
		}
		atts[row.ProjectID] = att
	}
	rules, anyFW, err := att.Rules(ctx, r.o.Ent, row.ID, row.Labels)
	if err != nil {
		return err
	}
	// An apply could only fail with the datacenter firewall off.
	if anyFW && fwOn != nil && !*fwOn {
		return nil
	}
	assign, err := ipam.Assignments(ctx, r.o.Ent, row.ID)
	if err != nil {
		return err
	}
	fips, err := floatingips.AllowedIPs(ctx, r.o.Ent, row.ID)
	if err != nil {
		return err
	}
	tctx, cancel := context.WithTimeout(ctx, pveTimeout)
	defer cancel()
	cfg, err := pc.Config(tctx, v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("read vm config: %w", err)
	}
	if !nicFirewall(cfg["net0"]) && r.nicWarnDue(row.ID) {
		log.Warn("reconcile: server public NIC firewall flag is off", "server_id", row.ID, "vmid", v.VMID)
	}
	cur, err := pc.FirewallRules(tctx, v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("read firewall rules: %w", err)
	}
	opts, err := pc.FirewallOptions(tctx, v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("read firewall options: %w", err)
	}
	cidrs, exists, err := pc.FirewallIPSet(tctx, v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("read firewall ipset: %w", err)
	}
	sys, err := networks.SystemFor(tctx, r.o.Ent, row.ID)
	if err != nil {
		return err
	}
	drift := firewalls.Drift(cur, opts, firewalls.Render(rules, anyFW, sys))
	if drift == "" {
		drift = ipsetDrift(cidrs, exists, assign, fips)
	}
	if drift == "" {
		if drift, err = r.privateSetDrift(tctx, pc, v, row.ID); err != nil {
			return err
		}
	}
	if drift == "" {
		return nil
	}
	if cool {
		log.Warn("reconcile: firewall still differs after a repair", "server_id", row.ID, "vmid", v.VMID, "diff", drift)
		return nil
	}
	return db.WithTx(ctx, r.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, row.ProjectID); err != nil {
			return err
		}
		q := etx.Server.Query().Where(server.ID(row.ID))
		q.Modify(func(s *entsql.Selector) { s.For(entsql.LockNoKeyUpdate) })
		now, err := q.Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !fwRepairable(now) || *now.Vmid != v.VMID {
			return nil
		}
		if pending, err := applyPending(ctx, etx, now); err != nil || pending {
			return err
		}
		log.Info("reconcile: firewall drift, queueing apply", "server_id", row.ID, "vmid", v.VMID, "diff", drift)
		_, err = firewalls.Bump(ctx, tx, etx, j, now.ProjectID, []int64{now.ID}, nil)
		return err
	})
}

const nodeOnline = "online"

// view is one run's Proxmox state: tagged VMs by vmid and the online nodes.
type view struct {
	vms    map[int]pve.VM
	online map[string]bool
}

// observe is the status and node Proxmox shows for row; known is false when Proxmox cannot tell.
func (w view) observe(row *ent.Server) (st server.Status, node string, known bool) {
	v, found := vmOf(row, w.vms)
	if !found {
		// A VM on a node that is not online may just be hidden.
		return server.StatusMissing, row.Node, w.online[row.Node]
	}
	st, known = mapStatus(v.Status)
	return st, v.Node, known
}

// Breaker limits: more than half of the servers with VMs, and at least minBreaker, turning missing at once.
const minBreaker = 3

// tooManyMissing refuses a run that would mark most servers missing; that looks like Proxmox, not deletions.
func tooManyMissing(rows []*ent.Server, w view) error {
	withVM, missing := 0, 0
	for _, row := range rows {
		if row.Vmid == nil {
			continue
		}
		withVM++
		if st, _, ok := desired(row, w); ok && st == server.StatusMissing && row.Status != server.StatusMissing {
			missing++
		}
	}
	if missing >= minBreaker && missing*2 > withVM {
		return fmt.Errorf("%w: %d of %d servers would turn missing", ErrProxmoxState, missing, withVM)
	}
	return nil
}

// tagged keeps the non-template VMs carrying the nebla tag, by vmid.
func tagged(all []pve.VM) map[int]pve.VM {
	out := map[int]pve.VM{}
	for _, v := range all {
		if !v.Template && slices.Contains(servers.SplitTags(v.Tags), servers.TagNebla) {
			out[v.VMID] = v
		}
	}
	return out
}

// vmOf is the server's VM: listed at its vmid and tagged for it.
func vmOf(row *ent.Server, vms map[int]pve.VM) (pve.VM, bool) {
	if row.Vmid == nil {
		return pve.VM{}, false
	}
	v, ok := vms[*row.Vmid]
	return v, ok && slices.Contains(servers.SplitTags(v.Tags), servers.ServerTag(row.ID))
}

// logOrphans logs, once per vmid, tagged VMs that are neither a server's vmid nor its rebuild_vmid.
func (r *Reconciler) logOrphans(log *slog.Logger, locationID int64, vms map[int]pve.VM, rows []*ent.Server) {
	known := map[int]int64{}
	for _, s := range rows {
		if s.Vmid != nil {
			known[*s.Vmid] = s.ID
		}
		if s.RebuildVmid != nil {
			known[*s.RebuildVmid] = s.ID
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for vmid, v := range vms {
		if id, ok := known[vmid]; ok && slices.Contains(servers.SplitTags(v.Tags), servers.ServerTag(id)) {
			continue
		}
		key := [2]int64{locationID, int64(vmid)}
		if r.orphans[key] {
			continue
		}
		r.orphans[key] = true
		log.Warn("reconcile: orphan nebla VM left alone", "vmid", vmid, "node", v.Node, "tags", v.Tags)
	}
}

// steady lists the statuses the reconciler may change; the rest belong to running jobs.
var steady = []server.Status{server.StatusRunning, server.StatusOff, server.StatusMissing, server.StatusError}

// desired is the status and node Proxmox implies for row; ok is false when nothing should change.
func desired(row *ent.Server, w view) (server.Status, string, bool) {
	if row.Vmid == nil || !slices.Contains(steady, row.Status) {
		return "", "", false
	}
	seen, node, known := w.observe(row)
	if !known {
		return "", "", false
	}
	st := seen
	if row.Status == server.StatusError {
		// error stays: a failed create may already have released the IPs.
		st = server.StatusError
	}
	return st, node, st != row.Status || node != row.Node
}

// mapStatus maps a Proxmox VM status; paused and suspended VMs still hold their resources, so they count as running.
func mapStatus(s string) (server.Status, bool) {
	switch s {
	case "running", "paused", "suspended":
		return server.StatusRunning, true
	case "stopped":
		return server.StatusOff, true
	}
	return "", false
}

// apply re-reads the row under its lock and writes the change unless an action ran since the list was read.
func (r *Reconciler) apply(ctx context.Context, log *slog.Logger, id int64, vms view, listedAt time.Time) error {
	return db.WithTx(ctx, r.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := etx.Server.Query().Where(server.ID(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		// Firewall applies never touch status or node, so they don't hold the sync off.
		busy, err := etx.Action.Query().Where(servers.ActionsOf(row.ProjectID, row.ID), action.CommandNEQ(firewalls.KindApply),
			action.Or(action.StatusEQ(action.StatusRunning), action.FinishedGTE(listedAt))).Exist(ctx)
		if err != nil || busy {
			return err
		}
		st, node, ok := desired(row, vms)
		if !ok {
			return nil
		}
		log.Info("reconcile: server changed in proxmox", "server_id", row.ID, "status", st, "was", row.Status,
			"node", node, "was_node", row.Node)
		return etx.Server.UpdateOneID(row.ID).SetStatus(st).SetNode(node).Exec(ctx)
	})
}

// revert is a change_type rescued in the DB whose VM config and disk size still need Proxmox.
type revert struct {
	row  *ent.Server
	args servers.ChangeTypeArgs
}

// rescue fails orphaned actions on this location's servers from the DB alone; err is set only if they cannot be listed.
// Volume and snapshot commands are skipped: volumes.RescueOrphaned and snapshots.RescueOrphaned settle them and their rows.
func (r *Reconciler) rescue(ctx context.Context, log *slog.Logger, locationID int64) (reverts []revert, rowErrs []error, err error) {
	q, err := r.o.SQL.QueryContext(ctx, `
		select a.id, s.id from actions a join servers s
		on s.location_id = $1 and a.resources @> jsonb_build_array(jsonb_build_object('id', s.id, 'type', $2::text))
		where a.command <> all($3) and `+jobs.Orphaned+` order by a.id`, locationID, servers.ResourceType, slices.Concat(volumes.Commands, snapshots.Commands))
	if err != nil {
		return nil, nil, fmt.Errorf("find orphaned actions: %w", err)
	}
	type stuck struct{ action, server int64 }
	var found []stuck
	for q.Next() {
		var s stuck
		if err := q.Scan(&s.action, &s.server); err != nil {
			q.Close()
			return nil, nil, err
		}
		found = append(found, s)
	}
	if err := errors.Join(q.Err(), q.Close()); err != nil {
		return nil, nil, err
	}
	for _, s := range found {
		rv, err := r.rescueOne(ctx, log, s.action, s.server)
		if err != nil {
			log.Error("reconcile: rescue action", "action_id", s.action, "err", err)
			rowErrs = append(rowErrs, fmt.Errorf("rescue action %d: %w", s.action, err))
			continue
		}
		if rv != nil {
			reverts = append(reverts, *rv)
		}
	}
	return reverts, rowErrs, nil
}

func (r *Reconciler) rescueOne(ctx context.Context, log *slog.Logger, actionID, serverID int64) (*revert, error) {
	var rv *revert
	err := db.WithTx(ctx, r.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		row, err := etx.Server.Query().Where(server.ID(serverID)).ForUpdate().Only(ctx)
		if err != nil {
			return err
		}
		var command string
		var args []byte
		err = tx.QueryRowContext(ctx, `
			update actions a set status = 'error', error_code = $2, error_message = $3, finished = now()
			where a.id = $1 and `+jobs.Orphaned+`
			returning a.command, (select j.args from river_job j where j.id = a.river_job_id)`,
			actionID, jobs.OrphanedCode, jobs.OrphanedMessage).Scan(&command, &args)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		log.Warn("reconcile: failed orphaned action", "action_id", actionID, "command", command, "server_id", serverID)
		if command == firewalls.KindApply {
			// Applies never change the server's status.
			return nil
		}
		u, dirty := etx.Server.UpdateOneID(serverID), false
		if r.rescueHook != nil {
			if err := r.rescueHook(actionID); err != nil {
				return err
			}
		}
		if command == servers.KindChangeType {
			if args == nil {
				log.Error("reconcile: change type job is gone; snapshot not restored", "server_id", serverID)
			} else {
				var a servers.ChangeTypeArgs
				if err := json.Unmarshal(args, &a); err != nil {
					return err
				}
				rv, dirty = &revert{row: row, args: a}, true
				back := a.Restored(nil)
				u.SetServerTypeID(back.TypeID).SetCores(back.Cores).SetMemoryMB(back.MemoryMB).SetDiskGB(back.DiskGB)
			}
		}
		switch row.Status {
		case server.StatusStarting, server.StatusStopping:
			// A lost power job: the status before it ran; the sync that follows corrects it from Proxmox.
			u.SetStatus(powerFrom(args, row.Status))
			dirty = true
		case server.StatusInitializing, server.StatusRebuilding, server.StatusDeleting:
			// The job's own cleanup never ran; DELETE is the way out.
			u.SetStatus(server.StatusError)
			dirty = true
		}
		if command == servers.KindRebuildServer {
			// No rebuild resumes, so a later power-on must not look like one, and the snapshot is free again.
			u.SetRebuildStep(0).ClearRebuildSnapshotID()
			dirty = true
		}
		if !dirty {
			return nil
		}
		return u.Exec(ctx)
	})
	if err != nil {
		return nil, err
	}
	return rv, nil
}

// restore puts the old cores and memory back on rescued change_type VMs and the row's disk at its real size, best effort.
func (r *Reconciler) restore(ctx context.Context, log *slog.Logger, pc pve.Client, reverts []revert, vms view) {
	for _, rv := range reverts {
		v, ok := vmOf(rv.row, vms.vms)
		if !ok {
			log.Warn("reconcile: vm config not restored, vm not listed", "server_id", rv.row.ID)
			continue
		}
		from := rv.args.From
		kv := map[string]string{"cores": strconv.Itoa(from.Cores), "memory": strconv.Itoa(from.MemoryMB)}
		tctx, cancel := context.WithTimeout(ctx, pveTimeout)
		if err := pc.SetConfig(tctx, v.Node, v.VMID, kv); err != nil {
			log.Error("reconcile: restore vm config", "server_id", rv.row.ID, "err", err)
		}
		actualGB := servers.BootDiskGB(tctx, pc, v)
		cancel()
		if actualGB == nil {
			log.Warn("reconcile: disk size not read, requested size kept", "server_id", rv.row.ID)
			continue
		}
		if err := r.restoreDisk(ctx, rv, rv.args.Restored(actualGB).DiskGB); err != nil {
			log.Error("reconcile: restore disk size", "server_id", rv.row.ID, "err", err)
		}
	}
}

// restoreDisk sets the rescued row's disk to gb unless an action or a new change has touched it since.
func (r *Reconciler) restoreDisk(ctx context.Context, rv revert, gb int) error {
	fallback := rv.args.Restored(nil)
	if gb == fallback.DiskGB {
		return nil
	}
	return db.WithTx(ctx, r.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := etx.Server.Query().Where(server.ID(rv.row.ID)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.DiskGB != fallback.DiskGB || row.ServerTypeID != fallback.TypeID {
			return nil
		}
		busy, err := etx.Action.Query().Where(servers.ActionsOf(row.ProjectID, row.ID), action.StatusEQ(action.StatusRunning),
			action.CommandNEQ(firewalls.KindApply)).Exist(ctx)
		if err != nil || busy {
			return err
		}
		return etx.Server.UpdateOneID(row.ID).SetDiskGB(gb).Exec(ctx)
	})
}

// powerFrom is the status a power job's args recorded before it ran, else the status the transient one implies.
func powerFrom(args []byte, cur server.Status) server.Status {
	var a servers.PowerServerArgs
	if args != nil && json.Unmarshal(args, &a) == nil && slices.Contains(steady, server.Status(a.From)) {
		return server.Status(a.From)
	}
	if cur == server.StatusStarting {
		return server.StatusOff
	}
	return server.StatusRunning
}
