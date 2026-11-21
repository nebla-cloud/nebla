package servers_test

import (
	"context"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/ent/firewallresource"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func port(p string) *string { return &p }

func tcpIn(p string) firewalls.RuleInput {
	return firewalls.RuleInput{Direction: "in", Protocol: "tcp", Port: port(p), IPs: []string{"0.0.0.0/0", "::/0"}}
}

func onServer(id int64) firewalls.ResourceInput {
	return firewalls.ResourceInput{Type: "server", ServerID: &id}
}

func bySelector(s string) firewalls.ResourceInput {
	return firewalls.ResourceInput{Type: "label_selector", Selector: &s}
}

func fwService(e *testutil.ServerEnv) *firewalls.Service {
	return firewalls.New(firewalls.Options{SQL: e.App.SQL, Ent: e.App.Ent, Jobs: e.App.Jobs})
}

// fwEnv queues jobs without working them and has one built, running server.
type fwEnv struct {
	*testutil.ServerEnv
	fw   *firewalls.Service
	w    *servers.ApplyFirewallsWorker
	srv  int64
	vmid int
}

func newFWEnv(t *testing.T) *fwEnv {
	t.Helper()
	e, cw := directEnv(t)
	s := builtServer(t, e, cw, "web")
	return &fwEnv{ServerEnv: e, fw: fwService(e), w: servers.NewApplyFirewallsWorker(opts(e)), srv: s.ServerID, vmid: vmidOf(t, e, s.ServerID)}
}

func (f *fwEnv) create(t *testing.T, name string, rules []firewalls.RuleInput, res ...firewalls.ResourceInput) (int64, []actions.Action) {
	t.Helper()
	fw, acts, err := f.fw.Create(context.Background(), f.Project, firewalls.CreateInput{Name: name, Rules: rules, ApplyTo: res})
	require.NoError(t, err)
	return fw.ID, acts
}

func (f *fwEnv) setRules(t *testing.T, id int64, rules ...firewalls.RuleInput) actions.Action {
	t.Helper()
	acts, err := f.fw.SetRules(context.Background(), f.Project, id, rules)
	require.NoError(t, err)
	require.Len(t, acts, 1)
	return acts[0]
}

func (f *fwEnv) applyJob(t *testing.T, a actions.Action, attempt int) *river.Job[firewalls.ApplyArgs] {
	t.Helper()
	return jobOf(queuedArgs[firewalls.ApplyArgs](t, f.ServerEnv, a.ID), attempt)
}

func (f *fwEnv) work(t *testing.T, a actions.Action, attempt int) error {
	t.Helper()
	return f.w.Work(context.Background(), f.applyJob(t, a, attempt))
}

func (f *fwEnv) action(t *testing.T, id int64) actions.Action {
	t.Helper()
	a, err := actions.Get(context.Background(), f.App.Ent, f.Project, id)
	require.NoError(t, err)
	return a
}

// fwWrites counts firewall writes the fake received.
func fwWrites(e *testutil.ServerEnv) int {
	n := 0
	for _, c := range e.App.PVE.Calls() {
		if strings.Contains(c, "/firewall/") && !strings.HasPrefix(c, "GET ") {
			n++
		}
	}
	return n
}

func dports(t *testing.T, e *testutil.ServerEnv, vmid int) []string {
	t.Helper()
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	var out []string
	for _, r := range vm.Rules {
		out = append(out, r.Dport)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func policyIn(t *testing.T, e *testutil.ServerEnv, vmid int) string {
	t.Helper()
	vm, ok := e.App.PVE.VM(vmid)
	require.True(t, ok)
	return vm.Firewall["policy_in"]
}

func gens(t *testing.T, e *testutil.ServerEnv, id int64) (desired, applied int64) {
	t.Helper()
	row, err := e.App.Ent.Server.Get(context.Background(), id)
	require.NoError(t, err)
	return row.FirewallGenDesired, row.FirewallGenApplied
}

func snoozed(err error) bool {
	var se *rivertype.JobSnoozeError
	return errors.As(err, &se)
}

func cancelled(err error) bool {
	var ce *rivertype.JobCancelError
	return errors.As(err, &ce)
}

func TestApplyNeverRegressesGen(t *testing.T) {
	f := newFWEnv(t)
	id, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	require.Len(t, acts, 1)

	// A newer change lands while the first apply is writing.
	var once sync.Once
	var newer actions.Action
	var hookErr error
	f.App.PVE.OnFirewallWrite(func() {
		once.Do(func() {
			var got []actions.Action
			got, hookErr = f.fw.SetRules(context.Background(), f.Project, id, []firewalls.RuleInput{tcpIn("80")})
			if len(got) == 1 {
				newer = got[0]
			}
		})
	})
	require.NoError(t, f.work(t, acts[0], 1))
	f.App.PVE.OnFirewallWrite(nil)
	require.NoError(t, hookErr)
	require.NotZero(t, newer.ID)

	desired, applied := gens(t, f.ServerEnv, f.srv)
	require.Equal(t, int64(2), desired)
	require.Equal(t, int64(2), applied, "the apply re-read the desired gen and rendered the newer rules")
	require.Equal(t, []string{"80"}, dports(t, f.ServerEnv, f.vmid))
	require.Equal(t, actions.StatusSuccess, f.action(t, acts[0].ID).Status)

	// The newer job, and the older one run again, find the work done.
	writes := fwWrites(f.ServerEnv)
	require.NoError(t, f.work(t, newer, 1))
	require.NoError(t, f.w.Work(context.Background(), jobOf(firewalls.ApplyArgs{ActionID: newer.ID, ServerID: f.srv, Gen: 1}, 1)))
	require.Equal(t, writes, fwWrites(f.ServerEnv))
	require.Equal(t, actions.StatusSuccess, f.action(t, newer.ID).Status)
	_, applied = gens(t, f.ServerEnv, f.srv)
	require.Equal(t, int64(2), applied)
	require.Equal(t, []string{"80"}, dports(t, f.ServerEnv, f.vmid))
}

func TestApplyVMGoneWhileDeleting(t *testing.T) {
	f := newFWEnv(t)
	_, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	servers.SetApplyBeforeApply(f.w, func() {
		require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetStatus(servers.StatusDeleting).Exec(context.Background()))
		f.App.PVE.DeleteVM(f.vmid)
	})
	writes := fwWrites(f.ServerEnv)
	require.NoError(t, f.work(t, acts[0], 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, acts[0].ID).Status)
	require.Equal(t, writes, fwWrites(f.ServerEnv), "nothing written to a VM that is gone")
}

func TestApplyVMGoneOtherwiseRetries(t *testing.T) {
	f := newFWEnv(t)
	_, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	servers.SetApplyBeforeApply(f.w, func() { f.App.PVE.DeleteVM(f.vmid) })
	writes := fwWrites(f.ServerEnv)

	err := f.work(t, acts[0], 1)
	require.Error(t, err)
	require.False(t, cancelled(err), "a missing VM is retried: %v", err)
	require.Equal(t, actions.StatusRunning, f.action(t, acts[0].ID).Status)

	err = f.work(t, acts[0], servers.MaxAttempts)
	require.True(t, cancelled(err), "%v", err)
	a := f.action(t, acts[0].ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, actions.CodeServerMissing, a.Error.Code)
	require.Equal(t, writes, fwWrites(f.ServerEnv))
	_, applied := gens(t, f.ServerEnv, f.srv)
	require.Zero(t, applied)
}

func TestApplyAfterSwapTargetsNewVM(t *testing.T) {
	f := newFWEnv(t)
	_, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	const newVMID = 4242
	servers.SetApplyBeforeLease(f.w, func() {
		f.App.PVE.AddVM("pve1", newVMID, "web")
		f.App.PVE.SetVMConfig(newVMID, "tags", "nebla;"+tag(f.srv))
		require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetVmid(newVMID).Exec(context.Background()))
	})
	require.NoError(t, f.work(t, acts[0], 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, acts[0].ID).Status)
	require.Equal(t, []string{"22"}, dports(t, f.ServerEnv, newVMID))
	require.Equal(t, "DROP", policyIn(t, f.ServerEnv, newVMID))
	require.Empty(t, dports(t, f.ServerEnv, f.vmid), "the old VM is left alone")
}

func TestApplyReclaimsExpiredLease(t *testing.T) {
	ctx := context.Background()
	f := newFWEnv(t)
	_, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))

	require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetFirewallLeaseUntil(time.Now().Add(time.Minute)).Exec(ctx))
	err := f.work(t, acts[0], 1)
	require.True(t, snoozed(err), "a live lease makes the apply wait: %v", err)
	require.Equal(t, actions.StatusRunning, f.action(t, acts[0].ID).Status)

	// The crashed apply's lease has expired: the next apply takes it over.
	require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetFirewallLeaseUntil(time.Now().Add(-time.Minute)).Exec(ctx))
	require.NoError(t, f.work(t, acts[0], 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, acts[0].ID).Status)
	require.Equal(t, []string{"22"}, dports(t, f.ServerEnv, f.vmid))
	row, err := f.App.Ent.Server.Get(ctx, f.srv)
	require.NoError(t, err)
	require.Nil(t, row.FirewallLeaseUntil, "the lease is released")
}

