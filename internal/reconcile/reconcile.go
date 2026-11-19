// Package reconcile keeps server rows in line with what Proxmox reports.
package reconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/ent/action"
	"github.com/Hexeption/nebla/internal/ent/server"
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/Hexeption/nebla/internal/servers"
)

// ErrProxmoxState means Proxmox answered with a state too implausible to act on.
var ErrProxmoxState = errors.New("reconcile: implausible proxmox state")

const pveTimeout = 30 * time.Second

type Options struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	Logger  *slog.Logger
}

type Reconciler struct {
	o       Options
	mu      sync.Mutex
	orphans map[[2]int64]bool
	// afterList runs once the VM list is read; tests use it to race a job.
	afterList func()
}

func New(o Options) *Reconciler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Reconciler{o: o, orphans: map[[2]int64]bool{}}
}

// Run reconciles one location: node load, stuck actions, then server status and node.
func (r *Reconciler) Run(ctx context.Context, locationID int64) error {
	log := r.o.Logger.With("location_id", locationID)
	// SyncNodes refreshes node load and fails when Proxmox is unreachable or lists no nodes.
	loc, err := r.o.Catalog.SyncNodes(ctx, locationID)
	if err != nil {
		log.Warn("reconcile: sync nodes", "err", err)
		return fmt.Errorf("sync nodes: %w", err)
	}
	if !slices.ContainsFunc(loc.Nodes, func(n catalog.Node) bool { return n.Status == "online" }) {
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
	vms := tagged(all)
	r.logOrphans(log, locationID, vms, rows)
	if err := r.rescue(ctx, log, pc, locationID, vms); err != nil {
		return err
	}
	var errs []error
	for _, row := range rows {
		if _, _, ok := desired(row, vms); !ok {
			continue
		}
		if err := r.apply(ctx, log, row.ID, vms, listedAt); err != nil {
			errs = append(errs, fmt.Errorf("server %d: %w", row.ID, err))
		}
	}
	return errors.Join(errs...)
}

// tagged keeps the non-template VMs carrying the nebla tag, by vmid.
func tagged(all []pve.VM) map[int]pve.VM {
	out := map[int]pve.VM{}
	for _, v := range all {
		if !v.Template && slices.Contains(tagList(v.Tags), servers.TagNebla) {
			out[v.VMID] = v
		}
	}
	return out
}

func tagList(tags string) []string {
	return strings.FieldsFunc(tags, func(r rune) bool { return r == ';' || r == ',' || r == ' ' })
}

// vmOf is the server's VM: listed at its vmid and tagged for it.
func vmOf(row *ent.Server, vms map[int]pve.VM) (pve.VM, bool) {
	if row.Vmid == nil {
		return pve.VM{}, false
	}
	v, ok := vms[*row.Vmid]
	return v, ok && slices.Contains(tagList(v.Tags), servers.ServerTag(row.ID))
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
		if id, ok := known[vmid]; ok && slices.Contains(tagList(v.Tags), servers.ServerTag(id)) {
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
func desired(row *ent.Server, vms map[int]pve.VM) (server.Status, string, bool) {
	if row.Vmid == nil || !slices.Contains(steady, row.Status) {
		return "", "", false
	}
	st, node := row.Status, row.Node
	v, found := vmOf(row, vms)
	switch {
	case !found && (row.Status == server.StatusRunning || row.Status == server.StatusOff):
		st = server.StatusMissing
	case !found:
	case row.Status == server.StatusError:
		// error stays: a failed create may already have released the IPs.
		node = v.Node
	default:
		node = v.Node
		if m, ok := mapStatus(v.Status); ok {
			st = m
		}
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
func (r *Reconciler) apply(ctx context.Context, log *slog.Logger, id int64, vms map[int]pve.VM, listedAt time.Time) error {
	return db.WithTx(ctx, r.o.SQL, func(_ *sql.Tx, etx *ent.Client) error {
		row, err := etx.Server.Query().Where(server.ID(id)).ForUpdate().Only(ctx)
		if ent.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		busy, err := etx.Action.Query().Where(servers.ActionsOf(row.ProjectID, row.ID),
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

// rescue fails running actions on this location's servers whose job can no longer finish them.
func (r *Reconciler) rescue(ctx context.Context, log *slog.Logger, pc pve.Client, locationID int64, vms map[int]pve.VM) error {
	q, err := r.o.SQL.QueryContext(ctx, `
		select a.id, s.id from actions a join servers s
		on s.location_id = $1 and a.resources @> jsonb_build_array(jsonb_build_object('id', s.id, 'type', $2::text))
		where `+jobs.Orphaned, locationID, servers.ResourceType)
	if err != nil {
		return fmt.Errorf("find orphaned actions: %w", err)
	}
	type stuck struct{ action, server int64 }
	var found []stuck
	for q.Next() {
		var s stuck
		if err := q.Scan(&s.action, &s.server); err != nil {
			q.Close()
			return err
		}
		found = append(found, s)
	}
	if err := q.Close(); err != nil {
		return err
	}
	if err := q.Err(); err != nil {
		return err
	}
	for _, s := range found {
		if err := r.rescueOne(ctx, log, pc, s.action, s.server, vms); err != nil {
			return fmt.Errorf("rescue action %d: %w", s.action, err)
		}
	}
	return nil
}

func (r *Reconciler) rescueOne(ctx context.Context, log *slog.Logger, pc pve.Client, actionID, serverID int64, vms map[int]pve.VM) error {
	var revert *servers.ChangeTypeArgs
	var row *ent.Server
	err := db.WithTx(ctx, r.o.SQL, func(tx *sql.Tx, etx *ent.Client) error {
		var err error
		row, err = etx.Server.Query().Where(server.ID(serverID)).ForUpdate().Only(ctx)
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
		u, dirty := etx.Server.UpdateOneID(serverID), false
		if command == servers.KindChangeType {
			if args == nil {
				log.Error("reconcile: change type job is gone; snapshot not restored", "server_id", serverID)
			} else {
				var a servers.ChangeTypeArgs
				if err := json.Unmarshal(args, &a); err != nil {
					return err
				}
				revert, dirty = &a, true
				u.SetServerTypeID(a.From.TypeID).SetCores(a.From.Cores).SetMemoryMB(a.From.MemoryMB).SetDiskGB(a.From.DiskGB)
			}
		}
		// A lost power job leaves a transient status; take it from Proxmox.
		if row.Status == server.StatusStarting || row.Status == server.StatusStopping {
			st := server.StatusMissing
			if v, ok := vmOf(row, vms); ok {
				st = row.Status
				if m, ok := mapStatus(v.Status); ok {
					st = m
				}
			}
			u.SetStatus(st)
			dirty = true
		} else if !slices.Contains(steady, row.Status) {
			log.Warn("reconcile: server left in a transient status by a lost job", "server_id", serverID, "status", row.Status)
		}
		if !dirty {
			return nil
		}
		return u.Exec(ctx)
	})
	if err != nil || revert == nil {
		return err
	}
	// Best effort, as the job's own failure path: put cores and memory back on the VM.
	if v, ok := vmOf(row, vms); ok {
		kv := map[string]string{"cores": strconv.Itoa(revert.From.Cores), "memory": strconv.Itoa(revert.From.MemoryMB)}
		tctx, cancel := context.WithTimeout(ctx, pveTimeout)
		defer cancel()
		if err := pc.SetConfig(tctx, v.Node, v.VMID, kv); err != nil {
			log.Error("reconcile: restore vm config", "server_id", serverID, "err", err)
		}
	}
	return nil
}
