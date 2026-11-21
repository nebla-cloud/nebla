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

package osimages_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/catalog"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/catalogbuild"
	"github.com/nebla-cloud/nebla/internal/ent/image"
	"github.com/nebla-cloud/nebla/internal/ent/imagetemplate"
	"github.com/nebla-cloud/nebla/internal/httpapi"
	"github.com/nebla-cloud/nebla/internal/osimages"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

// waitSettled polls until the row leaves building and returns it.
func (e *env) waitSettled(id int64) *ent.CatalogBuild {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		r, err := e.ent.CatalogBuild.Get(context.Background(), id)
		require.NoError(e.t, err)
		if r.Status != catalogbuild.StatusBuilding {
			return r
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("build %d still building at step %d", id, r.Step)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *env) enable(key string, loc int64) []*ent.CatalogBuild {
	e.t.Helper()
	_, err := e.svc.Enable(context.Background(), key, loc)
	require.NoError(e.t, err)
	var out []*ent.CatalogBuild
	for _, r := range e.rows(key, loc) {
		out = append(out, e.waitSettled(r.ID))
	}
	return out
}

func (e *env) refresh(key string, loc int64) []*ent.CatalogBuild {
	e.t.Helper()
	_, err := e.svc.Refresh(context.Background(), key, loc)
	require.NoError(e.t, err)
	var out []*ent.CatalogBuild
	for _, r := range e.rows(key, loc) {
		out = append(out, e.waitSettled(r.ID))
	}
	return out
}

func requireReady(t *testing.T, r *ent.CatalogBuild) {
	t.Helper()
	msg := ""
	if r.ErrorMessage != nil {
		msg = *r.ErrorMessage
	}
	require.Equal(t, catalogbuild.StatusReady, r.Status, msg)
	require.NotNil(t, r.Vmid)
	require.NotNil(t, r.Nonce)
	require.Nil(t, r.NewVmid)
	require.Nil(t, r.NewNonce)
	require.Nil(t, r.NewFile)
	require.Nil(t, r.NewChecksum)
	require.Nil(t, r.ErrorCode)
	require.NotNil(t, r.BuiltAt)
}

// tplVMs lists fake VMs carrying the catalog template tag.
func (e *env) tplVMs() []int {
	e.t.Helper()
	var out []int
	for id := 100; id < 10000; id++ {
		if v, ok := e.pve.VM(id); ok && strings.Contains(v.Tags, osimages.TagTemplate) {
			out = append(out, id)
		}
	}
	return out
}

func importVolume(r *ent.CatalogBuild, storage string) string {
	return fmt.Sprintf("%s:import/nebla-%s-%s-%d.qcow2", storage, r.CatalogKey, (*r.Checksum)[:12], r.ID)
}

func (e *env) setSum(dir, sum string) {
	e.up.mu.Lock()
	defer e.up.mu.Unlock()
	e.up.sums[dir] = sum
}

func TestBuildHappyPathShared(t *testing.T) {
	ctx := context.Background()
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	rows := e.enable("debian-13", loc)
	require.Len(t, rows, 1)
	r := rows[0]
	requireReady(t, r)
	require.Equal(t, "", r.Node)
	require.Equal(t, "debian.qcow2", *r.File)
	require.Equal(t, debianSum, *r.Checksum)

	img, err := e.ent.Image.Query().Where(image.CatalogKey("debian-13")).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "debian-13", img.Name)
	tpls, err := e.ent.ImageTemplate.Query().Where(imagetemplate.ImageID(img.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, tpls, 1)
	require.Equal(t, "", tpls[0].Node)
	require.Equal(t, loc, tpls[0].LocationID)
	require.Equal(t, *r.Vmid, tpls[0].Vmid)

	vm, ok := e.pve.VM(*r.Vmid)
	require.True(t, ok)
	require.True(t, vm.Template)
	tags := servers.SplitTags(vm.Tags)
	require.Contains(t, tags, "nebla-tpl")
	require.Contains(t, tags, osimages.TemplateTag(r.ID, *r.Nonce))
	require.NotContains(t, tags, "nebla")
	require.Equal(t, "0", vm.Config["agent"])
	require.Equal(t, "nebla-debian-13", vm.Name)
	require.Equal(t, "l26", vm.Config["ostype"])
	require.Equal(t, "order=scsi0", vm.Config["boot"])
	require.Empty(t, vm.Config["cpu"])

	_, ok = e.pve.Volume(importVolume(r, "imp"))
	require.False(t, ok)
	dl := e.pve.Downloads()
	require.Len(t, dl, 1)
	require.Equal(t, debianSum, dl[0].Checksum)
	require.Equal(t, "sha512", dl[0].Algorithm)
	require.Equal(t, "import", dl[0].Content)
	require.Equal(t, fmt.Sprintf("nebla-debian-13-%s-%d.qcow2", debianSum[:12], r.ID), dl[0].Filename)
	require.True(t, strings.HasSuffix(dl[0].URL, "/debian/debian.qcow2"))
}

func TestBuildSetsEntryCPU(t *testing.T) {
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	r := e.enable("alma-10", loc)[0]
	requireReady(t, r)
	vm, ok := e.pve.VM(*r.Vmid)
	require.True(t, ok)
	require.Equal(t, "x86-64-v3", vm.Config["cpu"])
}

func TestPerNodeBuildsSharedImportStorage(t *testing.T) {
	ctx := context.Background()
	e := runEnv(t)
	for _, n := range []string{"pve1", "pve2"} {
		e.pve.AddStorage(n, "nfs", []string{"import"}, true)
	}
	loc := e.location("eu-1", "local-lvm", "nfs")
	rows := e.enable("debian-13", loc)
	require.Len(t, rows, 2)
	for i, r := range rows {
		requireReady(t, r)
		require.Equal(t, []string{"pve1", "pve2"}[i], r.Node)
		vm, ok := e.pve.VM(*r.Vmid)
		require.True(t, ok)
		require.Equal(t, r.Node, vm.Node)
		_, ok = e.pve.Volume(importVolume(r, "nfs"))
		require.False(t, ok)
	}
	dl := e.pve.Downloads()
	require.Len(t, dl, 2)
	require.NotEqual(t, dl[0].Filename, dl[1].Filename)
	n, err := e.ent.Image.Query().Where(image.CatalogKey("debian-13")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	tpls, err := e.ent.ImageTemplate.Query().Where(imagetemplate.LocationID(loc)).Order(imagetemplate.ByNode()).All(ctx)
	require.NoError(t, err)
	require.Len(t, tpls, 2)
	require.Equal(t, []string{"pve1", "pve2"}, []string{tpls[0].Node, tpls[1].Node})
}

func TestBuildVMIDTakenByServerClone(t *testing.T) {
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	var once sync.Once
	taken := 0
	osimages.SetAfterStep(e.svc, func(id int64, step int) {
		if step != osimages.StepAllocate {
			return
		}
		once.Do(func() {
			r, err := e.ent.CatalogBuild.Get(context.Background(), id)
			require.NoError(t, err)
			taken = *r.NewVmid
			e.pve.AddVM("pve1", taken, "srv")
			e.pve.SetVMConfig(taken, "tags", fmt.Sprintf("nebla-tpl;nebla-tpl-%d-other", id))
		})
	})
	r := e.enable("debian-13", loc)[0]
	requireReady(t, r)
	require.NotZero(t, taken)
	require.NotEqual(t, taken, *r.Vmid)
	vm, ok := e.pve.VM(taken)
	require.True(t, ok)
	require.False(t, vm.Template)
	require.Equal(t, "srv", vm.Name)
	require.Equal(t, fmt.Sprintf("nebla-tpl;nebla-tpl-%d-other", r.ID), vm.Tags)
	require.Len(t, e.pve.Downloads(), 1)
}

func TestBuildResumesAfterCrash(t *testing.T) {
	// The fault hits the first Proxmox call after the step is saved, so the next attempt resumes from it.
	next := map[int]string{
		osimages.StepResolve:  ".qcow2",
		osimages.StepDownload: "/cluster/nextid",
		osimages.StepAllocate: "POST /nodes/pve1/qemu",
		osimages.StepCreate:   "/template",
		osimages.StepTemplate: ".qcow2",
	}
	for step := osimages.StepResolve; step <= osimages.StepTemplate; step++ {
		t.Run(fmt.Sprint(step), func(t *testing.T) {
			e := runEnv(t)
			loc := e.location("eu-1", "ceph", "imp")
			var once sync.Once
			osimages.SetAfterStep(e.svc, func(_ int64, s int) {
				if s == step {
					once.Do(func() { e.pve.FailNext(next[step], http.StatusServiceUnavailable) })
				}
			})
			r := e.enable("debian-13", loc)[0]
			requireReady(t, r)
			var attempt int
			require.NoError(t, e.sql.QueryRow(`select attempt from river_job where id = $1`, *r.RiverJobID).Scan(&attempt))
			require.Equal(t, 2, attempt)
			require.Equal(t, []int{*r.Vmid}, e.tplVMs())
			vm, _ := e.pve.VM(*r.Vmid)
			require.True(t, vm.Template)
			require.Len(t, e.pve.Downloads(), 1)
			_, ok := e.pve.Volume(importVolume(r, "imp"))
			require.False(t, ok)
		})
	}
}

func TestRefreshFailureKeepsOldTemplate(t *testing.T) {
	ctx := context.Background()
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	first := e.enable("debian-13", loc)[0]
	requireReady(t, first)
	a := *first.Vmid

	e.setSum("debian", strings.Repeat("ef56", 32))
	e.pve.FailDownload("debian.qcow2")
	r := e.refresh("debian-13", loc)[0]
	require.Equal(t, catalogbuild.StatusFailed, r.Status)
	require.Equal(t, "download_failed", *r.ErrorCode)
	require.NotContains(t, *r.ErrorMessage, "http")
	require.Equal(t, a, *r.Vmid)
	require.Equal(t, *first.Nonce, *r.Nonce)
	require.Equal(t, debianSum, *r.Checksum)
	require.Nil(t, r.NewVmid)
	require.Nil(t, r.NewNonce)
	require.Nil(t, r.NewFile)
	require.Nil(t, r.NewChecksum)

	tpl, err := e.ent.ImageTemplate.Query().Where(imagetemplate.LocationID(loc)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, a, tpl.Vmid)
	require.Equal(t, []int{a}, e.tplVMs())
	require.Empty(t, e.jobArgs(osimages.KindRetire))
}

func TestBuildFailureCleansPartialVM(t *testing.T) {
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	// A non-transient failure of the template task ends the build.
	osimages.SetAfterStep(e.svc, func(_ int64, s int) {
		if s == osimages.StepCreate {
			e.pve.FailNext("/template", http.StatusInternalServerError)
		}
	})
	r := e.enable("debian-13", loc)[0]
	require.Equal(t, catalogbuild.StatusFailed, r.Status)
	require.Equal(t, "build_failed", *r.ErrorCode)
	require.Nil(t, r.Vmid)
	require.Nil(t, r.NewVmid)
	require.Empty(t, e.tplVMs())
	_, ok := e.pve.VM(testutil.TemplateVMID)
	require.True(t, ok)
	for _, d := range e.pve.Downloads() {
		_, ok := e.pve.Volume("imp:import/" + d.Filename)
		require.False(t, ok)
	}
}

func TestRefreshUnchangedChecksumNoop(t *testing.T) {
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	first := e.enable("debian-13", loc)[0]
	requireReady(t, first)
	r := e.refresh("debian-13", loc)[0]
	require.Equal(t, catalogbuild.StatusReady, r.Status)
	require.Equal(t, *first.Vmid, *r.Vmid)
	require.Equal(t, *first.Nonce, *r.Nonce)
	require.Len(t, e.pve.Downloads(), 1)
	require.Equal(t, []int{*first.Vmid}, e.tplVMs())
	require.Empty(t, e.jobArgs(osimages.KindRetire))
}

func TestRefreshSwapsAndRetires(t *testing.T) {
	defer osimages.SetRetireDelay(100 * time.Millisecond)()
	ctx := context.Background()
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	first := e.enable("debian-13", loc)[0]
	requireReady(t, first)

	newSum := strings.Repeat("ef56", 32)
	e.setSum("debian", newSum)
	r := e.refresh("debian-13", loc)[0]
	requireReady(t, r)
	require.NotEqual(t, *first.Vmid, *r.Vmid)
	require.NotEqual(t, *first.Nonce, *r.Nonce)
	require.Equal(t, newSum, *r.Checksum)
	tpl, err := e.ent.ImageTemplate.Query().Where(imagetemplate.LocationID(loc)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, *r.Vmid, tpl.Vmid)
	require.Len(t, e.pve.Downloads(), 2)

	args := e.jobArgs(osimages.KindRetire)
	require.Len(t, args, 1)
	var a osimages.RetireArgs
	require.NoError(t, json.Unmarshal(args[0], &a))
	require.Equal(t, osimages.RetireArgs{LocationID: loc, Node: "", VMID: *first.Vmid, Tag: osimages.TemplateTag(r.ID, *first.Nonce)}, a)
	require.Eventually(t, func() bool { return slices.Equal(e.tplVMs(), []int{*r.Vmid}) }, 30*time.Second, 50*time.Millisecond)
}

func TestDownloadSnoozesPastBudget(t *testing.T) {
	defer osimages.SetWaitBudget(50 * time.Millisecond)()
	defer osimages.SetSnooze(10 * time.Millisecond)()
	e := runEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	e.pve.SetTaskDelay(300 * time.Millisecond)
	r := e.enable("debian-13", loc)[0]
	requireReady(t, r)
	var snoozes int
	require.NoError(t, e.sql.QueryRow(`select coalesce((metadata->>'snoozes')::int, 0) from river_job where id = $1`, *r.RiverJobID).Scan(&snoozes))
	require.GreaterOrEqual(t, snoozes, 1)
	require.Len(t, e.pve.Downloads(), 1)
	require.Equal(t, []int{*r.Vmid}, e.tplVMs())
}

func TestStaleBuildJobIsNoop(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	row := e.rows("debian-13", loc)[0]
	w := osimages.NewBuildWorker(e.svc)
	calls := len(e.pve.Calls())
	job := func(id int64) *river.Job[osimages.BuildArgs] {
		return &river.Job[osimages.BuildArgs]{JobRow: &rivertype.JobRow{ID: id, Attempt: 1, MaxAttempts: 5}, Args: osimages.BuildArgs{BuildID: row.ID}}
	}

	require.NoError(t, w.Work(ctx, job(*row.RiverJobID+1000)))
	after, err := e.ent.CatalogBuild.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, row.UpdatedAt, after.UpdatedAt)
	require.Equal(t, catalogbuild.StatusBuilding, after.Status)
	require.Zero(t, after.Step)

	// A ready row is left alone even by its own job.
	e.markReady(row.ID, 9101, "n1")
	require.NoError(t, w.Work(ctx, job(*row.RiverJobID)))
	after, err = e.ent.CatalogBuild.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, catalogbuild.StatusReady, after.Status)
	require.Len(t, e.pve.Calls(), calls)
	require.Empty(t, e.pve.Downloads())

	// A deleted row ends the job.
	require.NoError(t, e.ent.CatalogBuild.DeleteOneID(row.ID).Exec(ctx))
	require.NoError(t, w.Work(ctx, job(*row.RiverJobID)))
}