func TestApplySnoozesDuringRebuild(t *testing.T) {
	ctx := context.Background()
	f := newFWEnv(t)
	_, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	writes := fwWrites(f.ServerEnv)

	require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetRebuildStep(servers.StepClone).Exec(ctx))
	require.True(t, snoozed(f.work(t, acts[0], 1)))
	require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetRebuildStep(0).SetCreateStep(servers.StepClone).Exec(ctx))
	require.True(t, snoozed(f.work(t, acts[0], 1)), "a create still building waits too")
	require.Equal(t, writes, fwWrites(f.ServerEnv))
	require.Equal(t, actions.StatusRunning, f.action(t, acts[0].ID).Status)

	require.NoError(t, f.App.Ent.Server.UpdateOneID(f.srv).SetCreateStep(servers.StepDone).Exec(ctx))
	require.NoError(t, f.work(t, acts[0], 1))
	require.Equal(t, actions.StatusSuccess, f.action(t, acts[0].ID).Status)
	require.Equal(t, []string{"22"}, dports(t, f.ServerEnv, f.vmid))
}

func TestApplyClusterFirewallOff(t *testing.T) {
	f := newFWEnv(t)
	_, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	f.App.PVE.SetClusterFirewall(false)
	writes := fwWrites(f.ServerEnv)

	require.True(t, cancelled(f.work(t, acts[0], 1)))
	a := f.action(t, acts[0].ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Equal(t, "firewall_unenforced", a.Error.Code)
	require.Equal(t, "Firewall rules couldn't be enforced at this location; contact the administrator.", a.Error.Message)
	require.Contains(t, a.Error.Detail, "datacenter firewall is disabled at this location")
	require.Equal(t, writes, fwWrites(f.ServerEnv))
}

func TestApplyStoredRuleDiffersFailsOnce(t *testing.T) {
	f := newFWEnv(t)
	f.App.PVE.OnRuleAdd(func(r *pve.FirewallRule) { r.Proto = strings.ToUpper(r.Proto) })
	_, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))

	require.True(t, cancelled(f.work(t, acts[0], 1)), "no retry when Proxmox stores a rule differently")
	a := f.action(t, acts[0].ID)
	require.Equal(t, actions.StatusError, a.Status)
	require.Contains(t, a.Error.Detail, `proto is "TCP", want "tcp"`)
	require.Empty(t, dports(t, f.ServerEnv, f.vmid), "mismatched copies are removed")
}

