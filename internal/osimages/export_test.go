package osimages

import "time"

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