func TestNoTransportDetails(t *testing.T) {
	urlErr := &url.Error{Op: "Post", URL: "https://10.0.0.1:8006/api2/json/nodes/pve1/qemu", Err: errors.New("connection refused")}
	tests := []struct {
		name, step string
		err        error
		code, want string
	}{
		{"url error", "create", urlErr, "build_failed", "create: could not reach Proxmox"},
		{"wrapped url error", "download", fmt.Errorf("x: %w", urlErr), "download_failed", "download: could not reach Proxmox"},
		{"net error", "template", &net.OpError{Op: "dial", Err: errors.New("refused")}, "build_failed", "template: could not reach Proxmox"},
		{"deadline", "create", fmt.Errorf("wait: %w", context.DeadlineExceeded), "build_failed", "create: could not reach Proxmox"},
		{"task with url", "download", &pve.TaskError{UPID: "UPID:pve1:1", ExitStatus: "download 'https://example.org/x.qcow2' failed: checksum mismatch"},
			"download_failed", "download: download '[url]' failed: checksum mismatch"},
		{"pve error", "create", &pve.Error{Status: 500, Message: "storage 'x' does not exist"}, "build_failed", "create: storage 'x' does not exist"},
		{"download forbidden", "download", &pve.Error{Status: 403, Message: "Permission check failed (/, Sys.Modify)"}, "download_failed",
			"download: permission denied; the API token needs Datastore.AllocateTemplate and Datastore.Allocate on the import storage and Sys.AccessNetwork on the node"},
		{"upstream", "resolve", fmt.Errorf("%w: debian-13: request failed", osimages.ErrUpstream), "upstream_unavailable",
			"resolve: could not fetch the image checksum list from upstream"},
		{"unknown", "create", errors.New("dial tcp 10.0.0.1:8006: boom"), "build_failed", "create: unexpected error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := osimages.Failure(osimages.StepErr(tt.step, tt.err))
			require.Equal(t, tt.code, code)
			require.Equal(t, tt.want, msg)
			require.NotContains(t, msg, "10.0.0.1")
			require.NotContains(t, msg, "://")
		})
	}
}

