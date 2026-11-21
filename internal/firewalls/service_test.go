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

package firewalls_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/firewall"
	"github.com/nebla-cloud/nebla/internal/ent/firewallresource"
	"github.com/nebla-cloud/nebla/internal/firewalls"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

type env struct {
	t        *testing.T
	ent      *ent.Client
	sql      *sql.DB
	svc      *firewalls.Service
	loc, st  int64
	img      int64
	macCount *atomic.Int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := testutil.NewDB(t)
	client, sqlDB := db.NewEnt(pool)
	t.Cleanup(func() { client.Close() })
	j, err := jobs.New(pool, sqlDB, nil)
	require.NoError(t, err)
	loc := client.Location.Create().SetName("eu-1").SetPveURL("https://pve.invalid").SetTokenID("t").SetTokenSecret([]byte("s")).
		SetDiskStorage("local-lvm").SetIsoStorage("local").SaveX(ctx)
	st := client.ServerType.Create().SetName("small").SetCores(1).SetMemoryMB(512).SetDiskGB(10).SaveX(ctx)
	img := client.Image.Create().SetName("ubuntu").SetOsFlavor("ubuntu").SetOsVersion("24.04").SaveX(ctx)
	return &env{t: t, ent: client, sql: sqlDB, svc: firewalls.New(firewalls.Options{Ent: client, SQL: sqlDB, Jobs: j}),
		loc: loc.ID, st: st.ID, img: img.ID, macCount: &atomic.Int64{}}
}

func (e *env) project(name string) int64 {
	return e.ent.Project.Create().SetName(name).SaveX(context.Background()).ID
}

func (e *env) server(projectID int64, name string, lbls map[string]string) int64 {
	return e.serverCreate(e.ent, projectID, name, lbls).SaveX(context.Background()).ID
}

func (e *env) serverCreate(c *ent.Client, projectID int64, name string, lbls map[string]string) *ent.ServerCreate {
	n := e.macCount.Add(1)
	if lbls == nil {
		lbls = map[string]string{}
	}
	return c.Server.Create().SetProjectID(projectID).SetName(name).SetLocationID(e.loc).SetTemplateVmid(9000).
		SetServerTypeID(e.st).SetImageID(e.img).SetMAC(fmt.Sprintf("bc:24:11:00:%02x:%02x", n>>8, n&0xff)).SetLabels(lbls).
		SetCores(1).SetMemoryMB(512).SetDiskGB(10).SetSSHKeys([]string{})
}

func (e *env) gen(id int64) int64 {
	return e.ent.Server.GetX(context.Background(), id).FirewallGenDesired
}

func ptr[T any](v T) *T { return &v }

func onServer(id int64) firewalls.ResourceInput {
	return firewalls.ResourceInput{Type: "server", ServerID: &id}
}

func bySelector(s string) firewalls.ResourceInput {
	return firewalls.ResourceInput{Type: "label_selector", Selector: &s}
}

var sshRule = firewalls.RuleInput{Direction: "in", Protocol: "tcp", Port: ptr("22"), IPs: []string{"0.0.0.0/0", "::/0"}}

func requireAPI(t *testing.T, err error, status int, code string) apierr.E {
	t.Helper()
	var ae apierr.E
	require.True(t, errors.As(err, &ae), "want apierr %s, got %v", code, err)
	require.Equal(t, status, ae.Status)
	require.Equal(t, code, ae.Code)
	return ae
}

func (e *env) create(projectID int64, name string, in firewalls.CreateInput) (firewalls.Firewall, []actions.Action) {
	e.t.Helper()
	in.Name = name
	fw, acts, err := e.svc.Create(context.Background(), projectID, in)
	require.NoError(e.t, err)
	return fw, acts
}

