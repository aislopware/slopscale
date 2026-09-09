package v2

import (
	"net/netip"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestCompileSSHPolicy_Recording proves that a rule's recorder resolves
// to the recorder nodes' addresses, that the tailnet default fills in
// for a rule without one, that enforcing adds the reject and terminate
// messages, and that a reject rule records nothing.
func TestCompileSSHPolicy_Recording(t *testing.T) {
	t.Parallel()

	users := types.Users{{Name: "user1", ID: 1}, {Name: "user2", ID: 2}}

	server := types.Node{
		Hostname: "server",
		IPv4:     createAddr("100.64.0.1"),
		UserID:   new(users[0].ID),
		User:     new(users[0]),
		Tags:     []string{"tag:server"},
	}
	recorder := types.Node{
		Hostname: "recorder",
		IPv4:     createAddr("100.64.0.9"),
		IPv6:     createAddr("fd7a:115c:a1e0::9"),
		UserID:   new(users[0].ID),
		User:     new(users[0]),
		Tags:     []string{"tag:recorder"},
	}
	laptop := types.Node{
		Hostname: "laptop",
		IPv4:     createAddr("100.64.0.2"),
		UserID:   new(users[1].ID),
		User:     new(users[1]),
	}

	nodes := types.Nodes{&server, &recorder, &laptop}

	policy := &Policy{
		TagOwners: TagOwners{
			Tag("tag:server"):   Owners{up("user1@")},
			Tag("tag:recorder"): Owners{up("user1@")},
		},
		SSHs: []SSH{
			{
				Action:          "accept",
				Sources:         SSHSrcAliases{up("user2@")},
				Destinations:    SSHDstAliases{tp("tag:server")},
				Users:           []SSHUser{"root"},
				Recorder:        SSHRecorderAliases{tp("tag:recorder")},
				EnforceRecorder: true,
			},
			{
				Action:       "check",
				Sources:      SSHSrcAliases{up("user2@")},
				Destinations: SSHDstAliases{tp("tag:server")},
				Users:        []SSHUser{"ubuntu"},
			},
		},
	}

	require.NoError(t, policy.validate())

	// The compiler orders check rules before accept rules, so pick by kind.
	byKind := func(rules []*tailcfg.SSHRule) (tailcfg.SSHAction, tailcfg.SSHAction) {
		var accept, check tailcfg.SSHAction

		for _, rule := range rules {
			if rule.Action.HoldAndDelegate != "" {
				check = *rule.Action
			} else {
				accept = *rule.Action
			}
		}

		return accept, check
	}

	want := []netip.AddrPort{
		netip.MustParseAddrPort("100.64.0.9:80"),
		netip.MustParseAddrPort("[fd7a:115c:a1e0::9]:80"),
	}

	t.Run("a rule's own recorder, enforced", func(t *testing.T) {
		t.Parallel()

		got, err := policy.compileSSHPolicy("https://hs.test", users, server.View(), nodes.ViewSlice(), SSHRecording{})
		require.NoError(t, err)
		require.Len(t, got.Rules, 2)

		accept, check := byKind(got.Rules)
		assert.Equal(t, want, accept.Recorders, "IPv4 first")
		require.NotNil(t, accept.OnRecordingFailure)
		assert.NotEmpty(t, accept.OnRecordingFailure.RejectSessionWithMessage)
		assert.NotEmpty(t, accept.OnRecordingFailure.TerminateSessionWithMessage)
		assert.Equal(t, "https://hs.test"+SSHEventPath, accept.OnRecordingFailure.NotifyURL)

		assert.Empty(t, check.Recorders, "no default, so the check rule records nothing")
		assert.Nil(t, check.OnRecordingFailure)
	})

	t.Run("the tailnet default fills in, not enforced", func(t *testing.T) {
		t.Parallel()

		recording := SSHRecording{Recorders: []Alias{tp("tag:recorder")}}

		got, err := policy.compileSSHPolicy("https://hs.test", users, server.View(), nodes.ViewSlice(), recording)
		require.NoError(t, err)
		require.Len(t, got.Rules, 2)

		_, check := byKind(got.Rules)
		assert.Equal(t, want, check.Recorders)
		require.NotNil(t, check.OnRecordingFailure)
		assert.Empty(t, check.OnRecordingFailure.RejectSessionWithMessage, "fail open")
		assert.Empty(t, check.OnRecordingFailure.TerminateSessionWithMessage)
		assert.Equal(t, "https://hs.test"+SSHEventPath, check.OnRecordingFailure.NotifyURL)
	})

	t.Run("an address recorder needs no node", func(t *testing.T) {
		t.Parallel()

		var prefix Prefix
		require.NoError(t, prefix.parseString("192.0.2.7"))

		recording := SSHRecording{Recorders: []Alias{&prefix}, Enforce: true}

		got, err := policy.compileSSHPolicy("https://hs.test", users, server.View(), nodes.ViewSlice(), recording)
		require.NoError(t, err)

		_, check := byKind(got.Rules)
		assert.Equal(t, []netip.AddrPort{netip.MustParseAddrPort("192.0.2.7:80")}, check.Recorders)
		assert.NotEmpty(t, check.OnRecordingFailure.RejectSessionWithMessage, "the default enforces")
	})
}

func TestParseSSHRecorders(t *testing.T) {
	t.Parallel()

	aliases, err := ParseSSHRecorders([]string{"tag:recorder", "100.64.0.9", "recorder-host"})
	require.NoError(t, err)
	assert.Len(t, aliases, 3)

	_, err = ParseSSHRecorders([]string{"group:ops"})
	require.ErrorIs(t, err, ErrSSHRecorderAliasNotSupported)

	_, err = ParseSSHRecorders([]string{"user@"})
	require.ErrorIs(t, err, ErrSSHRecorderAliasNotSupported)
}

func TestSSHRecorderAliasesJSON(t *testing.T) {
	t.Parallel()

	pol, err := unmarshalPolicy([]byte(`{
		"tagOwners": {"tag:server": ["user1@"], "tag:recorder": ["user1@"]},
		"ssh": [{
			"action": "accept", "src": ["user1@"], "dst": ["tag:server"], "users": ["root"],
			"recorder": ["tag:recorder"], "enforceRecorder": true
		}]
	}`))
	require.NoError(t, err)
	require.Len(t, pol.SSHs, 1)
	assert.Len(t, pol.SSHs[0].Recorder, 1)
	assert.True(t, pol.SSHs[0].EnforceRecorder)

	_, err = unmarshalPolicy([]byte(`{
		"ssh": [{
			"action": "accept", "src": ["user1@"], "dst": ["tag:server"], "users": ["root"],
			"recorder": ["group:ops"]
		}]
	}`))
	require.ErrorIs(t, err, ErrSSHRecorderAliasNotSupported)
}

// TestRecorderGrants proves the policy opens the recorder port to every
// node for the tailnet default and for each rule's own recorders, and
// adds nothing when nothing records.
func TestRecorderGrants(t *testing.T) {
	t.Parallel()

	pol := &Policy{}
	assert.Nil(t, pol.recorderGrants(), "nothing records")

	defaults, err := ParseSSHRecorders([]string{"tag:recorder"})
	require.NoError(t, err)

	pol.recording = SSHRecording{Recorders: defaults}
	own := Prefix(netip.MustParsePrefix("100.64.0.9/32"))
	pol.SSHs = []SSH{{Recorder: SSHRecorderAliases{&own}}}

	grants := pol.recorderGrants()
	require.Len(t, grants, 1)

	assert.Equal(t, Aliases{Wildcard}, grants[0].Sources)
	assert.Len(t, grants[0].Destinations, 2, "the default and the rule's own")
	require.Len(t, grants[0].InternetProtocols, 1)
	assert.Equal(t, ProtocolNameTCP, grants[0].InternetProtocols[0].Protocol)
	assert.Equal(
		t,
		[]tailcfg.PortRange{{First: RecorderPort, Last: RecorderPort}},
		grants[0].InternetProtocols[0].Ports,
	)
}
