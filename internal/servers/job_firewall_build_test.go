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
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

// settles records each wait before a first start and the PVE call count at that moment.
type settles struct {
	mu    sync.Mutex
	waits []time.Duration
	at    []int
}

func recordSettles(t *testing.T, e *testutil.ServerEnv) *settles {
	t.Helper()
	s := &settles{}
	servers.SetSettleWait(t, func(_ context.Context, d time.Duration) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.waits = append(s.waits, d)
		s.at = append(s.at, len(e.App.PVE.Calls()))
		return nil
	})
	return s
}

func (s *settles) get() ([]time.Duration, []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...), append([]int(nil), s.at...)
}

// createWithFirewall creates a server with firewall fw applied, queued but not worked.
func createWithFirewall(t *testing.T, e *testutil.ServerEnv, name string, fw ...int64) testutil.CreatedServer {
	t.Helper()
	srv, a, err := serverService(e).Create(context.Background(), e.Project, servers.CreateInput{Name: name, ServerType: "small",
		Image: "ubuntu", Location: "eu-1", IPv4: servers.IPChoice{New: true}, Firewalls: fw})
	require.NoError(t, err)
	return testutil.CreatedServer{ServerID: srv.ID, ActionID: a.ID}
}

func newFirewall(t *testing.T, e *testutil.ServerEnv, name string, rules ...firewalls.RuleInput) int64 {
	t.Helper()
	f, _, err := fwService(e).Create(context.Background(), e.Project, firewalls.CreateInput{Name: name, Rules: rules})
	require.NoError(t, err)
	return f.ID
}

// startCall is the index of the first start of vmid in the fake's call log, or -1.
func startCall(e *testutil.ServerEnv, vmid int) int {
	want := "POST /nodes/pve1/qemu/" + strconv.Itoa(vmid) + "/status/start"
	for i, c := range e.App.PVE.Calls() {
		if c == want {
			return i
		}
	}
	return -1
}

// lastFWWrite is the index of the last firewall write to vmid, or -1.
func lastFWWrite(e *testutil.ServerEnv, vmid int) int {
	part := "/qemu/" + strconv.Itoa(vmid) + "/firewall/"
	last := -1
	for i, c := range e.App.PVE.Calls() {
		if strings.Contains(c, part) && !strings.HasPrefix(c, "GET ") {
			last = i
		}
	}
	return last
}

func requireProtectedBeforeStart(t *testing.T, e *testutil.ServerEnv, vmid int, ports ...string) {
	t.Helper()
	start := startCall(e, vmid)
	require.GreaterOrEqual(t, start, 0, "vm %d started", vmid)
	require.Less(t, lastFWWrite(e, vmid), start, "every firewall write came before the start")
	require.Equal(t, ports, dports(t, e, vmid))
	require.Equal(t, "DROP", policyIn(t, e, vmid))
}

func TestCreateAppliesRulesBeforeStart(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	rec := recordSettles(t, e)
	s := createWithFirewall(t, e, "web", newFirewall(t, e, "fw", tcpIn("22")))
	require.NoError(t, w.Work(ctx, job(s, 1)))
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	requireProtectedBeforeStart(t, e, vmid, "22")
	waits, at := rec.get()
	require.Equal(t, []time.Duration{servers.FirewallSettle}, waits)
	require.LessOrEqual(t, at[0], startCall(e, vmid), "the wait came before the start")
	require.Greater(t, at[0], lastFWWrite(e, vmid), "the wait came after the rules")
	desired, applied := gens(t, e, s.ServerID)
	require.Equal(t, desired, applied)
}

func TestCreateWaitsOnlyWithFirewall(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	rec := recordSettles(t, e)
	plain := e.CreateServer("plain", nil)
	require.NoError(t, w.Work(ctx, job(plain, 1)))
	waitOK(t, e, plain.ActionID)
	waits, _ := rec.get()
	require.Empty(t, waits, "no firewall, no wait")

	s := createWithFirewall(t, e, "web", newFirewall(t, e, "fw", tcpIn("22")))
	require.NoError(t, w.Work(ctx, job(s, 1)))
	waitOK(t, e, s.ActionID)
	waits, _ = rec.get()
	require.Equal(t, []time.Duration{12 * time.Second}, waits)
}

