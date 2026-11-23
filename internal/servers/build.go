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
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/cloudinit"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/servernetwork"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/networks"
	"github.com/nebla-cloud/nebla/internal/pve"
)

// VM build steps shared by create, rebuild and restore, persisted in that job's step column.
const (
	stepVMID = iota + 1
	stepClone
	stepConfig
	stepResize
	stepISO
	stepFirewall
)

// buildState is a VM build's resume state; saving it writes every field, nil clears.
type buildState struct {
	Vmid        *int
	Upid, Nonce *string
	Step        int
}

func (s buildState) proof() CloneProof { return CloneProof{UPID: s.Upid, Nonce: s.Nonce} }

// builder clones and configures a VM for row from template tmpl, or restores it from archive, saving progress through save.
type builder struct {
	pc     pve.Client
	loc    catalog.Location
	assign []ipam.Assignment
	row    *ent.Server
	tmpl   int
	// archive, when set, makes the VM by restoring this backup instead of cloning tmpl.
	archive        string
	isoGen         int
	instGen        int
	st             buildState
	save           func(context.Context, buildState) error
	afterClonePost func()
	// afterUpidSave runs once a restore's task is saved, before it is awaited.
	afterUpidSave func()
	poll          time.Duration
	tnode         string
	tcfg          map[string]string
	o             *WorkerOptions
	// fwGen is the desired firewall generation this run rendered; fwDone says it rendered at all.
	fwGen  int64
	fwDone bool
	anyFW  bool
}

type step struct {
	n    int
	name string
	fn   func(context.Context) error
}

// runSteps runs the steps after the one cur reports, recording progress and calling after once each is saved.
func runSteps(ctx context.Context, cur func() int, steps []step, progress func(context.Context, int) error, after func(int)) error {
	for _, s := range steps {
		if cur() >= s.n {
			continue
		}
		if err := s.fn(ctx); err != nil {
			return &stepError{s.name, err}
		}
		if err := progress(ctx, s.n); err != nil {
			return &stepError{"record progress", err}
		}
		if after != nil {
			after(s.n)
		}
	}
	return nil
}

func (b *builder) steps() []step {
	return []step{
		{stepVMID, "pick vmid", b.pickVMID},
		b.sourceStep(),
		{stepConfig, "configure vm", b.configure},
		{stepResize, "resize disk", b.resize},
		{stepISO, "upload cloud-init iso", b.uploadISO},
		{stepFirewall, "set firewall", b.firewall},
	}
}

func (b *builder) update(ctx context.Context, f func(*buildState)) error {
	st := b.st
	f(&st)
	if err := b.save(ctx, st); err != nil {
		return err
	}
	b.st = st
	return nil
}

func (b *builder) done(ctx context.Context, n int) error {
	return b.update(ctx, func(s *buildState) { s.Step = n })
}

func (b *builder) vmid() int { return *b.st.Vmid }

func (b *builder) pickVMID(ctx context.Context) error {
	if b.st.Vmid != nil {
		return b.done(ctx, stepVMID)
	}
	vmid, err := b.pc.NextID(ctx)
	if err != nil {
		return err
	}
	return b.update(ctx, func(s *buildState) { s.Vmid, s.Step = &vmid, stepVMID })
}

// sourceStep makes the VM: a template clone, or a backup restore.
func (b *builder) sourceStep() step {
	if b.archive != "" {
		return step{stepClone, "restore backup", b.restore}
	}
	return step{stepClone, "clone template", b.clone}
}

// sourceConfig is the config the VM was made from: the template's, or the restored VM's own.
func (b *builder) sourceConfig(ctx context.Context) (map[string]string, error) {
	if b.archive != "" {
		return b.pc.Config(ctx, b.row.Node, b.vmid())
	}
	return b.templateConfig(ctx)
}

func (b *builder) templateNode(ctx context.Context) (string, error) {
	if b.tnode == "" {
		n, err := templateNode(ctx, b.pc, b.tmpl, b.row.Node)
		if err != nil {
			return "", err
		}
		b.tnode = n
	}
	return b.tnode, nil
}

