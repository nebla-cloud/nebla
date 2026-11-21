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

package reconcile

import "time"

// SetAfterList installs a hook run right after the VM list is read.
func SetAfterList(r *Reconciler, f func()) { r.afterList = f }

// SetRescueHook runs f inside each rescue transaction; an error fails that rescue.
func SetRescueHook(r *Reconciler, f func(actionID int64) error) { r.rescueHook = f }

// SetDriftEvery sets how often a location's firewall drift is checked.
func SetDriftEvery(r *Reconciler, d time.Duration) { r.driftEvery = d }

// ForceDriftCheck makes the next run of every location check firewall drift.
func ForceDriftCheck(r *Reconciler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.driftAt)
}