func TestApplyCoalesces(t *testing.T) {
	f := newFWEnv(t)
	id, acts := f.create(t, "fw", []firewalls.RuleInput{tcpIn("22")}, onServer(f.srv))
	require.NoError(t, f.work(t, acts[0], 1))
	syncs := func() int {
		// Each sync reads the options exactly twice: before setting them and to verify.
		return countCalls(f.ServerEnv, "GET /nodes/pve1/qemu/"+strconv.Itoa(f.vmid)+"/firewall/options") / 2
	}
	first := syncs()

	quick := []actions.Action{f.setRules(t, id, tcpIn("80")), f.setRules(t, id, tcpIn("443")), f.setRules(t, id, tcpIn("8080"))}
	for _, a := range quick {
		require.NoError(t, f.work(t, a, 1))
		require.Equal(t, actions.StatusSuccess, f.action(t, a.ID).Status)
	}
	require.LessOrEqual(t, syncs()-first, 1, "quick changes collapse into one sync")
	require.Equal(t, []string{"8080"}, dports(t, f.ServerEnv, f.vmid))
	desired, applied := gens(t, f.ServerEnv, f.srv)
	require.Equal(t, desired, applied)
}

func applyActions(t *testing.T, e *testutil.ServerEnv, serverID int64) []int64 {
	t.Helper()
	ids, err := e.App.Ent.Action.Query().Where(servers.ActionsOf(e.Project, serverID), action.Command(firewalls.KindApply)).
		Order(action.ByID()).IDs(context.Background())
	require.NoError(t, err)
	return ids
}

