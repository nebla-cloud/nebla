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
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestContractExported(t *testing.T) {
	var got Failure
	run := func(err error, attempt int) error {
		return Contract{
			Job: &rivertype.JobRow{Attempt: attempt}, Name: "x", Log: slog.Default(),
			Run:  func(context.Context) error { return err },
			Fail: func(_ context.Context, f Failure) error { got = f; return nil },
		}.Work(context.Background())
	}
	err := run(StepError("step", Permanent("x")), 1)
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatalf("permanent: want JobCancel, got %v", err)
	}
	if got.Code != actions.CodeFailed || got.Detail != "step: x" {
		t.Fatalf("failure = %+v", got)
	}
	if f := Classify(StepError("attach", PermanentCode(actions.CodePowerOffRequired, "hotplug"))); f.Code != actions.CodePowerOffRequired || f.Detail != "attach: hotplug" {
		t.Fatalf("permanent code: %+v", f)
	}
	terr := Transient("later")
	if err := run(terr, 1); err != terr {
		t.Fatalf("transient below cap: got %v", err)
	}
	if Classify(terr).Code != actions.CodeFailed || InsertOpts().MaxAttempts <= MaxAttempts {
		t.Fatal("classify/insert opts")
	}
}