func (e *env) jobArgs(a actions.Action) firewalls.ApplyArgs {
	e.t.Helper()
	require.NotNil(e.t, a.RiverJobID)
	var kind string
	var raw []byte
	require.NoError(e.t, e.sql.QueryRowContext(context.Background(), "select kind, args from river_job where id=$1", *a.RiverJobID).Scan(&kind, &raw))
	require.Equal(e.t, firewalls.KindApply, kind)
	var args firewalls.ApplyArgs
	require.NoError(e.t, json.Unmarshal(raw, &args))
	return args
}

func serverIDs(acts []actions.Action) []int64 {
	var out []int64
	for _, a := range acts {
		for _, r := range a.Resources {
			if r.Type == "server" {
				out = append(out, r.ID)
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestService(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	t.Run("crud", func(t *testing.T) {
		p := e.project("crud")
		fw, acts := e.create(p, "web", firewalls.CreateInput{Labels: map[string]string{"env": "prod"},
			Rules: []firewalls.RuleInput{sshRule, {Direction: "out", Protocol: "udp", Port: ptr("53"), IPs: []string{"10.0.0.1/8"}}}})
		require.Empty(t, acts)
		require.Equal(t, "web", fw.Name)
		require.Equal(t, p, fw.ProjectID)
		require.Len(t, fw.Rules, 2)
		require.Equal(t, "10.0.0.0/8", fw.Rules[1].IPs[0].String())
		require.Empty(t, fw.AppliedTo)

		got, err := e.svc.Get(ctx, p, fw.ID)
		require.NoError(t, err)
		require.Equal(t, fw.Rules, got.Rules)
		require.Equal(t, map[string]string{"env": "prod"}, got.Labels)

		_, _, err = e.svc.Create(ctx, p, firewalls.CreateInput{Name: "web"})
		requireAPI(t, err, 409, "name_taken")
		_, _, err = e.svc.Create(ctx, p, firewalls.CreateInput{Name: "Bad Name"})
		requireAPI(t, err, 422, "invalid_input")

		other, _ := e.create(p, "db", firewalls.CreateInput{})
		_, err = e.svc.Update(ctx, p, other.ID, firewalls.UpdateInput{Name: ptr("web")})
		requireAPI(t, err, 409, "name_taken")
		up, err := e.svc.Update(ctx, p, other.ID, firewalls.UpdateInput{Name: ptr("db2"), Labels: &map[string]string{"env": "dev"}})
		require.NoError(t, err)
		require.Equal(t, "db2", up.Name)
		require.Equal(t, map[string]string{"env": "dev"}, up.Labels)

		list, total, err := e.svc.List(ctx, p, firewalls.ListFilter{}, 1, 10)
		require.NoError(t, err)
		require.Equal(t, 2, total)
		require.Equal(t, []string{"web", "db2"}, []string{list[0].Name, list[1].Name})
		list, total, err = e.svc.List(ctx, p, firewalls.ListFilter{LabelSelector: "env=dev"}, 1, 10)
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Equal(t, "db2", list[0].Name)
		list, _, err = e.svc.List(ctx, p, firewalls.ListFilter{Name: "web"}, 1, 10)
		require.NoError(t, err)
		require.Len(t, list, 1)
		_, _, err = e.svc.List(ctx, p, firewalls.ListFilter{LabelSelector: "bad key="}, 1, 10)
		requireAPI(t, err, 422, "invalid_input")
		list, total, err = e.svc.List(ctx, p, firewalls.ListFilter{}, 2, 1)
		require.NoError(t, err)
		require.Equal(t, 2, total)
		require.Equal(t, "db2", list[0].Name)

		q := e.project("crud-other")
		_, err = e.svc.Get(ctx, q, fw.ID)
		requireAPI(t, err, 404, "not_found")
		_, err = e.svc.Update(ctx, q, fw.ID, firewalls.UpdateInput{Name: ptr("x")})
		requireAPI(t, err, 404, "not_found")
		_, err = e.svc.SetRules(ctx, q, fw.ID, nil)
		requireAPI(t, err, 404, "not_found")
		_, err = e.svc.Delete(ctx, q, fw.ID)
		requireAPI(t, err, 404, "not_found")
		list, total, err = e.svc.List(ctx, q, firewalls.ListFilter{}, 1, 10)
		require.NoError(t, err)
		require.Zero(t, total)
		require.Empty(t, list)

		acts, err = e.svc.Delete(ctx, p, fw.ID)
		require.NoError(t, err)
		require.Empty(t, acts)
		_, err = e.svc.Get(ctx, p, fw.ID)
		requireAPI(t, err, 404, "not_found")
	})

	t.Run("51st firewall", func(t *testing.T) {
		p := e.project("many")
		for i := range 50 {
			e.create(p, fmt.Sprintf("fw-%d", i), firewalls.CreateInput{})
		}
		_, _, err := e.svc.Create(ctx, p, firewalls.CreateInput{Name: "fw-50"})
		requireAPI(t, err, 422, "limit_exceeded")
	})

	t.Run("apply to another project's server", func(t *testing.T) {
		p, q := e.project("own"), e.project("foreign")
		mine := e.server(p, "mine", nil)
		theirs := e.server(q, "theirs", nil)
		fw, _ := e.create(p, "fw", firewalls.CreateInput{Rules: []firewalls.RuleInput{sshRule}})
		_, err := e.svc.ApplyTo(ctx, p, fw.ID, []firewalls.ResourceInput{onServer(mine), onServer(theirs)})
		requireAPI(t, err, 404, "not_found")
		_, _, err = e.svc.Create(ctx, p, firewalls.CreateInput{Name: "fw2", ApplyTo: []firewalls.ResourceInput{onServer(theirs)}})
		requireAPI(t, err, 404, "not_found")
		_, err = e.svc.RemoveFrom(ctx, p, fw.ID, []firewalls.ResourceInput{onServer(theirs)})
		requireAPI(t, err, 404, "not_found")
		require.Zero(t, e.ent.FirewallResource.Query().Where(firewallresource.FirewallID(fw.ID)).CountX(ctx))
		require.False(t, e.ent.Firewall.Query().Where(firewall.Name("fw2")).ExistX(ctx))
		require.Zero(t, e.gen(mine))
		require.Zero(t, e.gen(theirs))
		_, err = e.svc.ApplyTo(ctx, q, fw.ID, []firewalls.ResourceInput{onServer(theirs)})
		requireAPI(t, err, 404, "not_found")
	})

	t.Run("selector stays in its project", func(t *testing.T) {
		p, q := e.project("sel-a"), e.project("sel-b")
		in := e.server(p, "web", map[string]string{"role": "web"})
		out := e.server(q, "web", map[string]string{"role": "web"})
		fw, acts := e.create(p, "fw", firewalls.CreateInput{Rules: []firewalls.RuleInput{sshRule},
			ApplyTo: []firewalls.ResourceInput{bySelector("role=web")}})
		require.Equal(t, []int64{in}, serverIDs(acts))
		require.Len(t, fw.AppliedTo, 1)
		require.Equal(t, []int64{in}, fw.AppliedTo[0].MatchedServerIDs)
		require.Equal(t, "role=web", *fw.AppliedTo[0].Selector)
		eff, err := firewalls.Effective(ctx, e.ent, out)
		require.NoError(t, err)
		require.Empty(t, eff)
		rules, anyFW, err := firewalls.RulesFor(ctx, e.ent, out)
		require.NoError(t, err)
		require.False(t, anyFW)
		require.Empty(t, rules)
		require.Zero(t, e.gen(out))
		require.EqualValues(t, 1, e.gen(in))
	})

	t.Run("set rules fans out to selector matches", func(t *testing.T) {
		p := e.project("fan")
		var ids []int64
		for i := range 3 {
			ids = append(ids, e.server(p, fmt.Sprintf("s%d", i), map[string]string{"role": "web"}))
		}
		e.server(p, "other", map[string]string{"role": "db"})
		gone := e.server(p, "gone", map[string]string{"role": "web"})
		e.ent.Server.UpdateOneID(gone).SetStatus("deleting").ExecX(ctx)
		fw, acts := e.create(p, "fw", firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{bySelector("role=web")}})
		require.Equal(t, ids, serverIDs(acts))
		before := map[int64]int64{}
		for _, id := range ids {
			before[id] = e.gen(id)
		}
		acts, err := e.svc.SetRules(ctx, p, fw.ID, []firewalls.RuleInput{sshRule})
		require.NoError(t, err)
		require.Len(t, acts, 3)
		require.Equal(t, ids, serverIDs(acts))
		for _, a := range acts {
			require.Equal(t, firewalls.KindApply, a.Command)
			require.Equal(t, actions.StatusRunning, a.Status)
			require.Contains(t, a.Resources, actions.Resource{ID: fw.ID, Type: "firewall"})
			args := e.jobArgs(a)
			require.Equal(t, a.ID, args.ActionID)
			require.Equal(t, before[args.ServerID]+1, e.gen(args.ServerID))
			require.Equal(t, e.gen(args.ServerID), args.Gen)
		}
		require.Zero(t, e.gen(gone))

		rules, anyFW, err := firewalls.RulesFor(ctx, e.ent, ids[0])
		require.NoError(t, err)
		require.True(t, anyFW)
		require.Len(t, rules, 1)
		require.Equal(t, fw.ID, rules[0].FirewallID)
		require.Equal(t, "22", *rules[0].Port)
		require.Len(t, rules[0].IPs, 2)
	})

	t.Run("sixth firewall is refused", func(t *testing.T) {
		p := e.project("limit")
		srv := e.server(p, "busy", map[string]string{"role": "web"})
		other := e.server(p, "free", map[string]string{"role": "web"})
		var fws []int64
		for i := range 4 {
			fw, _ := e.create(p, fmt.Sprintf("d%d", i), firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{onServer(srv)}})
			fws = append(fws, fw.ID)
		}
		// Direct and selector on the same firewall count once.
		fifth, _ := e.create(p, "sel", firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{onServer(srv), bySelector("role=web")}})
		eff, err := firewalls.Effective(ctx, e.ent, srv)
		require.NoError(t, err)
		require.Len(t, eff, 5)
		genBusy, genFree := e.gen(srv), e.gen(other)
		actsBefore := e.ent.Action.Query().CountX(ctx)

		sixth, _ := e.create(p, "sixth", firewalls.CreateInput{Rules: []firewalls.RuleInput{sshRule}})
		_, err = e.svc.ApplyTo(ctx, p, sixth.ID, []firewalls.ResourceInput{bySelector("role")})
		ae := requireAPI(t, err, 422, "limit_exceeded")
		require.Equal(t, "server busy would have more than 5 firewalls", ae.Msg)
		_, err = e.svc.ApplyTo(ctx, p, sixth.ID, []firewalls.ResourceInput{onServer(other), onServer(srv)})
		requireAPI(t, err, 422, "limit_exceeded")
		_, _, err = e.svc.Create(ctx, p, firewalls.CreateInput{Name: "seventh", ApplyTo: []firewalls.ResourceInput{onServer(srv)}})
		requireAPI(t, err, 422, "limit_exceeded")

		require.Zero(t, e.ent.FirewallResource.Query().Where(firewallresource.FirewallID(sixth.ID)).CountX(ctx))
		_, err = e.svc.Get(ctx, p, sixth.ID)
		require.NoError(t, err)
		list, _, err := e.svc.List(ctx, p, firewalls.ListFilter{Name: "seventh"}, 1, 10)
		require.NoError(t, err)
		require.Empty(t, list)
		require.Equal(t, genBusy, e.gen(srv))
		require.Equal(t, genFree, e.gen(other))
		require.Equal(t, actsBefore, e.ent.Action.Query().CountX(ctx))

		require.NoError(t, firewalls.CheckLimit(ctx, e.ent, p, other, map[string]string{}, fws))
		err = firewalls.CheckLimit(ctx, e.ent, p, other, map[string]string{"role": "web"}, append(fws, sixth.ID))
		requireAPI(t, err, 422, "limit_exceeded")
		require.NoError(t, firewalls.CheckLimit(ctx, e.ent, p, srv, map[string]string{"role": "web"}, []int64{fifth.ID, fws[0]}))

		// The other server can still take it.
		acts, err := e.svc.ApplyTo(ctx, p, sixth.ID, []firewalls.ResourceInput{onServer(other)})
		require.NoError(t, err)
		require.Equal(t, []int64{other}, serverIDs(acts))
	})

	t.Run("delete while applied", func(t *testing.T) {
		p := e.project("del")
		a := e.server(p, "a", map[string]string{"role": "web"})
		b := e.server(p, "b", nil)
		e.server(p, "c", nil)
		fw, _ := e.create(p, "fw", firewalls.CreateInput{Rules: []firewalls.RuleInput{sshRule},
			ApplyTo: []firewalls.ResourceInput{bySelector("role=web"), onServer(b)}})
		ga, gb := e.gen(a), e.gen(b)
		acts, err := e.svc.Delete(ctx, p, fw.ID)
		require.NoError(t, err)
		require.Equal(t, []int64{a, b}, serverIDs(acts))
		require.Equal(t, ga+1, e.gen(a))
		require.Equal(t, gb+1, e.gen(b))
		require.Zero(t, e.ent.FirewallResource.Query().Where(firewallresource.FirewallID(fw.ID)).CountX(ctx))
		_, anyFW, err := firewalls.RulesFor(ctx, e.ent, a)
		require.NoError(t, err)
		require.False(t, anyFW)
	})

	t.Run("remove from", func(t *testing.T) {
		p := e.project("rm")
		a := e.server(p, "a", map[string]string{"role": "web"})
		b := e.server(p, "b", map[string]string{"role": "web"})
		fw, _ := e.create(p, "fw", firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{bySelector("role=web"), onServer(a)}})
		acts, err := e.svc.RemoveFrom(ctx, p, fw.ID, []firewalls.ResourceInput{bySelector("role = web")})
		require.NoError(t, err)
		require.Equal(t, []int64{b}, serverIDs(acts))
		got, err := e.svc.Get(ctx, p, fw.ID)
		require.NoError(t, err)
		require.Len(t, got.AppliedTo, 1)
		require.Equal(t, a, *got.AppliedTo[0].ServerID)
		acts, err = e.svc.ApplyTo(ctx, p, fw.ID, []firewalls.ResourceInput{onServer(a)})
		require.NoError(t, err)
		require.Empty(t, acts)
	})

	t.Run("effective lists the reason", func(t *testing.T) {
		p := e.project("eff")
		srv := e.server(p, "s", map[string]string{"role": "web", "env": "dev"})
		direct, _ := e.create(p, "direct", firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{onServer(srv)}})
		sel, _ := e.create(p, "sel", firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{bySelector("env!=prod, role")}})
		e.create(p, "miss", firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{bySelector("role=db")}})
		eff, err := firewalls.Effective(ctx, e.ent, srv)
		require.NoError(t, err)
		require.Equal(t, []firewalls.Applied{
			{FirewallID: direct.ID, Name: "direct", Via: "direct"},
			{FirewallID: sel.ID, Name: "sel", Via: "env!=prod,role"},
		}, eff)
		_, err = firewalls.Effective(ctx, e.ent, 1<<40)
		requireAPI(t, err, 404, "not_found")
	})

	t.Run("concurrent direct applies to one server", func(t *testing.T) {
		p := e.project("race")
		for round := range 5 {
			srv := e.server(p, fmt.Sprintf("s%d", round), nil)
			fws := make([]int64, 2)
			for i := range fws {
				fw, _ := e.create(p, fmt.Sprintf("r%d-%d", round, i), firewalls.CreateInput{})
				fws[i] = fw.ID
			}
			start := make(chan struct{})
			errs := make([]error, len(fws))
			var wg sync.WaitGroup
			for i, id := range fws {
				wg.Go(func() {
					<-start
					_, errs[i] = e.svc.ApplyTo(ctx, p, id, []firewalls.ResourceInput{onServer(srv)})
				})
			}
			close(start)
			wg.Wait()
			for _, err := range errs {
				require.NoError(t, err)
			}
			require.EqualValues(t, 2, e.gen(srv))
		}
	})

	t.Run("apply to a deleting server", func(t *testing.T) {
		p := e.project("dying")
		srv := e.server(p, "s", nil)
		e.ent.Server.UpdateOneID(srv).SetStatus("deleting").ExecX(ctx)
		fw, _ := e.create(p, "fw", firewalls.CreateInput{})
		_, err := e.svc.ApplyTo(ctx, p, fw.ID, []firewalls.ResourceInput{onServer(srv)})
		ae := requireAPI(t, err, 409, "locked")
		require.Equal(t, "server is being deleted", ae.Msg)
		_, _, err = e.svc.Create(ctx, p, firewalls.CreateInput{Name: "fw2", ApplyTo: []firewalls.ResourceInput{onServer(srv)}})
		requireAPI(t, err, 409, "locked")
		require.Zero(t, e.ent.FirewallResource.Query().Where(firewallresource.FirewallID(fw.ID)).CountX(ctx))
	})

	t.Run("deleting a server cascades its resources", func(t *testing.T) {
		p := e.project("cascade")
		srv := e.server(p, "s", nil)
		fw, _ := e.create(p, "fw", firewalls.CreateInput{ApplyTo: []firewalls.ResourceInput{onServer(srv)}})
		require.Equal(t, 1, e.ent.FirewallResource.Query().Where(firewallresource.FirewallID(fw.ID)).CountX(ctx))
		e.ent.Server.DeleteOneID(srv).ExecX(ctx)
		require.Zero(t, e.ent.FirewallResource.Query().Where(firewallresource.FirewallID(fw.ID)).CountX(ctx))
		got, err := e.svc.Get(ctx, p, fw.ID)
		require.NoError(t, err)
		require.Empty(t, got.AppliedTo)
	})
}

