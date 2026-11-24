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

package networks

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
)

var (
	errNotFound            = apierr.New(http.StatusNotFound, "not_found", "not found")
	errServerNotFound      = apierr.New(http.StatusNotFound, "not_found", "server not found")
	errNoJobs              = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	errNameTaken           = apierr.New(http.StatusConflict, "uniqueness_error", "network name is already taken")
	errNotSetUp            = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "Private networks aren't available in this location.")
	errPaused              = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "Private networks are paused in this location.")
	errPoolEmpty           = apierr.New(http.StatusConflict, "no_private_networks_available", "No private networks are available in this location right now.")
	errNICLimit            = apierr.New(http.StatusUnprocessableEntity, "limit_exceeded", fmt.Sprintf("This server is already on %d private networks.", MaxNICs))
	errUnavailable         = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "This network can't take new servers right now.")
	errOtherLocation       = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "This server is in a different location than the network.")
	errAlready             = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "The server is already on this network.")
	errNotOn               = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "The server isn't on this network.")
	errBadIP               = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "That address isn't usable on this network.")
	errIPTaken             = apierr.New(http.StatusConflict, "conflict", "That address is already used on this network.")
	errNoFreeIP            = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "This network has no free addresses.")
	errOverlap             = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "That range overlaps another network or this location's public addresses.")
	errInUse               = apierr.New(http.StatusConflict, "conflict", "This location's private networks are in use; the zone and prefix can't change.")
	errZoneTaken           = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "Another location already uses this zone.")
	errLocationHasNetworks = apierr.New(http.StatusConflict, "location_in_use", "Delete this location's private networks first.")
	errLocked              = apierr.New(http.StatusConflict, "locked", "the server has a running action")
	errBusy                = apierr.New(http.StatusConflict, "locked", "the network is busy")
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

// detachFirst is the delete refusal; names are the servers still on the network.
func detachFirst(names []string) error {
	return apierr.New(http.StatusConflict, "conflict", "Detach all servers from this network first: "+strings.Join(names, ", ")+".")
}

// writeErr maps constraint violations on networks and server_networks rows.
func writeErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.ConstraintName {
		case "networks_project_name_key":
			return errNameTaken
		case "server_networks_network_ip_key":
			return errIPTaken
		case "server_networks_server_network_key":
			return errAlready
		case "server_networks_server_nic_key":
			return errNICLimit
		}
	}
	return notFound(err)
}

// lockErr turns a lock timeout or deadlock into the busy error.
func lockErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && (pe.Code == "55P03" || pe.Code == "40P01") {
		return errBusy
	}
	return err
}
