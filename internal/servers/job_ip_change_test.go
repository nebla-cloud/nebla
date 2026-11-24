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

package servers_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/kdomanski/iso9660"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/predicate"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/ipam"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/primaryips"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type fipBus struct {
	mu     sync.Mutex
	events []ext.Event
}

func (b *fipBus) Publish(_ context.Context, e ext.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *fipBus) of(kind ext.EventKind) []ext.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []ext.Event
	for _, e := range b.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// fipEnv has two built, running servers a and b, queue-only jobs and workers to run by hand.
type fipEnv struct {
	*testutil.ServerEnv
	o      servers.WorkerOptions
	svc    *floatingips.Service
	w      *servers.IPChangeWorker
	aw     *servers.ApplyFirewallsWorker
	bus    *fipBus
	a, b   int64
	va, vb int

	mu    sync.Mutex
	waits []time.Duration
}

func newFIPEnv(t *testing.T) *fipEnv {
	t.Helper()
	f := &fipEnv{bus: &fipBus{}}
	servers.SetSettleWait(t, func(_ context.Context, d time.Duration) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.waits = append(f.waits, d)
		return nil
	})
	e, cw := directEnv(t)
	f.ServerEnv = e
	a, b := builtServer(t, e, cw, "a"), builtServer(t, e, cw, "b")
	f.a, f.b, f.va, f.vb = a.ServerID, b.ServerID, vmidOf(t, e, a.ServerID), vmidOf(t, e, b.ServerID)
	f.o = opts(e)
	f.o.Bus = f.bus
	f.o.Jobs = func() *jobs.Jobs { return e.App.Jobs }
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	proj := projects.New(e.App.Ent, projects.Options{Hooks: hooks})
	f.svc = floatingips.New(floatingips.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog, Projects: proj, Jobs: e.App.Jobs,
		Logger: quiet(), Bus: f.bus})
	f.w = servers.NewIPChangeWorker(f.o)
	f.aw = servers.NewApplyFirewallsWorker(f.o)
	return f
}

func (f *fipEnv) waited() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.waits)
}

func (f *fipEnv) work(t *testing.T, actionID int64, attempt int) error {
	t.Helper()
	return f.w.Work(context.Background(), jobOf(queuedArgs[floatingips.ChangeArgs](t, f.ServerEnv, actionID), attempt))
}

func (f *fipEnv) applies(t *testing.T, serverID int64) []*ent.Action {
	t.Helper()
	acts, err := f.App.Ent.Action.Query().Where(servers.ActionsOf(f.Project, serverID), action.Command(firewalls.KindApply),
		action.StatusEQ(action.StatusRunning)).All(context.Background())
	require.NoError(t, err)
	return acts
}

// runApplies works every running apply_firewalls action of the server and returns how many there were.
func (f *fipEnv) runApplies(t *testing.T, serverID int64) int {
	t.Helper()
	acts := f.applies(t, serverID)
	for _, a := range acts {
		require.NoError(t, f.aw.Work(context.Background(), jobOf(queuedArgs[firewalls.ApplyArgs](t, f.ServerEnv, a.ID), 1)))
	}
	return len(acts)
}

func (f *fipEnv) action(t *testing.T, id int64) actions.Action {
	t.Helper()
	a, err := actions.Get(context.Background(), f.App.Ent, f.Project, id)
	require.NoError(t, err)
	return a
}

func (f *fipEnv) row(t *testing.T, id int64) *ent.IPAddress {
	t.Helper()
	r, err := f.App.Ent.IPAddress.Get(context.Background(), id)
	require.NoError(t, err)
	return r
}

func (f *fipEnv) holders(ip string) []int { return f.App.PVE.IPSetHolders(ip, pve.IPFilterSet) }

