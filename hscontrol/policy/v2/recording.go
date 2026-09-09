package v2

import (
	"fmt"
	"net/netip"
	"slices"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
	"tailscale.com/types/views"
)

// SSHRecording is the tailnet's default for session recording, applied
// to every SSH rule that names no recorder of its own: where sessions
// are streamed to and whether a session may go on when that fails. See
// docs/ref/ssh-recording.md.
type SSHRecording struct {
	Recorders []Alias
	Enforce   bool
}

// RecorderPort is the port a session recorder listens on; the client
// speaks plain HTTP to it over the tailnet.
const RecorderPort = types.SSHRecorderPort

// SSHEventPath is the control path a client reports recording failures
// to over Noise, without the host.
const SSHEventPath = "/machine/ssh/event"

// ParseSSHRecorders parses recorder aliases as the settings store them,
// with the same rules as a rule's recorder list.
func ParseSSHRecorders(names []string) ([]Alias, error) {
	out := make([]Alias, 0, len(names))

	for _, name := range names {
		alias, err := parseAlias(name)
		if err != nil {
			return nil, err
		}

		switch alias.(type) {
		case *Tag, *Host, *Prefix:
			out = append(out, alias)
		default:
			return nil, fmt.Errorf("%w: %q", ErrSSHRecorderAliasNotSupported, name)
		}
	}

	return out, nil
}

// recordersFor returns the recorders an SSH rule's sessions go to and
// whether recording is enforced: the rule's own when it names any, the
// tailnet default otherwise. A rule may enforce the default recorders.
func (pol *Policy) recordersFor(
	rule SSH,
	recording SSHRecording,
	users types.Users,
	nodes views.Slice[types.NodeView],
) ([]netip.AddrPort, bool) {
	aliases, enforce := rule.Recorder, rule.EnforceRecorder
	if len(aliases) == 0 {
		aliases = recording.Recorders
		enforce = enforce || recording.Enforce
	}

	return pol.resolveRecorders(aliases, users, nodes), enforce
}

// resolveRecorders turns recorder aliases into addresses: every single
// address the aliases resolve to, on the recorder port, IPv4 before IPv6
// so a dual-stack recorder is tried once the fast way.
func (pol *Policy) resolveRecorders(
	aliases []Alias,
	users types.Users,
	nodes views.Slice[types.NodeView],
) []netip.AddrPort {
	var out []netip.AddrPort

	for _, alias := range aliases {
		ips, err := alias.Resolve(pol, users, nodes)
		if err != nil || ips == nil {
			continue
		}

		for _, prefix := range ips.Prefixes() {
			if !prefix.IsSingleIP() {
				continue
			}

			addr := netip.AddrPortFrom(prefix.Addr().Unmap(), RecorderPort)
			if !slices.Contains(out, addr) {
				out = append(out, addr)
			}
		}
	}

	slices.SortStableFunc(out, func(a, b netip.AddrPort) int {
		switch {
		case a.Addr().Is4() && !b.Addr().Is4():
			return -1
		case !a.Addr().Is4() && b.Addr().Is4():
			return 1
		default:
			return 0
		}
	})

	return out
}

// recordingFailure is what the client does when it cannot record:
// always tell control, and when enforced refuse or end the session.
func recordingFailure(baseURL string, enforce bool) *tailcfg.SSHRecorderFailureAction {
	action := &tailcfg.SSHRecorderFailureAction{NotifyURL: baseURL + SSHEventPath}

	if enforce {
		action.RejectSessionWithMessage = "This session must be recorded and no session recorder is reachable."
		action.TerminateSessionWithMessage = "The session recording failed and the session is being ended."
	}

	return action
}

// withRecording adds the recorders to an accept or check action; a
// reject has no session to record.
func withRecording(
	action tailcfg.SSHAction,
	baseURL string,
	recorders []netip.AddrPort,
	enforce bool,
) tailcfg.SSHAction {
	if action.Reject || len(recorders) == 0 {
		return action
	}

	action.Recorders = recorders
	action.OnRecordingFailure = recordingFailure(baseURL, enforce)

	return action
}

// recorderGrants is the grant that lets every node upload to the
// recorders: the tailnet default's and every SSH rule's, on the
// recorder port. Without it an enforcing policy would have to name
// each recorder by hand, and a session that cannot reach its recorder
// is rejected when recording is enforced. Nil when nothing records.
func (pol *Policy) recorderGrants() []Grant {
	if pol == nil {
		return nil
	}

	var recorders []Alias

	recorders = append(recorders, pol.recording.Recorders...)

	for _, rule := range pol.SSHs {
		recorders = append(recorders, rule.Recorder...)
	}

	if len(recorders) == 0 {
		return nil
	}

	port := tailcfg.PortRange{First: RecorderPort, Last: RecorderPort}

	return []Grant{{
		Sources:           Aliases{Wildcard},
		Destinations:      Aliases(recorders),
		InternetProtocols: []ProtocolPort{{Protocol: ProtocolNameTCP, Ports: []tailcfg.PortRange{port}}},
	}}
}
