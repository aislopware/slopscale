package types_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientUpdatesConfigValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, types.ClientUpdatesConfig{Check: true, Interval: 24 * time.Hour}.Validate())
	require.NoError(
		t,
		types.ClientUpdatesConfig{Check: false, Interval: time.Minute}.Validate(),
		"off ignores the interval",
	)
	require.ErrorIs(t,
		types.ClientUpdatesConfig{Check: true, Interval: time.Minute}.Validate(),
		types.ErrClientUpdatesIntervalTooShort,
	)
}

func TestDialPlan(t *testing.T) {
	t.Parallel()

	assert.Nil(t, (&types.Config{}).DialPlan(), "no addresses means the client resolves the name")

	cfg := &types.Config{ControlDialPlan: []netip.Addr{
		netip.MustParseAddr("203.0.113.7"),
		netip.MustParseAddr("2001:db8::7"),
	}}

	plan := cfg.DialPlan()
	require.NotNil(t, plan)
	require.Len(t, plan.Candidates, 2)

	first, second := plan.Candidates[0], plan.Candidates[1]
	assert.Equal(t, netip.MustParseAddr("203.0.113.7"), first.IP)
	assert.InDelta(t, 0, first.DialStartDelaySec, 0)
	assert.Equal(t, 2, first.Priority)
	assert.Equal(t, netip.MustParseAddr("2001:db8::7"), second.IP)
	assert.Greater(t, second.DialStartDelaySec, first.DialStartDelaySec, "later addresses start later")
	assert.Equal(t, 1, second.Priority)
	assert.InDelta(t, 10, second.DialTimeoutSec, 0)
}
