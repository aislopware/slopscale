package v2

// This file enumerates [tailcfg.NodeCapability] values that the
// Tailscale-hosted control plane emits where slopscale has no
// equivalent concept yet. The compat test in
// tailscale_nodeattrs_compat_test.go builds the self-view CapMap via
// [types.Node.TailNode] -- the same call the mapper makes -- and
// strips these from BOTH sides before [cmp.Diff]; every other cap is
// compared in full as it lands on the wire.
//
// Each entry documents its purpose (cross-referenced to Tailscale
// source), why slopscale does not emit it, and a tracking issue where
// one exists.

import (
	"maps"
	"slices"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

// PeerCapMap returns the caps a peer entry carries: the few the Tailscale
// client reads from the peer view rather than the self view
// (suggest-exit-node at ipn/ipnlocal/local.go suggestExitNode,
// dns-subdomain-resolve at node_backend.go magicDNSSubdomainHost), each
// under its own condition. It returns nil when none applies, matching
// the empirical wire shape where [tailcfg.Node.CapMap] is omitted for
// most peers. The mapper calls it from
// [mapper.MapResponseBuilder.buildTailPeers] and the compat test calls
// it to compute the expected per-peer wire shape.
//
// suggest-exit-node goes on every peer with approved exit routes, which
// is what the hosted control plane does with no nodeAttrs at all (the
// issue_3212 captures with an exit auto-approver): a client without a
// suggested peer shows no exit nodes on Apple platforms since Tailscale
// 1.102 (aislopware/slopscale#3415). A global exit node narrows the set:
// while one is marked, globalExitNodes is true and only peers whose own
// caps carry suggest-exit-node (the marked ones, and any a nodeAttrs
// grant names) are suggested. Approval gates the cap in both cases so a
// suggestion never follows an advertised-but-not-yet-trusted node.
func PeerCapMap(peer types.NodeView, peerSelfCaps tailcfg.NodeCapMap, globalExitNodes bool) tailcfg.NodeCapMap {
	var out tailcfg.NodeCapMap

	if peer.IsExitNode() {
		v, marked := peerSelfCaps[nodecap.SuggestExitNode]
		if marked || !globalExitNodes {
			out = tailcfg.NodeCapMap{nodecap.SuggestExitNode: v}
		}
	}

	// dns-subdomain-resolve — the client answers *.<peer name> with the
	// peer's addresses when the peer carries the cap on its peer view;
	// the self view only covers the node's own name. Nothing gates it,
	// so it is copied whenever the policy stamps it on the peer.
	// See aislopware/slopscale#3322.
	if v, ok := peerSelfCaps[nodecap.DNSSubdomainResolve]; ok {
		if out == nil {
			out = tailcfg.NodeCapMap{}
		}

		out[nodecap.DNSSubdomainResolve] = v
	}

	return out
}

// unmodelledTailnetStateCaps lists [tailcfg.NodeCapability] values
// stripped on both sides of the compat diff. Order:
//
//  1. Caps gated on state the anonymised captures do not carry: user
//     roles, tailnet lock, services.
//  2. Caps gated on a tailnet feature slopscale does not implement.
//  3. Caps that are tailnet-state metadata (display name, key
//     duration, etc.) where the values are not derivable from
//     slopscale config in a way that round-trips through the
//     anonymized capture.
//  4. Caps that are internal magicsock or embedded-SSH tuning with no
//     slopscale-side equivalent.
var unmodelledTailnetStateCaps = []nodecap.Cap{
	// --- 1. User-role gated ---

	// [tailcfg.CapabilityAdmin]: the hosted control plane stamps this
	// on nodes whose owning user has the admin role; tagged nodes
	// inherit from a tagOwner with the role. Slopscale stamps it from
	// the user's role too (see stampRoleCaps), but the anonymised
	// captures carry no roles, so the replayed tailnet cannot
	// reproduce which users held one. Stripping on both sides keeps
	// the diff from failing on that missing input.
	nodecap.Admin,

	// [tailcfg.CapabilityOwner]: same shape as is-admin, conditional
	// on the "owner" role rather than admin; stripped for the same
	// reason.
	nodecap.Owner,

	// [tailcfg.CapabilityTailnetLock]: tailnet-lock signs node keys
	// with a tailnet-wide signing key so peers can detect silent
	// re-keying by the control plane. Client reads at
	// ipn/ipnlocal/local.go:1752 (b.capTailnetLock). The mapper stamps
	// it on every self node (see docs/ref/tailnet-lock.md); the
	// captures carry no lock state, so it is stripped here.
	nodecap.TailnetLock,

	// [tailcfg.NodeAttrServiceHost]: marks a node as approved to host
	// VIP services (Tailscale Services). Client reads via
	// UnmarshalNodeCapViewJSON at ipn/ipnlocal/local.go:2704.
	// Slopscale stamps it from the services table (see
	// stampServiceCaps), but the anonymised captures carry no
	// services, so the replayed tailnet cannot reproduce the mapping.
	nodecap.ServiceHost,

	// --- 2. Feature not implemented ---

	// [tailcfg.NodeAttrStoreAppCRoutes]: tells an app-connector node
	// to persist learned routes across restarts. Client reads via
	// controlknobs:148. Slopscale does not implement app connectors.
	nodecap.StoreAppCRoutes,

	// [tailcfg.CapabilityWarnFunnelNoHTTPS]: deprecated in Tailscale
	// 2023-08-09. Should not appear in fresh captures — listed
	// defensively in case a stale tailnet still emits it.
	nodecap.WarnFunnelNoHTTPS,

	// --- 3. Tailnet-state metadata not derivable from slopscale config ---

	// [tailcfg.NodeAttrTailnetDisplayName]: tailnet display name
	// surfaced in the client UI. The hosted control plane emits the
	// tailnet admin's email; slopscale would have to invent a value
	// from cfg.Domain() that does not round-trip through the
	// anonymized capture string. Skip rather than diverge on a value
	// with no real-world equivalent.
	nodecap.TailnetDisplayName,

	// [tailcfg.NodeAttrNativeIPV4]: peer-consumed cap conditional on
	// tailnet ipv4 reachability state. Out of scope for the current
	// peer-cap adoption (only suggest-exit-node is wired in this
	// PR).
	nodecap.NativeIPV4,

	// --- 4. Internal tuning, no slopscale equivalent ---

	// [tailcfg.NodeAttrProbeUDPLifetime]: tunes magicsock's UDP
	// path-lifetime probe behavior. Internal performance knob; not
	// policy-driven. Client reads via controlknobs:147.
	nodecap.ProbeUDPLifetime,

	// [tailcfg.NodeAttrSSHBehaviorV1]: configures the embedded SSH
	// server (no su, in-process SFTP). Internal tuning; the embedded
	// server picks Tailscale-vendored defaults without the cap.
	nodecap.SSHBehaviorV1,

	// [tailcfg.NodeAttrSSHEnvironmentVariables]: gates SendEnv
	// forwarding in the embedded SSH server. Internal; default chosen
	// by the server.
	nodecap.SSHEnvironmentVariables,
}

// strippedCapPrefixes lists URL/string prefixes for parameterized or
// pattern-named caps that should be stripped alongside
// [unmodelledTailnetStateCaps].
var strippedCapPrefixes = []string{}

// stripUnmodelledTailnetStateCaps returns a copy of cm with
// [unmodelledTailnetStateCaps] and [strippedCapPrefixes] removed. Used
// by the compat test on both sides before [cmp.Diff].
func stripUnmodelledTailnetStateCaps(cm tailcfg.NodeCapMap) tailcfg.NodeCapMap {
	if len(cm) == 0 {
		return nil
	}

	out := maps.Clone(cm)
	maps.DeleteFunc(out, func(k nodecap.Cap, _ []tailcfg.RawMessage) bool {
		return isUnmodelledTailnetStateCap(k)
	})

	if len(out) == 0 {
		return nil
	}

	return out
}

func isUnmodelledTailnetStateCap(k nodecap.Cap) bool {
	if slices.Contains(unmodelledTailnetStateCaps, k) {
		return true
	}

	s := string(k)
	for _, p := range strippedCapPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}

	return false
}
