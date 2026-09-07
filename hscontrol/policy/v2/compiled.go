package v2

import (
	"fmt"
	"net/netip"
	"slices"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/rs/zerolog/log"
	"go4.org/netipx"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tailcfg/peercap"
	"tailscale.com/types/views"
	"tailscale.com/util/set"
)

// grantCategory classifies a grant by what per-node work it needs.
type grantCategory int

const (
	// grantCategoryRegular requires no per-node work. The pre-compiled
	// rules are complete and only need [policyutil.ReduceFilterRules].
	grantCategoryRegular grantCategory = iota

	// grantCategorySelf has autogroup:self destinations that must be
	// expanded per-node to same-user untagged device IPs.
	grantCategorySelf

	// grantCategoryVia has Via tags that route rules to specific
	// nodes based on their tags and advertised routes.
	grantCategoryVia

	// grantCategoryShared has an autogroup:shared source that expands
	// per node to the devices of the users the node is shared with.
	grantCategoryShared
)

// compiledGrant is a grant with its sources already resolved to IP
// addresses. The expensive work (alias → IP resolution) is done once
// here. Extracting rules for a specific node reads from pre-resolved
// data without re-resolving.
type compiledGrant struct {
	category grantCategory

	// srcIPStrings is the final SrcIPs for non-self rules, with
	// nonWildcardSrcs appended to match Tailscale SaaS behavior.
	srcIPStrings []string

	hasWildcard  bool
	hasDangerAll bool

	// rules are the pre-compiled filter rules for non-self, non-via
	// destinations. For regular grants this is the complete output.
	// For self grants with mixed destinations (self + other), this
	// is the non-self portion only.
	rules []tailcfg.FilterRule

	// self is non-nil when the grant has autogroup:self destinations.
	self *selfGrantData

	// via is non-nil when the grant has Via tags.
	via *viaGrantData

	// shared is non-nil when the grant has an autogroup:shared source.
	shared *sharedGrantData
}

// sharedGrantData holds what [compileAutogroupShared] needs: the
// destinations already resolved so the per-node step only has to test
// whether the node is among them.
type sharedGrantData struct {
	// dstIPs is the union of the resolved non-wildcard destinations.
	dstIPs *netipx.IPSet
	// hasWildcardDst is set when a destination is "*".
	hasWildcardDst    bool
	internetProtocols []ProtocolPort
	app               tailcfg.PeerCapMap
}

// selfGrantData holds data needed for per-node autogroup:self
// compilation. Sources are already resolved.
type selfGrantData struct {
	resolvedSrcs      []ResolvedAddresses
	internetProtocols []ProtocolPort
	app               tailcfg.PeerCapMap
}

// viaGrantData holds data needed for per-node via-grant compilation.
// Sources are already resolved into srcIPStrings; destinations are
// pre-resolved into prefixes plus a flag for autogroup:internet, which
// the consumers handle separately because its gate is per-node
// (IsExitNode()) rather than per-prefix overlap.
type viaGrantData struct {
	viaTags              []Tag
	resolvedDsts         []netip.Prefix
	hasAutoGroupInternet bool
	internetProtocols    []ProtocolPort
	srcIPStrings         []string
}

// resolveViaDestinations splits a via grant's destinations into the
// flat list of IP prefixes they resolve to plus a flag for
// autogroup:internet. Every alias kind goes through [Alias.Resolve] so
// adding a new alias type to the policy parser does not silently
// disappear from the via path. Non-IP alias kinds (tag, user, group,
// wildcard) resolve to /32 host IPs that never overlap with subnet
// route advertisements and therefore contribute nothing here.
func resolveViaDestinations(
	pol *Policy,
	users types.Users,
	nodes views.Slice[types.NodeView],
	dsts Aliases,
) ([]netip.Prefix, bool) {
	var (
		prefixes             []netip.Prefix
		hasAutoGroupInternet bool
	)

	for _, d := range dsts {
		if ag, ok := d.(*AutoGroup); ok && ag.Is(AutoGroupInternet) {
			hasAutoGroupInternet = true

			continue
		}

		ips, err := d.Resolve(pol, users, nodes)
		if err != nil || ips == nil {
			continue
		}

		prefixes = append(prefixes, ips.Prefixes()...)
	}

	return prefixes, hasAutoGroupInternet
}

// userNodeIndex maps user IDs to their untagged nodes. Built once per
// policy or node-set change and read from many goroutines under
// [PolicyManager.mu]; readers must hold the lock (or the snapshot
// returned to them).
type userNodeIndex map[uint][]types.NodeView

