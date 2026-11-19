package reconcile

// SetAfterList installs a hook run right after the VM list is read.
func SetAfterList(r *Reconciler, f func()) { r.afterList = f }

// SetRescueHook runs f inside each rescue transaction; an error fails that rescue.
func SetRescueHook(r *Reconciler, f func(actionID int64) error) { r.rescueHook = f }
