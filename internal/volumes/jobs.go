package volumes

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	jobTimeout = 5 * time.Minute
	// digestTries bounds the config writes retried on a digest mismatch.
	digestTries = 5
)

type WorkerOptions struct {
	SQL     *sql.DB
	Ent     *ent.Client
	Catalog *catalog.Service
	// Bus nil drops events.
	Bus    servers.Publisher
	Logger *slog.Logger
	// pveFor is Catalog.ProxmoxFor unless a test replaces it.
	pveFor func(ctx context.Context, locationID int64) (pve.Client, error)
}

// RegisterWorkers adds the volume job workers; they follow the jobs.StartAction worker contract.
func RegisterWorkers(w *river.Workers, o WorkerOptions) {
	river.AddWorker(w, NewCreateVolumeWorker(o))
	river.AddWorker(w, NewAttachVolumeWorker(o))
	river.AddWorker(w, NewDetachVolumeWorker(o))
	river.AddWorker(w, NewResizeVolumeWorker(o))
	river.AddWorker(w, NewDeleteVolumeWorker(o))
	river.AddWorker(w, NewUsageWorker(o))
}

func (o *WorkerOptions) defaults() {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.pveFor == nil && o.Catalog != nil {
		o.pveFor = o.Catalog.ProxmoxFor
	}
}

// begin loads a job's action and volume; when stop is true the worker returns ret.
func (o *WorkerOptions) begin(ctx context.Context, actionID, volumeID int64) (act *ent.Action, row *ent.Volume, ret error, stop bool) {
	act, err := o.Ent.Action.Get(ctx, actionID)
	if ent.IsNotFound(err) {
		return nil, nil, river.JobCancel(err), true
	}
	if err != nil {
		return nil, nil, err, true
	}
	row, err = o.Ent.Volume.Get(ctx, volumeID)
	if ent.IsNotFound(err) {
		if act.Finished != nil {
			return act, nil, nil, true
		}
		if ferr := failAction(ctx, o.Ent, actionID, servers.Failure{Code: actions.CodeFailed, Detail: "volume no longer exists"}); ferr != nil {
			return act, nil, ferr, true
		}
		return act, nil, river.JobCancel(err), true
	}
	if err != nil {
		return act, nil, err, true
	}
	return act, row, nil, false
}

// connect returns the location's Proxmox client after the reserved-VMID guard, with the cluster's VMs.
func (o *WorkerOptions) connect(ctx context.Context, locationID int64) (pve.Client, []pve.VM, error) {
	pc, err := o.pveFor(ctx, locationID)
	if err != nil {
		return nil, nil, servers.StepError("connect to proxmox", err)
	}
	vms, err := guard(ctx, pc)
	if err != nil {
		return nil, nil, servers.StepError("check reserved vmid", err)
	}
	return pc, vms, nil
}

func (o *WorkerOptions) contract(job *rivertype.JobRow, name string, act *ent.Action, volumeID int64,
	run func(context.Context) error, fail func(context.Context, servers.Failure) error) servers.Contract {
	return servers.Contract{Job: job, Name: name, Finished: act.Finished != nil,
		Log: o.Logger.With("volume_id", volumeID, "action_id", act.ID, "command", act.Command), Run: run, Fail: fail}
}

func failAction(ctx context.Context, c *ent.Client, id int64, f servers.Failure) error {
	return actions.Fail(context.WithoutCancel(ctx), c, id, f.Code, f.Detail)
}

// publish sends a volume event; volume events leave ServerID 0.
func (o *WorkerOptions) publish(ctx context.Context, kind ext.EventKind, row *ent.Volume, sizeGB int, attached bool) {
	if o.Bus == nil {
		return
	}
	o.Bus.Publish(ctx, ext.Event{Kind: kind, ProjectID: row.ProjectID, ResourceType: ResourceType, ResourceID: row.ID, At: time.Now(),
		Data: map[string]any{"size_gb": sizeGB, "location_id": row.LocationID, "attached": attached}})
}

// ReservedVMIDGuard fails permanently when a VM sits at the reserved owner VMID.
func ReservedVMIDGuard(ctx context.Context, pc pve.Client) error {
	_, err := guard(ctx, pc)
	return err
}