func buildUserNodeIndex(
	nodes views.Slice[types.NodeView],
) userNodeIndex {
	idx := make(userNodeIndex)

	for _, n := range nodes.All() {
		if !n.IsTagged() && n.User().Valid() {
			uid := n.User().ID()
			idx[uid] = append(idx[uid], n)
		}
	}

	return idx
}

// compileNodeAttrs returns the per-node CapMap derived from policy
// nodeAttrs, the tailnet-wide [Policy.RandomizeClientPort] flag and the
// global exit nodes, which need no policy at all.
//
// Returns an error when a target alias fails to resolve so the caller
// surfaces a corrupt policy instead of silently granting a partial set
// of attrs.
func (pol *Policy) compileNodeAttrs(
	users types.Users,
	nodes views.Slice[types.NodeView],
) (map[types.NodeID]tailcfg.NodeCapMap, error) {
	result := make(map[types.NodeID]tailcfg.NodeCapMap)
	stamp := func(id types.NodeID, attr nodecap.Cap) {
		capMap, ok := result[id]
		if !ok {
			capMap = tailcfg.NodeCapMap{}
			result[id] = capMap
		}

		// nil [tailcfg.RawMessage] matches the wire format from a
		// Tailscale-hosted control plane: capabilities without companion
		// data marshal as null rather than []. Storing nil keeps the
		// merge stable and lets the compat test diff cleanly against
		// captured netmaps.
		if _, exists := capMap[attr]; !exists {
			capMap[attr] = nil
		}
	}

	stampGlobalExitNodes(nodes, stamp)

	if pol == nil || (len(pol.NodeAttrs) == 0 && !pol.RandomizeClientPort) {
		return result, nil
	}

	return result, pol.compilePolicyNodeAttrs(users, nodes, stamp)
}

// stampGlobalExitNodes gives every global exit node suggest-exit-node,
// which [PeerCapMap] surfaces on its peer view once its exit routes are
// approved, and every node auto-exit-node while at least one exists, so
// clients may pick a suggested exit node automatically.
func stampGlobalExitNodes(nodes views.Slice[types.NodeView], stamp func(types.NodeID, nodecap.Cap)) {
	if !NodesHaveGlobalExitNode(nodes) {
		return
	}

	for _, n := range nodes.All() {
		if n.GlobalExitNode() {
			stamp(n.ID(), nodecap.SuggestExitNode)
		}

		stamp(n.ID(), nodecap.AutoExitNode)
	}
}

// NodesHaveGlobalExitNode reports whether any node is a global exit node.
func NodesHaveGlobalExitNode(nodes views.Slice[types.NodeView]) bool {
	for _, n := range nodes.All() {
		if n.GlobalExitNode() {
			return true
		}
	}

	return false
}

// compilePolicyNodeAttrs stamps the caps the policy's nodeAttrs and
// randomizeClientPort ask for.
func (pol *Policy) compilePolicyNodeAttrs(
	users types.Users,
	nodes views.Slice[types.NodeView],
	stamp func(types.NodeID, nodecap.Cap),
) error {
	// Cache each node's IPs once per call. Without the cache, the
	// node-attr inner loop would call [types.NodeView.IPs] once per attr
	// per node — O(grants × nodes) allocations of a 2-element slice
	// for what is invariant per node within a single policy compile.
	type nodeIPs struct {
		id  types.NodeID
		ips []netip.Addr
	}

	nodeList := make([]nodeIPs, 0, nodes.Len())
	for _, n := range nodes.All() {
		nodeList = append(nodeList, nodeIPs{id: n.ID(), ips: n.IPs()})
	}

	if pol.RandomizeClientPort {
		for _, ni := range nodeList {
			stamp(ni.id, nodecap.RandomizeClientPort)
		}
	}

	for _, na := range pol.NodeAttrs {
		if len(na.Attrs) == 0 {
			continue
		}

		resolved, err := na.Targets.Resolve(pol, users, nodes)
		if err != nil {
			return fmt.Errorf("nodeAttrs target %s: %w", na.Targets, err)
		}

		if resolved == nil {
			continue
		}

		for _, ni := range nodeList {
			if !slices.ContainsFunc(ni.ips, resolved.Contains) {
				continue
			}

			for _, attr := range na.Attrs {
				stamp(ni.id, attr)
			}
		}
	}

	return nil
}