// settled creates a floating IP on server a and finishes its assign.
func (f *fipEnv) settled(t *testing.T) (int64, string) {
	t.Helper()
	fip, act, err := f.svc.Create(context.Background(), f.Project, floatingips.CreateInput{Family: "ipv4", Server: strconv.FormatInt(f.a, 10)})
	require.NoError(t, err)
	require.NotNil(t, act)
	require.Equal(t, 1, f.runApplies(t, f.a))
	require.NoError(t, f.work(t, act.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	require.Equal(t, []int{f.va}, f.holders(fip.IP))
	return fip.ID, fip.IP
}

func (f *fipEnv) assign(t *testing.T, id, serverID int64) actions.Action {
	t.Helper()
	act, err := f.svc.Assign(context.Background(), f.Project, id, strconv.FormatInt(serverID, 10))
	require.NoError(t, err)
	return act
}

func TestMoveNeverDoubleHolds(t *testing.T) {
	f := newFIPEnv(t)
	id, ip := f.settled(t)

	var mu sync.Mutex
	var seen [][]int
	f.App.PVE.OnIPSetWrite(func() {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, f.holders(ip))
	})
	act := f.assign(t, id, f.b)
	require.Empty(t, f.applies(t, f.b), "b is bumped only after the release")

	err := f.work(t, act.ID, 1)
	require.True(t, snoozed(err), "%v", err)
	require.Nil(t, f.row(t, id).ReleasingFromID)
	require.Empty(t, f.holders(ip), "a dropped the address and b has not applied yet")
	require.Equal(t, 1, f.runApplies(t, f.b))
	require.NoError(t, f.work(t, act.ID, 1))
	f.App.PVE.OnIPSetWrite(nil)

	require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	require.Equal(t, []int{f.vb}, f.holders(ip))
	mu.Lock()
	defer mu.Unlock()
	for i, h := range seen {
		require.LessOrEqual(t, len(h), 1, "write %d left the address in %v", i, h)
	}
	require.Equal(t, [][]int{nil, {f.vb}}, seen, "a's removal comes before b's add")
	r := f.row(t, id)
	require.Equal(t, &f.b, r.ServerID)
	require.Nil(t, r.PendingGen)
	require.Equal(t, &f.a, r.PreviousServerID)
}

func TestMoveWaitsForInFlightRender(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, ip := f.settled(t)

	// a's ipset loses the address so the render below writes it back from the allow-set it read.
	vm, ok := f.App.PVE.VM(f.va)
	require.True(t, ok)
	primary := slices.DeleteFunc(slices.Clone(vm.IPSets[pve.IPFilterSet]), func(c string) bool { return c == ip })
	f.App.PVE.SetVMIPSet(f.va, pve.IPFilterSet, primary)
	require.Empty(t, f.holders(ip))

	blocked, unblock := make(chan struct{}), make(chan struct{})
	var once, unblockOnce sync.Once
	release := func() { unblockOnce.Do(func() { close(unblock) }) }
	t.Cleanup(release)
	f.App.PVE.OnIPSetWrite(func() { once.Do(func() { close(blocked); <-unblock }) })
	pc, err := f.App.Catalog.ProxmoxFor(ctx, f.Location)
	require.NoError(t, err)

	rendered := make(chan error, 1)
	go func() { rendered <- servers.ApplyFirewallsNow(ctx, f.o, pc, f.a, "pve1", f.va) }()
	select {
	case <-blocked:
	case <-time.After(testWait):
		t.Fatal("the render never wrote the ipset")
	}

	type result struct {
		act actions.Action
		err error
	}
	moved := make(chan result, 1)
	go func() {
		a, err := f.svc.Assign(ctx, f.Project, id, strconv.FormatInt(f.b, 10))
		moved <- result{a, err}
	}()
	early := false
	select {
	case <-moved:
		early = true
	case <-time.After(500 * time.Millisecond):
	}
	release()
	require.NoError(t, <-rendered)
	require.False(t, early, "the move committed while a render of a's old allow-set was still writing")
	m := <-moved
	require.NoError(t, m.err)
	require.Equal(t, []int{f.va}, f.holders(ip), "the stale render finished before the move")

	err = f.work(t, m.act.ID, 1)
	require.True(t, snoozed(err), "%v", err)
	require.Empty(t, f.holders(ip), "the release removed what the stale render wrote")
	require.Equal(t, 1, f.runApplies(t, f.b))
	require.NoError(t, f.work(t, m.act.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, m.act.ID).Status)
	require.Equal(t, []int{f.vb}, f.holders(ip))
}

func TestReleaseWritesRebuildVM(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, ip := f.settled(t)

	const rebuild = 900
	f.App.PVE.AddVM("pve1", rebuild, "a")
	f.App.PVE.SetVMConfig(rebuild, "tags", servers.TagNebla+";"+servers.ServerTag(f.a))
	f.App.PVE.SetVMIPSet(rebuild, pve.IPFilterSet, []string{ip})
	require.NoError(t, f.App.Ent.Server.UpdateOneID(f.a).SetRebuildVmid(rebuild).SetStatus(server.StatusRebuilding).Exec(ctx))
	require.Equal(t, []int{f.va, rebuild}, f.holders(ip))

	act, err := f.svc.Unassign(ctx, f.Project, id)
	require.NoError(t, err)
	require.NoError(t, f.work(t, act.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	require.Empty(t, f.holders(ip))
	r := f.row(t, id)
	require.Nil(t, r.ServerID)
	require.Nil(t, r.ReleasingFromID)
	require.Equal(t, "active", floatingips.Status(r))
}

func TestReleaseMissingVMIsNotProof(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, _ := f.settled(t)
	f.App.PVE.DeleteVM(f.va)

	act, err := f.svc.Unassign(ctx, f.Project, id)
	require.NoError(t, err)
	err = f.work(t, act.ID, 1)
	require.Error(t, err)
	require.False(t, cancelled(err) || snoozed(err), "%v", err)
	require.Equal(t, &f.a, f.row(t, id).ReleasingFromID)
	require.Equal(t, actions.StatusRunning, f.action(t, act.ID).Status)
}

func TestReleaseFailsThenUnassigned(t *testing.T) {
	f := newFIPEnv(t)
	id, _ := f.settled(t)
	act := f.assign(t, id, f.b)

	f.App.PVE.SetDown(true)
	for attempt := 1; attempt < servers.MaxAttempts; attempt++ {
		err := f.work(t, act.ID, attempt)
		require.Error(t, err)
		require.False(t, cancelled(err), "%v", err)
	}
	require.True(t, cancelled(f.work(t, act.ID, servers.MaxAttempts)))

	a := f.action(t, act.ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, actions.CodeReleasePending, a.Error.Code)
	require.Equal(t, actions.Message(floatingips.KindAssign, actions.CodeReleasePending), a.Error.Message)
	r := f.row(t, id)
	require.Nil(t, r.ServerID)
	require.Nil(t, r.PendingGen)
	require.Equal(t, &f.a, r.ReleasingFromID)
	require.Equal(t, "releasing", floatingips.Status(r))
	require.Empty(t, f.applies(t, f.b))
}

func TestAssignWaitsForApply(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	fip, _, err := f.svc.Create(ctx, f.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	act := f.assign(t, fip.ID, f.b)
	r := f.row(t, fip.ID)
	require.NotNil(t, r.PendingGen)

	err = f.work(t, act.ID, 1)
	require.True(t, snoozed(err), "%v", err)
	require.Equal(t, actions.StatusRunning, f.action(t, act.ID).Status)
	_, applied := gens(t, f.ServerEnv, f.b)
	require.Less(t, applied, *r.PendingGen)

	before := len(f.waited())
	require.Equal(t, 1, f.runApplies(t, f.b))
	_, applied = gens(t, f.ServerEnv, f.b)
	require.GreaterOrEqual(t, applied, *r.PendingGen)
	require.NoError(t, f.work(t, act.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	require.Equal(t, []time.Duration{servers.FirewallSettle}, f.waited()[before:], "the action waits for the rules to load")
	require.Nil(t, f.row(t, fip.ID).PendingGen)
	require.Equal(t, []int{f.vb}, f.holders(fip.IP))
}

func TestAssignFailsWithApplyCode(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	fip, _, err := f.svc.Create(ctx, f.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	act := f.assign(t, fip.ID, f.b)
	acts := f.applies(t, f.b)
	require.Len(t, acts, 1)
	require.NoError(t, actions.Fail(ctx, f.App.Ent, acts[0].ID, actions.CodeFirewallUnenforced, "off"))

	require.True(t, cancelled(f.work(t, act.ID, 1)))
	a := f.action(t, act.ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, actions.CodeFirewallUnenforced, a.Error.Code)
	r := f.row(t, fip.ID)
	require.Nil(t, r.PendingGen, "a failed wait is not resumed")
	require.Equal(t, &f.b, r.ServerID)
	require.Equal(t, "active", floatingips.Status(r))
}

func TestAssignSeesApplyFinishMidCheck(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	fip, _, err := f.svc.Create(ctx, f.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	act := f.assign(t, fip.ID, f.b)

	// b's apply raises applied and finishes its action after the wait read applied.
	ran := 0
	servers.SetIPChangeBeforeOutcome(f.w, func() {
		if ran == 0 {
			ran = f.runApplies(t, f.b)
		}
	})
	require.NoError(t, f.work(t, act.ID, 1))
	require.Equal(t, 1, ran)
	require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	require.Nil(t, f.row(t, fip.ID).PendingGen)
	require.Equal(t, []int{f.vb}, f.holders(fip.IP))
}

func TestDeleteFreesAfterRelease(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, ip := f.settled(t)
	allocID := f.row(t, id).AllocationID

	act, err := f.svc.Delete(ctx, f.Project, id)
	require.NoError(t, err)
	f.App.PVE.SetDown(true)
	err = f.work(t, act.ID, 1)
	require.Error(t, err)
	require.False(t, cancelled(err), "%v", err)
	_, err = f.App.Ent.IPAllocation.Get(ctx, allocID)
	require.NoError(t, err, "the address stays allocated until the release finishes")
	require.Equal(t, "deleting", floatingips.Status(f.row(t, id)))
	require.Empty(t, f.bus.of(ext.FloatingIPDeleted))

	f.App.PVE.SetDown(false)
	require.NoError(t, f.work(t, act.ID, 2))
	require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	require.Empty(t, f.holders(ip))
	_, err = f.App.Ent.IPAddress.Get(ctx, id)
	require.True(t, ent.IsNotFound(err), "%v", err)
	_, err = f.App.Ent.IPAllocation.Get(ctx, allocID)
	require.True(t, ent.IsNotFound(err), "%v", err)
	evs := f.bus.of(ext.FloatingIPDeleted)
	require.Len(t, evs, 1)
	require.Equal(t, floatingips.ResourceType, evs[0].ResourceType)
	require.Equal(t, id, evs[0].ResourceID)
	require.Equal(t, f.Project, evs[0].ProjectID)
}

func TestFloatingIPMoveUnderRiver(t *testing.T) {
	ctx := context.Background()
	servers.SetSettleWait(t, func(context.Context, time.Duration) error { return nil })
	e := workerEnv(t, nil)
	a, b := e.CreateServer("a", nil), e.CreateServer("b", nil)
	waitOK(t, e, a.ActionID)
	waitOK(t, e, b.ActionID)
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	svc := floatingips.New(floatingips.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog,
		Projects: projects.New(e.App.Ent, projects.Options{Hooks: hooks}), Jobs: e.App.Jobs, Logger: quiet()})

	fip, act, err := svc.Create(ctx, e.Project, floatingips.CreateInput{Family: "ipv4", Server: "a"})
	require.NoError(t, err)
	waitOK(t, e, act.ID)
	require.Equal(t, []int{vmidOf(t, e, a.ServerID)}, e.App.PVE.IPSetHolders(fip.IP, pve.IPFilterSet))

	moved, err := svc.Assign(ctx, e.Project, fip.ID, "b")
	require.NoError(t, err)
	waitOK(t, e, moved.ID)
	require.Equal(t, []int{vmidOf(t, e, b.ServerID)}, e.App.PVE.IPSetHolders(fip.IP, pve.IPFilterSet))
}

// netConfig reads network-config from an uploaded seed ISO.
func netConfig(t *testing.T, e *testutil.ServerEnv, volid string) string {
	t.Helper()
	raw, ok := e.App.PVE.Volume(volid)
	require.True(t, ok, "iso %s uploaded", volid)
	img, err := iso9660.OpenImage(bytes.NewReader(raw))
	require.NoError(t, err)
	root, err := img.RootDir()
	require.NoError(t, err)
	kids, err := root.GetChildren()
	require.NoError(t, err)
	for _, f := range kids {
		if f.Name() == "network-config" {
			b, err := io.ReadAll(f.Reader())
			require.NoError(t, err)
			return string(b)
		}
	}
	t.Fatalf("iso %s has no network-config", volid)
	return ""
}

// failRelease runs the action's job until it gives up with release_pending.
func (f *fipEnv) failRelease(t *testing.T, actionID int64) {
	t.Helper()
	for attempt := 1; attempt < servers.MaxAttempts; attempt++ {
		err := f.work(t, actionID, attempt)
		require.Error(t, err)
		require.False(t, cancelled(err), "%v", err)
	}
	require.True(t, cancelled(f.work(t, actionID, servers.MaxAttempts)))
	a := f.action(t, actionID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, actions.CodeReleasePending, a.Error.Code)
}

// age moves the finish time of the floating IP's finished actions back by d.
func (f *fipEnv) age(t *testing.T, id int64, d time.Duration) {
	t.Helper()
	ctx := context.Background()
	acts, err := f.App.Ent.Action.Query().Where(floatingips.ActionsOf(f.Project, id), action.FinishedNotNil()).All(ctx)
	require.NoError(t, err)
	for _, a := range acts {
		require.NoError(t, a.Update().SetFinished(a.Finished.Add(-d)).Exec(ctx))
	}
}

// running counts the floating IP's running actions.
func (f *fipEnv) running(t *testing.T, id int64) int {
	t.Helper()
	n, err := f.App.Ent.Action.Query().Where(floatingips.ActionsOf(f.Project, id), action.StatusEQ(action.StatusRunning)).Count(context.Background())
	require.NoError(t, err)
	return n
}

// resumed runs ResumeStuck and returns the one running action it started for the floating IP.
func (f *fipEnv) resumed(t *testing.T, id int64) actions.Action {
	t.Helper()
	require.NoError(t, f.svc.ResumeStuck(context.Background()))
	acts, err := f.App.Ent.Action.Query().Where(floatingips.ActionsOf(f.Project, id), action.StatusEQ(action.StatusRunning)).All(context.Background())
	require.NoError(t, err)
	require.Len(t, acts, 1)
	return f.action(t, acts[0].ID)
}

func TestAssignDuringCreate(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	fip, _, err := f.svc.Create(ctx, f.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	c := f.CreateServer("c", nil)
	cw := servers.NewCreateServerWorker(f.o)
	var act actions.Action
	var held []int
	vmStatus := ""
	servers.SetAfterStep(cw, func(n int) {
		switch n {
		case servers.StepResize:
			act = f.assign(t, fip.ID, c.ServerID)
			err := f.work(t, act.ID, 1)
			require.True(t, snoozed(err), "the assign waits for the build: %v", err)
		case servers.StepFirewall:
			held = f.holders(fip.IP)
			vm, _ := f.App.PVE.VM(vmidOf(t, f.ServerEnv, c.ServerID))
			vmStatus = vm.Status
		}
	})
	require.NoError(t, cw.Work(ctx, job(c, 1)))
	vc := vmidOf(t, f.ServerEnv, c.ServerID)
	require.Equal(t, []int{vc}, held, "the address is allowed before first start")
	require.Equal(t, "stopped", vmStatus)
	require.Contains(t, netConfig(t, f.ServerEnv, isoVolid(c.ServerID)), "- "+fip.IP+"/32\n")

	require.NoError(t, f.work(t, act.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	require.Nil(t, f.row(t, fip.ID).PendingGen)
	require.Equal(t, 1, f.runApplies(t, c.ServerID))
	require.Equal(t, []int{vc}, f.holders(fip.IP))
}

func TestRebuildKeepsFloatingIPs(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	_, ip := f.settled(t)
	f.App.PVE.AddTemplate("pve1", 9001, "ceph")
	f.Admin.CreateImage("debian", f.Location, 9001)
	id := startAction(t, f.ServerEnv, f.a, "rebuild", map[string]any{"image": "debian"})
	w := servers.NewRebuildServerWorker(f.o)
	nu, vmStatus := 0, ""
	var held []int
	servers.SetRebuildAfterStep(w, func(n int) {
		if n == servers.StepFirewall {
			row, err := f.App.Ent.Server.Get(ctx, f.a)
			require.NoError(t, err)
			nu = *row.RebuildVmid
			held = f.holders(ip)
			vm, _ := f.App.PVE.VM(nu)
			vmStatus = vm.Status
		}
	})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, f.ServerEnv, id), 1)))
	require.Equal(t, actions.StatusSuccess, f.action(t, id).Status)
	require.ElementsMatch(t, []int{f.va, nu}, held, "the new VM is allowed the address before first boot")
	require.Equal(t, "stopped", vmStatus)
	require.Equal(t, nu, vmidOf(t, f.ServerEnv, f.a))
	require.Equal(t, []int{nu}, f.holders(ip))
	require.Contains(t, netConfig(t, f.ServerEnv, fmt.Sprintf("local:iso/nb-srv-%d-2.iso", f.a)), "- "+ip+"/32\n")
}

func TestServerDeleteKeepsFloatingIPs(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	f.Admin.SetQuota(f.Project, 10, 100, 100_000, 1000, 3)
	id, ip := f.settled(t)
	allocID := f.row(t, id).AllocationID

	del, _ := expect[actionBody](t, f.C.Send(http.MethodDelete, spath(f.a), nil), http.StatusOK)
	dw := servers.NewDeleteServerWorker(f.o)
	require.NoError(t, dw.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, f.ServerEnv, del.Action.ID), 1)))
	require.Equal(t, actions.StatusSuccess, f.action(t, del.Action.ID).Status)
	_, err := f.App.Ent.Server.Get(ctx, f.a)
	require.True(t, ent.IsNotFound(err), "%v", err)

	r := f.row(t, id)
	require.Nil(t, r.ServerID)
	require.Nil(t, r.ReleasingFromID)
	require.Equal(t, "active", floatingips.Status(r))
	_, err = f.App.Ent.IPAllocation.Get(ctx, allocID)
	require.NoError(t, err, "the address stays allocated")
	require.Empty(t, f.holders(ip))

	// b's primary and the floating IP use 2 of 3 addresses.
	_, _, err = f.svc.Create(ctx, f.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	require.NoError(t, err)
	_, _, err = f.svc.Create(ctx, f.Project, floatingips.CreateInput{Family: "ipv4", Location: "eu-1"})
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "%v", err)
	require.Equal(t, "quota_exceeded", ae.Code)
}

// TestServerDeleteWaitsForFloatingIPLock holds a floating IP row, then its server, the way Assign does; the delete must wait, not deadlock.
func TestServerDeleteWaitsForFloatingIPLock(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, _ := f.settled(t)
	del, _ := expect[actionBody](t, f.C.Send(http.MethodDelete, spath(f.a), nil), http.StatusOK)
	job := jobOf(queuedArgs[servers.DeleteServerArgs](t, f.ServerEnv, del.Action.ID), 1)

	tx, err := f.App.SQL.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "SET LOCAL lock_timeout = '5s'")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SELECT 1 FROM ip_addresses WHERE id = $1 FOR UPDATE", id)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- servers.NewDeleteServerWorker(f.o).Work(ctx, job) }()
	require.Eventually(t, func() bool {
		var n int
		err := f.App.SQL.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&n)
		return err == nil && n > 0
	}, 10*time.Second, 20*time.Millisecond, "the delete waits on the floating IP row")

	_, err = tx.ExecContext(ctx, "SELECT 1 FROM servers WHERE id = $1 FOR NO KEY UPDATE", f.a)
	require.NoError(t, err, "the delete must not hold the server row while it waits")
	require.NoError(t, tx.Commit())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the delete never finished")
	}
	require.Equal(t, actions.StatusSuccess, f.action(t, del.Action.ID).Status)
	require.Nil(t, f.row(t, id).ServerID)
}

