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