func TestFirewallChangeDuringCreateLandsBeforeStart(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	recordSettles(t, e)
	fw := fwService(e)
	id := newFirewall(t, e, "fw", tcpIn("22"))
	s := createWithFirewall(t, e, "web", id)
	var changed []actions.Action
	servers.SetAfterStep(w, func(step int) {
		if step == servers.StepFirewall {
			var err error
			changed, err = fw.SetRules(ctx, e.Project, id, []firewalls.RuleInput{tcpIn("80")})
			require.NoError(t, err)
		}
	})
	require.NoError(t, w.Work(ctx, job(s, 1)))
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	requireProtectedBeforeStart(t, e, vmid, "80")
	desired, applied := gens(t, e, s.ServerID)
	require.Equal(t, int64(1), desired)
	require.Equal(t, int64(1), applied)

	// The queued apply finds the work done.
	require.Len(t, changed, 1)
	writes := fwWrites(e)
	aw := servers.NewApplyFirewallsWorker(opts(e))
	require.NoError(t, aw.Work(ctx, jobOf(queuedArgs[firewalls.ApplyArgs](t, e, changed[0].ID), 1)))
	require.Equal(t, writes, fwWrites(e))
	waitOK(t, e, changed[0].ID)
}

func TestResumedCreatePastFirewallStepGetsRules(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	recordSettles(t, e)
	s := createWithFirewall(t, e, "web", newFirewall(t, e, "fw", tcpIn("22")))
	crashCtx, crash := context.WithCancel(ctx)
	defer crash()
	servers.SetAfterStep(w, func(step int) {
		if step == servers.StepFirewall {
			crash()
		}
	})
	require.Error(t, w.Work(crashCtx, job(s, 1)))
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StepFirewall, row.CreateStep)

	// An older build reached this step having written only the ipset: no rules, open policies.
	vmid := *row.Vmid
	pc, err := e.App.Catalog.ProxmoxFor(ctx, e.Location)
	require.NoError(t, err)
	require.NoError(t, firewalls.Sync(ctx, pc, "pve1", vmid, firewalls.Render(nil, false, firewalls.System{})))
	require.Empty(t, dports(t, e, vmid))
	require.Equal(t, -1, startCall(e, vmid))

	servers.SetAfterStep(w, nil)
	require.NoError(t, w.Work(ctx, job(s, 2)))
	waitOK(t, e, s.ActionID)
	requireProtectedBeforeStart(t, e, vmid, "22")
}

func TestRebuildNewVMHasRules(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	rec := recordSettles(t, e)
	fw := fwService(e)
	old := vmidOf(t, e, s.ServerID)
	f, acts, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "fw", Rules: []firewalls.RuleInput{tcpIn("22")},
		ApplyTo: []firewalls.ResourceInput{onServer(s.ServerID)}})
	require.NoError(t, err)
	aw := servers.NewApplyFirewallsWorker(opts(e))
	require.NoError(t, aw.Work(ctx, jobOf(queuedArgs[firewalls.ApplyArgs](t, e, acts[0].ID), 1)))
	require.Equal(t, []string{"22"}, dports(t, e, old))

	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	var atSwap []string
	servers.SetRebuildAfterStep(w, func(n int) {
		row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
		require.NoError(t, err)
		switch n {
		case servers.StepFirewall:
			require.Equal(t, []string{"22"}, dports(t, e, *row.RebuildVmid), "the new VM got the rules in the build")
			require.Equal(t, "DROP", policyIn(t, e, *row.RebuildVmid))
			_, err := fw.SetRules(ctx, e.Project, f.ID, []firewalls.RuleInput{tcpIn("80")})
			require.NoError(t, err)
			// An apply still running on the old VM raised the applied generation.
			require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetFirewallGenApplied(row.FirewallGenDesired+1).Exec(ctx))
		case servers.StepSwap:
			atSwap = dports(t, e, *row.Vmid)
		}
	})
	require.NoError(t, w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)))
	waitOK(t, e, id)
	nu := vmidOf(t, e, s.ServerID)
	require.NotEqual(t, old, nu)
	require.Equal(t, []string{"80"}, atSwap, "the change landed on the new VM before the swap")
	requireProtectedBeforeStart(t, e, nu, "80")
	waits, _ := rec.get()
	require.Equal(t, []time.Duration{servers.FirewallSettle}, waits)
	desired, applied := gens(t, e, s.ServerID)
	require.Equal(t, desired, applied)
}