// guard checks every guest type, since QEMU and LXC share VMIDs, and returns the QEMU listing.
func guard(ctx context.Context, pc pve.Client) ([]pve.VM, error) {
	taken, err := pc.GuestExists(ctx, vref.OwnerVMID)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, servers.Permanent("a VM occupies reserved VMID %d", vref.OwnerVMID)
	}
	return pc.VMs(ctx)
}

// diskPresent reports whether storage lists the disk volid; "" is a disk never recorded as ours, so never present.
func diskPresent(ctx context.Context, pc pve.Client, node, storage, volid string) (pve.StorageItem, bool, error) {
	if volid == "" {
		return pve.StorageItem{}, false, nil
	}
	items, err := pc.StorageContent(ctx, node, storage, vref.OwnerVMID)
	if err != nil {
		return pve.StorageItem{}, false, err
	}
	i := slices.IndexFunc(items, func(it pve.StorageItem) bool { return it.Volid == volid })
	if i < 0 {
		return pve.StorageItem{}, false, nil
	}
	return items[i], true, nil
}

// serverVM finds srv's VM in vms; only a VM carrying the server's tags counts.
func serverVM(vms []pve.VM, srv *ent.Server) (pve.VM, error) {
	if srv.Vmid != nil {
		for _, v := range vms {
			tags := servers.SplitTags(v.Tags)
			if v.VMID == *srv.Vmid && !v.Template && slices.Contains(tags, servers.TagNebla) &&
				slices.Contains(tags, servers.ServerTag(srv.ID)) {
				return v, nil
			}
		}
	}
	return pve.VM{}, servers.PermanentCode(actions.CodeServerMissing, "server %d has no vm in proxmox", srv.ID)
}

// loadServer reads the server a volume job targets.
func (o *WorkerOptions) loadServer(ctx context.Context, id int64) (*ent.Server, error) {
	srv, err := o.Ent.Server.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, servers.PermanentCode(actions.CodeServerGone, "server %d no longer exists", id)
	}
	return srv, err
}

// storageNode is the node storage calls for row go to: its pin, else the server's node, else an online node.
func (o *WorkerOptions) storageNode(ctx context.Context, row *ent.Volume, srv *ent.Server) (string, error) {
	if row.Node != "" {
		return row.Node, nil
	}
	if srv != nil && srv.Node != "" {
		return srv.Node, nil
	}
	loc, err := o.Catalog.GetLocation(ctx, row.LocationID)
	if err != nil {
		return "", err
	}
	nodes := slices.Clone(loc.Nodes)
	slices.SortFunc(nodes, func(a, b catalog.Node) int {
		if a.Enabled != b.Enabled {
			if a.Enabled {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, n := range nodes {
		if n.Status == "online" {
			return n.Name, nil
		}
	}
	return "", servers.Transient("no online node in location %d", row.LocationID)
}

// ourKey is the virtioN key whose value references volid, or "".
func ourKey(cfg map[string]string, volid string) string {
	for _, k := range vref.Keys() {
		if vref.Matches(cfg[k], volid) {
			return k
		}
	}
	return ""
}

// freeKey is the first virtioN key neither in cfg nor in taken, the keys other volumes record on the server.
func freeKey(cfg map[string]string, taken []string) string {
	for _, k := range vref.Keys() {
		if _, used := cfg[k]; !used && !slices.Contains(taken, k) {
			return k
		}
	}
	return ""
}

// diskHotplug reports whether the VM hot-plugs disks; no key means Proxmox's default, which does.
func diskHotplug(cfg map[string]string) bool {
	h, ok := cfg["hotplug"]
	switch {
	case !ok || h == "1":
		return true
	case h == "0":
		return false
	}
	return slices.Contains(strings.Split(h, ","), "disk")
}

// needsPowerOff fails when vm runs without disk hot-plug.
func needsPowerOff(vm pve.VM, cfg map[string]string) error {
	if vm.Status == "running" && !diskHotplug(cfg) {
		return servers.PermanentCode(actions.CodePowerOffRequired, "vm %d is running and hotplug %q excludes disk", vm.VMID, cfg["hotplug"])
	}
	return nil
}

func isForbidden(err error) bool {
	var pe *pve.Error
	return errors.As(err, &pe) && pe.Status == 403
}

func alreadyExists(err error) bool {
	var pe *pve.Error
	return errors.As(err, &pe) && strings.Contains(pe.Message, "already exists")
}