func TestApplyFirewallsWhileServerBusy(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	fw := fwService(e)

	// A slow power action is running on the server.
	e.App.PVE.SetTaskDelay(time.Hour)
	power := startAction(t, e, s.ServerID, "reboot", nil)
	f, acts, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "fw", Rules: []firewalls.RuleInput{tcpIn("22")},
		ApplyTo: []firewalls.ResourceInput{onServer(s.ServerID)}})
	require.NoError(t, err)
	require.Len(t, acts, 1)
	waitOK(t, e, acts[0].ID)
	require.Equal(t, []string{"22"}, dports(t, e, vmid))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, power)
	require.NoError(t, err)
	require.Equal(t, actions.StatusRunning, a.Status, "the apply did not wait for the power action")
	e.App.PVE.SetTaskDelay(0)
	e.App.PVE.FinishTasks()
	waitOK(t, e, power)

	// While an apply runs, power is still accepted.
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.App.PVE.OnFirewallWrite(func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	})
	acts, err = fw.SetRules(ctx, e.Project, f.ID, []firewalls.RuleInput{tcpIn("80")})
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(testWait):
		t.Fatal("apply never wrote")
	}
	power = startAction(t, e, s.ServerID, "poweroff", nil)
	close(release)
	waitOK(t, e, acts[0].ID)
	waitOK(t, e, power)
	require.Equal(t, []string{"80"}, dports(t, e, vmid))
}

func TestLabelChangeFanOut(t *testing.T) {
	ctx := context.Background()
	e := workerEnv(t, nil)
	s := e.CreateServer("web", nil)
	waitOK(t, e, s.ActionID)
	vmid := vmidOf(t, e, s.ServerID)
	_, acts, err := fwService(e).Create(ctx, e.Project, firewalls.CreateInput{Name: "fw", Rules: []firewalls.RuleInput{tcpIn("22")},
		ApplyTo: []firewalls.ResourceInput{bySelector("role=web")}})
	require.NoError(t, err)
	require.Empty(t, acts)

	res := e.C.Send(http.MethodPut, spath(s.ServerID), map[string]any{"labels": map[string]string{"role": "web"}})
	require.Equal(t, http.StatusOK, res.StatusCode, body(t, res))
	ids := applyActions(t, e, s.ServerID)
	require.Len(t, ids, 1)
	waitOK(t, e, ids[0])
	require.Equal(t, []string{"22"}, dports(t, e, vmid))
	require.Equal(t, "DROP", policyIn(t, e, vmid))

	// A rename leaves the firewall alone.
	res = e.C.Send(http.MethodPut, spath(s.ServerID), map[string]any{"name": "web2"})
	require.Equal(t, http.StatusOK, res.StatusCode, body(t, res))
	require.Len(t, applyActions(t, e, s.ServerID), 1)

	res = e.C.Send(http.MethodPut, spath(s.ServerID), map[string]any{"labels": map[string]string{}})
	require.Equal(t, http.StatusOK, res.StatusCode, body(t, res))
	ids = applyActions(t, e, s.ServerID)
	require.Len(t, ids, 2)
	waitOK(t, e, ids[1])
	require.Empty(t, dports(t, e, vmid))
	require.Equal(t, "ACCEPT", policyIn(t, e, vmid))
}

