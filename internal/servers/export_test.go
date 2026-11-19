package servers

// SetAfterStep installs a hook called after each create step is persisted.
func SetAfterStep(w *CreateServerWorker, f func(step int)) { w.afterStep = f }
