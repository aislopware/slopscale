package v2

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestSSHPortForwardingFollowsRule pins that a session may forward ports
// only when its rule asks: tailscaled forwards Unix sockets as root, so
// a forward is root on the host whatever login the rule allows (the
// Tailworm report, findings 1-3). Agent forwarding stays on. A check
// rule's final action, which replaces the rule's, carries the same.
func TestSSHPortForwardingFollowsRule(t *testing.T) {
	t.Parallel()

	users := types.Users{
		{ID: 1, Name: "user1"},
		{ID: 2, Name: "user2"},
	}

	nodes := types.Nodes{
		{ID: 1, Hostname: "laptop1", IPv4: createAddr("100.64.0.1"), UserID: new(users[0].ID), User: new(users[0])},
		{ID: 2, Hostname: "laptop2", IPv4: createAddr("100.64.0.2"), UserID: new(users[1].ID), User: new(users[1])},
		{
			ID: 3, Hostname: "server", IPv4: createAddr("100.64.0.3"),
			UserID: new(users[0].ID), User: new(users[0]), Tags: []string{"tag:server"},
		},
	}

	policy := `{
		"tagOwners": {"tag:server": ["user1@"]},
		"grants": [{"src": ["*"], "dst": ["*"], "ip": ["*"]}],
		"ssh": [
			{"action": "accept", "src": ["user1@"], "dst": ["tag:server"], "users": ["deploy"]},
			{
				"action": "accept", "src": ["user2@"], "dst": ["tag:server"], "users": ["deploy"],
				"allowLocalPortForwarding": true
			},
			{
				"action": "check", "src": ["user1@"], "dst": ["tag:server"], "users": ["ops"],
				"allowRemotePortForwarding": true
			}
		]
	}`

	pm, err := NewPolicyManager([]byte(policy), users, nodes.ViewSlice())
	require.NoError(t, err)

	sshPol, err := pm.SSHPolicy("https://control.example", nodes[2].View())
	require.NoError(t, err)

	// Check rules go first, as on the hosted control plane, so find
	// each rule by its source and action rather than its position.
	ruleFor := func(src string, check bool) *tailcfg.SSHAction {
		t.Helper()

		for _, rule := range sshPol.Rules {
			if rule.Principals[0].NodeIP == src && (rule.Action.HoldAndDelegate != "") == check {
				return rule.Action
			}
		}

		require.Failf(t, "rule missing", "no rule from %s (check %t)", src, check)

		return nil
	}

	forwarding := func(a *tailcfg.SSHAction) [3]bool {
		return [3]bool{a.AllowAgentForwarding, a.AllowLocalPortForwarding, a.AllowRemotePortForwarding}
	}

	require.Len(t, sshPol.Rules, 3)

	plain := ruleFor("100.64.0.1", false)
	assert.True(t, plain.Accept)
	assert.Equal(t, [3]bool{true, false, false}, forwarding(plain),
		"an accept rule that does not ask forwards no ports")
	assert.Equal(t, [3]bool{true, true, false}, forwarding(ruleFor("100.64.0.2", false)),
		"allowLocalPortForwarding turns on local forwarding only")
	assert.Equal(t, [3]bool{false, false, false}, forwarding(ruleFor("100.64.0.1", true)),
		"the held action forwards nothing until the check passes")

	final := pm.SSHCheckAccept("https://control.example", 1, 3, "ops")
	assert.Equal(t, [3]bool{true, false, true}, forwarding(&final),
		"the final action carries the check rule's forwarding")
	assert.False(t, final.Accept, "the caller decides whether the check passed")

	assert.Equal(t, tailcfg.SSHAction{}, pm.SSHCheckAccept("https://control.example", 1, 3, "deploy"),
		"no check rule covers deploy, so nothing is allowed")
	assert.Equal(t, tailcfg.SSHAction{}, pm.SSHCheckAccept("https://control.example", 2, 3, "ops"),
		"no check rule covers user2")
}

// TestSSHPortForwardingRejectsNonBool pins that the forwarding switches
// are booleans, so a quoted "true" is an error rather than silently off.
func TestSSHPortForwardingRejectsNonBool(t *testing.T) {
	t.Parallel()

	_, err := unmarshalPolicy([]byte(`{
		"ssh": [{
			"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self"], "users": ["root"],
			"allowLocalPortForwarding": "true"
		}]
	}`))
	require.Error(t, err)
}