func TestLabelChangeOverLimit(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	settle(t, e, s, servers.StatusRunning)
	fw := fwService(e)
	for i := range firewalls.MaxEffective {
		_, _, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "d" + strconv.Itoa(i),
			ApplyTo: []firewalls.ResourceInput{onServer(s.ServerID)}})
		require.NoError(t, err)
	}
	_, _, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "sel", ApplyTo: []firewalls.ResourceInput{bySelector("role=web")}})
	require.NoError(t, err)
	before := len(applyActions(t, e, s.ServerID))

	res := e.C.Send(http.MethodPut, spath(s.ServerID), map[string]any{"labels": map[string]string{"role": "web"}})
	expectErr(t, res, http.StatusUnprocessableEntity, "limit_exceeded")
	row, err := e.App.Ent.Server.Get(ctx, s.ServerID)
	require.NoError(t, err)
	require.Empty(t, row.Labels)
	require.Equal(t, before, len(applyActions(t, e, s.ServerID)))
}

func serverService(e *testutil.ServerEnv) *servers.Service {
	return servers.New(servers.Options{SQL: e.App.SQL, Ent: e.App.Ent, Catalog: e.App.Catalog,
		Projects: projects.New(e.App.Ent, projects.Options{}), Jobs: e.App.Jobs})
}

func TestCreateWithFirewalls(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fw := fwService(e)
	f, _, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "fw", Rules: []firewalls.RuleInput{tcpIn("22")}})
	require.NoError(t, err)
	svc := serverService(e)
	in := servers.CreateInput{Name: "web", ServerType: "small", Image: "ubuntu", Location: "eu-1", IPv4: true, Firewalls: []int64{f.ID}}
	srv, _, err := svc.Create(ctx, e.Project, in)
	require.NoError(t, err)
	n, err := e.App.Ent.FirewallResource.Query().Where(firewallresource.FirewallID(f.ID), firewallresource.ServerID(srv.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []firewalls.Applied{{FirewallID: f.ID, Name: "fw", Via: firewalls.ViaDirect}}, srv.Firewalls)
	desired, applied := gens(t, e, srv.ID)
	require.Zero(t, desired, "the build applies the firewall; nothing is queued")
	require.Zero(t, applied)
	require.Empty(t, applyActions(t, e, srv.ID))

	// Another project's firewall is not found, and nothing is created.
	other := e.App.Ent.Project.Create().SetName("other").SaveX(ctx).ID
	g, _, err := fw.Create(ctx, other, firewalls.CreateInput{Name: "theirs"})
	require.NoError(t, err)
	in.Name, in.Firewalls = "web2", []int64{f.ID, g.ID}
	_, _, err = svc.Create(ctx, e.Project, in)
	var ae apierr.E
	require.ErrorAs(t, err, &ae)
	require.Equal(t, http.StatusNotFound, ae.Status)
	exists, err := e.App.Ent.Server.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, exists)
}