// stampRoleCaps adds to capMaps the capabilities a node inherits from its
// user's role, as the hosted control plane does: is-admin for the owner and
// admins, is-owner for the owner. Tagged nodes carry neither. Clients use
// them for the admin-console affordances in their UI only; access is still
// the filter's job.
func stampRoleCaps(
	users types.Users,
	nodes views.Slice[types.NodeView],
	capMaps map[types.NodeID]tailcfg.NodeCapMap,
) {
	roles := make(map[types.UserID]types.Role, len(users))

	for i := range users {
		if users[i].Role.IsAdmin() {
			roles[types.UserID(users[i].ID)] = users[i].Role
		}
	}

	if len(roles) == 0 {
		return
	}

	for _, node := range nodes.All() {
		if node.IsTagged() || !node.UserID().Valid() {
			continue
		}

		role, ok := roles[types.UserID(node.UserID().Get())]
		if !ok {
			continue
		}

		capMap, ok := capMaps[node.ID()]
		if !ok {
			capMap = tailcfg.NodeCapMap{}
			capMaps[node.ID()] = capMap
		}

		capMap[nodecap.Admin] = nil

		if role == types.RoleOwner {
			capMap[nodecap.Owner] = nil
		}
	}
}

// usersHaveAdmin reports whether any user holds a role that stamps caps.
func usersHaveAdmin(users types.Users) bool {
	for i := range users {
		if users[i].Role.IsAdmin() {
			return true
		}
	}

	return false
}

// compileGrants resolves all policy grants into [compiledGrant] structs.
// Source resolution and non-self destination resolution happens once
// here. This is the single resolution path that replaces the
// duplicated work in [Policy.compileFilterRules] and the autogroup:self
// expansion.
func (pol *Policy) compileGrants(
	users types.Users,
	nodes views.Slice[types.NodeView],
) []compiledGrant {
	if !pol.enforces() {
		return nil
	}

	grants := slices.Clone(pol.Grants)
	for _, acl := range pol.ACLs {
		grants = append(grants, aclToGrants(acl)...)
	}

	grants = append(grants, accessGrants(pol.access)...)

	compiled := make([]compiledGrant, 0, len(grants))

	for _, grant := range grants {
		// autogroup:shared is one source among the grant's sources; it
		// compiles to its own per-node grant while the other sources
		// compile as usual.
		rest, hasShared := withoutSharedSource(grant)
		if hasShared {
			if cg := pol.compileOneSharedGrant(grant, users, nodes); cg != nil {
				compiled = append(compiled, *cg)
			}

			if len(rest.Sources) == 0 {
				continue
			}

			grant = rest
		}

		cg, err := pol.compileOneGrant(grant, users, nodes)
		if err != nil {
			log.Trace().Err(err).Msg("compiling grant")

			continue
		}

		if cg != nil {
			compiled = append(compiled, *cg)
		}
	}

	return compiled
}

// withoutSharedSource returns the grant with autogroup:shared removed
// from its sources and whether it was there.
func withoutSharedSource(grant Grant) (Grant, bool) {
	if !sourcesHaveShared(grant.Sources) {
		return grant, false
	}

	rest := grant
	rest.Sources = make(Aliases, 0, len(grant.Sources)-1)

	for _, src := range grant.Sources {
		if ag, ok := src.(*AutoGroup); ok && ag.Is(AutoGroupShared) {
			continue
		}

		rest.Sources = append(rest.Sources, src)
	}

	return rest, true
}

// compileOneSharedGrant resolves the destinations of a grant whose
// sources include autogroup:shared. The sources are the sharees of
// each destination node, so they are resolved in
// [compileAutogroupShared]. Via grants and autogroup:self destinations
// are rejected at validation and skipped here.
func (pol *Policy) compileOneSharedGrant(
	grant Grant,
	users types.Users,
	nodes views.Slice[types.NodeView],
) *compiledGrant {
	if len(grant.Via) > 0 || len(grant.Destinations) == 0 {
		return nil
	}

	if len(grant.InternetProtocols) == 0 && grant.App == nil {
		return nil
	}

	data := &sharedGrantData{
		internetProtocols: grant.InternetProtocols,
		app:               grant.App,
	}

	var b netipx.IPSetBuilder

	for _, dst := range grant.Destinations {
		if _, isWildcard := dst.(Asterix); isWildcard {
			data.hasWildcardDst = true

			continue
		}

		if ag, ok := dst.(*AutoGroup); ok && ag.Is(AutoGroupSelf) {
			continue
		}

		ips, err := dst.Resolve(pol, users, nodes)
		if err != nil {
			log.Trace().Caller().Err(err).Msg("resolving shared grant destination")
		}

		if ips != nil {
			for _, pref := range ips.Prefixes() {
				b.AddPrefix(pref)
			}
		}
	}

	dstIPs, err := b.IPSet()
	if err != nil {
		return nil
	}

	data.dstIPs = dstIPs

	return &compiledGrant{category: grantCategoryShared, shared: data}
}

