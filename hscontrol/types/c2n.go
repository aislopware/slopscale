package types

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/net/netutil"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/types/opt"
	"tailscale.com/types/views"
)

// RemoteConfigCapVer is the capability version at which a client answers
// the c2n /remoteapi/localapi/* proxy the managed preferences ride on;
// an older client has no handler for it whatever its prefs say.
const RemoteConfigCapVer tailcfg.CapabilityVersion = 142

// ErrPreferencesInvalid is returned when a preferences patch cannot be
// turned into an edit the client would accept.
var ErrPreferencesInvalid = errors.New("preferences are invalid")

// ClientUpdate is what a client says about updating its own Tailscale
// installation, the answer to the c2n /update request. Enabled is the
// machine owner's opt-in (`tailscale set --auto-update` or
// TS_ALLOW_REMOTE_UPDATE), Supported whether the platform can update
// itself at all, and Error the reason the client gave for refusing.
type ClientUpdate struct {
	Enabled   bool
	Supported bool
	Started   bool
	Error     string
}

// ClientHealth is the client's own health report: the warnings it would
// show its user, which explain a node that is connected but not working.
type ClientHealth struct {
	Warnings []ClientWarning
}

// ClientWarning is one unhealthy warnable on a client.
type ClientWarning struct {
	Code                string
	Severity            string
	Title               string
	Text                string
	BrokenSince         *time.Time
	ImpactsConnectivity bool
}

// DiagnosticKind names one of the dumps a client will hand over for
// support, each a c2n path of its own.
type DiagnosticKind string

// The diagnostics an operator may ask a client for.
const (
	DiagnosticPrefs      DiagnosticKind = "prefs"
	DiagnosticNetmap     DiagnosticKind = "netmap"
	DiagnosticMetrics    DiagnosticKind = "metrics"
	DiagnosticGoroutines DiagnosticKind = "goroutines"
	DiagnosticSockstats  DiagnosticKind = "sockstats"
	DiagnosticTKALog     DiagnosticKind = "tka-log"
)

// DiagnosticKinds lists the kinds in the order the API and the CLI
// present them.
var DiagnosticKinds = []DiagnosticKind{
	DiagnosticPrefs,
	DiagnosticNetmap,
	DiagnosticMetrics,
	DiagnosticGoroutines,
	DiagnosticSockstats,
	DiagnosticTKALog,
}

// AppConnectorRoutes is what an app connector has learned: the domains it
// answers for and the addresses it resolved for each.
type AppConnectorRoutes struct {
	Domains map[string][]netip.Addr
}

// TLSCertStatus is the state of the TLS certificate a client caches for
// its MagicDNS name, which Serve and Funnel need.
type TLSCertStatus struct {
	Valid   bool
	Missing bool
	Expired bool
	Error   string
}

// NodePreferences is the curated part of a client's [ipn.Prefs] the
// console shows and, on a machine that opted into remote config, edits.
// AdvertiseRoutes leaves the two default routes out; they are
// AdvertiseExitNode, as the client's own CLI presents them.
type NodePreferences struct {
	AdvertiseRoutes        []string
	AdvertiseExitNode      bool
	AcceptRoutes           bool
	AcceptDNS              bool
	ExitNode               string
	ExitNodeAllowLANAccess bool
	RunSSH                 bool
	ShieldsUp              bool
	Hostname               string
	AutoUpdateCheck        bool
	AutoUpdateApply        bool
	AdvertiseConnector     bool
	PostureChecking        bool
}

// NodePreferencesPatch changes the fields it names and leaves the rest
// alone: a nil field is untouched.
type NodePreferencesPatch struct {
	AdvertiseRoutes        *[]string
	AdvertiseExitNode      *bool
	AcceptRoutes           *bool
	AcceptDNS              *bool
	ExitNode               *string
	ExitNodeAllowLANAccess *bool
	RunSSH                 *bool
	ShieldsUp              *bool
	Hostname               *string
	AutoUpdateCheck        *bool
	AutoUpdateApply        *bool
	AdvertiseConnector     *bool
	PostureChecking        *bool
}

// NodePreferencesFrom reads the curated fields out of a client's prefs.
func NodePreferencesFrom(prefs ipn.Prefs) NodePreferences {
	out := NodePreferences{
		AdvertiseRoutes:        []string{},
		AdvertiseExitNode:      prefs.AdvertisesExitNode(),
		AcceptRoutes:           prefs.RouteAll,
		AcceptDNS:              prefs.CorpDNS,
		ExitNodeAllowLANAccess: prefs.ExitNodeAllowLANAccess,
		RunSSH:                 prefs.RunSSH,
		ShieldsUp:              prefs.ShieldsUp,
		Hostname:               prefs.Hostname,
		AutoUpdateCheck:        prefs.AutoUpdate.Check,
		AutoUpdateApply:        prefs.AutoUpdate.Apply.EqualBool(true),
		AdvertiseConnector:     prefs.AppConnector.Advertise,
		PostureChecking:        prefs.PostureChecking,
	}

	for _, route := range prefs.AdvertiseRoutes {
		if route.Bits() == 0 {
			continue
		}

		out.AdvertiseRoutes = append(out.AdvertiseRoutes, route.String())
	}

	switch {
	case prefs.ExitNodeID != "":
		out.ExitNode = string(prefs.ExitNodeID)
	case prefs.ExitNodeIP.IsValid():
		out.ExitNode = prefs.ExitNodeIP.String()
	}

	return out
}