func TestPendingCleanupDestroysOnlyProvenVM(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	row := e.rows("debian-13", loc)[0]
	require.NoError(t, row.Update().SetStep(osimages.StepCreate).SetNewVmid(7000).SetNewNonce("aa").
		SetNewFile("debian.qcow2").SetNewChecksum(debianSum).Exec(ctx))
	e.pve.AddVM("pve1", 7000, "nebla-debian-13")
	e.pve.SetVMConfig(7000, "tags", "nebla-tpl;"+osimages.TemplateTag(row.ID, "aa"))
	e.pve.AddVM("pve1", 7001, "other")
	e.pve.SetVMConfig(7001, "tags", "nebla-tpl;"+osimages.TemplateTag(row.ID, "bb"))

	job := &river.Job[osimages.BuildArgs]{JobRow: &rivertype.JobRow{ID: *row.RiverJobID, Attempt: 3, MaxAttempts: 5,
		Errors: []rivertype.AttemptError{{Error: osimages.CleanupPending + "build_failed: template: boom"}}}, Args: osimages.BuildArgs{BuildID: row.ID}}
	err = osimages.NewBuildWorker(e.svc).Work(ctx, job)
	var cancel *rivertype.JobCancelError
	require.ErrorAs(t, err, &cancel)
	_, ok := e.pve.VM(7000)
	require.False(t, ok)
	_, ok = e.pve.VM(7001)
	require.True(t, ok)
	r, err := e.ent.CatalogBuild.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, catalogbuild.StatusFailed, r.Status)
	require.Equal(t, "build_failed", *r.ErrorCode)
	require.Equal(t, "template: boom", *r.ErrorMessage)
	require.Nil(t, r.NewVmid)

	// The other nonce's VM is not this build's, so cleanup leaves it.
	require.NoError(t, r.Update().SetStatus(catalogbuild.StatusBuilding).SetNewVmid(7001).SetNewNonce("aa").Exec(ctx))
	require.ErrorAs(t, osimages.NewBuildWorker(e.svc).Work(ctx, job), &cancel)
	_, ok = e.pve.VM(7001)
	require.True(t, ok)
}

