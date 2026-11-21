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

package auth

import (
	"context"
	"slices"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/nebla-cloud/nebla/internal/jobs"
)

const (
	KindChallengeCleanup     = "webauthn_challenge_cleanup"
	ChallengeCleanupInterval = 15 * time.Minute
)

// ChallengeCleanupArgs is the periodic job that deletes expired passkey challenges.
type ChallengeCleanupArgs struct{}

func (ChallengeCleanupArgs) Kind() string { return KindChallengeCleanup }

// InsertOpts allow one run per interval, even across restarts.
func (ChallengeCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: ChallengeCleanupInterval,
		ByState: append(slices.Clone(jobs.LiveStates), rivertype.JobStateCompleted)}}
}

func ChallengeCleanupPeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(ChallengeCleanupInterval),
		func() (river.JobArgs, *river.InsertOpts) { return ChallengeCleanupArgs{}, nil },
		&river.PeriodicJobOpts{ID: KindChallengeCleanup, RunOnStart: true})
}

func RegisterPasskeyJobs(w *river.Workers, p *Passkeys) {
	river.AddWorker(w, &challengeCleanupWorker{p: p})
}

type challengeCleanupWorker struct {
	river.WorkerDefaults[ChallengeCleanupArgs]
	p *Passkeys
}

func (*challengeCleanupWorker) Timeout(*river.Job[ChallengeCleanupArgs]) time.Duration {
	return time.Minute
}

func (w *challengeCleanupWorker) Work(ctx context.Context, _ *river.Job[ChallengeCleanupArgs]) error {
	_, err := w.p.DeleteExpiredChallenges(ctx, 0)
	return err
}
