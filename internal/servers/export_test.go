package servers

// SetAfterStep installs a hook called after each create step is persisted.
func SetAfterStep(w *CreateServerWorker, f func(step int)) { w.afterStep = f }

const CleanupPending = cleanupPending

// SetCleanupTxHook runs f before cleanup's transaction; an error fails that transaction.
func SetCleanupTxHook(w *CreateServerWorker, f func() error) { w.cleanupTxHook = f }

// SetAfterClonePost installs a hook between the clone request and saving its UPID.
func SetAfterClonePost(w *CreateServerWorker, f func()) { w.afterClonePost = f }

var CloneDescription = cloneDescription

const (
	StepClone = stepClone
	StepSwap  = stepSwap
)

// SetRebuildAfterStep installs a hook called after each rebuild step is persisted.
func SetRebuildAfterStep(w *RebuildServerWorker, f func(step int)) { w.afterStep = f }

var CloudinitDrives = cloudinitDrives

func Failure(err error) (code, detail string) { f := classify(err); return f.code, f.detail }

var Permanent = permanent

var Missing = missing

var TimedOut = timedOut

func StepError(step string, err error) error { return &stepError{step: step, err: err} }

const StepDone = stepDone

// SetApplyBeforeLease runs f between loading the server and claiming its firewall lease.
func SetApplyBeforeLease(w *ApplyFirewallsWorker, f func()) { w.beforeLease = f }

// SetApplyBeforeApply runs f just before the firewall is rendered and written.
func SetApplyBeforeApply(w *ApplyFirewallsWorker, f func()) { w.beforeApply = f }