// TestApplyToRacesLabelUpdate races a direct attach against a label change on the same server.
func TestApplyToRacesLabelUpdate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	settle(t, e, s, servers.StatusRunning)
	fw := fwService(e)
	svc := serverService(e)
	_, _, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "sel", ApplyTo: []firewalls.ResourceInput{bySelector("r")}})
	require.NoError(t, err)
	for i := range 12 {
		f, _, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "d" + strconv.Itoa(i)})
		require.NoError(t, err)
		lbls := map[string]string{}
		if i%2 == 0 {
			lbls["r"] = strconv.Itoa(i)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, errs[0] = fw.ApplyTo(ctx, e.Project, f.ID, []firewalls.ResourceInput{onServer(s.ServerID)})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errs[1] = svc.Update(ctx, e.Project, s.ServerID, servers.UpdateInput{Labels: &lbls})
		}()
		close(start)
		wg.Wait()
		require.NoError(t, errors.Join(errs...), "round %d", i)
		_, err = fw.RemoveFrom(ctx, e.Project, f.ID, []firewalls.ResourceInput{onServer(s.ServerID)})
		require.NoError(t, err)
	}
	desired, _ := gens(t, e, s.ServerID)
	require.Equal(t, int64(12*3), desired, "every attach, label change and detach bumped the server")
}

func TestCreateWithLabelsOverLimit(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fw := fwService(e)
	for i := range firewalls.MaxEffective + 1 {
		_, _, err := fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "s" + strconv.Itoa(i),
			ApplyTo: []firewalls.ResourceInput{bySelector("role=web")}})
		require.NoError(t, err)
	}
	_, _, err := serverService(e).Create(ctx, e.Project, servers.CreateInput{Name: "web", ServerType: "small", Image: "ubuntu",
		Location: "eu-1", Labels: map[string]string{"role": "web"}})
	var ae apierr.E
	require.ErrorAs(t, err, &ae)
	require.Equal(t, http.StatusUnprocessableEntity, ae.Status)
	require.Equal(t, "limit_exceeded", ae.Code)
	n, err := e.App.Ent.Server.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestFailedCreateGetsNoApply(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	f, acts, err := fwService(e).Create(ctx, e.Project, firewalls.CreateInput{Name: "fw", Rules: []firewalls.RuleInput{tcpIn("22")},
		ApplyTo: []firewalls.ResourceInput{onServer(s.ServerID)}})
	require.NoError(t, err)
	require.Len(t, acts, 1, "a server still building is bumped")

	// The create fails: no build will ever resume.
	require.NoError(t, e.App.Ent.Server.UpdateOneID(s.ServerID).SetStatus(servers.StatusError).SetCreateStep(servers.StepClone).Exec(ctx))
	got, err := fwService(e).SetRules(ctx, e.Project, f.ID, []firewalls.RuleInput{tcpIn("80")})
	require.NoError(t, err)
	require.Empty(t, got)
	require.Len(t, applyActions(t, e, s.ServerID), 1)

	w := servers.NewApplyFirewallsWorker(opts(e))
	job := jobOf(queuedArgs[firewalls.ApplyArgs](t, e, acts[0].ID), 1)
	writes := fwWrites(e)
	require.NoError(t, w.Work(ctx, job))
	a, err := actions.Get(ctx, e.App.Ent, e.Project, acts[0].ID)
	require.NoError(t, err)
	require.Equal(t, actions.StatusSuccess, a.Status)
	require.Equal(t, writes, fwWrites(e))
}