func tcp(p string) firewalls.RuleInput {
	return firewalls.RuleInput{Direction: "in", Protocol: "tcp", Port: ptr(p), IPs: []string{"0.0.0.0/0", "::/0"}}
}

func ruleIDs(fw firewalls.Firewall) map[string]int64 {
	out := map[string]int64{}
	for _, r := range fw.Rules {
		out[*r.Port+"|"+r.Description] = r.ID
	}
	return out
}

func TestSetRulesKeepsUnchangedIDs(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.project("p")
	fw, _ := e.create(p, "web", firewalls.CreateInput{Rules: []firewalls.RuleInput{tcp("22"), tcp("80"), tcp("443")}})
	before := ruleIDs(fw)

	desc := tcp("80")
	desc.Description = "http"
	_, err := e.svc.SetRules(ctx, p, fw.ID, []firewalls.RuleInput{tcp("443"), desc, tcp("22"), tcp("8080")})
	require.NoError(t, err)
	got, err := e.svc.Get(ctx, p, fw.ID)
	require.NoError(t, err)
	after := ruleIDs(got)
	require.Equal(t, before["22|"], after["22|"])
	require.Equal(t, before["443|"], after["443|"])
	require.NotEqual(t, before["80|"], after["80|http"], "a changed rule is a new rule")
	require.NotContains(t, after, "80|")
	var ports []string
	for _, r := range got.Rules {
		ports = append(ports, *r.Port)
	}
	require.Equal(t, []string{"443", "80", "22", "8080"}, ports, "positions follow the new order")

	// Duplicates each keep a row of their own.
	_, err = e.svc.SetRules(ctx, p, fw.ID, []firewalls.RuleInput{tcp("22"), tcp("22")})
	require.NoError(t, err)
	got, err = e.svc.Get(ctx, p, fw.ID)
	require.NoError(t, err)
	require.Len(t, got.Rules, 2)
	require.Equal(t, after["22|"], got.Rules[0].ID)
	require.NotEqual(t, got.Rules[0].ID, got.Rules[1].ID)
}