func TestCreateFailsWhenClusterFirewallOff(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	rec := recordSettles(t, e)
	e.App.PVE.SetClusterFirewall(false)

	plain := e.CreateServer("plain", nil)
	require.NoError(t, w.Work(ctx, job(plain, 1)))
	waitOK(t, e, plain.ActionID)

	s := createWithFirewall(t, e, "web", newFirewall(t, e, "fw", tcpIn("22")))
	err := w.Work(ctx, job(s, 1))
	var cancel *river.JobCancelError
	require.True(t, errors.As(err, &cancel), "%v", err)
	a, err := actions.Get(ctx, e.App.Ent, e.Project, s.ActionID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, actions.CodeFirewallUnenforced, a.Error.Code)
	require.Contains(t, a.Error.Detail, "datacenter firewall is disabled at this location")
	require.Empty(t, taggedVMs(t, e, s.ServerID), "vm destroyed")
	_, ok := e.App.PVE.Volume(isoVolid(s.ServerID))
	require.False(t, ok, "iso deleted")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, servers.StatusError, string(row.Status))
	require.Nil(t, row.Vmid)
	n, err := e.App.Ent.IPAllocation.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "only the plain server keeps its IP")
	waits, _ := rec.get()
	require.Empty(t, waits)
}

// TestV1ProtectionUnchanged pins the firewall a server without user firewalls has always had.
func TestV1ProtectionUnchanged(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	vm, ok := e.App.PVE.VM(*row.Vmid)
	require.True(t, ok)
	require.Equal(t, "running", vm.Status)
	require.Equal(t, map[string]string{"enable": "1", "ipfilter": "1", "macfilter": "1", "policy_in": "ACCEPT", "policy_out": "ACCEPT",
		"ndp": "1", "radv": "0", "dhcp": "0"}, vm.Firewall)
	require.Equal(t, "virtio="+row.MAC+",bridge=vmbr0,firewall=1", vm.Config["net0"])
	require.Equal(t, map[string][]string{"ipfilter-net0": {"10.0.0.10"}}, vm.IPSets)
	require.Empty(t, vm.Rules)
}

func TestStoppedCreateWaitsWithFirewall(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	rec := recordSettles(t, e)
	s := createWithFirewall(t, e, "web", newFirewall(t, e, "fw", tcpIn("22")))
	j := job(s, 1)
	j.Args.StartAfter = false
	require.NoError(t, w.Work(ctx, j))
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	require.Equal(t, -1, startCall(e, vmid))
	require.Equal(t, []string{"22"}, dports(t, e, vmid))
	waits, _ := rec.get()
	require.Equal(t, []time.Duration{servers.FirewallSettle}, waits, "a later power-on finds the rules loaded")
}

func TestFirewallChangeDuringWaitLandsBeforeStart(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	fw := fwService(e)
	id := newFirewall(t, e, "fw", tcpIn("22"))
	s := createWithFirewall(t, e, "web", id)
	var waits, at []int
	servers.SetSettleWait(t, func(context.Context, time.Duration) error {
		at = append(at, len(e.App.PVE.Calls()))
		waits = append(waits, len(waits))
		if len(waits) == 1 {
			_, err := fw.SetRules(ctx, e.Project, id, []firewalls.RuleInput{tcpIn("80")})
			require.NoError(t, err)
		}
		return nil
	})
	require.NoError(t, w.Work(ctx, job(s, 1)))
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	requireProtectedBeforeStart(t, e, vmid, "80")
	require.Len(t, waits, 2, "the new rules got their own wait")
	require.Greater(t, at[1], lastFWWrite(e, vmid), "the second wait came after the new rules")
	desired, applied := gens(t, e, s.ServerID)
	require.Equal(t, desired, applied)
}