// compileOneGrant resolves a single grant into a [compiledGrant].
// All source resolution happens here. Non-self, non-via destination
// resolution also happens here. Per-node data (self dests, via
// matching) is stored for deferred compilation.
func (pol *Policy) compileOneGrant(
	grant Grant,
	users types.Users,
	nodes views.Slice[types.NodeView],
) (*compiledGrant, error) {
	// Via grants: resolve sources, store deferred data.
	if len(grant.Via) > 0 {
		return pol.compileOneViaGrant(grant, users, nodes)
	}

	// Split destinations into self vs other.
	var autogroupSelfDests, otherDests []Alias

	for _, dest := range grant.Destinations {
		if ag, ok := dest.(*AutoGroup); ok && ag.Is(AutoGroupSelf) {
			autogroupSelfDests = append(autogroupSelfDests, dest)
		} else {
			otherDests = append(otherDests, dest)
		}
	}

	// Resolve sources per-alias, tracking non-wildcard sources
	// separately so we can preserve their IPs alongside the
	// wildcard CGNAT ranges (matching Tailscale SaaS behavior).
	resolvedSrcs, nonWildcardSrcs, err := resolveSources(
		pol, grant.Sources, users, nodes,
	)
	if err != nil {
		return nil, err
	}

	// Literally empty src=[] or dst=[] produces no rules.
	if len(grant.Sources) == 0 || len(grant.Destinations) == 0 {
		return nil, nil //nolint:nilnil // intentional: empty sources or destinations produce no grant
	}

	if len(resolvedSrcs) == 0 && grant.App == nil {
		return nil, nil //nolint:nilnil // intentional: empty resolved sources without app produce no grant
	}

	hasWildcard := sourcesHaveWildcard(grant.Sources)
	hasDangerAll := sourcesHaveDangerAll(grant.Sources)
	srcIPStrings := buildSrcIPStrings(
		resolvedSrcs, nonWildcardSrcs,
		hasWildcard, hasDangerAll, nodes,
	)

	cg := &compiledGrant{
		srcIPStrings: srcIPStrings,
		hasWildcard:  hasWildcard,
		hasDangerAll: hasDangerAll,
	}

	// Compile non-self destination rules (done once, shared).
	if len(otherDests) > 0 {
		cg.rules = pol.compileOtherDests(
			users, nodes, grant, otherDests,
			resolvedSrcs, srcIPStrings,
		)
	}

	// Classify and store deferred self data. The struct literal already
	// initializes category to grantCategoryRegular (the zero value).
	if len(autogroupSelfDests) > 0 {
		cg.category = grantCategorySelf
		cg.self = &selfGrantData{
			resolvedSrcs:      resolvedSrcs,
			internetProtocols: grant.InternetProtocols,
			app:               grant.App,
		}
	}

	return cg, nil
}

// mergeResolvedSrcs merges every prefix from the resolved sources into a
// single [resolved] address set.
func mergeResolvedSrcs(resolvedSrcs []ResolvedAddresses) (resolved, error) {
	var b netipx.IPSetBuilder

	for _, ips := range resolvedSrcs {
		for _, pref := range ips.Prefixes() {
			b.AddPrefix(pref)
		}
	}

	return newResolved(&b)
}

// compileOneViaGrant resolves sources for a via grant and stores the
// deferred per-node data. The actual via-node matching and route
// intersection happens in [compileViaForNode].
func (pol *Policy) compileOneViaGrant(
	grant Grant,
	users types.Users,
	nodes views.Slice[types.NodeView],
) (*compiledGrant, error) {
	if len(grant.InternetProtocols) == 0 {
		return nil, nil //nolint:nilnil // intentional: grant without internet protocols produces no via grant
	}

	resolvedSrcs, _, err := resolveSources(
		pol, grant.Sources, users, nodes,
	)
	if err != nil {
		return nil, err
	}

	if len(resolvedSrcs) == 0 {
		return nil, nil //nolint:nilnil // intentional: empty resolved sources produce no via grant
	}

	// Build merged SrcIPs.
	srcResolved, err := mergeResolvedSrcs(resolvedSrcs)
	if err != nil {
		return nil, err
	}

	if srcResolved.Empty() {
		return nil, nil //nolint:nilnil // intentional: empty merged source IP set produces no via grant
	}

	hasWildcard := sourcesHaveWildcard(grant.Sources)
	hasDangerAll := sourcesHaveDangerAll(grant.Sources)

	resolvedDsts, hasAutoGroupInternet := resolveViaDestinations(
		pol, users, nodes, grant.Destinations,
	)

	return &compiledGrant{
		category: grantCategoryVia,
		via: &viaGrantData{
			viaTags:              grant.Via,
			resolvedDsts:         resolvedDsts,
			hasAutoGroupInternet: hasAutoGroupInternet,
			internetProtocols:    grant.InternetProtocols,
			srcIPStrings: srcIPsWithRoutes(
				srcResolved, hasWildcard, hasDangerAll, nodes,
			),
		},
	}, nil
}

