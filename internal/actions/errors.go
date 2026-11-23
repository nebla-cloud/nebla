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

package actions

import "strings"

// Codes are the stable failure kinds members and the UI see.
const (
	CodeFailed        = "failed"
	CodeTimedOut      = "timed_out"
	CodeUnreachable   = "unreachable"
	CodeServerMissing = "server_missing"
	CodeServerGone    = "server_gone"
	CodeOrphaned      = "orphaned"
	// CodeFirewallUnenforced means the location's datacenter firewall is off, so rules would be ignored.
	CodeFirewallUnenforced = "firewall_unenforced"
	// CodePowerOffRequired means the change needs the server powered off first.
	CodePowerOffRequired = "power_off_required"
	// CodeReleasePending means the address is still being released from its previous server.
	CodeReleasePending = "release_pending"
)

var verbs = map[string]string{
	"create_server":        "create the server",
	"delete_server":        "delete the server",
	"rebuild_server":       "rebuild the server",
	"change_server_type":   "change the server type",
	"start_server":         "start the server",
	"stop_server":          "stop the server",
	"shutdown_server":      "shut down the server",
	"reboot_server":        "reboot the server",
	"reset_server":         "reset the server",
	"apply_firewalls":      "apply the firewall rules",
	"create_volume":        "create the volume",
	"attach_volume":        "attach the volume",
	"detach_volume":        "detach the volume",
	"attach_to_network":    "attach the server to the network",
	"detach_from_network":  "detach the server from the network",
	"resize_volume":        "resize the volume",
	"delete_volume":        "delete the volume",
	"assign_floating_ip":   "assign the floating IP",
	"unassign_floating_ip": "unassign the floating IP",
	"delete_floating_ip":   "delete the floating IP",
	"create_backup":        "back up the server",
	"restore_backup":       "restore the backup",
	"delete_backup":        "delete the backup",
}

var templates = map[string]string{
	CodeFailed:             "Couldn't %s. Try again, or contact your admin.",
	CodeTimedOut:           "Couldn't %s because it took too long. Try again, or contact your admin.",
	CodeUnreachable:        "Couldn't %s because the server's location can't be reached right now. Try again later.",
	CodeServerMissing:      "Couldn't %s because the server's machine is missing. Contact your admin.",
	CodeServerGone:         "The server no longer exists.",
	CodeOrphaned:           "Couldn't %s because the request was interrupted. Try again.",
	CodeFirewallUnenforced: "Firewall rules couldn't be enforced at this location; contact the administrator.",
	CodePowerOffRequired:   "Power off the server to %s.",
	CodeReleasePending:     "Couldn't %s because the address is still being released from the previous server. It will finish on its own; try again later.",
}

// ValidCode reports whether code is one of the Code* kinds.
func ValidCode(code string) bool {
	_, ok := templates[code]
	return ok
}

// normalizeCode maps unknown and legacy codes to CodeFailed.
func normalizeCode(code string) string {
	if _, ok := templates[code]; ok {
		return code
	}
	return CodeFailed
}

// Message is the member-facing text for a failed command; it never includes stored detail.
func Message(command, code string) string {
	verb, ok := verbs[command]
	if !ok {
		verb = "complete the action"
	}
	return strings.Replace(templates[normalizeCode(code)], "%s", verb, 1)
}
