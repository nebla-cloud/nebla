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

package primaryips

import (
	"fmt"
	"net/http"

	"github.com/nebla-cloud/nebla/internal/apierr"
	"github.com/nebla-cloud/nebla/internal/ent"
	"github.com/nebla-cloud/nebla/internal/floatingips"
	"github.com/nebla-cloud/nebla/internal/ipam"
)

var (
	errNotFound    = apierr.New(http.StatusNotFound, "not_found", "not found")
	errNameTaken   = apierr.New(http.StatusConflict, "uniqueness_error", "primary IP name is already taken")
	errReleasing   = apierr.New(http.StatusConflict, "locked", "The address is still being released from the previous server.")
	errBusy        = apierr.New(http.StatusConflict, "locked", "the primary IP has a running action")
	errInUse       = apierr.New(http.StatusConflict, "ip_in_use", "Unassign it first.")
	errNoLocation  = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "location is required")
	errBadType     = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "type must be ipv4 or ipv6")
	errNoJobs      = apierr.New(http.StatusServiceUnavailable, "service_unavailable", "the job runtime is not running")
	errLocation    = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "That IP is in a different location.")
	errNotStopped  = apierr.New(http.StatusConflict, "server_not_stopped", "Stop the server first.")
	errSegment     = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "This IP can't be used with that server; it's in a different network.")
	errNotAssigned = apierr.New(http.StatusUnprocessableEntity, "invalid_input", "The primary IP isn't assigned.")
)

// OldServerWarning is returned with an IP change on a server without boot-time network refresh.
const OldServerWarning = "This server was created before IP changes were supported. Changing its IP re-runs its cloud-init user data and gives it new SSH host keys."

func errInUseOn(server string) error {
	return apierr.New(http.StatusConflict, "ip_in_use", fmt.Sprintf("Unassign it from %s first.", server))
}

func errHasIP(family string) error {
	return apierr.New(http.StatusConflict, "server_has_ip", fmt.Sprintf("This server already has a primary %s.", familyName(family)))
}

func errFloating(family string) error {
	return apierr.New(http.StatusConflict, "floating_ips_attached", fmt.Sprintf("Unassign this server's %s floating IPs first.", familyName(family)))
}

func familyName(family string) string {
	if family == ipam.FamilyIPv6 {
		return "IPv6"
	}
	return "IPv4"
}

func notFound(err error) error {
	if ent.IsNotFound(err) {
		return errNotFound
	}
	return err
}

func writeErr(err error) error { return floatingips.WriteErr(err, errNameTaken) }
