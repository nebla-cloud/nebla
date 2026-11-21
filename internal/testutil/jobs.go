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

package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/ent/action"
	"github.com/nebla-cloud/nebla/internal/jobs"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"
)

// WaitAction polls until the action has finished and returns it; it fails the test after 15s.
func WaitAction(t testing.TB, c *ent.Client, id int64) actions.Action {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(15 * time.Second)
	for {
		a, err := c.Action.Query().Where(action.ID(id)).Only(ctx)
		if err != nil {
			t.Fatalf("wait action %d: %v", id, err)
		}
		if a.Finished != nil {
			got, err := actions.Get(ctx, c, a.ProjectID, id)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("action %d still running at %d%%", id, a.Progress)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// WorkJob inserts args and runs worker on it synchronously in a committed transaction, without a running client.
func WorkJob[T river.JobArgs](t testing.TB, pool *pgxpool.Pool, worker river.Worker[T], args T) (*rivertest.WorkResult, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	w := rivertest.NewWorker(t, riverpgxv5.New(pool), &river.Config{}, worker)
	res, err := w.Work(ctx, t, tx, args, nil)
	if cerr := tx.Commit(ctx); cerr != nil {
		t.Fatal(cerr)
	}
	return res, err
}

// WaitJob polls until the River job is finalized and returns its row; it fails the test after 15s.
func WaitJob(t testing.TB, j *jobs.Jobs, id int64) *rivertype.JobRow {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		row, err := j.Client.JobGet(context.Background(), id)
		if err != nil {
			t.Fatalf("wait job %d: %v", id, err)
		}
		if row.FinalizedAt != nil {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d still %s", id, row.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