func TestReleaseFailsThenReconcilerFinishes(t *testing.T) {
	f := newFIPEnv(t)
	id, ip := f.settled(t)
	act := f.assign(t, id, f.b)
	f.App.PVE.SetDown(true)
	f.failRelease(t, act.ID)
	require.Equal(t, "releasing", floatingips.Status(f.row(t, id)))

	f.App.PVE.SetDown(false)
	require.NoError(t, f.svc.ResumeStuck(context.Background()))
	require.Zero(t, f.running(t, id), "a fresh release_pending failure cools down")
	f.age(t, id, 5*time.Minute)
	res := f.resumed(t, id)
	require.Equal(t, floatingips.KindUnassign, res.Command)
	require.NoError(t, f.work(t, res.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, res.ID).Status)
	r := f.row(t, id)
	require.Nil(t, r.ReleasingFromID)
	require.Nil(t, r.ServerID)
	require.Equal(t, "active", floatingips.Status(r))
	require.Empty(t, f.holders(ip))
	require.Empty(t, f.applies(t, f.b))
}

func TestDeleteWhileReleasing(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, ip := f.settled(t)
	allocID := f.row(t, id).AllocationID
	act := f.assign(t, id, f.b)
	f.App.PVE.SetDown(true)
	f.failRelease(t, act.ID)

	del, err := f.svc.Delete(ctx, f.Project, id)
	require.NoError(t, err)
	f.failRelease(t, del.ID)
	require.Equal(t, "deleting", floatingips.Status(f.row(t, id)))
	_, err = f.App.Ent.IPAllocation.Get(ctx, allocID)
	require.NoError(t, err, "the address stays allocated until the release finishes")
	require.Empty(t, f.bus.of(ext.FloatingIPDeleted))

	f.App.PVE.SetDown(false)
	f.age(t, id, 15*time.Minute)
	res := f.resumed(t, id)
	require.Equal(t, floatingips.KindDelete, res.Command)
	require.NoError(t, f.work(t, res.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, res.ID).Status)
	_, err = f.App.Ent.IPAddress.Get(ctx, id)
	require.True(t, ent.IsNotFound(err), "%v", err)
	_, err = f.App.Ent.IPAllocation.Get(ctx, allocID)
	require.True(t, ent.IsNotFound(err), "%v", err)
	require.Empty(t, f.holders(ip))
	require.Len(t, f.bus.of(ext.FloatingIPDeleted), 1)
}

