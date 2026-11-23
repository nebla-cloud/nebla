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

package snapshots

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
)

var (
	errNotFound       = apierr.New(http.StatusNotFound, "not_found", "not found")
	errServerNotFound = apierr.New(http.StatusNotFound, "not_found", "server not found")
	errNoJobs         = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	errLocked         = apierr.New(http.StatusConflict, "locked", "the snapshot has a running action")
	errServerLocked   = apierr.New(http.StatusConflict, "locked", "the server has a running action")
	errNameTaken      = apierr.New(http.StatusConflict, "uniqueness_error", "snapshot name is already taken")
	errNotReady       = apierr.New(http.StatusConflict, "server_not_ready", "The server must be running or off to take a snapshot.")
	errUnavailable    = apierr.New(http.StatusUnprocessableEntity, "snapshots_unavailable", "Snapshots are not available in this location")
	errUnreachable    = apierr.New(http.StatusServiceUnavailable, "pve_unreachable", "This location can't be reached right now. Try again in a moment.")
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

// writeErr maps constraint violations on snapshots rows.
func writeErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.ConstraintName == "snapshots_project_name_key" {
		return errNameTaken
	}
	return notFound(err)
}
