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

package osimages

import (
	"context"
	"log/slog"
	"time"

	"github.com/nebla-cloud/nebla/internal/catalog"
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
