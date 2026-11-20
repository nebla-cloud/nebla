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
	terr := Transient("later")
	if err := run(terr, 1); err != terr {
		t.Fatalf("transient below cap: got %v", err)
	}
	if Classify(terr).Code != actions.CodeFailed || InsertOpts().MaxAttempts <= MaxAttempts {
		t.Fatal("classify/insert opts")
	}
}
