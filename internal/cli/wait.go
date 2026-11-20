package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Hexeption/nebla/internal/cli/apiclient"
	"github.com/spf13/cobra"
)

var (
	pollBase = time.Second
	pollMax  = 5 * time.Second
)

// ErrInterrupted means waiting stopped while the action kept running.
var ErrInterrupted = errors.New("interrupted")

type interruptedError struct{ id int64 }

func (e interruptedError) Error() string {
	return fmt.Sprintf("stopped waiting; action %d keeps running", e.id)
}

func (e interruptedError) Is(target error) bool { return target == ErrInterrupted }

type waitOpts struct{ NoWait bool }

func addWaitFlags(cmd *cobra.Command) *waitOpts {
	o := &waitOpts{}
	cmd.Flags().BoolVar(&o.NoWait, "no-wait", false, "print the action id and return without waiting")
	return o
}

// Wait polls each action in order until it finishes, writing progress to w.
func (a *API) Wait(ctx context.Context, w io.Writer, tty bool, ids ...int64) ([]apiclient.Action, error) {
	parent := ctx
	if a.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.Timeout)
		defer cancel()
	}
	done := make([]apiclient.Action, 0, len(ids))
	for _, id := range ids {
		act, err := a.waitOne(ctx, w, tty, id)
		if err != nil {
			if ctx.Err() != nil {
				if tty {
					fmt.Fprintln(w)
				}
				if parent.Err() == nil {
					return done, fmt.Errorf("timed out after %s waiting for action %d; it keeps running", a.Timeout, id)
				}
				return done, interruptedError{id}
			}
			return done, err
		}
		done = append(done, act)
	}
	return done, nil
}

func (a *API) waitOne(ctx context.Context, w io.Writer, tty bool, id int64) (apiclient.Action, error) {
	delay := pollBase
	for n := 0; ; n++ {
		res, err := a.C.GetActionWithResponse(ctx, id)
		if err != nil {
			return apiclient.Action{}, err
		}
		v, err := result(res.StatusCode(), res.Body, res.JSON200)
		if err != nil {
			return apiclient.Action{}, err
		}
		act := *v
		label := fmt.Sprintf("%s (%d)", act.Command, act.Id)
		if tty {
			fmt.Fprintf(w, "\rWaiting for %s ... %d%%", label, act.Progress)
		} else if n == 0 {
			fmt.Fprintf(w, "Waiting for %s\n", label)
		}
		switch act.Status {
		case apiclient.ActionStatusSuccess:
			if tty {
				fmt.Fprintf(w, "\rWaiting for %s ... done\n", label)
			} else {
				fmt.Fprintf(w, "%s: success\n", label)
			}
			return act, nil
		case apiclient.ActionStatusError:
			if tty {
				fmt.Fprintf(w, "\rWaiting for %s ... error\n", label)
			} else {
				fmt.Fprintf(w, "%s: error\n", label)
			}
			code, msg := "", "unknown error"
			if act.Error != nil {
				code, msg = act.Error.Code, act.Error.Message
			}
			return act, fmt.Errorf("action %s failed: %s (%s)", label, msg, code)
		}
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return act, ctx.Err()
		}
		delay = min(delay*3/2, pollMax)
	}
}