func TestReleaseSkipsRebuildVMNotYetCloned(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, ip := f.settled(t)
	f.App.PVE.AddTemplate("pve1", 9001, "ceph")
	f.Admin.CreateImage("debian", f.Location, 9001)
	rid := startAction(t, f.ServerEnv, f.a, "rebuild", map[string]any{"image": "debian"})
	w := servers.NewRebuildServerWorker(f.o)
	nu := 0
	servers.SetRebuildAfterStep(w, func(n int) {
		if n != servers.StepVMID {
			return
		}
		row, err := f.App.Ent.Server.Get(ctx, f.a)
		require.NoError(t, err)
		nu = *row.RebuildVmid
		_, exists := f.App.PVE.VM(nu)
		require.False(t, exists, "the rebuild has not cloned yet")
		act, err := f.svc.Unassign(ctx, f.Project, id)
		require.NoError(t, err)
		require.NoError(t, f.work(t, act.ID, 1), "a VM the rebuild has not created yet is skipped")
		require.Equal(t, actions.StatusSuccess, f.action(t, act.ID).Status)
	})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, f.ServerEnv, rid), 1)))
	require.Equal(t, actions.StatusSuccess, f.action(t, rid).Status)
	require.Equal(t, nu, vmidOf(t, f.ServerEnv, f.a))
	require.Empty(t, f.holders(ip), "the rebuild wrote the allow-set without the address")
	require.Nil(t, f.row(t, id).ReleasingFromID)
}

