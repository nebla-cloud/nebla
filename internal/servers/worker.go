package servers

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Hexeption/nebla/internal/actions"
	"github.com/Hexeption/nebla/internal/db"
	"github.com/Hexeption/nebla/internal/ent"
	"github.com/Hexeption/nebla/internal/pve"
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

// pendingCleanup returns the action message when the previous attempt's cleanup did not finish.
func pendingCleanup(job *rivertype.JobRow) (string, bool) {
	if n := len(job.Errors); n > 0 {
		return strings.CutPrefix(job.Errors[n-1].Error, cleanupPending)
	}
	return "", false
}

func lastError(job *rivertype.JobRow) string {
	if n := len(job.Errors); n > 0 {
		return ": " + job.Errors[n-1].Error
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
	fail func(ctx context.Context, msg string) error
}

// work runs the attempt: transient errors retry below the cap, anything else ends in fail and JobCancel.
func (k contract) work(ctx context.Context) error {
	msg, pending := pendingCleanup(k.job)
	if k.finished && !pending {
		return nil
	}
	if pending || k.job.Attempt > MaxAttempts {
		if !pending {
			msg = k.name + " did not finish after " + strconv.Itoa(MaxAttempts) + " attempts" + lastError(k.job)
		}
		return k.finish(ctx, msg, errors.New(msg))
	}
	err := k.run(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	retry := retryable(err) || errors.Is(ctx.Err(), context.DeadlineExceeded)
	if retry && k.job.Attempt < MaxAttempts {
		return err
	}
	return k.finish(ctx, errorMessage(err), err)
}

// finish cancels the job once the failure is recorded, else leaves it for the next attempt.
func (k contract) finish(ctx context.Context, msg string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := k.fail(ctx, msg); err != nil {
		k.log.Error("cleanup will be retried", "err", err)
		return errors.New(cleanupPending + msg)
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
		if ferr := failAction(ctx, o.Ent, actionID, "server no longer exists"); ferr != nil {
			return act, nil, ferr, true
		}
		return act, nil, river.JobCancel(err), true
	}
	if err != nil {
		return act, nil, err, true
	}
	return act, row, nil, false
}

func failAction(ctx context.Context, c *ent.Client, id int64, msg string) error {
	return actions.Fail(context.WithoutCancel(ctx), c, id, "pve_error", msg)
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
