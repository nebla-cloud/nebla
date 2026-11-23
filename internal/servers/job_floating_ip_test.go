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
	"context"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/jobs"
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
	w      *servers.FloatingIPWorker
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
	f.w = servers.NewFloatingIPWorker(f.o)
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

func (f *fipEnv) holders(ip string) []int { return f.App.PVE.IPSetHolders(ip) }

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
	servers.SetFloatingIPBeforeOutcome(f.w, func() {
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
	require.Equal(t, []int{vmidOf(t, e, a.ServerID)}, e.App.PVE.IPSetHolders(fip.IP))

	moved, err := svc.Assign(ctx, e.Project, fip.ID, "b")
	require.NoError(t, err)
	waitOK(t, e, moved.ID)
	require.Equal(t, []int{vmidOf(t, e, b.ServerID)}, e.App.PVE.IPSetHolders(fip.IP))
}