// resolveSources resolves grant sources per-alias, returning the
// resolved addresses and a separate slice of non-wildcard sources.
// This is the canonical source-resolution path. Its output lands in
// [compiledGrant.srcIPStrings] (among other places) and callers on the
// hot path should prefer reading that over calling [Alias.Resolve] again.
func resolveSources(
	pol *Policy,
	sources Aliases,
	users types.Users,
	nodes views.Slice[types.NodeView],
) ([]ResolvedAddresses, []ResolvedAddresses, error) {
	var all, nonWild []ResolvedAddresses

	for i, src := range sources {
		if ag, ok := src.(*AutoGroup); ok && ag.Is(AutoGroupSelf) {
			return nil, nil, errSelfInSources
		}

		ips, err := src.Resolve(pol, users, nodes)
		if err != nil {
			log.Trace().Caller().Err(err).
				Msg("resolving source ips")
		}

		if ips != nil {
			all = append(all, ips)

			if _, isWildcard := sources[i].(Asterix); !isWildcard {
				nonWild = append(nonWild, ips)
			}
		}
	}

	return all, nonWild, nil
}

// buildSrcIPStrings builds the final SrcIPs string slice from
// resolved sources, preserving non-wildcard IPs alongside wildcard
// CGNAT ranges to match Tailscale SaaS behavior.
func buildSrcIPStrings(
	resolvedSrcs, nonWildcardSrcs []ResolvedAddresses,
	hasWildcard, hasDangerAll bool,
	nodes views.Slice[types.NodeView],
) []string {
	srcResolved, err := mergeResolvedSrcs(resolvedSrcs)
	if err != nil || srcResolved.Empty() {
		return nil
	}

	srcIPStrs := srcIPsWithRoutes(
		srcResolved, hasWildcard, hasDangerAll, nodes,
	)

	// When sources include a wildcard (*) alongside explicit
	// sources (tags, groups, etc.), Tailscale preserves the
	// individual IPs from non-wildcard sources alongside the
	// merged CGNAT ranges rather than absorbing them.
	if hasWildcard && len(nonWildcardSrcs) > 0 {
		seen := set.SetOf(srcIPStrs)

		for _, ips := range nonWildcardSrcs {
			for _, s := range ips.Strings() {
				if !seen.Contains(s) {
					seen.Add(s)
					srcIPStrs = append(srcIPStrs, s)
				}
			}
		}
	}

	return srcIPStrs
}

// compileOtherDests compiles filter rules for non-self, non-via
// destinations. This produces both [tailcfg.FilterRule.DstPorts] rules
// (from [Grant.InternetProtocols]) and [tailcfg.CapGrant] rules (from
// [Grant.App]).
func (pol *Policy) compileOtherDests(
	users types.Users,
	nodes views.Slice[types.NodeView],
	grant Grant,
	otherDests Aliases,
	resolvedSrcs []ResolvedAddresses,
	srcIPStrings []string,
) []tailcfg.FilterRule {
	var rules []tailcfg.FilterRule

	// DstPorts rules from InternetProtocols.
	for _, ipp := range grant.InternetProtocols {
		destPorts := pol.destinationsToNetPortRange(
			users, nodes, otherDests, ipp.Ports,
		)

		if len(destPorts) > 0 && len(srcIPStrings) > 0 {
			rules = append(rules, tailcfg.FilterRule{
				SrcIPs:   srcIPStrings,
				DstPorts: destPorts,
				IPProto:  ipp.Protocol.toIANAProtocolNumbers(),
			})
		}
	}

	// CapGrant rules from App.
	if grant.App != nil {
		capSrcIPStrs := srcIPStrings

		// When sources resolved to empty but App is set,
		// Tailscale still produces the CapGrant rule with
		// empty SrcIPs.
		if capSrcIPStrs == nil {
			capSrcIPStrs = []string{}
		}

		var (
			capGrants    []tailcfg.CapGrant
			dstIPStrings []string
		)

		for _, dst := range otherDests {
			ips, err := dst.Resolve(pol, users, nodes)
			if err != nil {
				continue
			}

			capGrants = append(capGrants, tailcfg.CapGrant{
				Dsts:   ips.Prefixes(),
				CapMap: grant.App,
			})

			dstIPStrings = append(dstIPStrings, ips.Strings()...)
		}

		if len(capGrants) > 0 {
			srcPrefixes := make([]netip.Prefix, 0, len(resolvedSrcs)*2)
			for _, ips := range resolvedSrcs {
				srcPrefixes = append(
					srcPrefixes, ips.Prefixes()...,
				)
			}

			rules = append(rules, tailcfg.FilterRule{
				SrcIPs:   capSrcIPStrs,
				CapGrant: capGrants,
			})

			dstsHaveWildcard := sourcesHaveWildcard(otherDests)
			if dstsHaveWildcard {
				dstIPStrings = append(
					dstIPStrings,
					approvedSubnetRoutes(nodes)...,
				)
			}

			rules = append(
				rules,
				companionCapGrantRules(
					dstIPStrings, srcPrefixes, grant.App,
				)...,
			)
		}
	}

	return rules
}