func (b *builder) templateConfig(ctx context.Context) (map[string]string, error) {
	if b.tcfg != nil {
		return b.tcfg, nil
	}
	node, err := b.templateNode(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := b.pc.Config(ctx, node, b.tmpl)
	if errors.Is(err, pve.ErrNotFound) {
		return nil, templateGone(b.tmpl)
	}
	if err != nil {
		return nil, err
	}
	b.tcfg = cfg
	return cfg, nil
}

func (b *builder) diskStorage() string {
	for _, n := range b.loc.Nodes {
		if n.Name == b.row.Node {
			return n.DiskStorage(b.loc)
		}
	}
	return b.loc.DiskStorage
}

func alreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already exists")
}

func (b *builder) clone(ctx context.Context) error {
	for range maxVMIDTries {
		if b.st.Upid != nil {
			if moved, err := b.awaitOrMove(ctx, *b.st.Upid); err != nil || !moved {
				return err
			}
			continue
		}
		tnode, err := b.templateNode(ctx)
		if err != nil {
			return err
		}
		vmid := b.vmid()
		v, found, err := findVM(ctx, b.pc, vmid)
		if err != nil {
			return err
		}
		if found && ownedBy(v, b.row.ID) {
			return b.tag(ctx)
		}
		if found {
			cfg, err := b.pc.Config(ctx, v.Node, vmid)
			if errors.Is(err, pve.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if cfg["lock"] != "" {
				return transient("vm %d is locked (%s); retrying", vmid, cfg["lock"])
			}
			if b.st.Nonce != nil && hasNonce(cfg, b.row.ID, *b.st.Nonce) {
				return b.tag(ctx)
			}
			if err := b.newVMID(ctx); err != nil {
				return err
			}
			continue
		}
		// No VM there: a saved nonce means an earlier request never landed, so send it again.
		nonce := newNonce()
		if b.st.Nonce != nil {
			nonce = *b.st.Nonce
		}
		if err := b.update(ctx, func(s *buildState) { s.Nonce = &nonce }); err != nil {
			return err
		}
		p := pve.CloneParams{NewID: vmid, Name: b.row.Name, Storage: b.diskStorage(), Full: true,
			Description: cloneDescription(b.row.ID, nonce)}
		if tnode != b.row.Node {
			p.Target = b.row.Node
		}
		up, err := b.pc.Clone(ctx, tnode, b.tmpl, p)
		switch {
		case errors.Is(err, pve.ErrNotFound):
			return templateGone(b.tmpl)
		case alreadyExists(err):
			continue
		case err != nil:
			return err
		}
		if b.afterClonePost != nil {
			b.afterClonePost()
		}
		upid := string(up)
		if err := b.update(ctx, func(s *buildState) { s.Upid = &upid }); err != nil {
			return err
		}
		if moved, err := b.awaitOrMove(ctx, upid); err != nil || !moved {
			return err
		}
	}
	return permanent("no free VMID after %d tries", maxVMIDTries)
}

// errVMIDTaken means our clone task lost its VMID to a concurrent create and made nothing.
var errVMIDTaken = errors.New("vmid taken by another vm")

// lostVMIDRace reports a clone task that failed because another VM took vmid: /cluster/nextid
// does not reserve IDs, so two creates can clone to the same one. PVE then fails the loser
// with a lock timeout on the new VM's config, or "already exists" once the winner is done.
func lostVMIDRace(err error, vmid int) bool {
	var te *pve.TaskError
	if !errors.As(err, &te) {
		return false
	}
	return strings.Contains(te.ExitStatus, fmt.Sprintf("lock-%d.conf", vmid)) || strings.Contains(te.ExitStatus, "already exists")
}

// awaitOrMove waits on our clone task; when it lost its VMID, it picks a new one and reports moved.
func (b *builder) awaitOrMove(ctx context.Context, upid string) (moved bool, err error) {
	err = b.awaitClone(ctx, upid)
	if !errors.Is(err, errVMIDTaken) {
		return false, err
	}
	return true, b.newVMID(ctx)
}

// awaitClone waits on our saved clone or restore task; success proves the VM at vmid is ours.
func (b *builder) awaitClone(ctx context.Context, upid string) error {
	if err := b.pc.WaitTask(ctx, upidNode(upid), pve.UPID(upid)); err != nil {
		if ctx.Err() != nil || retryable(err) {
			return err
		}
		if lostVMIDRace(err, b.vmid()) {
			return errVMIDTaken
		}
		if b.archive != "" {
			return permanent("restore task failed: %s", err)
		}
		return permanent("clone task failed: %s", err)
	}
	if b.archive != "" {
		return b.sanitize(ctx)
	}
	return b.tag(ctx)
}

// newVMID replaces a VMID that another VM took.
func (b *builder) newVMID(ctx context.Context) error {
	id, err := b.pc.NextID(ctx)
	if err != nil {
		return err
	}
	return b.update(ctx, func(s *buildState) { s.Vmid, s.Nonce, s.Upid = &id, nil, nil })
}

func (b *builder) tag(ctx context.Context) error {
	if err := b.pc.SetConfig(ctx, b.row.Node, b.vmid(), map[string]string{"tags": vmTags(b.row.ID)}); err != nil {
		return err
	}
	// Once tagged, the clone proof is spent: it must never vouch for a later VM at this vmid.
	return b.update(ctx, func(s *buildState) { s.Upid, s.Nonce, s.Step = nil, nil, stepClone })
}

func (b *builder) net0(ctx context.Context) (string, error) {
	bridge, vlan := "", (*int)(nil)
	if len(b.assign) > 0 {
		bridge, vlan = b.assign[0].Bridge, b.assign[0].VLAN
	} else {
		cfg, err := b.sourceConfig(ctx)
		if err != nil {
			return "", err
		}
		bridge = cmp.Or(bridgeOf(cfg["net0"]), "vmbr0")
	}
	s := "virtio=" + b.row.MAC + ",bridge=" + bridge
	if vlan != nil {
		s += ",tag=" + strconv.Itoa(*vlan)
	}
	return s + ",firewall=1", nil
}

func (b *builder) configure(ctx context.Context) error {
	net0, err := b.net0(ctx)
	if err != nil {
		return err
	}
	kv := map[string]string{
		"cores":  strconv.Itoa(b.row.Cores),
		"memory": strconv.Itoa(b.row.MemoryMB),
		"name":   b.row.Name,
		"tags":   vmTags(b.row.ID),
		"net0":   net0,
	}
	cfg, err := b.pc.Config(ctx, b.row.Node, b.vmid())
	if err != nil {
		return err
	}
	// A template's Proxmox cloud-init drive would clash with our cidata seed ISO.
	del := cloudinitDrives(cfg)
	nics, err := b.privateNICs(ctx)
	if err != nil {
		return err
	}
	for i := 1; i <= networks.MaxNICs; i++ {
		key := "net" + strconv.Itoa(i)
		if j := slices.IndexFunc(nics, func(n networks.NIC) bool { return n.Index == i }); j >= 0 {
			kv[key] = nicValue(nics[j])
		} else if _, has := cfg[key]; has {
			del = append(del, key)
		}
	}
	if len(del) > 0 {
		kv["delete"] = strings.Join(del, ",")
	}
	if err := b.pc.SetConfig(ctx, b.row.Node, b.vmid(), kv); err != nil {
		return err
	}
	if err := b.o.allowNetworkHotplug(ctx, b.pc, b.row.Node, b.vmid()); err != nil {
		return err
	}
	return b.done(ctx, stepConfig)
}

// privateNICs returns the server's private NICs that belong on its VM, by index.
func (b *builder) privateNICs(ctx context.Context) ([]networks.NIC, error) {
	all, err := networks.PrivateNICs(ctx, b.o.Ent, b.row.ID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(n networks.NIC) bool { return n.State == string(servernetwork.StateDetaching) }), nil
}

// withNetworkHotplug returns hotplug with network added; changed is false when it is unset or already allows it.
func withNetworkHotplug(h string, set bool) (merged string, changed bool) {
	switch {
	case !set || h == "1":
		return h, false
	case h == "0" || h == "":
		return "network", true
	case slices.Contains(strings.Split(h, ","), "network"):
		return h, false
	}
	return h + ",network", true
}

// allowNetworkHotplug adds network to a set hotplug list with the config digest; an unset one stays unset.
func (o *WorkerOptions) allowNetworkHotplug(ctx context.Context, pc pve.Client, node string, vmid int) error {
	for range digestTries {
		cfg, err := pc.Config(ctx, node, vmid)
		if err != nil {
			return err
		}
		h, set := cfg["hotplug"]
		merged, changed := withNetworkHotplug(h, set)
		if !changed {
			return nil
		}
		err = pc.SetConfigDigest(ctx, node, vmid, map[string]string{"hotplug": merged}, cfg["digest"])
		if errors.Is(err, pve.ErrDigestMismatch) {
			continue
		}
		return err
	}
	return transient("vm %d config kept changing", vmid)
}

// markAttached settles the server's attaching rows once its VM is built.
func markAttached(ctx context.Context, etx *ent.Client, serverID int64) error {
	return etx.ServerNetwork.Update().Where(servernetwork.ServerID(serverID), servernetwork.StateEQ(servernetwork.StateAttaching)).
		SetState(servernetwork.StateAttached).Exec(ctx)
}

func (b *builder) resize(ctx context.Context) error {
	cfg, err := b.sourceConfig(ctx)
	if err != nil {
		return err
	}
	disk := catalog.BootDisk(cfg)
	switch {
	case disk == "" && b.archive != "":
		return permanent("restored vm %d has no boot disk", b.vmid())
	case disk == "":
		return permanent("template %d has no boot disk", b.tmpl)
	}
	if cur, ok := diskBytes(cfg[disk]); !ok || int64(b.row.DiskGB)<<30 > cur {
		if err := b.pc.ResizeDisk(ctx, b.row.Node, b.vmid(), disk, b.row.DiskGB); err != nil {
			return err
		}
	}
	return b.done(ctx, stepResize)
}

func (b *builder) volid() string { return isoVolid(b.loc.ISOStorage, b.row.ID, b.isoGen) }

func (b *builder) uploadISO(ctx context.Context) error {
	sts, err := b.pc.Storages(ctx, b.row.Node)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(sts, func(s pve.Storage) bool { return s.ID == b.loc.ISOStorage && slices.Contains(s.Content, "iso") }) {
		return permanent("ISO storage %q is not available on node %s", b.loc.ISOStorage, b.row.Node)
	}
	in, err := seedInput(ctx, b.o.Ent, b.row, b.assign)
	if err != nil {
		return err
	}
	in.InstanceID = fmt.Sprintf("nb-srv-%d-%d", b.row.ID, b.instGen)
	iso, err := cloudinit.Build(in)
	if err != nil {
		return permanent("%s", err)
	}
	volid := b.volid()
	// A retry may find its own earlier upload; errors here surface on the upload.
	_ = b.pc.DeleteVolume(ctx, b.row.Node, b.loc.ISOStorage, volid)
	if err := b.pc.UploadISO(ctx, b.row.Node, b.loc.ISOStorage, isoName(b.row.ID, b.isoGen), iso); err != nil {
		return err
	}
	if err := b.pc.SetConfig(ctx, b.row.Node, b.vmid(), map[string]string{"ide2": volid + ",media=cdrom"}); err != nil {
		return err
	}
	return b.done(ctx, stepISO)
}

func (b *builder) firewall(ctx context.Context) error {
	// applyFirewalls writes the ipset and anti-spoofing flags too.
	if err := b.applyFirewalls(ctx, b.vmid()); err != nil {
		return err
	}
	return b.done(ctx, stepFirewall)
}

// applyFirewalls renders the server's user firewalls onto vmid.
func (b *builder) applyFirewalls(ctx context.Context, vmid int) error {
	gen, anyFW, err := b.o.applyFirewallsNow(ctx, b.pc, b.row.ID, b.row.Node, vmid)
	if err != nil {
		return err
	}
	b.fwGen, b.fwDone, b.anyFW = gen, true, anyFW
	return nil
}

// firewallCurrent re-renders onto vmid unless this run already rendered the desired generation.
func (b *builder) firewallCurrent(ctx context.Context, vmid int) error {
	cur, err := b.o.Ent.Server.Get(ctx, b.row.ID)
	if err != nil {
		return err
	}
	// Compare with what this run rendered: an apply may have raised firewall_gen_applied on another VM.
	if b.fwDone && cur.FirewallGenDesired <= b.fwGen {
		return nil
	}
	return b.applyFirewalls(ctx, vmid)
}

// protect renders the current rules onto vmid and waits for Proxmox to load them, again if they change during the wait.
func (b *builder) protect(ctx context.Context, vmid int) error {
	for range maxRenders {
		if err := b.firewallCurrent(ctx, vmid); err != nil {
			return err
		}
		if !b.anyFW {
			return nil
		}
		if err := settleWait(ctx, FirewallSettle); err != nil {
			return err
		}
		cur, err := b.o.Ent.Server.Get(ctx, b.row.ID)
		if err != nil {
			return err
		}
		if cur.FirewallGenDesired <= b.fwGen {
			return nil
		}
	}
	return transient("firewall kept changing during %d waits", maxRenders)
}

// raiseApplied records fwGen as applied without lowering it.
func (b *builder) raiseApplied(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `update servers set firewall_gen_applied = greatest(firewall_gen_applied, $2) where id = $1`, b.row.ID, b.fwGen)
	return err
}

