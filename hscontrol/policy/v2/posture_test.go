package v2

import (
	"net/netip"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/posture"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// postureFixture is one user with a mac on a current client, a linux box
// on an old client, and a tagged server every rule points at.
func postureFixture() (types.Users, types.Nodes) {
	users := types.Users{{ID: 1, Name: "alice"}}

	nodes := types.Nodes{
		{
			ID: 1, Hostname: "mac", User: new(users[0]), UserID: new(users[0].ID), IPv4: ap("100.64.0.1"),
			Hostinfo:   &tailcfg.Hostinfo{OS: "macOS", IPNVersion: "1.86.2-t123"},
			SourceAddr: netip.MustParseAddr("203.0.113.7"),
		},
		{
			ID: 2, Hostname: "old-linux", User: new(users[0]), UserID: new(users[0].ID), IPv4: ap("100.64.0.2"),
			Hostinfo:   &tailcfg.Hostinfo{OS: "linux", IPNVersion: "1.30.0"},
			SourceAddr: netip.MustParseAddr("198.51.100.9"),
		},
		{ID: 3, Hostname: "server", Tags: []string{"tag:server"}, IPv4: ap("100.64.0.3")},
	}

	return users, nodes
}

func srcIPsOfServerRules(t *testing.T, pol *Policy, users types.Users, nodes types.Nodes) []string {
	t.Helper()

	rules := pol.compileFilterRules(users, nodes.ViewSlice())

	var srcs []string
	for _, r := range rules {
		srcs = append(srcs, r.SrcIPs...)
	}

	return srcs
}

func TestSrcPostureNarrowsSources(t *testing.T) {
	t.Parallel()

	users, nodes := postureFixture()

	pol, err := unmarshalPolicy([]byte(`{
		"tagOwners": {"tag:server": ["alice@"]},
		"postures": {
			"posture:current": ["node:tsVersion >= '1.40'"],
			"posture:mac": ["node:os == 'macos'"]
		},
		"grants": [
			{"src": ["*"], "dst": ["tag:server"], "ip": ["tcp:22"], "srcPosture": ["posture:current"]}
		]
	}`))
	require.NoError(t, err)
	require.NoError(t, pol.validate())

	assert.Equal(t, []string{"100.64.0.1"}, srcIPsOfServerRules(t, pol, users, nodes),
		"the wildcard narrows to the nodes on a current client")
}

func TestSrcPostureAnyOfSeveral(t *testing.T) {
	t.Parallel()

	users, nodes := postureFixture()

	pol, err := unmarshalPolicy([]byte(`{
		"tagOwners": {"tag:server": ["alice@"]},
		"postures": {
			"posture:current": ["node:tsVersion >= '1.40'"],
			"posture:linux": ["node:os == 'linux'"]
		},
		"acls": [
			{"action": "accept", "src": ["alice@"], "dst": ["tag:server:*"],
			 "srcPosture": ["posture:current", "posture:linux"]}
		]
	}`))
	require.NoError(t, err)
	require.NoError(t, pol.validate())

	assert.Equal(t, []string{"100.64.0.1-100.64.0.2"}, srcIPsOfServerRules(t, pol, users, nodes),
		"either posture admits the source")
}

func TestDefaultSrcPosture(t *testing.T) {
	t.Parallel()

	users, nodes := postureFixture()

	pol, err := unmarshalPolicy([]byte(`{
		"tagOwners": {"tag:server": ["alice@"]},
		"postures": {"posture:mac": ["node:os == 'macos'"]},
		"defaultSrcPosture": ["posture:mac"],
		"grants": [
			{"src": ["alice@"], "dst": ["tag:server"], "ip": ["tcp:22"]},
			{"src": ["alice@"], "dst": ["tag:server"], "ip": ["tcp:80"], "srcPosture": []}
		]
	}`))
	require.NoError(t, err)
	require.NoError(t, pol.validate())

	rules := pol.compileFilterRules(users, nodes.ViewSlice())
	require.Len(t, rules, 2)
	assert.Equal(t, []string{"100.64.0.1"}, rules[0].SrcIPs, "the default applies to the first grant")
	assert.Equal(t, []string{"100.64.0.1-100.64.0.2"}, rules[1].SrcIPs,
		"an explicit empty srcPosture turns the default off")
}

func TestSrcPostureSourceAddress(t *testing.T) {
	t.Parallel()

	users, nodes := postureFixture()

	pol, err := unmarshalPolicy([]byte(`{
		"tagOwners": {"tag:server": ["alice@"]},
		"postures": {
			"posture:office": ["ip:address IN ['203.0.113.0/24']"],
			"posture:vn": ["ip:country == 'VN'"]
		},
		"grants": [
			{"src": ["alice@"], "dst": ["tag:server"], "ip": ["tcp:22"], "srcPosture": ["posture:office"]},
			{"src": ["alice@"], "dst": ["tag:server"], "ip": ["tcp:80"], "srcPosture": ["posture:vn"]}
		]
	}`))
	require.NoError(t, err)
	require.NoError(t, pol.validate())
	assert.True(t, pol.usesSourceAddress())

	rules := pol.compileFilterRules(users, nodes.ViewSlice())
	require.Len(t, rules, 1, "without a GeoIP lookup ip:country never matches, so the second grant is empty")
	assert.Equal(t, []string{"100.64.0.1"}, rules[0].SrcIPs)

	pol.country = func(a netip.Addr) string {
		if a == netip.MustParseAddr("198.51.100.9") {
			return "VN"
		}

		return "US"
	}

	rules = pol.compileFilterRules(users, nodes.ViewSlice())
	require.Len(t, rules, 2)
	assert.Equal(t, []string{"100.64.0.2"}, rules[1].SrcIPs, "the lookup places the linux box in Vietnam")
}

func TestAccessRulePostures(t *testing.T) {
	t.Parallel()

	users, nodes := postureFixture()

	model := types.AccessModel{
		Groups: []types.AccessGroup{
			{ID: 1, Name: "All", Builtin: types.GroupBuiltinAll},
			{ID: 2, Name: "Servers", NodeIDs: []types.NodeID{3}},
		},
		Postures: []types.Posture{
			{ID: 7, Name: "Current client", Expressions: []string{"node:tsVersion >= '1.40'"}},
			{
				ID: 8, Name: "Never", Expressions: []string{"node:os == 'plan9'"},
				Schedule: &posture.Schedule{Days: []string{"mon"}, Start: "00:00", End: "00:01"},
			},
		},
		Rules: []types.AccessRule{{
			ID: 1, Name: "ssh", Enabled: true, Protocol: types.AccessProtocolTCP, Ports: "22",
			SourceGroupIDs: []types.GroupID{1}, DestinationGroupIDs: []types.GroupID{2},
			PostureIDs: []types.PostureID{7, 8},
		}},
	}

	pm, err := NewPolicyManager(nil, users, nodes.ViewSlice())
	require.NoError(t, err)

	_, err = pm.SetAccessModel(model)
	require.NoError(t, err)

	filter, _ := pm.Filter()
	require.Len(t, filter, 1)
	assert.Equal(t, []string{"100.64.0.1"}, filter[0].SrcIPs, "only the current client passes the rule's postures")
	assert.False(t, pm.UsesSourceAddress())
	assert.False(t, pm.NextScheduleBoundary(time.Now()).IsZero(), "the scheduled posture sets a boundary")

	matches := pm.MatchingPostures(nodes[0].View())
	require.Len(t, matches, 1)
	assert.Equal(t, "Current client", matches[0].Name)
	assert.Empty(t, pm.MatchingPostures(nodes[1].View()))
}

func TestPostureValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  string
		wantErr error
	}{
		{
			name:    "posture name without prefix",
			policy:  `{"postures": {"mac": ["node:os == 'macos'"]}}`,
			wantErr: ErrPostureName,
		},
		{
			name:    "reserved database posture name",
			policy:  `{"postures": {"posture:#1": ["node:os == 'macos'"]}}`,
			wantErr: ErrPostureName,
		},
		{
			name:    "empty posture",
			policy:  `{"postures": {"posture:mac": []}}`,
			wantErr: ErrPostureNoExprs,
		},
		{
			name:    "bad expression",
			policy:  `{"postures": {"posture:mac": ["os == macos"]}}`,
			wantErr: ErrPostureExpression,
		},
		{
			name:    "unknown reference in acl",
			policy:  `{"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"], "srcPosture": ["posture:nope"]}]}`,
			wantErr: ErrPostureUnknown,
		},
		{
			name:    "unknown default",
			policy:  `{"defaultSrcPosture": ["posture:nope"]}`,
			wantErr: ErrPostureUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := unmarshalPolicy([]byte(tt.policy))
			require.ErrorContains(t, err, tt.wantErr.Error())
		})
	}
}
