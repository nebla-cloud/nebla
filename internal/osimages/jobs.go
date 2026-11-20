package osimages

import (
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/riverqueue/river"
)

const (
	KindBuild   = "os_template_build"
	KindRetire  = "os_template_retire"
	KindCleanup = "os_template_cleanup"
)

// BuildArgs builds or refreshes one catalog_builds row; the worker no-ops unless the row is building with its job id.
type BuildArgs struct {
	BuildID int64 `json:"build_id"`
}

func (BuildArgs) Kind() string { return KindBuild }

func (BuildArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, MaxAttempts: MaxAttempts + spareAttempts}
}

// RetireArgs destroys a catalog template VM once no server can still clone it.
type RetireArgs struct {
	LocationID int64  `json:"location_id"`
	Node       string `json:"node"`
	VMID       int    `json:"vmid"`
	Tag        string `json:"tag"`
}

func (RetireArgs) Kind() string { return KindRetire }

func (RetireArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: jobs.LiveStates}}
}

// CleanupArgs destroys a failed build's half-built VM and its import volume; Volume "" skips the volume.
type CleanupArgs struct {
	BuildID    int64  `json:"build_id"`
	LocationID int64  `json:"location_id"`
	Node       string `json:"node"`
	VMID       int    `json:"vmid"`
	Tag        string `json:"tag"`
	Volume     string `json:"volume"`
}

func (CleanupArgs) Kind() string { return KindCleanup }

func (CleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: jobs.QueueMaintenance, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: jobs.LiveStates}}
}