// hasPerNodeGrants reports whether any [compiledGrant] requires
// per-node filter compilation (via grants or autogroup:self).
func hasPerNodeGrants(grants []compiledGrant) bool {
	for i := range grants {
		if grants[i].category != grantCategoryRegular {
			return true
		}
	}

	return false
}

// collectRelayTargetIPs returns the set of IPs that are destinations of a
// tailscale.com/cap/relay grant. A node whose IP is in this set is a relay
// target: when it goes offline, peers holding a PeerRelay allocation through
// it must recompute their netmap to drop the now-dead allocation. The relay
// cap is carried on each grant's [tailcfg.CapGrant] with Dsts set to the
// resolved relay destinations (see [Policy.compileOtherDests]); the reversed
// companion rule carries [tailcfg.PeerCapabilityRelayTarget] instead and is
// intentionally skipped.
func collectRelayTargetIPs(grants []compiledGrant) (*netipx.IPSet, error) {
	var b netipx.IPSetBuilder

	for i := range grants {
		for _, rule := range grants[i].rules {
			for _, cg := range rule.CapGrant {
				if _, ok := cg.CapMap[peercap.Relay]; !ok {
					continue
				}

				for _, dst := range cg.Dsts {
					b.AddPrefix(dst)
				}
			}
		}
	}

	ipset, err := b.IPSet()
	if err != nil {
		return nil, fmt.Errorf("building relay target IP set: %w", err)
	}

	return ipset, nil
}

// collectViaTargetTags returns the set of tags used as via targets across all
// grants. A node carrying any of these tags is a via target: peers steering
// traffic through it must recompute when it goes offline. Returns nil when no
// via grants exist.
func collectViaTargetTags(grants []compiledGrant) map[Tag]struct{} {
	tags := make(map[Tag]struct{})

	for i := range grants {
		if grants[i].via == nil {
			continue
		}

		for _, t := range grants[i].via.viaTags {
			tags[t] = struct{}{}
		}
	}

	if len(tags) == 0 {
		return nil
	}

	return tags
}

// globalFilterRules extracts global filter rules from [compiledGrant]s.
// Via grants produce no global rules (they are per-node only); regular
// grants contribute their full pre-compiled ruleset; self grants
// contribute their non-self portion.
func globalFilterRules(grants []compiledGrant) []tailcfg.FilterRule {
	var rules []tailcfg.FilterRule

	for i := range grants {
		if grants[i].category == grantCategoryVia {
			continue
		}

		rules = append(rules, grants[i].rules...)
	}

	return mergeFilterRules(rules)
}

// filterRulesForNode produces unreduced filter rules for a specific
// node by combining pre-compiled global rules with per-node self and
// via rules. Regular grants emit their pre-compiled rules as-is.
// Self grants add autogroup:self expansion. Via grants add
// tag-matched, route-intersected rules.
func filterRulesForNode(
	grants []compiledGrant,
	node types.NodeView,
	userIdx userNodeIndex,
) []tailcfg.FilterRule {
	var rules []tailcfg.FilterRule

	for i := range grants {
		cg := &grants[i]

		// Pre-compiled rules apply to all grant categories
		// (empty for via-only grants).
		rules = append(rules, cg.rules...)

		switch cg.category {
		case grantCategoryRegular:
			// Nothing more to do.

		case grantCategorySelf:
			rules = append(
				rules,
				compileAutogroupSelf(cg, node, userIdx)...,
			)

		case grantCategoryVia:
			rules = append(
				rules,
				compileViaForNode(cg, node)...,
			)

		case grantCategoryShared:
			rules = append(
				rules,
				compileAutogroupShared(cg, node, userIdx)...,
			)
		}
	}

	return mergeFilterRules(rules)
}

