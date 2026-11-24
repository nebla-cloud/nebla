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
	"time"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// SetAfterStep installs a hook called after each create step is persisted.
func SetAfterStep(w *CreateServerWorker, f func(step int)) { w.afterStep = f }

const CleanupPending = cleanupPending

// SetCleanupTxHook runs f before cleanup's transaction; an error fails that transaction.
func SetCleanupTxHook(w *CreateServerWorker, f func() error) { w.cleanupTxHook = f }

// SetAfterClonePost installs a hook between the clone request and saving its UPID.
func SetAfterClonePost(w *CreateServerWorker, f func()) { w.afterClonePost = f }

var CloneDescription = cloneDescription

const (
	StepVMID     = stepVMID
	StepResize   = stepResize
	StepClone    = stepClone
	StepSwap     = stepSwap
	StepFirewall = stepFirewall
)

// SetRebuildAfterStep installs a hook called after each rebuild step is persisted.
func SetRebuildAfterStep(w *RebuildServerWorker, f func(step int)) { w.afterStep = f }

var CloudinitDrives = cloudinitDrives

var Missing = missing

var TimedOut = timedOut

const StepDone = stepDone

// SetApplyBeforeLease runs f between loading the server and claiming its firewall lease.
func SetApplyBeforeLease(w *ApplyFirewallsWorker, f func()) { w.beforeLease = f }

// SetApplyBeforeApply runs f just before the firewall is rendered and written.
func SetApplyBeforeApply(w *ApplyFirewallsWorker, f func()) { w.beforeApply = f }

// SetSettleWait replaces the wait before a protected VM's first start until the test ends.
func SetSettleWait(t interface{ Cleanup(func()) }, f func(context.Context, time.Duration) error) {
	old := settleWait
	settleWait = f
	t.Cleanup(func() { settleWait = old })
}

// ApplyFirewallsNow renders server id's firewalls and allow-set onto vmid, as an apply or build does.
func ApplyFirewallsNow(ctx context.Context, o WorkerOptions, pc pve.Client, id int64, node string, vmid int) error {
	_, _, err := o.applyFirewallsNow(ctx, pc, id, node, vmid)
	return err
}

// SetIPChangeBeforeOutcome runs f before the assign wait checks for an unfinished apply.
func SetIPChangeBeforeOutcome(w *IPChangeWorker, f func()) { w.beforeOutcome = f }

// SetRestoreAfterStep installs a hook called after each restore step is persisted.
func SetRestoreAfterStep(w *RestoreServerWorker, f func(step int)) { w.afterStep = f }

// SetRestoreAfterPost installs a hook between the restore request and saving its UPID.
func SetRestoreAfterPost(w *RestoreServerWorker, f func()) { w.afterPost = f }

// SetRestoreAfterUpid installs a hook between saving the restore's UPID and awaiting it.
func SetRestoreAfterUpid(w *RestoreServerWorker, f func()) { w.afterUpid = f }

// RestoreSteps are the restore job's steps in order.
var RestoreSteps = []int{stepVMID, stepClone, stepConfig, stepResize, stepISO, stepFirewall, stepSwap, stepRetire, stepRestart}

// SyncISO runs one seed ISO sync for server id, as the sync_iso job does.
func SyncISO(ctx context.Context, o WorkerOptions, id int64, bump bool) (bool, error) {
	o.defaults()
	row, err := o.Ent.Server.Get(ctx, id)
	if err != nil {
		return false, err
	}
	pc, err := o.Catalog.ProxmoxFor(ctx, row.LocationID)
	if err != nil {
		return false, err
	}
	return o.syncISO(ctx, pc, row, bump)
}

var WithSegment = withSegment

// SetISOBeforeUpdate runs f between a seed ISO upload and its server update until the test ends.
func SetISOBeforeUpdate(t interface{ Cleanup(func()) }, f func(serverID int64)) {
	isoBeforeUpdate = f
	t.Cleanup(func() { isoBeforeUpdate = nil })
}