func TestReleaseWaitsForServerDelete(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, ip := f.settled(t)
	act := f.assign(t, id, f.b)
	// The move's job was lost: the row still points at b and releases from a.
	require.NoError(t, actions.Fail(ctx, f.App.Ent, act.ID, actions.CodeFailed, "lost"))
	del, _ := expect[actionBody](t, f.C.Send(http.MethodDelete, spath(f.a), nil), http.StatusOK)

	res := f.resumed(t, id)
	require.Equal(t, floatingips.KindAssign, res.Command)
	for attempt := 1; attempt <= servers.MaxAttempts; attempt++ {
		err := f.work(t, res.ID, attempt)
		require.True(t, snoozed(err), "the release waits for the delete: %v", err)
	}
	require.Equal(t, actions.StatusRunning, f.action(t, res.ID).Status)
	require.Equal(t, &f.a, f.row(t, id).ReleasingFromID)
	require.Empty(t, f.applies(t, f.b), "b gets nothing while a may still hold the address")

	dw := servers.NewDeleteServerWorker(f.o)
	require.NoError(t, dw.Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, f.ServerEnv, del.Action.ID), 1)))
	require.Empty(t, f.holders(ip))
	require.Nil(t, f.row(t, id).ReleasingFromID, "deleting a's row completes the release")

	err := f.work(t, res.ID, 1)
	require.True(t, snoozed(err), "%v", err)
	require.Equal(t, 1, f.runApplies(t, f.b))
	require.NoError(t, f.work(t, res.ID, 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, res.ID).Status)
	require.Equal(t, []int{f.vb}, f.holders(ip))
	r := f.row(t, id)
	require.Equal(t, &f.b, r.ServerID)
	require.Nil(t, r.PendingGen)
}

func TestResumeCoolsDownAfterReleasePending(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, _ := f.settled(t)
	act := f.assign(t, id, f.b)
	f.App.PVE.SetDown(true)
	f.failRelease(t, act.ID)

	// Two passes right after the failure start nothing; b stays free for its own actions.
	require.NoError(t, f.svc.ResumeStuck(ctx))
	require.NoError(t, f.svc.ResumeStuck(ctx))
	require.Zero(t, f.running(t, id))

	f.age(t, id, 5*time.Minute)
	require.NoError(t, f.svc.ResumeStuck(ctx))
	require.NoError(t, f.svc.ResumeStuck(ctx))
	require.Equal(t, 1, f.running(t, id), "one action once the first cooldown passed")
	f.failRelease(t, f.resumed(t, id).ID)

	// Two failures in a row: 15 minutes.
	f.age(t, id, 5*time.Minute)
	require.NoError(t, f.svc.ResumeStuck(ctx))
	require.Zero(t, f.running(t, id))
	f.age(t, id, 10*time.Minute)
	f.failRelease(t, f.resumed(t, id).ID)

	// Three or more: an hour.
	f.age(t, id, 59*time.Minute)
	require.NoError(t, f.svc.ResumeStuck(ctx))
	require.Zero(t, f.running(t, id))
	f.age(t, id, time.Minute)
	res := f.resumed(t, id)

	// A member's delete is never held back, and stays deleting while the release can't finish.
	f.failRelease(t, res.ID)
	del, err := f.svc.Delete(ctx, f.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, f.action(t, del.ID).Status)
	require.Equal(t, "deleting", floatingips.Status(f.row(t, id)))
}

func TestRebuildISOHasAddressStillReleasingToIt(t *testing.T) {
	ctx := context.Background()
	f := newFIPEnv(t)
	id, ip := f.settled(t)
	act := f.assign(t, id, f.b)
	// The move's job was lost: the address is b's but still releasing from a.
	require.NoError(t, actions.Fail(ctx, f.App.Ent, act.ID, actions.CodeFailed, "lost"))
	f.App.PVE.AddTemplate("pve1", 9001, "ceph")
	f.Admin.CreateImage("debian", f.Location, 9001)
	rid := startAction(t, f.ServerEnv, f.b, "rebuild", map[string]any{"image": "debian"})
	w := servers.NewRebuildServerWorker(f.o)
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, f.ServerEnv, rid), 1)))
	require.Equal(t, actions.StatusSuccess, f.action(t, rid).Status)

	require.Contains(t, netConfig(t, f.ServerEnv, fmt.Sprintf("local:iso/nb-srv-%d-2.iso", f.b)), "- "+ip+"/32\n")
	require.Equal(t, []int{f.va}, f.holders(ip), "b's new VM is not allowed the address until the release finishes")
	require.Equal(t, &f.a, f.row(t, id).ReleasingFromID)
}

const oldServerWarning = "This server was created before IP changes were supported. Changing its IP re-runs its cloud-init user data and gives it new SSH host keys."

// pipEnv is fipEnv plus the primary IP service; servers a and b are stopped.
type pipEnv struct {
	*fipEnv
	psvc *primaryips.Service
}

func newPIPEnv(t *testing.T) *pipEnv {
	t.Helper()
	f := newFIPEnv(t)
	hooks, _ := httpapi.ServerHooks(projects.Hooks{}, catalog.Hooks{})
	proj := projects.New(f.App.Ent, projects.Options{Hooks: hooks})
	p := &pipEnv{fipEnv: f, psvc: primaryips.New(primaryips.Options{SQL: f.App.SQL, Ent: f.App.Ent, Catalog: f.App.Catalog,
		Projects: proj, Jobs: f.App.Jobs})}
	p.stop(t, f.a)
	p.stop(t, f.b)
	return p
}

func (p *pipEnv) stop(t *testing.T, id int64) {
	t.Helper()
	setStatus(t, p.ServerEnv, id, servers.StatusOff)
	p.App.PVE.SetVMStatus(vmidOf(t, p.ServerEnv, id), "stopped")
}

// bare builds a stopped server with no public IP.
func (p *pipEnv) bare(t *testing.T, name string) (int64, int) {
	t.Helper()
	s := p.CreateServer(name, map[string]any{"public_net": map[string]bool{"ipv4": false}})
	require.NoError(t, servers.NewCreateServerWorker(p.o).Work(context.Background(), job(s, 1)))
	p.stop(t, s.ServerID)
	return s.ServerID, vmidOf(t, p.ServerEnv, s.ServerID)
}

func (p *pipEnv) standalone(t *testing.T, typ string) primaryips.PrimaryIP {
	t.Helper()
	ip, err := p.psvc.Create(context.Background(), p.Project, primaryips.CreateInput{Type: typ, Location: "eu-1"})
	require.NoError(t, err)
	return ip
}

func (p *pipEnv) primaryOf(t *testing.T, ref string) *ent.IPAddress {
	t.Helper()
	row, err := primaryips.ByRef(context.Background(), p.App.Ent, p.Project, ref)
	require.NoError(t, err)
	return row
}

func (p *pipEnv) passign(t *testing.T, id, serverID int64) (actions.Action, []string) {
	t.Helper()
	act, warns, err := p.psvc.Assign(context.Background(), p.Project, id, strconv.FormatInt(serverID, 10))
	require.NoError(t, err)
	return act, warns
}

