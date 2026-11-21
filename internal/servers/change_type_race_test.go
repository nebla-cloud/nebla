package servers_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/server"
	"github.com/nebla-cloud/nebla/internal/projects"
	"github.com/nebla-cloud/nebla/internal/servers"
	"github.com/stretchr/testify/require"
)

func isDeadlock(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "40P01"
}

// ChangeType must take the project lock before the server lock, like other project-then-server ops.
func TestChangeTypeVsProjectThenServerNoDeadlock(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	a := e.App
	proj := projects.New(a.Ent, projects.Options{})
	srvs := servers.New(servers.Options{SQL: a.SQL, Ent: a.Ent, Catalog: a.Catalog, Projects: proj, Jobs: a.Jobs})
	e.Admin.CreateServerType("large", 4, 4096, 40)
	require.NoError(t, a.Ent.Project.UpdateOneID(e.Project).SetMaxVcpu(1000).SetMaxMemoryMB(1<<20).SetMaxDiskGB(1<<20).Exec(ctx))
	s := e.RunningServer("web")

	const iterations = 50
	deadlocks, changed := 0, 0
	for i := range iterations {
		// Back to an off small server so the change grows and ReserveQuota runs.
		require.NoError(t, a.Ent.Server.UpdateOneID(s.ServerID).SetStatus(server.StatusOff).SetServerTypeID(e.Type).
			SetCores(2).SetMemoryMB(2048).SetDiskGB(20).Exec(ctx))

		start := make(chan struct{})
		var wg sync.WaitGroup
		var ctAct, opAct actions.Action
		var ctErr, opErr error
		wg.Go(func() { <-start; ctAct, ctErr = srvs.ChangeType(ctx, e.Project, s.ServerID, "large", false) })
		wg.Go(func() {
			<-start
			time.Sleep(time.Duration(i%10) * 300 * time.Microsecond)
			opErr = db.WithTx(ctx, a.SQL, func(_ *sql.Tx, etx *ent.Client) error {
				if err := projects.LockProject(ctx, etx, e.Project); err != nil {
					return err
				}
				if _, err := servers.LockServer(ctx, etx, e.Project, s.ServerID); err != nil {
					return err
				}
				var err error
				opAct, err = actions.Create(ctx, etx, e.Project, "power_on", []actions.Resource{{ID: s.ServerID, Type: servers.ResourceType}})
				return err
			})
		})
		close(start)
		wg.Wait()
		for _, act := range []actions.Action{ctAct, opAct} {
			if act.ID != 0 {
				require.NoError(t, actions.Succeed(ctx, a.Ent, act.ID))
			}
		}
		if isDeadlock(ctErr) || isDeadlock(opErr) {
			t.Logf("deadlock i=%d ct=%v op=%v", i, ctErr, opErr)
			deadlocks++
			continue
		}
		// One side may lose to the other's running action.
		for _, err := range []error{ctErr, opErr} {
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