func TestExhaustedAttemptsFailRow(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	row := e.rows("debian-13", loc)[0]
	require.NoError(t, row.Update().SetStep(osimages.StepCreate).SetNewVmid(7000).SetNewNonce("aa").SetBuildNode("pve1").
		SetNewFile("debian.qcow2").SetNewChecksum(debianSum).Exec(ctx))
	e.pve.AddVM("pve1", 7000, "nebla-debian-13")
	e.pve.SetVMConfig(7000, "tags", "nebla-tpl;"+osimages.TemplateTag(row.ID, "aa"))

	job := &river.Job[osimages.BuildArgs]{JobRow: &rivertype.JobRow{ID: *row.RiverJobID, Attempt: osimages.MaxAttempts + 1,
		MaxAttempts: osimages.MaxAttempts + 2}, Args: osimages.BuildArgs{BuildID: row.ID}}
	var cancel *rivertype.JobCancelError
	require.ErrorAs(t, osimages.NewBuildWorker(e.svc).Work(ctx, job), &cancel)
	_, ok := e.pve.VM(7000)
	require.False(t, ok)
	r, err := e.ent.CatalogBuild.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, catalogbuild.StatusFailed, r.Status)
	require.Equal(t, "build_failed", *r.ErrorCode)
	require.Equal(t, "build did not finish after 5 attempts", *r.ErrorMessage)
	require.Nil(t, r.NewVmid)
	require.Nil(t, r.BuildNode)
	require.Empty(t, e.pve.Downloads())
	_, err = e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	require.Empty(t, e.jobArgs(osimages.KindCleanup))
}