// compileAutogroupShared produces the filter rules of an autogroup:shared
// source for one destination node: the personal devices of the users
// the node is shared with may reach the node on the grant's ports. The
// destination is narrowed to the node itself, so a share never opens
// anything but the shared node, and the node gets no rule back to the
// sharees' devices.
func compileAutogroupShared(
	cg *compiledGrant,
	node types.NodeView,
	userIdx userNodeIndex,
) []tailcfg.FilterRule {
	if cg.shared == nil || node.SharedWith().Len() == 0 {
		return nil
	}

	if !cg.shared.hasWildcardDst && !node.InIPSet(cg.shared.dstIPs) {
		return nil
	}

	srcResolved := sharedSources(node, userIdx)
	if srcResolved == nil || srcResolved.Empty() {
		return nil
	}

	var rules []tailcfg.FilterRule

	for _, ipp := range cg.shared.internetProtocols {
		var destPorts []tailcfg.NetPortRange

		for _, port := range ipp.Ports {
			for _, ip := range node.IPs() {
				destPorts = append(destPorts, tailcfg.NetPortRange{IP: ip.String(), Ports: port})
			}
		}

		if len(destPorts) > 0 {
			rules = append(rules, tailcfg.FilterRule{
				SrcIPs:   srcResolved.Strings(),
				DstPorts: destPorts,
				IPProto:  ipp.Protocol.toIANAProtocolNumbers(),
			})
		}
	}

	if cg.shared.app != nil {
		dsts := node.Prefixes()

		dstIPStrings := make([]string, 0, len(dsts))
		for _, ip := range node.IPs() {
			dstIPStrings = append(dstIPStrings, ip.String())
		}

		rules = append(rules, tailcfg.FilterRule{
			SrcIPs:   srcResolved.Strings(),
			CapGrant: []tailcfg.CapGrant{{Dsts: dsts, CapMap: cg.shared.app}},
		})

		rules = append(
			rules,
			companionCapGrantRules(dstIPStrings, srcResolved.Prefixes(), cg.shared.app)...,
		)
	}

	return rules
}

// sharedSources resolves autogroup:shared for one node: the addresses of
// the personal devices of every user the node is shared with.
func sharedSources(node types.NodeView, userIdx userNodeIndex) ResolvedAddresses {
	var b netipx.IPSetBuilder

	for _, uid := range node.SharedWith().All() {
		for _, n := range userIdx[uint(uid)] {
			n.AppendToIPSet(&b)
		}
	}

	srcResolved, err := newResolved(&b)
	if err != nil {
		return nil
	}

	return srcResolved
}

// compileAutogroupSelf produces filter rules for autogroup:self
// destinations for a specific node. Only called for grants with
// self destinations and only produces rules for untagged nodes.
func compileAutogroupSelf(
	cg *compiledGrant,
	node types.NodeView,
	userIdx userNodeIndex,
) []tailcfg.FilterRule {
	if node.IsTagged() || cg.self == nil {
		return nil
	}

	if !node.User().Valid() {
		return nil
	}

	sameUserNodes := userIdx[node.User().ID()]
	if len(sameUserNodes) == 0 {
		return nil
	}

	var rules []tailcfg.FilterRule

	// Filter sources to only same-user untagged devices.
	srcResolved := filterSourcesToSameUser(
		cg.self.resolvedSrcs, sameUserNodes,
	)
	if srcResolved == nil || srcResolved.Empty() {
		return nil
	}

	// DstPorts rules from InternetProtocols.
	for _, ipp := range cg.self.internetProtocols {
		var destPorts []tailcfg.NetPortRange

		for _, n := range sameUserNodes {
			for _, port := range ipp.Ports {
				for _, ip := range n.IPs() {
					destPorts = append(
						destPorts,
						tailcfg.NetPortRange{
							IP:    ip.String(),
							Ports: port,
						},
					)
				}
			}
		}

		if len(destPorts) > 0 {
			rules = append(rules, tailcfg.FilterRule{
				SrcIPs:   srcResolved.Strings(),
				DstPorts: destPorts,
				IPProto:  ipp.Protocol.toIANAProtocolNumbers(),
			})
		}
	}

	// CapGrant rules from App.
	if cg.self.app != nil {
		var (
			capGrants    []tailcfg.CapGrant
			dstIPStrings []string
		)

		for _, n := range sameUserNodes {
			var dsts []netip.Prefix
			for _, ip := range n.IPs() {
				dsts = append(
					dsts,
					netip.PrefixFrom(ip, ip.BitLen()),
				)
				dstIPStrings = append(
					dstIPStrings, ip.String(),
				)
			}

			capGrants = append(capGrants, tailcfg.CapGrant{
				Dsts:   dsts,
				CapMap: cg.self.app,
			})
		}

		if len(capGrants) > 0 {
			rules = append(rules, tailcfg.FilterRule{
				SrcIPs:   srcResolved.Strings(),
				CapGrant: capGrants,
			})

			rules = append(
				rules,
				companionCapGrantRules(
					dstIPStrings,
					srcResolved.Prefixes(),
					cg.self.app,
				)...,
			)
		}
	}

	return rules
}

