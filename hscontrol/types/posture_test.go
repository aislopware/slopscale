package types

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/posture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestPostureAttributesReportedOnly covers what a node that reports
// nothing looks like to a posture: an unreported attribute is absent, so
// it fails every check but NOT SET, matching Tailscale.
func TestPostureAttributesReportedOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostinfo *tailcfg.Hostinfo
		expr     string
		want     bool
	}{
		{
			name:     "no hostinfo, NOT IN does not match",
			hostinfo: nil,
			expr:     "node:os NOT IN ['ios', 'android']",
			want:     false,
		},
		{
			name:     "empty hostinfo, NOT IN does not match",
			hostinfo: &tailcfg.Hostinfo{},
			expr:     "node:os NOT IN ['ios', 'android']",
			want:     false,
		},
		{
			name:     "empty hostinfo, NOT SET matches",
			hostinfo: &tailcfg.Hostinfo{},
			expr:     "node:os NOT SET",
			want:     true,
		},
		{
			name:     "reported os, NOT IN matches",
			hostinfo: &tailcfg.Hostinfo{OS: "linux"},
			expr:     "node:os NOT IN ['ios', 'android']",
			want:     true,
		},
		{
			name:     "reported os, IN matches",
			hostinfo: &tailcfg.Hostinfo{OS: "Linux"},
			expr:     "node:os IN ['linux']",
			want:     true,
		},
		{
			name:     "reported os, NOT SET does not match",
			hostinfo: &tailcfg.Hostinfo{OS: "linux"},
			expr:     "node:os NOT SET",
			want:     false,
		},
		{
			name:     "no version reported, release track is absent",
			hostinfo: &tailcfg.Hostinfo{OS: "linux"},
			expr:     "node:tsReleaseTrack NOT SET",
			want:     true,
		},
		{
			name:     "version reported, release track derived",
			hostinfo: &tailcfg.Hostinfo{IPNVersion: "1.86.2-t1a2b"},
			expr:     "node:tsReleaseTrack == 'stable'",
			want:     true,
		},
		{
			name:     "no version reported, auto update is absent",
			hostinfo: &tailcfg.Hostinfo{OS: "linux"},
			expr:     "node:tsAutoUpdate NOT SET",
			want:     true,
		},
		{
			name:     "version reported, auto update is present",
			hostinfo: &tailcfg.Hostinfo{IPNVersion: "1.86.2", AllowsUpdate: true},
			expr:     "node:tsAutoUpdate == true",
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			node := Node{Hostinfo: tc.hostinfo}
			attrs := node.postureAttributes(time.Now())

			expr, err := posture.Parse(tc.expr)
			require.NoError(t, err)

			assert.Equal(t, tc.want, expr.Eval(attrs), "attrs: %v", attrs)
		})
	}
}

// TestPostureAttributesOmitsUnreported pins that an empty Hostinfo adds
// no string attribute at all, only what the server knows itself.
func TestPostureAttributesOmitsUnreported(t *testing.T) {
	t.Parallel()

	node := Node{Hostinfo: &tailcfg.Hostinfo{}}
	attrs := node.postureAttributes(time.Now())

	assert.Equal(t, PostureAttributes{AttrTagged: false}, attrs)
}