func TestLastCleanupFailureKeepsHalfBuiltVM(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	loc := e.location("eu-1", "ceph", "imp")
	_, err := e.svc.Enable(ctx, "debian-13", loc)
	require.NoError(t, err)
	row := e.rows("debian-13", loc)[0]
	require.NoError(t, row.Update().SetStep(osimages.StepCreate).SetNewVmid(7000).SetNewNonce("aa").SetBuildNode("pve1").
		SetNewFile("debian.qcow2").SetNewChecksum(debianSum).Exec(ctx))
	e.pve.AddVM("pve1", 7000, "nebla-debian-13")
	e.pve.SetVMConfig(7000, "tags", "nebla-tpl;"+osimages.TemplateTag(row.ID, "aa"))
	e.pve.SetVMConfig(7000, "lock", "create")

	job := &river.Job[osimages.BuildArgs]{JobRow: &rivertype.JobRow{ID: *row.RiverJobID, Attempt: osimages.MaxAttempts + 2,
		MaxAttempts: osimages.MaxAttempts + 2}, Args: osimages.BuildArgs{BuildID: row.ID}}
	var cancel *rivertype.JobCancelError
	require.ErrorAs(t, osimages.NewBuildWorker(e.svc).Work(ctx, job), &cancel)
	_, ok := e.pve.VM(7000)
	require.True(t, ok)
	r, err := e.ent.CatalogBuild.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, catalogbuild.StatusFailed, r.Status)
	require.Equal(t, 7000, *r.NewVmid)
	require.Equal(t, "aa", *r.NewNonce)
	require.Equal(t, "pve1", *r.BuildNode)
	require.Equal(t, debianSum, *r.NewChecksum)
	require.Nil(t, r.Upid)
}