// longV6 is n distinct IPv6 /128s of 42 characters each, so 11 fit in one 512-character Proxmox list.
func longV6(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("2001:db8:1111:2222:3333:4444:5555:%x/128", 0x1000+i)
	}
	return out
}

func TestRenderedRuleLimit(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	p := e.project("p")
	srv := e.server(p, "web", map[string]string{"role": "web"})

	// 50 rules of 50 CIDRs each split into 5 Proxmox rules: exactly the limit.
	big := make([]firewalls.RuleInput, 50)
	for i := range big {
		big[i] = firewalls.RuleInput{Direction: "in", Protocol: "tcp", Port: ptr(strconv.Itoa(1000 + i)), IPs: longV6(50)}
	}
	fw, _ := e.create(p, "big", firewalls.CreateInput{Rules: big, ApplyTo: []firewalls.ResourceInput{onServer(srv)}})
	one := []firewalls.RuleInput{{Direction: "in", Protocol: "icmp", IPs: []string{"0.0.0.0/0"}}}

	_, err := e.svc.SetRules(ctx, p, fw.ID, append(slices.Clone(big), one...))
	ae := requireAPI(t, err, 422, "limit_exceeded")
	require.Equal(t, fmt.Sprintf("server web would have more than %d firewall rules", firewalls.MaxRenderedRules), ae.Msg)

	_, _, err = e.svc.Create(ctx, p, firewalls.CreateInput{Name: "extra", Rules: one, ApplyTo: []firewalls.ResourceInput{bySelector("role=web")}})
	requireAPI(t, err, 422, "limit_exceeded")

	small, _ := e.create(p, "small", firewalls.CreateInput{Rules: one})
	_, err = e.svc.ApplyTo(ctx, p, small.ID, []firewalls.ResourceInput{onServer(srv)})
	requireAPI(t, err, 422, "limit_exceeded")
	_, err = e.svc.ApplyTo(ctx, p, small.ID, []firewalls.ResourceInput{bySelector("role=web")})
	requireAPI(t, err, 422, "limit_exceeded")
	err = firewalls.CheckLimit(ctx, e.ent, p, srv, map[string]string{}, []int64{small.ID})
	requireAPI(t, err, 422, "limit_exceeded")
	require.NoError(t, firewalls.CheckLimit(ctx, e.ent, p, srv, map[string]string{}, nil))

	// A selector cannot slip past a matching server whose creation has yet to commit.
	selector := []firewalls.ResourceInput{bySelector("role=db")}
	var concurrentErr error
	err = db.WithTx(ctx, e.sql, func(_ *sql.Tx, etx *ent.Client) error {
		if err := projects.LockProject(ctx, etx, p); err != nil {
			return err
		}
		pending, err := e.serverCreate(etx, p, "db", map[string]string{"role": "db"}).Save(ctx)
		if err != nil {
			return err
		}
		if err := etx.FirewallResource.Create().SetFirewallID(fw.ID).SetType(firewallresource.TypeServer).
			SetServerID(pending.ID).Exec(ctx); err != nil {
			return err
		}
		wait, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_, concurrentErr = e.svc.ApplyTo(wait, p, small.ID, selector)
		return nil
	})
	require.NoError(t, err)
	require.ErrorIs(t, concurrentErr, context.DeadlineExceeded)
	_, err = e.svc.ApplyTo(ctx, p, small.ID, selector)
	requireAPI(t, err, 422, "limit_exceeded")

	// Shrinking is never refused.
	_, err = e.svc.SetRules(ctx, p, fw.ID, big[:10])
	require.NoError(t, err)
	_, err = e.svc.ApplyTo(ctx, p, small.ID, []firewalls.ResourceInput{onServer(srv)})
	require.NoError(t, err)
}
