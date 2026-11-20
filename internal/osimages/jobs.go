package osimages

import (
	"github.com/Hexeption/nebla/internal/jobs"
	"github.com/riverqueue/river"
)

const (
	KindBuild  = "os_template_build"
	KindRetire = "os_template_retire"
)

// BuildArgs builds or refreshes one catalog_builds row; the worker no-ops unless the row is building with its job id.
type BuildArgs struct {
	BuildID int64 `json:"build_id"`
}

func (BuildArgs) Kind() string { return KindBuild }

func (BuildArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{Queue: jobs.QueueMaintenance} }

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
