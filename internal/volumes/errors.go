package volumes

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/volumes/vref"
)

var (
	errNotFound       = apierr.New(http.StatusNotFound, "not_found", "not found")
	errServerNotFound = apierr.New(http.StatusNotFound, "not_found", "server not found")
	errNoJobs         = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	// errServerLocked matches servers' own locked error.
	errServerLocked = apierr.New(http.StatusConflict, "locked", "the server has a running action")
	errLocked       = apierr.New(http.StatusConflict, "locked", "the volume has a running action")
	errNotReady     = apierr.New(http.StatusConflict, "volume_not_ready", "This volume can only be deleted")
	errNameTaken    = apierr.New(http.StatusConflict, "uniqueness_error", "volume name is already taken")
	errAttached     = apierr.New(http.StatusConflict, "volume_attached", "This volume is already attached to a server")
	errNotAttached  = apierr.New(http.StatusConflict, "volume_not_attached", "This volume is not attached to a server")
	errOtherHost    = apierr.New(http.StatusUnprocessableEntity, "volume_server_mismatch", "This volume is on a different host in this location")
	errOtherLoc     = apierr.New(http.StatusUnprocessableEntity, "volume_server_mismatch", "This volume and the server are in different locations")
	errLimit        = apierr.New(http.StatusUnprocessableEntity, "volume_limit", fmt.Sprintf("A server can hold at most %d volumes", vref.MaxPerServer))
	errShrink       = apierr.New(http.StatusUnprocessableEntity, "volume_shrink_not_allowed", "Volumes can only grow")
	errUnavailable  = apierr.New(http.StatusUnprocessableEntity, "volumes_unavailable", "Volumes are not available in this location")
	errNoCapacity   = apierr.New(http.StatusUnprocessableEntity, "resource_unavailable", "no host in this location has room for this volume")
)

func invalid(format string, a ...any) error {
	return apierr.New(http.StatusUnprocessableEntity, "invalid_input", fmt.Sprintf(format, a...))
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

// writeErr maps constraint violations on volumes rows.
func writeErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.ConstraintName == "volumes_project_name_key" {
		return errNameTaken
	}
	return notFound(err)
}
