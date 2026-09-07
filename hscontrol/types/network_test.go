package types

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNetworkPrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
		err   error
	}{
		{
			name:  "masks dedupes and skips blanks",
			input: []string{" 10.10.0.5/24 ", "", "10.10.0.0/24", "192.168.1.1"},
			want:  []string{"10.10.0.0/24", "192.168.1.1/32"},
		},
		{
			name:  "one exit route brings the other",
			input: []string{"0.0.0.0/0"},
			want:  []string{"0.0.0.0/0", "::/0"},
		},
		{
			name:  "bare ipv6 address becomes a host prefix",
			input: []string{"fd7a::1"},
			want:  []string{"fd7a::1/128"},
		},
		{
			name:  "garbage is rejected",
			input: []string{"10.10.0.0/24", "office"},
			err:   ErrNetworkPrefixInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseNetworkPrefixes(tt.input)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)

				return
			}

			require.NoError(t, err)

			want := make([]netip.Prefix, 0, len(tt.want))
			for _, w := range tt.want {
				want = append(want, netip.MustParsePrefix(w))
			}

			assert.Equal(t, want, got)
		})
	}
}

func TestValidateNetwork(t *testing.T) {
	t.Parallel()

	valid := Network{
		Name:     "office",
		Prefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/24")},
		GroupIDs: []GroupID{1},
	}
	require.NoError(t, ValidateNetwork(valid))

	tests := []struct {
		name   string
		mutate func(n *Network)
		err    error
	}{
		{"blank name", func(n *Network) { n.Name = "  " }, ErrNetworkNameEmpty},
		{"long name", func(n *Network) { n.Name = strings.Repeat("x", maxAccessNameLength+1) }, ErrNetworkNameTooLong},
		{"no prefixes", func(n *Network) { n.Prefixes = nil }, ErrNetworkNoPrefixes},
		{"no groups", func(n *Network) { n.GroupIDs = nil }, ErrNetworkNoGroups},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n := valid
			tt.mutate(&n)
			assert.ErrorIs(t, ValidateNetwork(n), tt.err)
		})
	}
}

func TestNetworkHelpers(t *testing.T) {
	t.Parallel()

	n := Network{
		Enabled:       true,
		Prefixes:      []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
		RouterNodeIDs: []NodeID{3},
	}

	assert.True(t, n.IsExitNode())
	assert.True(t, n.Routes(3))
	assert.False(t, n.Routes(4))
	assert.True(t, n.Covers(netip.MustParsePrefix("::/0")))

	n.Enabled = false
	assert.False(t, n.Routes(3), "a disabled network routes nothing")
}