// finishAssign runs the assign until it waits on the server's apply, runs the apply, then finishes it.
func (p *pipEnv) finishAssign(t *testing.T, actID, serverID int64) {
	t.Helper()
	err := p.work(t, actID, 1)
	require.True(t, snoozed(err), "%v", err)
	require.Equal(t, 1, p.runApplies(t, serverID))
	require.NoError(t, p.work(t, actID, 1))
	require.Equal(t, actions.StatusSuccess, p.action(t, actID).Status)
}

func (p *pipEnv) pipActions(id int64) predicate.Action {
	return floatingips.ActionsOfType(p.Project, id, primaryips.ResourceType)
}

func TestAssignToStoppedServer(t *testing.T) {
	p := newPIPEnv(t)
	before := srvRow(t, p.ServerEnv, p.a)
	ip := p.standalone(t, "ipv6")
	act, warns := p.passign(t, ip.ID, p.a)
	require.Empty(t, warns)
	require.Equal(t, primaryips.KindAssign, act.Command)
	p.finishAssign(t, act.ID, p.a)

	require.Equal(t, []int{p.va}, p.holders(ip.IP))
	row := srvRow(t, p.ServerEnv, p.a)
	require.Equal(t, before.IsoGeneration+1, row.IsoGeneration)
	vol := genVolid(p.a, row.IsoGeneration)
	require.Equal(t, vol+",media=cdrom", ide2(t, p.ServerEnv, p.a))
	require.Contains(t, netConfig(t, p.ServerEnv, vol), ip.IP)
	require.Equal(t, before.InstanceGen, row.InstanceGen)
	require.Equal(t, fmt.Sprintf("nb-srv-%d-%d", p.a, before.InstanceGen), instanceID(t, isoFiles(t, p.ServerEnv, vol)))
	_, gone := p.App.PVE.Volume(genVolid(p.a, before.IsoGeneration))
	require.False(t, gone, "the old ISO is deleted")
	r := p.row(t, ip.ID)
	require.Nil(t, r.PendingGen)
	require.Equal(t, &p.a, r.ServerID)
	require.Contains(t, p.waited(), servers.FirewallSettle)
}

func TestAssignOldServerWarnsAndBumpsInstance(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	require.NoError(t, p.App.Ent.Server.UpdateOneID(p.a).SetNetRefresh(false).Exec(ctx))
	ip := p.standalone(t, "ipv6")
	act, warns := p.passign(t, ip.ID, p.a)
	require.Equal(t, []string{oldServerWarning}, warns)
	p.finishAssign(t, act.ID, p.a)

	row := srvRow(t, p.ServerEnv, p.a)
	require.Equal(t, 2, row.IsoGeneration)
	require.Equal(t, int64(2), row.InstanceGen)
	require.Equal(t, fmt.Sprintf("nb-srv-%d-2", p.a), instanceID(t, isoFiles(t, p.ServerEnv, genVolid(p.a, 2))))
}

func TestAssignSetsNet0WhenNoPrimary(t *testing.T) {
	p := newPIPEnv(t)
	c, vc := p.bare(t, "c")
	vm, ok := p.App.PVE.VM(vc)
	require.True(t, ok)
	mac := srvRow(t, p.ServerEnv, c).MAC
	require.Equal(t, "virtio="+mac+",bridge=vmbr0,firewall=1", vm.Config["net0"])

	p.Admin.CreateSegmentPool(p.Location, "ipv4", "vmbr1", new(42), "10.1.0.0/24", "10.1.0.1", "10.1.0.10", "10.1.0.20")
	id, addr := p.PrimaryIPOn("ipv4", ipam.Segment{Bridge: "vmbr1", VLAN: new(42)})
	act, _ := p.passign(t, id, c)
	p.finishAssign(t, act.ID, c)

	vm, _ = p.App.PVE.VM(vc)
	require.Equal(t, "virtio="+mac+",bridge=vmbr1,tag=42,firewall=1", vm.Config["net0"])
	require.Equal(t, []int{vc}, p.holders(addr))

	// With a primary IP on the server, a second family leaves net0 alone.
	p.Admin.CreateSegmentPool(p.Location, "ipv6", "vmbr1", new(42), "2001:db8:1::/64", "2001:db8:1::1", "2001:db8:1::10", "2001:db8:1::20")
	id6, _ := p.PrimaryIPOn("ipv6", ipam.Segment{Bridge: "vmbr1", VLAN: new(42)})
	p.App.PVE.SetVMConfig(vc, "net0", "e1000="+mac+",bridge=vmbr1,tag=42,firewall=1,queues=2")
	act, _ = p.passign(t, id6, c)
	p.finishAssign(t, act.ID, c)
	vm, _ = p.App.PVE.VM(vc)
	require.Equal(t, "e1000="+mac+",bridge=vmbr1,tag=42,firewall=1,queues=2", vm.Config["net0"])
}

func TestAssignNet0KeepsOtherKeysAndDropsTag(t *testing.T) {
	p := newPIPEnv(t)
	c, vc := p.bare(t, "c")
	mac := srvRow(t, p.ServerEnv, c).MAC
	p.App.PVE.SetVMConfig(vc, "net0", "e1000="+mac+",bridge=vmbr9,tag=7,firewall=1,queues=2")
	ip := p.standalone(t, "ipv4")
	act, _ := p.passign(t, ip.ID, c)
	p.finishAssign(t, act.ID, c)
	vm, _ := p.App.PVE.VM(vc)
	require.Equal(t, "e1000="+mac+",bridge=vmbr0,firewall=1,queues=2", vm.Config["net0"])
}

func TestStartRefusedDuringPrimaryAssign(t *testing.T) {
	p := newPIPEnv(t)
	ip := p.standalone(t, "ipv6")
	act, _ := p.passign(t, ip.ID, p.a)
	expectErr(t, p.C.Send(http.MethodPost, spath(p.a)+"/actions/poweron", nil), http.StatusConflict, "locked")
	p.finishAssign(t, act.ID, p.a)
	startAction(t, p.ServerEnv, p.a, "poweron", nil)
}