// Changed names the fields the patch sets, in the order they appear
// above, for the audit record.
func (patch NodePreferencesPatch) Changed() []string {
	fields := []struct {
		name string
		set  bool
	}{
		{"advertiseRoutes", patch.AdvertiseRoutes != nil},
		{"advertiseExitNode", patch.AdvertiseExitNode != nil},
		{"acceptRoutes", patch.AcceptRoutes != nil},
		{"acceptDns", patch.AcceptDNS != nil},
		{"exitNode", patch.ExitNode != nil},
		{"exitNodeAllowLanAccess", patch.ExitNodeAllowLANAccess != nil},
		{"runSsh", patch.RunSSH != nil},
		{"shieldsUp", patch.ShieldsUp != nil},
		{"hostname", patch.Hostname != nil},
		{"autoUpdateCheck", patch.AutoUpdateCheck != nil},
		{"autoUpdateApply", patch.AutoUpdateApply != nil},
		{"advertiseConnector", patch.AdvertiseConnector != nil},
		{"postureChecking", patch.PostureChecking != nil},
	}

	out := make([]string, 0, len(fields))

	for _, f := range fields {
		if f.set {
			out = append(out, f.name)
		}
	}

	return out
}

// IsEmpty reports whether the patch would change nothing.
func (patch NodePreferencesPatch) IsEmpty() bool {
	return len(patch.Changed()) == 0
}

// MaskedPrefs turns the patch into the edit the client's LocalAPI takes,
// against the prefs it currently holds. The exit node advertisement lives
// inside AdvertiseRoutes as the two default routes, so a patch that
// touches either it or the routes recomputes both together, the way
// `tailscale set` does.
func (patch NodePreferencesPatch) MaskedPrefs(current ipn.Prefs) (*ipn.MaskedPrefs, error) {
	masked := &ipn.MaskedPrefs{}

	err := patch.maskRoutes(current, masked)
	if err != nil {
		return nil, err
	}

	patch.maskExitNode(masked)
	patch.maskBools(masked)

	if patch.Hostname != nil {
		masked.Hostname = *patch.Hostname
		masked.HostnameSet = true
	}

	if patch.AutoUpdateCheck != nil {
		masked.AutoUpdate.Check = *patch.AutoUpdateCheck
		masked.AutoUpdateSet.CheckSet = true
	}

	if patch.AutoUpdateApply != nil {
		masked.AutoUpdate.Apply = opt.NewBool(*patch.AutoUpdateApply)
		masked.AutoUpdateSet.ApplySet = true
	}

	if patch.AdvertiseConnector != nil {
		masked.AppConnector.Advertise = *patch.AdvertiseConnector
		masked.AppConnectorSet = true
	}

	return masked, nil
}

// maskBools sets the fields that are a bool on both sides.
func (patch NodePreferencesPatch) maskBools(masked *ipn.MaskedPrefs) {
	if patch.AcceptRoutes != nil {
		masked.RouteAll = *patch.AcceptRoutes
		masked.RouteAllSet = true
	}

	if patch.AcceptDNS != nil {
		masked.CorpDNS = *patch.AcceptDNS
		masked.CorpDNSSet = true
	}

	if patch.ExitNodeAllowLANAccess != nil {
		masked.ExitNodeAllowLANAccess = *patch.ExitNodeAllowLANAccess
		masked.ExitNodeAllowLANAccessSet = true
	}

	if patch.RunSSH != nil {
		masked.RunSSH = *patch.RunSSH
		masked.RunSSHSet = true
	}

	if patch.ShieldsUp != nil {
		masked.ShieldsUp = *patch.ShieldsUp
		masked.ShieldsUpSet = true
	}

	if patch.PostureChecking != nil {
		masked.PostureChecking = *patch.PostureChecking
		masked.PostureCheckingSet = true
	}
}

// maskExitNode points the node at an exit node named by stable id or by
// address, clearing the other field so at most one is set; an empty
// value clears both and stops using an exit node.
func (patch NodePreferencesPatch) maskExitNode(masked *ipn.MaskedPrefs) {
	if patch.ExitNode == nil {
		return
	}

	masked.ExitNodeIDSet = true
	masked.ExitNodeIPSet = true

	value := strings.TrimSpace(*patch.ExitNode)
	if value == "" {
		return
	}

	addr, err := netip.ParseAddr(value)
	if err != nil {
		masked.ExitNodeID = tailcfg.StableNodeID(value)

		return
	}

	masked.ExitNodeIP = addr
}

// maskRoutes recomputes AdvertiseRoutes from the routes and the exit node
// advertisement, whichever of the two the patch names.
func (patch NodePreferencesPatch) maskRoutes(current ipn.Prefs, masked *ipn.MaskedPrefs) error {
	if patch.AdvertiseRoutes == nil && patch.AdvertiseExitNode == nil {
		return nil
	}

	masked.AdvertiseRoutesSet = true

	if patch.AdvertiseRoutes != nil {
		exitNode := current.AdvertisesExitNode()
		if patch.AdvertiseExitNode != nil {
			exitNode = *patch.AdvertiseExitNode
		}

		routes, err := netutil.CalcAdvertiseRoutes(strings.Join(*patch.AdvertiseRoutes, ","), exitNode)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrPreferencesInvalid, err)
		}

		masked.AdvertiseRoutes = routes

		return nil
	}

	if current.AdvertisesExitNode() == *patch.AdvertiseExitNode {
		masked.AdvertiseRoutes = current.AdvertiseRoutes

		return nil
	}

	routes := tsaddr.FilterPrefixesCopy(views.SliceOf(current.AdvertiseRoutes), func(p netip.Prefix) bool {
		return p.Bits() != 0
	})
	if *patch.AdvertiseExitNode {
		routes = append(routes, tsaddr.AllIPv4(), tsaddr.AllIPv6())
	}

	masked.AdvertiseRoutes = routes

	return nil
}
