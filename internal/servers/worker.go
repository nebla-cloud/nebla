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

package servers

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/db"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/pve"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// MaxAttempts is the logical retry cap of every server job; River gets spare attempts that only clean up.
const MaxAttempts = 3

func insertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: MaxAttempts + spareAttempts} }

// retryable reports errors a later attempt may get past.
func retryable(err error) bool {
	var te *transientError
	return pve.IsTransient(err) || db.IsTransient(err) || errors.As(err, &te)
}

// pendingCleanup returns the failure when the previous attempt's cleanup did not finish.
func pendingCleanup(job *rivertype.JobRow) (failure, bool) {
	n := len(job.Errors)
	if n == 0 {
		return failure{}, false
	}
	rest, ok := strings.CutPrefix(job.Errors[n-1].Error, cleanupPending)
	if !ok {
		return failure{}, false
	}
	if code, detail, ok := strings.Cut(rest, ": "); ok && actions.ValidCode(code) {
		return failure{code, detail}, true
	}
	return failure{actions.CodeFailed, rest}, true
}

func (f failure) pending() string { return cleanupPending + f.code + ": " + f.detail }

func lastError(job *rivertype.JobRow) string {
	if n := len(job.Errors); n > 0 {
		return job.Errors[n-1].Error
	}
	return ""
}

// contract is one attempt of a server job under the worker contract (see jobs.StartAction).
type contract struct {
	job *rivertype.JobRow
	// name prefixes the message when the attempts run out.
	name     string
	finished bool
	log      *slog.Logger
	run      func(context.Context) error
	// fail undoes what it can and fails the action; it must be idempotent and fail the action last.
	fail func(ctx context.Context, f failure) error
}

// work runs the attempt: transient errors retry below the cap, anything else ends in fail and JobCancel.
func (k contract) work(ctx context.Context) error {
	f, pending := pendingCleanup(k.job)
	if k.finished && !pending {
		return nil
	}
	if pending || k.job.Attempt > MaxAttempts {
		if !pending {
			f = failure{actions.CodeFailed, k.name + " did not finish after " + strconv.Itoa(MaxAttempts) + " attempts: " + lastError(k.job)}
			k.log.Error("attempts exhausted", "last_error", lastError(k.job))
		}
		return k.finish(ctx, f, errors.New(f.detail))
	}
	err := k.run(ctx)
	var snooze *rivertype.JobSnoozeError
	if err == nil || errors.As(err, &snooze) {
		return err
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	retry := retryable(err) || errors.Is(ctx.Err(), context.DeadlineExceeded)
	if retry && k.job.Attempt < MaxAttempts {
		return err
	}
	f = classify(err)
	k.log.Error("action failed", "err", err, "code", f.code)
	return k.finish(ctx, f, err)
}

// finish cancels the job once the failure is recorded, else leaves it for the next attempt.
func (k contract) finish(ctx context.Context, f failure, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := k.fail(ctx, f); err != nil {
		k.log.Error("cleanup will be retried", "err", err)
		return errors.New(f.pending())
	}
	return river.JobCancel(cause)
}

// begin loads a job's action and server; when stop is true the worker returns ret.
func (o *WorkerOptions) begin(ctx context.Context, actionID, serverID int64) (act *ent.Action, row *ent.Server, ret error, stop bool) {
	act, err := o.Ent.Action.Get(ctx, actionID)
	if ent.IsNotFound(err) {
		return nil, nil, river.JobCancel(err), true
	}
	if err != nil {
		return nil, nil, err, true
	}
	row, err = o.Ent.Server.Get(ctx, serverID)
	if ent.IsNotFound(err) {
		if act.Finished != nil {
			return act, nil, nil, true
		}
		if ferr := failAction(ctx, o.Ent, actionID, failure{actions.CodeServerGone, "server no longer exists"}); ferr != nil {
			return act, nil, ferr, true
		}
		return act, nil, river.JobCancel(err), true
	}
	if err != nil {
		return act, nil, err, true
	}
	return act, row, nil, false
}

func failAction(ctx context.Context, c *ent.Client, id int64, f failure) error {
	return actions.Fail(context.WithoutCancel(ctx), c, id, f.code, f.detail)
}

// realStatus is the server status Proxmox shows for vmid, or fallback when it cannot tell.
func realStatus(ctx context.Context, pc pve.Client, serverID int64, vmid *int, fallback string) string {
	if pc == nil || vmid == nil {
		return fallback
	}
	v, found, err := findVM(ctx, pc, *vmid)
	switch {
	case err != nil:
		return fallback
	case !found || !ownedBy(v, serverID):
		return StatusMissing
	case v.Status == "running":
		return StatusRunning
	}
	return StatusOff
}

func actionLog(l *slog.Logger, act *ent.Action, serverID int64) *slog.Logger {
	return l.With("server_id", serverID, "action_id", act.ID, "command", act.Command)
}
