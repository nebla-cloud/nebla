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
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Contract is one attempt of a job under the worker contract, for other resources (volumes) to share.
type Contract struct {
	Job *rivertype.JobRow
	// Name prefixes the message when the attempts run out.
	Name     string
	Finished bool
	Log      *slog.Logger
	Run      func(context.Context) error
	// Fail undoes what it can and fails the action; it must be idempotent and fail the action last.
	Fail func(ctx context.Context, f Failure) error
}

// Failure is how an action failed: a stable code for members and admin-only detail.
type Failure struct{ Code, Detail string }

// Work runs the attempt: transient errors retry below the cap, anything else ends in Fail and JobCancel.
func (c Contract) Work(ctx context.Context) error {
	return contract{
		job: c.Job, name: c.Name, finished: c.Finished, log: c.Log, run: c.Run,
		fail: func(ctx context.Context, f failure) error { return c.Fail(ctx, Failure{f.code, f.detail}) },
	}.work(ctx)
}

// Permanent fails the job without retry.
func Permanent(format string, a ...any) error { return permanent(format, a...) }

// PermanentCode fails the job without retry under an actions Code* kind.
func PermanentCode(code, format string, a ...any) error {
	return &permanentError{code, fmt.Sprintf(format, a...)}
}

// Transient is retried like a transient Proxmox error.
func Transient(format string, a ...any) error { return transient(format, a...) }

// StepError names the step that failed.
func StepError(step string, err error) error { return &stepError{step, err} }

// Classify maps err to a failure code; the detail is the full error chain.
func Classify(err error) Failure {
	f := classify(err)
	return Failure{f.code, f.detail}
}

// InsertOpts are the River insert options of a job under the contract.
func InsertOpts() river.InsertOpts { return insertOpts() }