// adoptEnv puts a locked VM at the build's first vmid before create, then unlocks it as ours after delay.
func adoptEnv(t *testing.T, tagged bool, delay time.Duration) (*env, *int) {
	e := runEnv(t)
	var once sync.Once
	vmid := new(int)
	osimages.SetAfterStep(e.svc, func(id int64, step int) {
		if step != osimages.StepAllocate {
			return
		}
		once.Do(func() {
			r, err := e.ent.CatalogBuild.Get(context.Background(), id)
			require.NoError(t, err)
			*vmid = *r.NewVmid
			tag := "nebla-tpl;" + osimages.TemplateTag(id, *r.NewNonce)
			e.pve.AddVM("pve1", *vmid, "nebla-debian-13")
			e.pve.SetVMConfig(*vmid, "lock", "create")
			if tagged {
				e.pve.SetVMConfig(*vmid, "tags", tag)
			}
			time.AfterFunc(delay, func() {
				e.pve.SetVMConfig(*vmid, "tags", tag)
				e.pve.SetVMConfig(*vmid, "lock", "")
			})
		})
	})
	return e, vmid
}

// requireAdopted checks the build took over vmid; posts is how many create requests Proxmox saw, all rejected after the first.
func requireAdopted(t *testing.T, e *env, r *ent.CatalogBuild, vmid, posts int) {
	t.Helper()
	requireReady(t, r)
	require.Equal(t, vmid, *r.Vmid)
	require.Equal(t, []int{vmid}, e.tplVMs())
	vm, _ := e.pve.VM(vmid)
	require.True(t, vm.Template)
	n := 0
	for _, c := range e.pve.Calls() {
		if c == "POST /nodes/pve1/qemu" {
			n++
		}
	}
	if posts > 0 {
		require.Equal(t, posts, n)
	}
}

