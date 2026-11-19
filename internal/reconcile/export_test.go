package reconcile

// SetAfterList installs a hook run right after the VM list is read.
func SetAfterList(r *Reconciler, f func()) { r.afterList = f }