// filterSourcesToSameUser intersects resolved source addresses with
// same-user untagged device IPs, returning only the addresses that
// belong to those devices.
func filterSourcesToSameUser(
	resolvedSrcs []ResolvedAddresses,
	sameUserNodes []types.NodeView,
) ResolvedAddresses {
	var srcIPs netipx.IPSetBuilder

	for _, ips := range resolvedSrcs {
		for _, n := range sameUserNodes {
			if slices.ContainsFunc(n.IPs(), ips.Contains) {
				n.AppendToIPSet(&srcIPs)
			}
		}
	}

	srcResolved, err := newResolved(&srcIPs)
	if err != nil {
		return nil
	}

	return srcResolved
}

// compileViaForNode produces via-grant filter rules for a specific
// node. Only produces rules when the node matches one of the via
// tags and advertises routes that match the grant destinations.
func compileViaForNode(
	cg *compiledGrant,
	node types.NodeView,
) []tailcfg.FilterRule {
	if cg.via == nil {
		return nil
	}

	// Check if node matches any via tag.
	matchesVia := false

	for _, viaTag := range cg.via.viaTags {
		if node.HasTag(string(viaTag)) {
			matchesVia = true

			break
		}
	}

	if !matchesVia {
		return nil
	}

	// [types.NodeView.SubnetRoutes] excludes exit routes, so the overlap
	// gate below sees only subnet advertisements. autogroup:internet on
	// a via-tagged exit advertiser is handled separately because its
	// eligibility is per-node ([types.NodeView.IsExitNode]) rather than
	// per-prefix overlap.
	nodeSubnetRoutes := node.SubnetRoutes()

	var viaDstPrefixes []netip.Prefix

	for _, dstPrefix := range cg.via.resolvedDsts {
		// Equality would reject any broader or narrower dst relative
		// to the advertised route. Containment in either direction
		// matches the operator's authorisation: a broader dst restricts
		// traffic to the subset the router serves; a narrower dst rides
		// on a router covering more than the operator asked for. The
		// rule emits the literal dst either way because that is what
		// the policy authorised.
		if slices.ContainsFunc(nodeSubnetRoutes, dstPrefix.Overlaps) {
			viaDstPrefixes = append(viaDstPrefixes, dstPrefix)
		}
	}

	// autogroup:internet on a via-tagged exit advertiser becomes a rule
	// whose DstPorts enumerate [util.TheInternet]. The matchers derived
	// from this rule let [types.NodeView.CanAccess] surface the exit node
	// to the grant source via [matcher.Match.DestsIsTheInternet].
	// [policyutil.ReduceFilterRules] strips the rule from the wire format
	// on non-exit advertisers, preserving SaaS PacketFilter encoding.
	if cg.via.hasAutoGroupInternet && node.IsExitNode() {
		viaDstPrefixes = append(
			viaDstPrefixes,
			util.TheInternet().Prefixes()...,
		)
	}

	if len(viaDstPrefixes) == 0 {
		return nil
	}

	// Build rules using pre-resolved srcIPStrings.
	var rules []tailcfg.FilterRule

	for _, ipp := range cg.via.internetProtocols {
		var destPorts []tailcfg.NetPortRange

		for _, prefix := range viaDstPrefixes {
			for _, port := range ipp.Ports {
				destPorts = append(
					destPorts,
					tailcfg.NetPortRange{
						IP:    prefix.String(),
						Ports: port,
					},
				)
			}
		}

		if len(destPorts) > 0 {
			rules = append(rules, tailcfg.FilterRule{
				SrcIPs:   cg.via.srcIPStrings,
				DstPorts: destPorts,
				IPProto:  ipp.Protocol.toIANAProtocolNumbers(),
			})
		}
	}

	return rules
}