func TestCreateAdoptsOwnLockedVM(t *testing.T) {
	e, vmid := adoptEnv(t, true, 300*time.Millisecond)
	loc := e.location("eu-1", "ceph", "imp")
	r := e.enable("debian-13", loc)[0]
	requireAdopted(t, e, r, *vmid, 1)
}

func TestCreateWaitsOnLockedUntaggedVM(t *testing.T) {
	// Past the wait budget the job snoozes and keeps new_vmid rather than allocating again.
	defer osimages.SetWaitBudget(50 * time.Millisecond)()
	defer osimages.SetSnooze(10 * time.Millisecond)()
	e, vmid := adoptEnv(t, false, 500*time.Millisecond)
	loc := e.location("eu-1", "ceph", "imp")
	r := e.enable("debian-13", loc)[0]
	// Each resume re-posts create and gets "already exists" again.
	requireAdopted(t, e, r, *vmid, 0)
	var snoozes int
	require.NoError(t, e.sql.QueryRow(`select coalesce((metadata->>'snoozes')::int, 0) from river_job where id = $1`, *r.RiverJobID).Scan(&snoozes))
	require.GreaterOrEqual(t, snoozes, 1)
}

func TestCreateGivesUpOnLongLockedUntaggedVM(t *testing.T) {
	defer osimages.SetWaitBudget(50 * time.Millisecond)()
	defer osimages.SetSnooze(10 * time.Millisecond)()
	defer osimages.SetLockedForeignLimit(200 * time.Millisecond)()
	e := runEnv(t)
	var once sync.Once
	foreign := 0
	osimages.SetAfterStep(e.svc, func(id int64, step int) {
		if step != osimages.StepAllocate {
			return
		}
		once.Do(func() {
			r, err := e.ent.CatalogBuild.Get(context.Background(), id)
			require.NoError(t, err)
			foreign = *r.NewVmid
			e.pve.AddVM("pve1", foreign, "someone-else")
			e.pve.SetVMConfig(foreign, "lock", "create")
		})
	})
	loc := e.location("eu-1", "ceph", "imp")
	r := e.enable("debian-13", loc)[0]
	requireReady(t, r)
	require.NotEqual(t, foreign, *r.Vmid)
	require.Equal(t, []int{*r.Vmid}, e.tplVMs())
	vm, ok := e.pve.VM(foreign)
	require.True(t, ok, "foreign VM kept")
	require.Equal(t, "someone-else", vm.Name)
	var snoozes int
	require.NoError(t, e.sql.QueryRow(`select coalesce((metadata->>'snoozes')::int, 0) from river_job where id = $1`, *r.RiverJobID).Scan(&snoozes))
	require.GreaterOrEqual(t, snoozes, 1)
}