// TestSelectorApplyRacesLabelUpdate races a selector attach or firewall create against the label change that makes the server match it.
func TestSelectorApplyRacesLabelUpdate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	settle(t, e, s, servers.StatusRunning)
	fw := fwService(e)
	svc := serverService(e)
	for i := range 12 {
		v := strconv.Itoa(i)
		sel := []firewalls.ResourceInput{bySelector("x=" + v)}
		// Odd rounds attach through firewalls.Create, even ones through ApplyTo.
		var f firewalls.Firewall
		var err error
		if i%2 == 0 {
			f, _, err = fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "s" + v})
			require.NoError(t, err)
		}
		lbls := map[string]string{"x": v}
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				_, errs[0] = fw.ApplyTo(ctx, e.Project, f.ID, sel)
			} else {
				f, _, errs[0] = fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "s" + v, ApplyTo: sel})
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errs[1] = svc.Update(ctx, e.Project, s.ServerID, servers.UpdateInput{Labels: &lbls})
		}()
		close(start)
		wg.Wait()
		require.NoError(t, errors.Join(errs...), "round %d", i)
		n, err := e.App.Ent.Action.Query().Where(servers.ActionsOf(e.Project, s.ServerID), action.Command(firewalls.KindApply),
			func(sel *entsql.Selector) {
				sel.Where(entsql.P(func(b *entsql.Builder) {
					b.Ident(sel.C(action.FieldResources)).WriteString(" @> ").
						Arg(`[{"id":` + strconv.FormatInt(f.ID, 10) + `,"type":"firewall"}]`).WriteString("::jsonb")
				}))
			}).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, n, "round %d: the server was bumped for the new firewall exactly once", i)
	}
}

// TestFirewallCreateRacesPower races a firewall create applied to S against a power action on S.
func TestFirewallCreateRacesPower(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	s := e.CreateServer("web", nil)
	settle(t, e, s, servers.StatusRunning)
	fw := fwService(e)
	svc := serverService(e)
	for i := range 12 {
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		var power actions.Action
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, errs[0] = fw.Create(ctx, e.Project, firewalls.CreateInput{Name: "p" + strconv.Itoa(i),
				ApplyTo: []firewalls.ResourceInput{onServer(s.ServerID)}})
		}()
		go func() {
			defer wg.Done()
			<-start
			power, errs[1] = svc.Power(ctx, e.Project, s.ServerID, pve.PowerReboot)
		}()
		close(start)
		wg.Wait()
		require.NoError(t, errors.Join(errs...), "round %d", i)
		require.NoError(t, actions.Succeed(ctx, e.App.Ent, power.ID))
		_, err := fw.Delete(ctx, e.Project, mustFirewall(t, fw, e.Project, "p"+strconv.Itoa(i)))
		require.NoError(t, err)
	}
}

func mustFirewall(t *testing.T, fw *firewalls.Service, projectID int64, name string) int64 {
	t.Helper()
	list, _, err := fw.List(context.Background(), projectID, firewalls.ListFilter{Name: name}, 1, 1)
	require.NoError(t, err)
	require.Len(t, list, 1)
	return list[0].ID
}

// TestLabelUpdateRacesChangeType races a label change on S against a change type on S.
func TestLabelUpdateRacesChangeType(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.Admin.CreateServerType("medium", 2, 2048, 20)
	s := e.CreateServer("web", nil)
	settle(t, e, s, servers.StatusOff)
	_, _, err := fwService(e).Create(ctx, e.Project, firewalls.CreateInput{Name: "sel", ApplyTo: []firewalls.ResourceInput{bySelector("r")}})
	require.NoError(t, err)
	svc := serverService(e)
	for i := range 12 {
		lbls := map[string]string{}
		if i%2 == 0 {
			lbls["r"] = strconv.Itoa(i)
		}
		typ := []string{"medium", "small"}[i%2]
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		var change actions.Action
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, errs[0] = svc.Update(ctx, e.Project, s.ServerID, servers.UpdateInput{Labels: &lbls})
		}()
		go func() {
			defer wg.Done()
			<-start
			change, errs[1] = svc.ChangeType(ctx, e.Project, s.ServerID, typ, false)
		}()
		close(start)
		wg.Wait()
		for _, err := range errs {
			var ae apierr.E
			if err != nil && !(errors.As(err, &ae) && ae.Status == http.StatusConflict && ae.Code == "locked") {
				t.Fatalf("round %d: %v", i, err)
			}
		}
		if errs[1] == nil {
			require.NoError(t, actions.Succeed(ctx, e.App.Ent, change.ID))
		}
	}
}
