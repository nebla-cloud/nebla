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

package backups

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
	errNoJobs         = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	errDisabled       = apierr.New(http.StatusUnprocessableEntity, "backups_disabled", "Backups are turned off for this server")
	errUnavailable    = apierr.New(http.StatusUnprocessableEntity, "backups_unavailable", "Backups are not available in this location")
	errBusy           = apierr.New(http.StatusConflict, "backup_busy", "This backup is busy. Try again in a moment.")
	errServerNotReady = apierr.New(http.StatusConflict, "server_not_ready", "the server has no usable VM")
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

// pendingExists is whether err is the one-pending-backup-per-server violation.
func pendingExists(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.ConstraintName == "backups_server_pending_key"
}