func TestUnassignOrdering(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	c, vc := p.bare(t, "c")
	pip := p.primaryOf(t, "a-ipv4")
	require.Equal(t, []int{p.va}, p.holders(pip.IP))
	isoBefore := srvRow(t, p.ServerEnv, p.a).IsoGeneration

	var mu sync.Mutex
	var seen [][]int
	p.App.PVE.OnIPSetWrite(func() {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, p.holders(pip.IP))
	})
	un, warns, err := p.psvc.Unassign(ctx, p.Project, pip.ID)
	require.NoError(t, err)
	require.Empty(t, warns)
	require.Equal(t, primaryips.KindUnassign, un.Command)
	require.Equal(t, "releasing", primaryips.Status(p.row(t, pip.ID)))
	_, _, err = p.psvc.Assign(ctx, p.Project, pip.ID, strconv.FormatInt(c, 10))
	requireAPIErr(t, err, http.StatusConflict, "locked", "the primary IP has a running action")

	require.NoError(t, p.work(t, un.ID, 1))
	require.Equal(t, actions.StatusSuccess, p.action(t, un.ID).Status)
	require.Empty(t, p.holders(pip.IP))
	r := p.row(t, pip.ID)
	require.Nil(t, r.ServerID)
	require.Nil(t, r.ReleasingFromID)
	aRow := srvRow(t, p.ServerEnv, p.a)
	require.Equal(t, isoBefore+1, aRow.IsoGeneration)
	require.NotContains(t, netConfig(t, p.ServerEnv, genVolid(p.a, aRow.IsoGeneration)), pip.IP)

	act, _ := p.passign(t, pip.ID, c)
	err = p.work(t, act.ID, 1)
	require.True(t, snoozed(err), "%v", err)
	require.Empty(t, p.holders(pip.IP), "c holds it only after its apply")
	require.Equal(t, 1, p.runApplies(t, c))
	require.NoError(t, p.work(t, act.ID, 1))
	p.App.PVE.OnIPSetWrite(nil)
	require.Equal(t, actions.StatusSuccess, p.action(t, act.ID).Status)
	require.Equal(t, []int{vc}, p.holders(pip.IP))

	mu.Lock()
	defer mu.Unlock()
	for i, h := range seen {
		require.LessOrEqual(t, len(h), 1, "write %d left the address in %v", i, h)
	}
	require.NotEmpty(t, seen)
	require.Empty(t, seen[0], "a's removal is the first write")
	require.Equal(t, []int{vc}, seen[len(seen)-1])
	for _, h := range seen {
		require.NotContains(t, h, p.va, "a never regains the address")
	}
}

func TestUnassignRefusedWithFloating(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	setStatus(t, p.ServerEnv, p.a, servers.StatusRunning)
	p.settled(t)
	p.stop(t, p.a)
	_, _, err := p.psvc.Unassign(ctx, p.Project, p.primaryOf(t, "a-ipv4").ID)
	requireAPIErr(t, err, http.StatusConflict, "floating_ips_attached", "Unassign this server's IPv4 floating IPs first.")
}

func TestUnassignUnreachableThenReconciler(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	c, vc := p.bare(t, "c")
	pip := p.primaryOf(t, "a-ipv4")
	un, _, err := p.psvc.Unassign(ctx, p.Project, pip.ID)
	require.NoError(t, err)
	p.App.PVE.SetDown(true)
	p.failRelease(t, un.ID)
	r := p.row(t, pip.ID)
	require.Equal(t, "releasing", primaryips.Status(r))
	require.Equal(t, &p.a, r.ReleasingFromID)
	_, _, err = p.psvc.Assign(ctx, p.Project, pip.ID, strconv.FormatInt(c, 10))
	requireAPIErr(t, err, http.StatusConflict, "locked", "The address is still being released from the previous server.")

	p.App.PVE.SetDown(false)
	require.NoError(t, p.psvc.ResumeStuck(ctx))
	n, err := p.App.Ent.Action.Query().Where(p.pipActions(pip.ID), action.StatusEQ(action.StatusRunning)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n, "a fresh release_pending failure cools down")
	acts, err := p.App.Ent.Action.Query().Where(p.pipActions(pip.ID), action.FinishedNotNil()).All(ctx)
	require.NoError(t, err)
	for _, a := range acts {
		require.NoError(t, a.Update().SetFinished(a.Finished.Add(-5*time.Minute)).Exec(ctx))
	}
	require.NoError(t, p.psvc.ResumeStuck(ctx))
	running, err := p.App.Ent.Action.Query().Where(p.pipActions(pip.ID), action.StatusEQ(action.StatusRunning)).All(ctx)
	require.NoError(t, err)
	require.Len(t, running, 1)
	require.Equal(t, primaryips.KindUnassign, running[0].Command)
	require.NoError(t, p.work(t, running[0].ID, 1))
	require.Equal(t, actions.StatusSuccess, p.action(t, running[0].ID).Status)
	require.Empty(t, p.holders(pip.IP))
	require.Equal(t, "active", primaryips.Status(p.row(t, pip.ID)))

	act, _ := p.passign(t, pip.ID, c)
	p.finishAssign(t, act.ID, c)
	require.Equal(t, []int{vc}, p.holders(pip.IP))
}

func requireAPIErr(t *testing.T, err error, status int, code, msg string) {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "%v", err)
	require.Equal(t, status, ae.Status, ae.Msg)
	require.Equal(t, code, ae.Code, ae.Msg)
	if msg != "" {
		require.Equal(t, msg, ae.Msg)
	}
}

// agePIP moves the finish time of the primary IP's finished actions back by d.
func (p *pipEnv) agePIP(t *testing.T, id int64, d time.Duration) {
	t.Helper()
	ctx := context.Background()
	acts, err := p.App.Ent.Action.Query().Where(p.pipActions(id), action.FinishedNotNil()).All(ctx)
	require.NoError(t, err)
	for _, a := range acts {
		require.NoError(t, a.Update().SetFinished(a.Finished.Add(-d)).Exec(ctx))
	}
}

func (p *pipEnv) runningPIP(t *testing.T, id int64) []*ent.Action {
	t.Helper()
	acts, err := p.App.Ent.Action.Query().Where(p.pipActions(id), action.StatusEQ(action.StatusRunning)).All(context.Background())
	require.NoError(t, err)
	return acts
}

// vlanIP is an unassigned primary IPv4 on vmbr1 tag 42.
func (p *pipEnv) vlanIP(t *testing.T) int64 {
	t.Helper()
	p.Admin.CreateSegmentPool(p.Location, "ipv4", "vmbr1", new(42), "10.1.0.0/24", "10.1.0.1", "10.1.0.10", "10.1.0.20")
	id, _ := p.PrimaryIPOn("ipv4", ipam.Segment{Bridge: "vmbr1", VLAN: new(42)})
	return id
}

func TestResumeAssignWaitsForStoppedServer(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	c, vc := p.bare(t, "c")
	id := p.vlanIP(t)
	act, _ := p.passign(t, id, c)
	vm, _ := p.App.PVE.VM(vc)
	net0 := vm.Config["net0"]
	// The job was lost and the action failed as orphaned; the member then starts the server.
	require.NoError(t, actions.Fail(ctx, p.App.Ent, act.ID, actions.CodeFailed, "orphaned"))
	setStatus(t, p.ServerEnv, c, servers.StatusRunning)
	p.agePIP(t, id, time.Hour)

	require.NoError(t, p.psvc.ResumeStuck(ctx))
	require.Empty(t, p.runningPIP(t, id), "no assign is resumed on a running server")
	vm, _ = p.App.PVE.VM(vc)
	require.Equal(t, net0, vm.Config["net0"])

	p.stop(t, c)
	require.NoError(t, p.psvc.ResumeStuck(ctx))
	res := p.runningPIP(t, id)
	require.Len(t, res, 1)
	p.finishAssign(t, res[0].ID, c)
	vm, _ = p.App.PVE.VM(vc)
	require.Contains(t, vm.Config["net0"], "bridge=vmbr1,tag=42")
}

