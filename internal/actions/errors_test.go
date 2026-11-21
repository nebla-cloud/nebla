package actions_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/nebla-cloud/nebla/internal/actions"
	"github.com/nebla-cloud/nebla/internal/testutil"
	"github.com/stretchr/testify/require"
)

var codes = []string{actions.CodeFailed, actions.CodeTimedOut, actions.CodeUnreachable, actions.CodeServerMissing, actions.CodeServerGone, actions.CodeOrphaned, actions.CodeFirewallUnenforced}

func TestMessageTable(t *testing.T) {
	commands := []string{"create_server", "delete_server", "rebuild_server", "change_server_type", "start_server",
		"stop_server", "shutdown_server", "reboot_server", "reset_server", "something_new"}
	digit := regexp.MustCompile(`\d`)
	banned := []string{"vm", "vmid", "node", "storage", "proxmox", "pve", "template", "iso"}
	for _, cmd := range commands {
		for _, code := range codes {
			m := actions.Message(cmd, code)
			require.NotEmpty(t, m, "%s/%s", cmd, code)
			require.False(t, digit.MatchString(m), m)
			for _, w := range strings.Fields(strings.ToLower(strings.Trim(m, "."))) {
				require.NotContains(t, banned, strings.Trim(w, ".,'"), m)
			}
		}
	}
	require.Equal(t, "Couldn't reboot the server. Try again, or contact your admin.", actions.Message("reboot_server", "failed"))
	require.Equal(t, "The server no longer exists.", actions.Message("create_server", "server_gone"))
	require.Equal(t, "Couldn't complete the action. Try again, or contact your admin.", actions.Message("x", "failed"))
	require.Equal(t, actions.Message("delete_server", "failed"), actions.Message("delete_server", "pve_error"))
}

func TestFailHidesDetail(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid := project(t, c)
	a, err := actions.Create(ctx, c, pid, "reboot_server", nil)
	require.NoError(t, err)
	detail := "reboot: vm 4242 on pve-node-7 local-lvm: HTTP 500"
	require.NoError(t, actions.Fail(ctx, c, a.ID, actions.CodeTimedOut, detail))
	got := mustGet(t, c, pid, a.ID)
	require.Equal(t, &actions.Error{Code: "timed_out", Message: actions.Message("reboot_server", "timed_out"), Detail: detail}, got.Error)
	require.NotContains(t, got.Error.Message, "4242")
}

func TestLegacyCodeReadsAsFailed(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	pid := project(t, c)
	a, err := actions.Create(ctx, c, pid, "create_server", nil)
	require.NoError(t, err)
	require.NoError(t, actions.Fail(ctx, c, a.ID, "pve_error", "clone: boom 4242"))
	got := mustGet(t, c, pid, a.ID)
	require.Equal(t, actions.CodeFailed, got.Error.Code)
	require.Equal(t, actions.Message("create_server", "failed"), got.Error.Message)
	require.Equal(t, "clone: boom 4242", got.Error.Detail)
}

func TestGetAnyAndListAll(t *testing.T) {
	ctx := context.Background()
	c := testutil.NewEnt(t)
	p1, p2 := project(t, c), project(t, c)
	a1, err := actions.Create(ctx, c, p1, "x", nil)
	require.NoError(t, err)
	a2, err := actions.Create(ctx, c, p2, "x", nil)
	require.NoError(t, err)

	all, total, err := actions.ListAll(ctx, c, nil, actions.ListFilter{}, 1, 50)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Equal(t, []int64{a2.ID, a1.ID}, []int64{all[0].ID, all[1].ID})

	one, total, err := actions.ListAll(ctx, c, &p1, actions.ListFilter{}, 1, 50)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, a1.ID, one[0].ID)

	got, err := actions.GetAny(ctx, c, a2.ID)
	require.NoError(t, err)
	require.Equal(t, p2, got.ProjectID)
	_, err = actions.GetAny(ctx, c, a2.ID+100)
	require.Error(t, err)
}

func TestFirewallUnenforcedMessage(t *testing.T) {
	require.True(t, actions.ValidCode(actions.CodeFirewallUnenforced))
	require.Equal(t, "Firewall rules couldn't be enforced at this location; contact the administrator.",
		actions.Message("apply_firewalls", actions.CodeFirewallUnenforced))
	require.Equal(t, "Couldn't apply the firewall rules. Try again, or contact your admin.", actions.Message("apply_firewalls", actions.CodeFailed))
}
