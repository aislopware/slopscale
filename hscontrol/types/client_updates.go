package types

import (
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"tailscale.com/tailcfg"
)

// Client update checks: how often the latest release is looked up, and
// the least often the config may ask for.
const (
	DefaultClientUpdatesInterval = 24 * time.Hour
	MinClientUpdatesInterval     = time.Hour
)

// ErrClientUpdatesIntervalTooShort is returned for an interval under
// [MinClientUpdatesInterval].
var ErrClientUpdatesIntervalTooShort = errors.New("client_updates.interval must be at least 1h")

// ClientUpdatesConfig is whether and how often the server looks up the
// latest stable Tailscale client release at pkgs.tailscale.com, to tell
// each client whether it runs it ([tailcfg.MapResponse.ClientVersion]).
type ClientUpdatesConfig struct {
	Check    bool
	Interval time.Duration
}

// Validate checks the interval.
func (c ClientUpdatesConfig) Validate() error {
	if c.Check && c.Interval < MinClientUpdatesInterval {
		return fmt.Errorf("%w: got %s", ErrClientUpdatesIntervalTooShort, c.Interval)
	}

	return nil
}

// Dial plan timing: the first address is tried at once and each later
// one a little after, so they race without a thundering herd; each try
// has its own timeout before the client falls back to resolving the
// server URL.
const (
	dialPlanStagger = 250 * time.Millisecond
	dialPlanTimeout = 10 * time.Second
)

// DialPlan is what clients are told about reaching the server without
// resolving its name ([tailcfg.MapResponse.ControlDialPlan]): the
// control_dial_plan addresses in order of preference, or nil when the
// config names none.
func (c *Config) DialPlan() *tailcfg.ControlDialPlan {
	if len(c.ControlDialPlan) == 0 {
		return nil
	}

	plan := &tailcfg.ControlDialPlan{Candidates: make([]tailcfg.ControlIPCandidate, 0, len(c.ControlDialPlan))}

	for i, addr := range c.ControlDialPlan {
		plan.Candidates = append(plan.Candidates, tailcfg.ControlIPCandidate{
			IP:                addr,
			DialStartDelaySec: (time.Duration(i) * dialPlanStagger).Seconds(),
			DialTimeoutSec:    dialPlanTimeout.Seconds(),
			Priority:          len(c.ControlDialPlan) - i,
		})
	}

	return plan
}

func clientUpdatesConfig() (ClientUpdatesConfig, error) {
	cfg := ClientUpdatesConfig{
		Check:    conf.GetBool("client_updates.check"),
		Interval: conf.GetDuration("client_updates.interval"),
	}

	err := cfg.Validate()
	if err != nil {
		return ClientUpdatesConfig{}, err
	}

	return cfg, nil
}

// ErrControlDialPlanAddrInvalid is returned for a control_dial_plan entry
// that is not an IP address.
var ErrControlDialPlanAddrInvalid = errors.New("control_dial_plan entries must be IP addresses")

func controlDialPlanConfig() ([]netip.Addr, error) {
	raw := conf.GetStringSlice("control_dial_plan")
	addrs := make([]netip.Addr, 0, len(raw))

	for _, s := range raw {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrControlDialPlanAddrInvalid, s)
		}

		addrs = append(addrs, addr)
	}

	return addrs, nil
}