func TestOldServerFailedAssignBumpsOnce(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	require.NoError(t, p.App.Ent.Server.UpdateOneID(p.a).SetNetRefresh(false).Exec(ctx))
	before := srvRow(t, p.ServerEnv, p.a)
	ip := p.standalone(t, "ipv6")
	act, _ := p.passign(t, ip.ID, p.a)
	require.True(t, srvRow(t, p.ServerEnv, p.a).InstanceBumpPending)
	acts := p.applies(t, p.a)
	require.Len(t, acts, 1)
	require.NoError(t, actions.Fail(ctx, p.App.Ent, acts[0].ID, actions.CodeFirewallUnenforced, "off"))
	require.True(t, cancelled(p.work(t, act.ID, 1)))
	require.Equal(t, actions.StatusError, p.action(t, act.ID).Status)
	require.NotNil(t, p.row(t, ip.ID).PendingGen, "a primary assign is resumed after a failure")

	// The generic sync, as queued by the failed job, still bumps the instance.
	require.True(t, syncNow(t, p.o, p.a, false))
	row := srvRow(t, p.ServerEnv, p.a)
	require.False(t, row.InstanceBumpPending)
	require.Equal(t, before.IsoGeneration+1, row.IsoGeneration)
	require.Equal(t, int64(row.IsoGeneration), row.InstanceGen)
	require.NotEqual(t, before.InstanceGen, row.InstanceGen)
	require.Contains(t, netConfig(t, p.ServerEnv, genVolid(p.a, row.IsoGeneration)), ip.IP)
	require.False(t, syncNow(t, p.o, p.a, false), "the bump happens once")
	require.Equal(t, row.InstanceGen, srvRow(t, p.ServerEnv, p.a).InstanceGen)
}

func TestNet0FailureRetriedByResume(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	c, vc := p.bare(t, "c")
	id := p.vlanIP(t)
	act, _ := p.passign(t, id, c)
	p.App.PVE.SetDown(true)
	for attempt := 1; attempt < servers.MaxAttempts; attempt++ {
		err := p.work(t, act.ID, attempt)
		require.Error(t, err)
		require.False(t, cancelled(err), "%v", err)
	}
	require.True(t, cancelled(p.work(t, act.ID, servers.MaxAttempts)))
	require.Equal(t, actions.StatusError, p.action(t, act.ID).Status)
	require.NotNil(t, p.row(t, id).PendingGen)
	p.App.PVE.SetDown(false)

	require.NoError(t, p.psvc.ResumeStuck(ctx))
	require.Empty(t, p.runningPIP(t, id), "a fresh failure cools down")
	p.agePIP(t, id, 5*time.Minute)
	require.NoError(t, p.psvc.ResumeStuck(ctx))
	res := p.runningPIP(t, id)
	require.Len(t, res, 1)
	require.Equal(t, primaryips.KindAssign, res[0].Command)
	p.finishAssign(t, res[0].ID, c)
	vm, _ := p.App.PVE.VM(vc)
	require.Equal(t, "virtio="+srvRow(t, p.ServerEnv, c).MAC+",bridge=vmbr1,tag=42,firewall=1", vm.Config["net0"])
	require.Nil(t, p.row(t, id).PendingGen)
}

func TestRebuildClearsInstanceBump(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	require.NoError(t, p.App.Ent.Server.UpdateOneID(p.a).SetNetRefresh(false).SetInstanceBumpPending(true).Exec(ctx))
	p.App.PVE.AddTemplate("pve1", 9001, "ceph")
	p.Admin.CreateImage("debian", p.Location, 9001)
	rid := startAction(t, p.ServerEnv, p.a, "rebuild", map[string]any{"image": "debian"})
	w := servers.NewRebuildServerWorker(p.o)
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, p.ServerEnv, rid), 1)))
	require.Equal(t, actions.StatusSuccess, p.action(t, rid).Status)
	row := srvRow(t, p.ServerEnv, p.a)
	require.False(t, row.InstanceBumpPending)
	require.True(t, row.NetRefresh)
}

func TestWithSegment(t *testing.T) {
	got, err := servers.WithSegment("virtio=AA,bridge=vmbr0", "vmbr1", new(5))
	require.NoError(t, err)
	require.Equal(t, "virtio=AA,bridge=vmbr1,tag=5,firewall=1", got)
	got, err = servers.WithSegment("virtio=AA,bridge=vmbr0,tag=3,firewall=0", "vmbr2", nil)
	require.NoError(t, err)
	require.Equal(t, "virtio=AA,bridge=vmbr2,firewall=1", got)
	_, err = servers.WithSegment("", "vmbr0", nil)
	require.Error(t, err)
}

func TestISOSyncKeepsBumpRequestedMidSync(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	before := srvRow(t, p.ServerEnv, p.a)
	// A stale hash gives the generic sync work to do.
	require.NoError(t, p.App.Ent.Server.UpdateOneID(p.a).SetIsoNetHash("stale").Exec(ctx))
	once := false
	servers.SetISOBeforeUpdate(t, func(id int64) {
		if once || id != p.a {
			return
		}
		once = true
		// An Assign on the old server commits its bump request while the sync uploads.
		require.NoError(t, p.App.Ent.Server.UpdateOneID(p.a).SetInstanceBumpPending(true).Exec(ctx))
	})
	_, err := servers.SyncISO(ctx, p.o, p.a, false)
	require.Error(t, err, "the sync that read no bump must not clear one")
	require.True(t, once)
	row := srvRow(t, p.ServerEnv, p.a)
	require.True(t, row.InstanceBumpPending)
	require.Equal(t, before.InstanceGen, row.InstanceGen)

	require.True(t, syncNow(t, p.o, p.a, false))
	row = srvRow(t, p.ServerEnv, p.a)
	require.False(t, row.InstanceBumpPending)
	require.Equal(t, int64(row.IsoGeneration), row.InstanceGen)
	require.NotEqual(t, before.InstanceGen, row.InstanceGen)
}

func TestServerDeleteClearsPrimaryPendingGen(t *testing.T) {
	ctx := context.Background()
	p := newPIPEnv(t)
	ip := p.standalone(t, "ipv6")
	require.NoError(t, p.App.Ent.IPAddress.UpdateOneID(ip.ID).SetAutoDelete(false).Exec(ctx))
	act, _ := p.passign(t, ip.ID, p.a)
	require.NoError(t, actions.Fail(ctx, p.App.Ent, act.ID, actions.CodeFailed, "lost"))
	require.NotNil(t, p.row(t, ip.ID).PendingGen)
	del, _ := expect[actionBody](t, p.C.Send(http.MethodDelete, spath(p.a), nil), http.StatusOK)
	require.NoError(t, servers.NewDeleteServerWorker(p.o).Work(ctx, jobOf(queuedArgs[servers.DeleteServerArgs](t, p.ServerEnv, del.Action.ID), 1)))
	r := p.row(t, ip.ID)
	require.Nil(t, r.ServerID)
	require.Nil(t, r.PendingGen)
}