// startVM starts vmid unless it is running, then waits for it to run.
func startVM(ctx context.Context, pc pve.Client, vmid int, poll time.Duration) error {
	v, found, err := findVM(ctx, pc, vmid)
	if err != nil {
		return err
	}
	if !found {
		return missing("vm %d disappeared before start", vmid)
	}
	if v.Status != "running" {
		up, err := pc.Power(ctx, v.Node, vmid, pve.PowerStart)
		if err == nil {
			err = pc.WaitTask(ctx, v.Node, up)
		}
		if err != nil {
			return err
		}
	}
	return waitStatus(ctx, pc, vmid, "running", startWait, poll)
}

// waitStatus polls until vmid reports want, failing permanently after limit.
func waitStatus(ctx context.Context, pc pve.Client, vmid int, want string, limit, poll time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		v, found, err := findVM(ctx, pc, vmid)
		if err != nil {
			return err
		}
		if found && v.Status == want {
			return nil
		}
		if time.Now().After(deadline) {
			return timedOut("vm %d did not reach %s within %s", vmid, want, limit)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// loadTarget reads what every server job needs: the location, its Proxmox client and the server's IPs.
func (o *WorkerOptions) loadTarget(ctx context.Context, row *ent.Server) (catalog.Location, pve.Client, []ipam.Assignment, error) {
	loc, err := o.Catalog.GetLocation(ctx, row.LocationID)
	if err != nil {
		return loc, nil, nil, &stepError{"load location", err}
	}
	pc, err := o.Catalog.ProxmoxFor(ctx, row.LocationID)
	if err != nil {
		return loc, nil, nil, &stepError{"connect to proxmox", err}
	}
	assign, err := ipam.Assignments(ctx, o.Ent, row.ID)
	if err != nil {
		return loc, pc, nil, &stepError{"load ips", err}
	}
	return loc, pc, assign, nil
}

var cloudinitVolume = regexp.MustCompile(`(^|[:/])(vm|base)-\d+-cloudinit(\.[a-z0-9]+)?$`)

// cloudinitDrives returns the sorted config keys that hold a Proxmox cloud-init drive.
func cloudinitDrives(cfg map[string]string) []string {
	var keys []string
	for k, v := range cfg {
		vol, _, _ := strings.Cut(v, ",")
		if cloudinitVolume.MatchString(vol) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// FirewallSettle is about one Proxmox firewall reload cycle, waited before a protected VM's first start.
var FirewallSettle = 12 * time.Second

// settleWait waits d or until ctx ends.
var settleWait = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