func TestBuildNodePersisted(t *testing.T) {
	ctx := context.Background()
	t.Run("set during the build and cleared after", func(t *testing.T) {
		e := runEnv(t)
		loc := e.location("eu-1", "local-lvm", "imp")
		seen := map[string]string{}
		var mu sync.Mutex
		osimages.SetAfterStep(e.svc, func(id int64, step int) {
			r, err := e.ent.CatalogBuild.Get(context.Background(), id)
			require.NoError(t, err)
			require.NotNil(t, r.BuildNode)
			mu.Lock()
			seen[r.Node] = *r.BuildNode
			mu.Unlock()
		})
		for _, r := range e.enable("debian-13", loc) {
			requireReady(t, r)
			require.Nil(t, r.BuildNode)
		}
		require.Equal(t, map[string]string{"pve1": "pve1", "pve2": "pve2"}, seen)
	})
	t.Run("a saved node is reused", func(t *testing.T) {
		e := newEnv(t)
		loc := e.location("eu-1", "ceph", "imp")
		_, err := e.svc.Enable(ctx, "debian-13", loc)
		require.NoError(t, err)
		row := e.rows("debian-13", loc)[0]
		require.NoError(t, row.Update().SetBuildNode("pve2").Exec(ctx))
		job := &river.Job[osimages.BuildArgs]{JobRow: &rivertype.JobRow{ID: *row.RiverJobID, Attempt: 1, MaxAttempts: 7},
			Args: osimages.BuildArgs{BuildID: row.ID}}
		require.NoError(t, osimages.NewBuildWorker(e.svc).Work(ctx, job))
		r, err := e.ent.CatalogBuild.Get(ctx, row.ID)
		require.NoError(t, err)
		requireReady(t, r)
		require.Equal(t, "pve2", e.pve.Downloads()[0].Node)
		vm, _ := e.pve.VM(*r.Vmid)
		require.Equal(t, "pve2", vm.Node)
	})
}

func TestWaitBudgetClampedToDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.LessOrEqual(t, osimages.BudgetFor(ctx), time.Duration(0))
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Hour)
	defer cancel2()
	require.Equal(t, 10*time.Minute, osimages.BudgetFor(ctx2))
	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel3()
	require.InDelta(t, float64(4*time.Minute+30*time.Second), float64(osimages.BudgetFor(ctx3)), float64(time.Second))
}

func TestFinalizeWaitsForTemplateFlag(t *testing.T) {
	ctx := context.Background()
	up := newUpstream(t)
	var svc *osimages.Service
	app := testutil.NewAppWithWorkers(t, testutil.ServerWorkers, func(d *httpapi.Deps) {
		d.Catalog = catalog.New(d.SQL, d.Ent, testutil.CatalogOptions(catalog.Hooks{}))
		svc = osimages.New(osimages.Options{SQL: d.SQL, Ent: d.Ent, Catalog: d.Catalog, Entries: up.entries, HTTP: up.client,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		d.OSImages = svc
	})
	se := testutil.NewServerEnv(app)
	for _, n := range []string{"pve1", "pve2"} {
		app.PVE.AddStorage(n, "imp", []string{"import"}, false)
	}
	require.NoError(t, app.Ent.Location.UpdateOneID(se.Location).SetImportStorage("imp").Exec(ctx))
	app.PVE.SetTemplateListDelay(1500 * time.Millisecond)

	_, err := svc.Enable(ctx, "debian-13", se.Location)
	require.NoError(t, err)
	row, err := app.Ent.CatalogBuild.Query().Where(catalogbuild.CatalogKey("debian-13")).Only(ctx)
	require.NoError(t, err)
	e := &env{t: t, ent: app.Ent}
	r := e.waitSettled(row.ID)
	requireReady(t, r)

	pc, err := app.Catalog.ProxmoxFor(ctx, se.Location)
	require.NoError(t, err)
	vms, err := pc.VMs(ctx)
	require.NoError(t, err)
	listed := false
	for _, v := range vms {
		if v.VMID == *r.Vmid {
			listed = v.Template
		}
	}
	require.True(t, listed, "ready before Proxmox listed the template")

	s := se.CreateServer("web", map[string]any{"image": "debian-13"})
	a := testutil.WaitAction(t, app.Ent, s.ActionID)
	require.Equal(t, actions.StatusSuccess, a.Status, "%+v", a.Error)
}
