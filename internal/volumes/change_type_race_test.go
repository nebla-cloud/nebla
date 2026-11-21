package volumes_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/ent/volume"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/nebla-cloud/nebla/internal/volumes"
	"github.com/stretchr/testify/require"
)

func isDeadlock(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "40P01"
}

func TestChangeTypeVsVolumeOpsNoDeadlock(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	a := e.App
	proj := projects.New(a.Ent, projects.Options{Hooks: projects.Hooks{Usage: volumes.UsageWith(servers.Usage)}})
	srvs := servers.New(servers.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs})
	e.Admin.CreateServerType("large", 4, 4096, 40)
	require.NoError(t, a.Ent.Project.UpdateOneID(e.Project).SetMaxVcpu(1000).SetMaxMemoryMB(1<<20).SetMaxDiskGB(1<<20).
		SetMaxVolumeGB(1<<20).Exec(ctx))
	s := e.RunningServer("web")
	on := e.put(t, e.Project, "on", "", "ceph", 10, &s.ServerID, "virtio1")
	off := e.put(t, e.Project, "off", "", "ceph", 10, nil, "")

	const iterations = 50
	deadlocks, changed := 0, 0
	for i := range iterations {
		// Back to an off small server with "off" detached, so the change grows and the attach is allowed.
		require.NoError(t, a.Ent.Server.UpdateOneID(s.ServerID).SetStatus(server.StatusOff).SetServerTypeID(e.Type).
			SetCores(2).SetMemoryMB(2048).SetDiskGB(20).Exec(ctx))
		require.NoError(t, a.Ent.Volume.UpdateOneID(off).ClearServerID().SetDevice("").SetStatus(volume.StatusAvailable).Exec(ctx))
		size := e.row(t, on).SizeGB

		start := make(chan struct{})
		var wg sync.WaitGroup
		var ctAct, volAct actions.Action
		var ctErr, volErr error
		wg.Go(func() { <-start; ctAct, ctErr = srvs.ChangeType(ctx, e.Project, s.ServerID, "large", false) })
		wg.Go(func() {
			<-start
			time.Sleep(time.Duration(i%10) * 300 * time.Microsecond)
			if i%2 == 0 {
				volAct, volErr = e.svc.Resize(ctx, e.Project, on, size+1)
			} else {
				volAct, volErr = e.svc.Attach(ctx, e.Project, off, "web")
			}
		})
		close(start)
		wg.Wait()
		for _, act := range []actions.Action{ctAct, volAct} {
			if act.ID != 0 {
				require.NoError(t, actions.Succeed(ctx, a.Ent, act.ID))
			}
		}
		if isDeadlock(ctErr) || isDeadlock(volErr) {
			t.Logf("deadlock i=%d ct=%v vol=%v", i, ctErr, volErr)
			deadlocks++
			continue
		}
		// One side may lose to the other's running action.
		for _, err := range []error{ctErr, volErr} {
			if err != nil {
				var ae apierr.E
				require.True(t, errors.As(err, &ae), "iteration %d: %v", i, err)
				require.Equal(t, http.StatusConflict, ae.Status, "iteration %d: %v", i, err)
			}
		}
		if ctErr == nil {
			changed++
		}
	}
	t.Logf("deadlocks: %d, type changes: %d of %d", deadlocks, changed, iterations)
	require.Zero(t, deadlocks)
}
