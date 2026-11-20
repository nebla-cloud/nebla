package osimages

import (
	"context"
	"log/slog"
	"time"

	"github.com/Hexeption/nebla/internal/catalog"
)

var ParseChecksums = parseChecksums

var TemplateTag = templateTag

const (
	StepResolve  = stepResolve
	StepDownload = stepDownload
	StepAllocate = stepAllocate
	StepCreate   = stepCreate
	StepTemplate = stepTemplate
)

// SetAfterStep installs a hook called after each build step is saved.
func SetAfterStep(s *Service, f func(buildID int64, step int)) { s.afterStep = f }

func NewBuildWorker(s *Service) *BuildWorker { return &BuildWorker{s: s} }

// SetWaitBudget overrides waitBudget and returns a restore func.
func SetWaitBudget(d time.Duration) func() {
	old := waitBudget
	waitBudget = d
	return func() { waitBudget = old }
}

// SetSnooze overrides the snooze after an expired wait and returns a restore func.
func SetSnooze(d time.Duration) func() {
	old := snoozeFor
	snoozeFor = d
	return func() { snoozeFor = old }
}

var Failure = failure

func StepErr(step string, err error) error { return &stepError{step: step, err: err} }

const CleanupPending = cleanupPending

var BudgetFor = budgetFor

func NewRetireWorker(s *Service) *RetireWorker { return &RetireWorker{s: s} }

// SetRetireDelay overrides retireDelay and returns a restore func.
func SetRetireDelay(d time.Duration) func() {
	old := retireDelay
	retireDelay = d
	return func() { retireDelay = old }
}

// SetLockedForeignLimit overrides lockedForeignLimit and returns a restore func.
func SetLockedForeignLimit(d time.Duration) func() {
	old := lockedForeignLimit
	lockedForeignLimit = d
	return func() { lockedForeignLimit = old }
}

// BuildNodes lists the build node of each availability target, in order.
func BuildNodes(s *Service, loc catalog.Location) []string {
	var out []string
	for _, t := range s.availability(context.Background(), loc).targets {
		out = append(out, t.BuildNode)
	}
	return out
}
func NewCleanupWorker(s *Service) *CleanupWorker { return &CleanupWorker{s: s} }

func SetLogger(s *Service, l *slog.Logger) { s.o.Logger = l }
