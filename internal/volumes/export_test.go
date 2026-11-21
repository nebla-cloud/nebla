package volumes

import (
	"context"

	"github.com/nebla-cloud/nebla/internal/pve"
)

// Candidate is a placement candidate for tests.
type Candidate = candidate

var Pick = pick

// SetPVE makes the workers built from o reach Proxmox through f.
func SetPVE(o WorkerOptions, f func(ctx context.Context, locationID int64) (pve.Client, error)) WorkerOptions {
	o.pveFor = f
	return o
}