func TestFirewallKeepsChangingDuringWaitRetries(t *testing.T) {
	ctx := context.Background()
	e, w := directEnv(t)
	fw := fwService(e)
	id := newFirewall(t, e, "fw", tcpIn("22"))
	s := createWithFirewall(t, e, "web", id)
	n := 0
	servers.SetSettleWait(t, func(context.Context, time.Duration) error {
		n++
		_, err := fw.SetRules(ctx, e.Project, id, []firewalls.RuleInput{tcpIn(strconv.Itoa(1000 + n))})
		require.NoError(t, err)
		return nil
	})
	err := w.Work(ctx, job(s, 1))
	require.Error(t, err)
	var cancel *river.JobCancelError
	require.False(t, errors.As(err, &cancel), "retried, not failed: %v", err)
	require.Equal(t, -1, startCall(e, vmidOf(t, e, s.ServerID)), "never started on stale rules")
}

func TestApplyLoopSnoozesWhenRebuildStarts(t *testing.T) {
	ctx := context.Background()
	f := newFWEnv(t)
	id, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	var once sync.Once
	f.App.PVE.OnFirewallWrite(func() {
		once.Do(func() {
			_, err := f.fw.SetRules(ctx, f.Project, id, []firewalls.RuleInput{tcpIn("80")})
			require.NoError(t, err)
			require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetRebuildStep(servers.StepClone).Exec(ctx))
		})
	})
	err := f.work(t, acts[0], 1)
	f.App.PVE.OnFirewallWrite(nil)
	require.True(t, snoozed(err), "%v", err)
	require.Equal(t, []string{"22"}, dports(t, f.ServerEnv, f.vmid), "the rebuild's VM is left to the rebuild")
	require.Equal(t, actions.StatusRunning, f.action(t, acts[0].ID).Status)
}

func TestRebuildFailsWhenClusterFirewallOff(t *testing.T) {
	ctx := context.Background()
	e, s, w := rebuildEnv(t)
	recordSettles(t, e)
	old := vmidOf(t, e, s.ServerID)
	_, acts, err := fwService(e).Create(ctx, e.Project, firewalls.CreateInput{Name: "fw", Rules: []firewalls.RuleInput{tcpIn("22")},
		ApplyTo: []firewalls.ResourceInput{onServer(s.ServerID)}})
	require.NoError(t, err)
	aw := servers.NewApplyFirewallsWorker(opts(e))
	require.NoError(t, aw.Work(ctx, jobOf(queuedArgs[firewalls.ApplyArgs](t, e, acts[0].ID), 1)))
	before, _ := e.App.PVE.VM(old)
	writes := lastFWWrite(e, old)

	e.App.PVE.SetClusterFirewall(false)
	id := startAction(t, e, s.ServerID, "rebuild", map[string]any{"image": "debian"})
	var cancel *river.JobCancelError
	require.True(t, errors.As(w.Work(ctx, jobOf(queuedArgs[servers.RebuildServerArgs](t, e, id), 1)), &cancel))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, id)
	require.NoError(t, err)
	require.Equal(t, actions.CodeFirewallUnenforced, a.Error.Code)

	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Equal(t, old, *row.Vmid, "never swapped")
	require.Nil(t, row.RebuildVmid)
	require.Equal(t, 1, vmCount(t, e), "the new VM was destroyed")
	after, ok := e.App.PVE.VM(old)
	require.True(t, ok)
	require.Equal(t, before.Rules, after.Rules)
	require.Equal(t, before.Firewall, after.Firewall)
	require.Equal(t, "running", after.Status)
	require.Equal(t, writes, lastFWWrite(e, old), "nothing written to the old VM")
	require.Equal(t, servers.StatusRunning, string(row.Status))
}
