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
)

var verbs = map[string]string{
	"create_server":      "create the server",
	"delete_server":      "delete the server",
	"rebuild_server":     "rebuild the server",
	"change_server_type": "change the server type",
	"start_server":       "start the server",
	"stop_server":        "stop the server",
	"shutdown_server":    "shut down the server",
	"reboot_server":      "reboot the server",
	"reset_server":       "reset the server",
	"apply_firewalls":    "apply the firewall rules",
}

var templates = map[string]string{
	CodeFailed:             "Couldn't %s. Try again, or contact your admin.",
	CodeTimedOut:           "Couldn't %s because it took too long. Try again, or contact your admin.",
	CodeUnreachable:        "Couldn't %s because the server's location can't be reached right now. Try again later.",
	CodeServerMissing:      "Couldn't %s because the server's machine is missing. Contact your admin.",
	CodeServerGone:         "The server no longer exists.",
	CodeOrphaned:           "Couldn't %s because the request was interrupted. Try again.",
	CodeFirewallUnenforced: "Firewall rules couldn't be enforced at this location; contact the administrator.",
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
