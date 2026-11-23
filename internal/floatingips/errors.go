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

package floatingips

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
	errNameTaken      = apierr.New(http.StatusConflict, "uniqueness_error", "floating IP name is already taken")
	errSegment        = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "This floating IP can't be used with that server; it's in a different network.")
	errLimit          = apierr.New(http.StatusUnprocessableEntity, "limit_exceeded", fmt.Sprintf("A server can have at most %d floating IPs.", MaxPerServer))
	errReleasing      = apierr.New(http.StatusConflict, "locked", "The address is still being released from the previous server.")
	errBusy           = apierr.New(http.StatusConflict, "locked", "the floating IP has a running action")
	errServerDeleting = apierr.New(http.StatusConflict, "locked", "server is being deleted")
	errSameServer     = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "The floating IP is already assigned to that server.")
	errNotAssigned    = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "The floating IP isn't assigned.")
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

// writeErr maps constraint violations on ip_addresses rows.
func writeErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.ConstraintName == "ip_addresses_project_name_key" {
		return errNameTaken
	}
	return notFound(err)
}

// lockErr turns a lock timeout into the busy error.
func lockErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "55P03" {
		return errBusy
	}
	return err
}
