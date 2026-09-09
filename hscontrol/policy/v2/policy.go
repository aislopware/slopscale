package v2

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/policy/matcher"
	"github.com/aislopware/slopscale/hscontrol/policy/policyutil"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog/log"
	"go4.org/netipx"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/types/views"
	"tailscale.com/util/deephash"
	"tailscale.com/util/multierr"
)

// ErrInvalidTagOwner is returned when a tag owner is not an [Alias] type.
var ErrInvalidTagOwner = errors.New("tag owner is not an Alias")

type PolicyManager struct {
	// RWMutex, not Mutex, so concurrent map generation does not serialise on
	// reads. The per-node caches are xsync.Maps so a read can fill them without
	// taking the write lock.
	mu    sync.RWMutex
	pol   *Policy
	users []types.User
	nodes views.Slice[types.NodeView]

	// access is the database's groups and rules; see [Policy.access].
	access types.AccessModel

	// country is the GeoIP lookup handed to every compile; see
	// [PolicyManager.SetCountryLookup].
	country func(netip.Addr) string

	// usesSourceAddress is set by the last compile when a posture in use
	// reads an ip: attribute, so a node's source address change is only a
	// policy change then.
	usesSourceAddress bool

	filterHash deephash.Sum
	filter     []tailcfg.FilterRule
	matchers   []matcher.Match

	tagOwnerMapHash deephash.Sum
	tagOwnerMap     map[Tag]*netipx.IPSet

	exitSetHash        deephash.Sum
	exitSet            *netipx.IPSet
	autoApproveMapHash deephash.Sum
	autoApproveMap     map[netip.Prefix]*netipx.IPSet

	// vipServices are the tailnet's services; see
	// [PolicyManager.SetVIPServices]. autoApproveServices is
	// autoApprovers.services resolved to the addresses of the nodes each
	// entry lets host the service.
	vipServices         []types.VIPService
	autoApproveServices map[tailcfg.ServiceName]*netipx.IPSet

	// relayTargetIPs holds the IPs of nodes that are destinations of a
	// tailscale.com/cap/relay grant; viaTargetTags holds the tags used as
	// via targets. A node matching either, or that is a subnet router,
	// forces peers to recompute their netmap when its online state changes
	// (see [PolicyManager.NodeNeedsPeerRecompute]). Recomputed from the
	// compiled grants on every policy/user/node change.
	relayTargetIPs *netipx.IPSet
	viaTargetTags  map[Tag]struct{}

	// Lazy map of SSH policies
	sshPolicyMap *xsync.Map[types.NodeID, *tailcfg.SSHPolicy]

	// sshRecording is the tailnet's default session recording; see
	// [PolicyManager.SetSSHRecording].
	sshRecording SSHRecording

	// compiledGrants are the grants with sources pre-resolved.
	// The single source of truth for filter compilation. Both
	// global and per-node filter rules are derived from these.
	compiledGrants []compiledGrant
	userNodeIdx    userNodeIndex

	// viaGrants is every grant with its addresses resolved for
	// [PolicyManager.ViaRoutesForPeer]; hasViaGrants is false when none
	// of them steers through a via tag, which lets that call return
	// before touching the viewer.
	viaGrants    []resolvedViaGrant
	hasViaGrants bool

	// Lazy map of per-node filter rules (reduced, for packet filters)
	filterRulesMap *xsync.Map[types.NodeID, []tailcfg.FilterRule]

	// Lazy map of per-node matchers derived from UNREDUCED filter
	// rules. Only populated on the slow path when needsPerNodeFilter
	// is true; the fast path returns pm.matchers directly.
	matchersForNodeMap *xsync.Map[types.NodeID, []matcher.Match]

	// needsPerNodeFilter is true when any compiled grant requires
	// per-node work (autogroup:self or via grants).
	needsPerNodeFilter bool

	// nodeAttrsMap is the per-node CapMap compiled from policy.NodeAttrs.
	// nodeAttrsHashes shadow it for change detection between updateLocked
	// runs. nodeAttrsChanged accumulates the union of all per-call diffs
	// since the last drain — refresh APPENDS, never overwrites, so a
	// concurrent SetUsers/SetNodes between SetPolicy and the drain
	// cannot silently lose the policy-reload diff.
	nodeAttrsMap     map[types.NodeID]tailcfg.NodeCapMap
	nodeAttrsHashes  map[types.NodeID]deephash.Sum
	nodeAttrsChanged []types.NodeID
}

// filterAndPolicy combines the compiled filter rules with policy content for hashing.
// This ensures filterHash changes when policy changes, even for autogroup:self where
// the compiled filter is always empty.
type filterAndPolicy struct {
	Filter   []tailcfg.FilterRule
	Policy   *Policy
	Access   types.AccessModel
	Services []types.VIPService
}

// checkUsernameRef resolves a single user@ token and records an error in
// *errs when it is ambiguous. Missing-user tokens stay tolerant.
func checkUsernameRef(u *Username, users types.Users, errs *[]error) {
	if u == nil {
		return
	}

	_, err := u.resolveUser(users)
	if err != nil && errors.Is(err, ErrMultipleUsersFound) {
		*errs = append(*errs, err)
	}
}

// checkAliasUsernameRef checks a as a user@ token when it is one.
func checkAliasUsernameRef(a Alias, users types.Users, errs *[]error) {
	if u, ok := a.(*Username); ok {
		checkUsernameRef(u, users, errs)
	}
}

// validateGroupUserReferences checks user@ tokens in policy groups.
func validateGroupUserReferences(pol *Policy, users types.Users, errs *[]error) {
	for _, usernames := range pol.Groups {
		for i := range usernames {
			checkUsernameRef(&usernames[i], users, errs)
		}
	}
}

// validateTagOwnerUserReferences checks user@ tokens in tag owners.
func validateTagOwnerUserReferences(pol *Policy, users types.Users, errs *[]error) {
	for _, owners := range pol.TagOwners {
		for _, o := range owners {
			if u, ok := o.(*Username); ok {
				checkUsernameRef(u, users, errs)
			}
		}
	}
}

// validateAutoApproverUserReferences checks user@ tokens in route and
// exit-node auto-approvers.
func validateAutoApproverUserReferences(pol *Policy, users types.Users, errs *[]error) {
	for _, approvers := range pol.AutoApprovers.Routes {
		for _, aa := range approvers {
			if u, ok := aa.(*Username); ok {
				checkUsernameRef(u, users, errs)
			}
		}
	}

	for _, aa := range pol.AutoApprovers.ExitNode {
		if u, ok := aa.(*Username); ok {
			checkUsernameRef(u, users, errs)
		}
	}

	for _, approvers := range pol.AutoApprovers.Services {
		for _, aa := range approvers {
			if u, ok := aa.(*Username); ok {
				checkUsernameRef(u, users, errs)
			}
		}
	}
}

// validateACLUserReferences checks user@ tokens in ACL sources and
// destinations.
func validateACLUserReferences(pol *Policy, users types.Users, errs *[]error) {
	for _, acl := range pol.ACLs {
		for _, src := range acl.Sources {
			checkAliasUsernameRef(src, users, errs)
		}

		for _, dst := range acl.Destinations {
			checkAliasUsernameRef(dst.Alias, users, errs)
		}
	}
}

// validateSSHUserReferences checks user@ tokens in SSH rule sources and
// destinations.
func validateSSHUserReferences(pol *Policy, users types.Users, errs *[]error) {
	for _, ssh := range pol.SSHs {
		for _, src := range ssh.Sources {
			checkAliasUsernameRef(src, users, errs)
		}

		for _, dst := range ssh.Destinations {
			checkAliasUsernameRef(dst, users, errs)
		}
	}
}

// validateUserReferences surfaces ambiguous user@ tokens at policy load so
// duplicate DB rows fail loudly instead of silently dropping rules.
// Missing-user tokens stay tolerant. Empty users → no-op for
// syntax-only checks.
func validateUserReferences(pol *Policy, users types.Users) error {
	if pol == nil || len(users) == 0 {
		return nil
	}

	var errs []error

	validateGroupUserReferences(pol, users, &errs)
	validateTagOwnerUserReferences(pol, users, &errs)
	validateAutoApproverUserReferences(pol, users, &errs)
	validateACLUserReferences(pol, users, &errs)
	validateSSHUserReferences(pol, users, &errs)

	err := multierr.New(errs...)
	if err != nil {
		return fmt.Errorf("validating user references: %w", err)
	}

	return nil
}

// NewPolicyManager creates a new [PolicyManager] from a policy file and a list of users and nodes.
// It returns an error if the policy file is invalid.
// The policy manager will update the filter rules based on the users and nodes.
func NewPolicyManager(b []byte, users []types.User, nodes views.Slice[types.NodeView]) (*PolicyManager, error) {
	policy, err := unmarshalPolicy(b)
	if err != nil {
		return nil, fmt.Errorf("parsing policy: %w", err)
	}

	err = validateUserReferences(policy, users)
	if err != nil {
		return nil, fmt.Errorf("validating policy user references: %w", err)
	}

	pm := PolicyManager{
		pol:                policy,
		users:              users,
		nodes:              nodes,
		sshPolicyMap:       xsync.NewMap[types.NodeID, *tailcfg.SSHPolicy](),
		filterRulesMap:     xsync.NewMap[types.NodeID, []tailcfg.FilterRule](),
		matchersForNodeMap: xsync.NewMap[types.NodeID, []matcher.Match](),
	}

	_, err = pm.updateLocked()
	if err != nil {
		return nil, err
	}

	// Boot path: log a warning if the stored policy's tests would
	// fail against the current users and nodes, but keep the server
	// running. A stale stored policy (e.g. referencing a user that
	// was deleted while the server was offline) should not block
	// boot; the operator finds out via logs and re-runs the write
	// boundary when they are ready.
	testErr := pm.RunTests()
	if testErr != nil {
		log.Warn().Err(testErr).Msg("policy tests failed at boot; server starting anyway, fix the policy and reload")
	}

	sshTestErr := pm.RunSSHTests()
	if sshTestErr != nil {
		log.Warn().
			Err(sshTestErr).
			Msg("policy sshTests failed at boot; server starting anyway, fix the policy and reload")
	}

	return &pm, nil
}

// NodeNeedsPeerRecompute reports whether peers must recompute their netmap
// when node's online state changes. A plain node only needs the lightweight
// online/offline peer patch; these roles change what peers compute when the
// node goes up or down, so they require a full recompute:
//   - subnet router: primary-route failover changes peers' AllowedIPs
//   - relay target (tailscale.com/cap/relay): peers must drop a stale
//     PeerRelay allocation
//   - via target: peers steer traffic through this node
//
// The check is keyed on the node itself, so an ordinary node in a tailnet
// that uses relay or via for other nodes is correctly classified as not
// needing a recompute.
func (pm *PolicyManager) NodeNeedsPeerRecompute(node types.NodeView) bool {
	if !node.Valid() {
		return false
	}

	// Subnet-router status and hosting a service are intrinsic to the
	// node, so they need no policy state and are checked without the lock.
	if node.IsSubnetRouter() || len(node.HostedServices()) > 0 {
		return true
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.relayTargetIPs != nil && node.InIPSet(pm.relayTargetIPs) {
		return true
	}

	for tag := range pm.viaTargetTags {
		if node.HasTag(string(tag)) {
			return true
		}
	}

	return false
}

// SSHPolicy returns the [tailcfg.SSHPolicy] for node, compiling and
// caching on first access. Rules use SessionDuration = 0 (no
// auto-approval) and emit check URLs of the form
// /machine/ssh/action/{src}/to/{dst}?local_user={local_user} per the
// SaaS wire format. Cache is invalidated on policy reload.
func (pm *PolicyManager) SSHPolicy(baseURL string, node types.NodeView) (*tailcfg.SSHPolicy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if sshPol, ok := pm.sshPolicyMap.Load(node.ID()); ok {
		return sshPol, nil
	}

	sshPol, err := pm.pol.compileSSHPolicy(baseURL, pm.users, node, pm.nodes, pm.sshRecording)
	if err != nil {
		return nil, fmt.Errorf("compiling SSH policy: %w", err)
	}

	pm.sshPolicyMap.Store(node.ID(), sshPol)

	return sshPol, nil
}

// SSHCheckParams resolves the SSH check period for a source-destination
// node pair by looking up the current policy. This avoids trusting URL
// parameters that a client could tamper with. First-match wins across
// the policy's SSH rules.
//
// Returns (duration, true) when a matching rule is found and
// (0, false) when none is. A (0, true) return means the matched rule
// uses a zero check period (re-check every session).
func (pm *PolicyManager) SSHCheckParams(
	srcNodeID, dstNodeID types.NodeID,
) (time.Duration, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.pol == nil || len(pm.pol.SSHs) == 0 {
		return 0, false
	}

	srcNode, dstNode := pm.findNodePairLocked(srcNodeID, dstNodeID)
	if !srcNode.Valid() || !dstNode.Valid() {
		return 0, false
	}

	// Iterate SSH rules to find the first matching check rule.
	for _, rule := range pm.pol.SSHs {
		if d, ok := pm.sshCheckPeriodForRule(rule, srcNode, dstNode); ok {
			return d, true
		}
	}

	return 0, false
}

// SetSSHRecording replaces the tailnet's default session recording and
// drops the cached SSH policies so the next map carries it.
func (pm *PolicyManager) SetSSHRecording(recording SSHRecording) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.sshRecording = recording
	pm.sshPolicyMap.Clear()

	// The filter changes too: every node may reach a recorder on its
	// port, so the recorders' grant is part of the compile.
	return pm.updateLocked()
}

// SSHRecordingFor returns the recorders and failure action for the
// session a check-mode rule admits between src and dst, so the final
// action control returns carries them like the accept action would. Nil
// when no rule matches or the rule records nothing.
func (pm *PolicyManager) SSHRecordingFor(
	baseURL string, srcNodeID, dstNodeID types.NodeID,
) ([]netip.AddrPort, *tailcfg.SSHRecorderFailureAction) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.pol == nil || len(pm.pol.SSHs) == 0 {
		return nil, nil
	}

	srcNode, dstNode := pm.findNodePairLocked(srcNodeID, dstNodeID)
	if !srcNode.Valid() || !dstNode.Valid() {
		return nil, nil
	}

	for _, rule := range pm.pol.SSHs {
		if _, ok := pm.sshCheckPeriodForRule(rule, srcNode, dstNode); !ok {
			continue
		}

		recorders, enforce := pm.pol.recordersFor(rule, pm.sshRecording, pm.users, pm.nodes)
		if len(recorders) == 0 {
			return nil, nil
		}

		return recorders, recordingFailure(baseURL, enforce)
	}

	return nil, nil
}

func (pm *PolicyManager) SetPolicy(polB []byte) (bool, error) {
	if len(polB) == 0 {
		return false, nil
	}

	pol, err := unmarshalPolicy(polB)
	if err != nil {
		return false, fmt.Errorf("parsing policy: %w", err)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	err = validateUserReferences(pol, pm.users)
	if err != nil {
		return false, fmt.Errorf("validating policy user references: %w", err)
	}

	pol.access = pm.access

	// SetPolicy is the user-write boundary. Tests evaluate against a
	// sandbox compiled from the new policy + current users/nodes; if
	// they fail, return without mutating the live PolicyManager so the
	// failed write does not knock the running config offline.
	//
	// Aggregate ACL and SSH test failures via multierr so operators
	// see both classes in a single response instead of having to
	// fix-and-retry to discover the second one.
	testErr := multierr.New(
		evaluateTests(pol, pm.users, pm.nodes),
		evaluateSSHTests(pol, pm.users, pm.nodes),
	)
	if testErr != nil {
		return false, fmt.Errorf("evaluating policy tests: %w", testErr)
	}

	// Log policy metadata for debugging
	log.Debug().
		Int("policy.bytes", len(polB)).
		Int("acls.count", len(pol.ACLs)).
		Int("groups.count", len(pol.Groups)).
		Int("hosts.count", len(pol.Hosts)).
		Int("tagOwners.count", len(pol.TagOwners)).
		Int("nodeAttrs.count", len(pol.NodeAttrs)).
		Int("autoApprovers.routes.count", len(pol.AutoApprovers.Routes)).
		Int("tests.count", len(pol.Tests)).
		Msg("Policy parsed successfully")

	pm.pol = pol

	return pm.updateLocked()
}

// SetAccessModel replaces the database's groups and rules and recompiles.
// It reports whether the compiled output changed.
func (pm *PolicyManager) SetAccessModel(model types.AccessModel) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.access = model

	return pm.updateLocked()
}

// SetCountryLookup installs the GeoIP lookup postures read ip:country
// from and recompiles, since the attribute may now resolve.
func (pm *PolicyManager) SetCountryLookup(lookup func(netip.Addr) string) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.country = lookup

	return pm.updateLocked()
}

// UsesSourceAddress reports whether a posture in use reads where a node
// connects from, so the caller knows a source address change matters.
func (pm *PolicyManager) UsesSourceAddress() bool {
	if pm == nil {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.usesSourceAddress
}

// NextScheduleBoundary returns when a scheduled posture in use next
// opens or closes, or the zero time when none is scheduled.
func (pm *PolicyManager) NextScheduleBoundary(now time.Time) time.Time {
	if pm == nil {
		return time.Time{}
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.pol.nextScheduleBoundary(now)
}

// Recompile rebuilds the filter from the same inputs, for the moments a
// posture's outcome changes without any input the manager sees changing:
// a schedule boundary, an attribute expiry.
func (pm *PolicyManager) Recompile() (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	changed, err := pm.updateLocked()
	if err != nil {
		return false, err
	}

	if changed {
		pm.sshPolicyMap.Clear()
		pm.filterRulesMap.Clear()
		pm.matchersForNodeMap.Clear()
	}

	return changed, nil
}

// MatchingPostures lists the database postures the node satisfies now.
func (pm *PolicyManager) MatchingPostures(node types.NodeView) []types.Posture {
	if pm == nil {
		return nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pol := pm.pol
	if pol == nil {
		pol = &Policy{access: pm.access, country: pm.country}
	}

	return pol.matchingPostures(node, pol.postureContext())
}

// FileEnforces reports whether the policy file has acls or grants of its
// own; without them the access rules are the only thing keeping the
// tailnet from allow-all.
func (pm *PolicyManager) FileEnforces() bool {
	if pm == nil {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.pol.fileEnforces()
}

// Enforces reports whether the tailnet runs with a packet filter at all:
// the file restricts, or an enabled access rule does. Without one the
// narrowing on networks (protocol, ports) has nothing to apply to.
func (pm *PolicyManager) Enforces() bool {
	if pm == nil {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.pol.enforces()
}

// Filter returns the current filter rules for the entire tailnet and the associated matchers.
func (pm *PolicyManager) Filter() ([]tailcfg.FilterRule, []matcher.Match) {
	if pm == nil {
		return nil, nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.filter, pm.matchers
}

// BuildPeerMap constructs peer relationship maps for the given nodes.
// For global filters, it uses the global filter matchers for all nodes.
// For autogroup:self policies (empty global filter), it builds per-node
// peer maps using each node's specific filter rules.
//
// Compared to [policy.ReduceNodes], which builds the list per node, we end
// up with doing the full work for every node O(n^2), while this will reduce
// the list as we see relationships while building the map, making it
// O(n^2/2) in the end, but with less work per node.
func (pm *PolicyManager) BuildPeerMap(nodes views.Slice[types.NodeView]) map[types.NodeID][]types.NodeView {
	if pm == nil {
		return nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Precompute each node's subnet routes and exit-node status once; the
	// O(n^2) pair scans below would otherwise recompute them for every pair.
	// Both scans index by loop position rather than node ID, so a pair costs
	// two slice loads instead of four map lookups, and the result map is
	// built once at the end instead of on every match.
	routes := make([]nodeRoutes, nodes.Len())
	for i := range nodes.Len() {
		routes[i] = routesOf(nodes.At(i))
	}

	// If we have a global filter, use it for all nodes (normal case).
	// Via grants require the per-node path because the global filter
	// skips via grants (compileFilterRules: if len(grant.Via) > 0 { continue }).
	return peerMapOf(nodes, pm.peerPositionsLocked(nodes, routes))
}

// BuildPeerPositions is [PolicyManager.BuildPeerMap] with the result as
// positions into nodes: out[i] lists the positions of node i's peers, nil
// for a node without any. The node store keeps this form so it can carry
// the relationship across batches that change none of its inputs.
func (pm *PolicyManager) BuildPeerPositions(nodes views.Slice[types.NodeView]) [][]int32 {
	if pm == nil {
		return nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	routes := make([]nodeRoutes, nodes.Len())
	for i := range nodes.Len() {
		routes[i] = routesOf(nodes.At(i))
	}

	return pm.peerPositionsLocked(nodes, routes)
}

// peerMapOf keys the position-indexed peer lists by node ID, dropping the
// nodes that ended up with no peers, as the pair scans never gave them a
// map entry.
func peerMapOf(nodes views.Slice[types.NodeView], peers [][]int32) map[types.NodeID][]types.NodeView {
	ret := make(map[types.NodeID][]types.NodeView, len(peers))

	for i, p := range peers {
		if len(p) == 0 {
			continue
		}

		list := make([]types.NodeView, 0, len(p))
		for _, pos := range p {
			list = append(list, nodes.At(int(pos)))
		}

		// Two entries can carry the same ID; the pair scans skip such a
		// pair but still fill both positions, so merge them here.
		id := nodes.At(i).ID()
		ret[id] = append(ret[id], list...)
	}

	return ret
}

// nodeRoutes is a node's subnet routes and exit-node status, computed
// once per peer scan.
type nodeRoutes struct {
	subnet []netip.Prefix
	isExit bool
}

func routesOf(n types.NodeView) nodeRoutes {
	return nodeRoutes{subnet: n.SubnetRoutes(), isExit: n.IsExitNode()}
}

// perNodePeers reports whether a and b see each other under per-node
// filters. Visibility is symmetric: if EITHER node can access the other,
// BOTH see each other, so one-way rules (admin -> tagged server) still
// connect. Each node's own matchers are checked in both directions: for
// via grants the rules live on the via node with the client as source,
// and for autogroup:shared they live on the shared node with the sharee
// as source, so the other node's matchers are the ones that admit it.
func perNodePeers(a, b types.NodeView, ma, mb []matcher.Match, ra, rb nodeRoutes) bool {
	hasA, hasB := len(ma) > 0, len(mb) > 0

	return (hasA && a.CanAccessWithRoutes(ma, b, ra.subnet, rb.subnet, rb.isExit)) ||
		(hasB && b.CanAccessWithRoutes(mb, a, rb.subnet, ra.subnet, ra.isExit)) ||
		(hasA && b.CanAccessWithRoutes(ma, a, rb.subnet, ra.subnet, ra.isExit)) ||
		(hasB && a.CanAccessWithRoutes(mb, b, ra.subnet, rb.subnet, rb.isExit))
}

// VisiblePeers narrows candidates to the ones node may see: the decision
// [PolicyManager.BuildPeerMap] makes for every pair of the tailnet, made
// for one node against a candidate list. The incremental map paths use
// it so a node added or changed reaches only the netmaps the full map
// would show it in. Without a policy every candidate is visible; with a
// policy that leaves node's own filter empty (autogroup:shared before
// anything is shared) the candidates' filters still decide, and a
// candidate nobody's filter admits stays hidden.
func (pm *PolicyManager) VisiblePeers(
	node types.NodeView,
	candidates views.Slice[types.NodeView],
) views.Slice[types.NodeView] {
	if pm == nil {
		return candidates
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	rn := routesOf(node)
	out := make([]types.NodeView, 0, candidates.Len())

	var nodeMatchers []matcher.Match
	if pm.needsPerNodeFilter {
		nodeMatchers = pm.matchersForNodeLocked(node)
	}

	for _, peer := range candidates.All() {
		if peer.ID() == node.ID() {
			continue
		}

		rp := routesOf(peer)

		var visible bool
		if pm.needsPerNodeFilter {
			visible = perNodePeers(node, peer, nodeMatchers, pm.matchersForNodeLocked(peer), rn, rp)
		} else {
			visible = pm.globalPeersLocked(node, peer, rn, rp)
		}

		if visible {
			out = append(out, peer)
		}
	}

	return views.SliceOf(out)
}

// FilterForNode returns the filter rules for a specific node, already reduced
// to only include rules relevant to that node.
// If the policy uses autogroup:self, this returns node-specific compiled rules.
// Otherwise, it returns the global filter reduced for this node.
//
// Cache is invalidated by [PolicyManager.updateLocked] on policy reload,
// node-set change, or tag-state change.
func (pm *PolicyManager) FilterForNode(node types.NodeView) ([]tailcfg.FilterRule, error) {
	if pm == nil {
		return nil, nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.filterForNodeLocked(node), nil
}

// MatchersForNode returns the matchers for peer relationship determination for a specific node.
// These are UNREDUCED matchers - they include all rules where the node could be either source or destination.
// This is different from [PolicyManager.FilterForNode] which returns REDUCED rules for packet filtering.
//
// For global policies: returns the global matchers (same for all nodes)
// For autogroup:self: returns node-specific matchers from unreduced compiled rules.
//
// Per-node results are cached and invalidated on policy/node updates
// so [PolicyManager.BuildPeerMap]'s O(N²) slow path avoids recomputing
// matchers for every pair.
func (pm *PolicyManager) MatchersForNode(node types.NodeView) ([]matcher.Match, error) {
	if pm == nil {
		return nil, nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// For global policies, return the shared global matchers.
	// Via grants require per-node matchers because the global matchers
	// are empty for via-grant-only policies.
	if !pm.needsPerNodeFilter {
		return pm.matchers, nil
	}

	return pm.matchersForNodeLocked(node), nil
}

// SetUsers updates the users in the policy manager and updates the filter rules.
func (pm *PolicyManager) SetUsers(users []types.User) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.users = users

	// Clear SSH policy map when users change to force SSH policy recomputation
	// This ensures that if SSH policy compilation previously failed due to missing users,
	// it will be retried with the new user list
	pm.sshPolicyMap.Clear()

	changed, err := pm.updateLocked()
	if err != nil {
		return false, err
	}

	// If SSH policies exist, force a policy change when users are updated
	// This ensures nodes get updated SSH policies even if other policy hashes didn't change
	if pm.pol != nil && len(pm.pol.SSHs) > 0 {
		return true, nil
	}

	return changed, nil
}

// SetNodes updates the nodes in the policy manager and updates the filter rules.
func (pm *PolicyManager) SetNodes(nodes views.Slice[types.NodeView]) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	policyChanged := pm.nodesHavePolicyAffectingChanges(nodes)

	// Invalidate cache entries for nodes that changed.
	// For autogroup:self: invalidate all nodes belonging to affected users (peer changes).
	// For global policies: invalidate only nodes whose properties changed (IPs, routes).
	pm.invalidateNodeCache(nodes)

	pm.nodes = nodes

	// When policy-affecting node properties change, we must recompile filters because:
	// 1. User/group aliases (like "user1@") resolve to node IPs
	// 2. Tag aliases (like "tag:server") match nodes based on their tags
	// 3. Filter compilation needs nodes to generate rules
	//
	// For autogroup:self: return true when nodes change even if the global filter
	// hash didn't change. The global filter is empty for autogroup:self (each node
	// has its own filter), so the hash never changes. But peer relationships DO
	// change when nodes are added/removed, so we must signal this to trigger updates.
	// For global policies: the filter must be recompiled to include the new nodes.
	if policyChanged {
		// Recompile filter with the new node list
		needsUpdate, err := pm.updateLocked()
		if err != nil {
			return false, err
		}

		if !needsUpdate {
			// This ensures fresh filter rules are generated for all nodes
			pm.sshPolicyMap.Clear()
			pm.filterRulesMap.Clear()
			pm.matchersForNodeMap.Clear()
		}
		// Always return true when nodes changed, even if filter hash didn't change
		// (can happen with autogroup:self or when nodes are added but don't affect rules)
		return true, nil
	}

	return false, nil
}

// nodeIDViewMap indexes a slice of node views by node ID. On duplicate IDs the
// last view wins, matching the open-coded loops it replaces.
func nodeIDViewMap(s views.Slice[types.NodeView]) map[types.NodeID]types.NodeView {
	m := make(map[types.NodeID]types.NodeView, s.Len())
	for _, n := range s.All() {
		m[n.ID()] = n
	}

	return m
}

// NodeCanHaveTag checks if a node can have the specified tag during client-initiated
// registration or reauth flows (e.g., tailscale up --advertise-tags).
//
// This function is NOT used by the admin API's [state.State.SetNodeTags] - admins can
// set any existing tag on any node by calling [state.State.SetNodeTags] directly,
// which bypasses this authorization check.
func (pm *PolicyManager) NodeCanHaveTag(node types.NodeView, tag string) bool {
	if pm == nil {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// pm.pol is written by SetPolicy under pm.mu; reading it before the
	// lock races with concurrent policy reloads.
	if pm.pol == nil {
		return false
	}

	// Check if tag exists in policy
	owners, exists := pm.pol.TagOwners[Tag(tag)]
	if !exists {
		return false
	}

	// Check if node's owner can assign this tag via the pre-resolved tagOwnerMap.
	// The tagOwnerMap contains IP sets built from resolving TagOwners entries
	// (usernames/groups) to their nodes' IPs, so checking if the node's IP
	// is in the set answers "does this node's owner own this tag?"
	if ips, ok := pm.tagOwnerMap[Tag(tag)]; ok {
		if slices.ContainsFunc(node.IPs(), ips.Contains) {
			return true
		}
	}

	// For new nodes being registered, their IP may not yet be in the tagOwnerMap.
	// Fall back to checking the node's user directly against the TagOwners.
	// This handles the case where a user registers a new node with --advertise-tags.
	if node.User().Valid() {
		for _, owner := range owners {
			if pm.userMatchesOwner(node.User(), owner) {
				return true
			}
		}
	}

	return false
}

// UserCanHaveTag reports whether the given user is one of the tag's owners
// (directly or via a group). It is the user half of [PolicyManager.NodeCanHaveTag]:
// re-authentication authorises requested tags against the authenticating user,
// because a tag-owned node carries no user and its IP is not in any owner set,
// so only the user presenting the credential can prove ownership.
func (pm *PolicyManager) UserCanHaveTag(user types.UserView, tag string) bool {
	if pm == nil || !user.Valid() {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.pol == nil {
		return false
	}

	owners, exists := pm.pol.TagOwners[Tag(tag)]
	if !exists {
		return false
	}

	for _, owner := range owners {
		if pm.userMatchesOwner(user, owner) {
			return true
		}
	}

	return false
}

// TagOwnedByTags reports whether a credential holding ownerTags is authorised to
// apply tag. It is true when tag is one of ownerTags, or when tag's tagOwners
// chain (tag-to-tag ownership) transitively includes one of ownerTags. This is
// the tag-level check used when an OAuth access token mints an auth key: the
// requested tags must each be owned by the token's tags, so an operator token
// tagged tag:k8s-operator may mint tag:k8s keys when the policy declares
// "tag:k8s": ["tag:k8s-operator"]. It is purely tag-relational and does not
// consult node IPs.
func (pm *PolicyManager) TagOwnedByTags(tag string, ownerTags []string) bool {
	if pm == nil {
		return false
	}

	owns := make(map[string]bool, len(ownerTags))
	for _, t := range ownerTags {
		owns[t] = true
	}

	// A credential may always apply a tag it directly holds; this needs no policy.
	if owns[tag] {
		return true
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Owned-by delegation requires the policy's tagOwners.
	if pm.pol == nil {
		return false
	}

	// Walk tag-to-tag ownership transitively, guarding against cycles.
	visited := make(map[Tag]bool)

	var walk func(t Tag) bool

	walk = func(t Tag) bool {
		if visited[t] {
			return false
		}

		visited[t] = true

		for _, owner := range pm.pol.TagOwners[t] {
			ot, ok := owner.(*Tag)
			if !ok {
				continue
			}

			if owns[string(*ot)] || walk(*ot) {
				return true
			}
		}

		return false
	}

	return walk(Tag(tag))
}

// HasTagOwners reports whether the policy defines any tag at all.
// Without one no tag can be validated, so callers keep slopscale's
// historical behaviour of taking any well-formed tag.
func (pm *PolicyManager) HasTagOwners() bool {
	if pm == nil {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.pol == nil {
		return false
	}

	return len(pm.pol.TagOwners) > 0
}

// TagExists reports whether the given tag is defined in the policy.
func (pm *PolicyManager) TagExists(tag string) bool {
	if pm == nil {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// pm.pol is written by SetPolicy under pm.mu; reading it before the
	// lock races with concurrent policy reloads.
	if pm.pol == nil {
		return false
	}

	_, exists := pm.pol.TagOwners[Tag(tag)]

	return exists
}

func (pm *PolicyManager) NodeCanApproveRoute(node types.NodeView, route netip.Prefix) bool {
	if pm == nil {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// If the route to-be-approved is an exit route, then we need to check
	// if the node is in allowed to approve it. This is treated differently
	// than the auto-approvers, as the auto-approvers are not allowed to
	// approve the whole /0 range.
	// However, an auto approver might be /0, meaning that they can approve
	// all routes available, just not exit nodes.
	if tsaddr.IsExitRoute(route) {
		if pm.exitSet == nil {
			return false
		}

		return slices.ContainsFunc(node.IPs(), pm.exitSet.Contains)
	}

	// The fast path is that a node requests to approve a prefix
	// where there is an exact entry, e.g. 10.0.0.0/8, then
	// check and return quickly
	if approvers, ok := pm.autoApproveMap[route]; ok {
		canApprove := slices.ContainsFunc(node.IPs(), approvers.Contains)
		if canApprove {
			return true
		}
	}

	// The slow path is that the node tries to approve
	// 10.0.10.0/24, which is a part of 10.0.0.0/8, then we
	// cannot just lookup in the prefix map and have to check
	// if there is a "parent" prefix available.
	for prefix, approveAddrs := range pm.autoApproveMap {
		// Check if prefix is larger (so containing) and then overlaps
		// the route to see if the node can approve a subset of an autoapprover
		if prefix.Bits() <= route.Bits() && prefix.Overlaps(route) {
			canApprove := slices.ContainsFunc(node.IPs(), approveAddrs.Contains)
			if canApprove {
				return true
			}
		}
	}

	return false
}

// ViaRoutesForPeer computes via grant effects for a viewer-peer pair.
// For each via grant where the viewer matches the source, it checks whether the
// peer advertises any of the grant's destination prefixes. If the peer has the
// via tag, those prefixes go into [types.ViaRouteResult.Include]; otherwise
// into [types.ViaRouteResult.Exclude].
//
// Performance note: this holds [PolicyManager.mu] for its full duration. Hot
// callers should memoise by (policy-hash, viewer-id) rather than invoking
// this per-pair.
//
// legacy: three-pass via-grant resolution (match, primary election,
// regular-overlap); splitting risks diverging the passes.
//
//nolint:gocyclo,gocognit,nestif,cyclop,maintidx // see above
func (pm *PolicyManager) ViaRoutesForPeer(viewer, peer types.NodeView) types.ViaRouteResult {
	var result types.ViaRouteResult

	if pm == nil {
		return result
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// pm.pol is written by SetPolicy under pm.mu; reading it before the
	// lock races with concurrent policy reloads.
	if pm.pol == nil {
		return result
	}

	// Self-steering doesn't apply.
	if viewer.ID() == peer.ID() {
		return result
	}

	if !pm.hasViaGrants {
		return result
	}

	// Sources and destinations were resolved when the policy, users or
	// nodes last changed (see [resolveViaGrants]); only the viewer match
	// is per call. The three passes below share it.
	grants := pm.viaGrants
	viewerIPs := viewer.IPs()
	viewerMatchesGrant := make([]bool, len(grants))

	for i := range grants {
		viewerMatchesGrant[i] = grants[i].matches(viewerIPs)
	}

	for i, grant := range grants {
		if len(grant.via) == 0 {
			continue
		}

		if !viewerMatchesGrant[i] {
			continue
		}

		// Filter rules and [tailcfg.Node.AllowedIPs] are different layers.
		// The filter rule carries the dst (the authorisation surface).
		// [tailcfg.Node.AllowedIPs] carries the advertised route (the
		// routing fact the viewer needs to pick this peer). This loop
		// builds the AllowedIPs side, so it emits routes — not dst
		// prefixes.
		peerSubnetRoutes := peer.SubnetRoutes()

		var matchedPrefixes []netip.Prefix

		for _, dstPrefix := range grant.dsts {
			for _, route := range peerSubnetRoutes {
				if dstPrefix.Overlaps(route) {
					matchedPrefixes = append(matchedPrefixes, route)
				}
			}
		}

		// Per-viewer steering for autogroup:internet: a peer advertising
		// approved exit routes is the via-tagged node's analogue of
		// "advertises the destination". The downstream Include/Exclude
		// split below restricts the viewer to exit nodes carrying the
		// via tag.
		if grant.internet && peer.IsExitNode() {
			matchedPrefixes = append(matchedPrefixes, peer.ExitRoutes()...)
		}

		if len(matchedPrefixes) == 0 {
			continue
		}

		// Check if peer has any of the via tags.
		peerHasVia := false

		for _, viaTag := range grant.via {
			if peer.HasTag(string(viaTag)) {
				peerHasVia = true

				break
			}
		}

		if peerHasVia {
			result.Include = append(result.Include, matchedPrefixes...)
		} else {
			result.Exclude = append(result.Exclude, matchedPrefixes...)
		}
	}

	// Detect prefixes that should fall back to HA primary election
	// rather than per-viewer via steering. Two conditions trigger this:
	//
	// 1. Multi-router via: a via grant's tag matches multiple peers
	//    advertising the same prefix.
	// 2. Regular grant overlap: a non-via grant also covers the same
	//    prefix for this viewer.
	//
	// When neither condition is met, per-viewer via steering applies.
	if len(result.Include) > 0 || len(result.Exclude) > 0 {
		// Multi-router via election: when a via grant's tag matches
		// multiple peers advertising the same prefix, only the
		// lowest-ID peer (the via-group primary) keeps the prefix in
		// Include. The others move to Exclude. This mirrors HA
		// primary election scoped to the via tag group.
		//
		// Unlike the global [tailcfg.Node.PrimaryRoutes] election
		// (routes/primary.go), which picks one primary across ALL
		// advertisers of a prefix, this election is scoped to the via tag.
		// Two via grants with different tags (e.g., tag:ha-a vs tag:ha-b)
		// each elect their own winner independently.
		//
		// Only process via grants where the viewer matches the source,
		// otherwise grants for other viewer groups would incorrectly
		// demote the peer.
		for i, grant := range grants {
			if len(grant.via) == 0 {
				continue
			}

			if !viewerMatchesGrant[i] {
				continue
			}

			// Elect per matched route, not per dst — a peer can only
			// be primary for a prefix it actually advertises, and one
			// dst may cover multiple distinct routes.
			for _, dstPrefix := range grant.dsts {
				for _, included := range slices.Clone(result.Include) {
					if !dstPrefix.Overlaps(included) {
						continue
					}

					var viaPrimaryID types.NodeID

					for _, viaTag := range grant.via {
						for _, node := range pm.nodes.All() {
							if node.HasTag(string(viaTag)) &&
								slices.Contains(node.SubnetRoutes(), included) {
								if viaPrimaryID == 0 || node.ID() < viaPrimaryID {
									viaPrimaryID = node.ID()
								}
							}
						}
					}

					if viaPrimaryID != 0 && peer.ID() != viaPrimaryID {
						result.Include = slices.DeleteFunc(result.Include, func(p netip.Prefix) bool {
							return p == included
						})
						if !slices.Contains(result.Exclude, included) {
							result.Exclude = append(result.Exclude, included)
						}
					}
				}
			}
		}

		// Check for regular (non-via) grants covering the same prefix.
		// When a regular grant also covers a prefix that a via grant
		// included, defer to global HA primary election (UsePrimary).
		// When a regular grant covers a prefix that a via grant excluded
		// (peer lacks via tag), remove the exclusion so
		// [state.State.RoutesForPeer] can apply normal
		// [policy.ReduceRoutes] + primary logic.
		for i, grant := range grants {
			if len(grant.via) > 0 {
				continue
			}

			if !viewerMatchesGrant[i] {
				continue
			}

			// A non-via grant covering routes that a via grant included
			// defers to global HA primary election. Match by overlap so
			// a broader or narrower regular dst still catches the
			// routes the via grant added to Include.
			for _, dstPrefix := range grant.dsts {
				for _, p := range result.Include {
					if dstPrefix.Overlaps(p) &&
						!slices.Contains(result.UsePrimary, p) {
						result.UsePrimary = append(result.UsePrimary, p)
					}
				}

				result.Exclude = slices.DeleteFunc(result.Exclude, dstPrefix.Overlaps)
			}
		}
	}

	return result
}

// resolvedViaGrant is one grant of the policy, ACLs included, with its
// sources and destinations resolved to addresses. It is what
// [PolicyManager.ViaRoutesForPeer] reads; resolving there instead cost
// every viewer-peer pair a full pass of alias resolution, which dominated
// full map builds.
type resolvedViaGrant struct {
	via      []Tag
	srcs     []ResolvedAddresses
	dsts     []netip.Prefix
	internet bool
}

// matches reports whether any of ips is a source of the grant.
func (g *resolvedViaGrant) matches(ips []netip.Addr) bool {
	for _, src := range g.srcs {
		if slices.ContainsFunc(ips, src.Contains) {
			return true
		}
	}

	return false
}

// resolveViaGrants resolves every grant of pol for
// [PolicyManager.ViaRoutesForPeer] and reports whether any of them has a
// via tag. Sources that fail to resolve are skipped, as they were when the
// resolution happened per call.
func resolveViaGrants(
	pol *Policy,
	users types.Users,
	nodes views.Slice[types.NodeView],
) ([]resolvedViaGrant, bool) {
	if pol == nil {
		return nil, false
	}

	grants := slices.Clone(pol.Grants)
	for _, acl := range pol.ACLs {
		grants = append(grants, aclToGrants(acl)...)
	}

	resolved := make([]resolvedViaGrant, len(grants))
	hasVia := false

	for i, grant := range grants {
		resolved[i].via = grant.Via
		hasVia = hasVia || len(grant.Via) > 0

		for _, src := range grant.Sources {
			ips, err := src.Resolve(pol, users, nodes)
			if err != nil || ips == nil {
				continue
			}

			resolved[i].srcs = append(resolved[i].srcs, ips)
		}

		resolved[i].dsts, resolved[i].internet = resolveViaDestinations(pol, users, nodes, grant.Destinations)
	}

	return resolved, hasVia
}

func (pm *PolicyManager) Version() int {
	return 2
}

func (pm *PolicyManager) DebugString() string {
	if pm == nil {
		return "PolicyManager is not setup"
	}

	// pm.pol, filter, matchers, and the derived maps are all written
	// under pm.mu by SetPolicy/SetUsers/SetNodes.
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var sb strings.Builder

	fmt.Fprintf(&sb, "PolicyManager (v%d):\n\n", pm.Version())

	sb.WriteString("\n\n")

	if pm.pol != nil {
		pol, err := json.MarshalIndent(pm.pol, "", "  ")
		if err == nil {
			sb.WriteString("Policy:\n")
			sb.Write(pol)
			sb.WriteString("\n\n")
		}
	}

	fmt.Fprintf(&sb, "AutoApprover (%d):\n", len(pm.autoApproveMap))

	for prefix, approveAddrs := range pm.autoApproveMap {
		fmt.Fprintf(&sb, "\t%s:\n", prefix)

		for _, iprange := range approveAddrs.Ranges() {
			fmt.Fprintf(&sb, "\t\t%s\n", iprange)
		}
	}

	sb.WriteString("\n\n")

	fmt.Fprintf(&sb, "TagOwner (%d):\n", len(pm.tagOwnerMap))

	for prefix, tagOwners := range pm.tagOwnerMap {
		fmt.Fprintf(&sb, "\t%s:\n", prefix)

		for _, iprange := range tagOwners.Ranges() {
			fmt.Fprintf(&sb, "\t\t%s\n", iprange)
		}
	}

	sb.WriteString("\n\n")

	if pm.filter != nil {
		filter, err := json.MarshalIndent(pm.filter, "", "  ")
		if err == nil {
			sb.WriteString("Compiled filter:\n")
			sb.Write(filter)
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString("Matchers:\n")
	sb.WriteString("an internal structure used to filter nodes and routes\n")

	for _, match := range pm.matchers {
		sb.WriteString(match.DebugString())
		sb.WriteString("\n")
	}

	sb.WriteString("\n\n")
	sb.WriteString("Nodes:\n")

	for _, node := range pm.nodes.All() {
		sb.WriteString(node.String())
		sb.WriteString("\n")
	}

	return sb.String()
}

// flattenTags resolves nested tag-owner references. Cycles
// (tag:a -> tag:b -> tag:a, or tag:a -> tag:a) drop the cycle-causing
// edge and contribute no addresses; non-cycle owners on the cycled tags
// still resolve. Undefined-tag references remain a hard error.
func flattenTags(tagOwners TagOwners, tag Tag, visiting map[Tag]bool, chain []Tag) (Owners, error) {
	if visiting[tag] {
		return nil, nil
	}

	visiting[tag] = true

	chain = append(chain, tag)
	defer delete(visiting, tag)

	var result Owners

	for _, owner := range tagOwners[tag] {
		switch o := owner.(type) {
		case *Tag:
			if _, ok := tagOwners[*o]; !ok {
				return nil, fmt.Errorf("tag %q %w %q", tag, ErrUndefinedTagReference, *o)
			}

			nested, err := flattenTags(tagOwners, *o, visiting, chain)
			if err != nil {
				return nil, err
			}

			result = append(result, nested...)
		default:
			result = append(result, owner)
		}
	}

	return result, nil
}

// flattenTagOwners flattens all [TagOwners] by resolving nested tags and detecting cycles.
// It will return a new [TagOwners] map where all the [Tag] types have been resolved to their underlying [Owners].
func flattenTagOwners(tagOwners TagOwners) (TagOwners, error) {
	ret := make(TagOwners)

	for tag := range tagOwners {
		flattened, err := flattenTags(tagOwners, tag, make(map[Tag]bool), nil)
		if err != nil {
			return nil, err
		}

		slices.SortFunc(flattened, func(a, b Owner) int {
			return cmp.Compare(a.String(), b.String())
		})
		ret[tag] = slices.CompactFunc(flattened, func(a, b Owner) bool {
			return a.String() == b.String()
		})
	}

	return ret, nil
}

// resolveTagOwners resolves the [TagOwners] to a map of [Tag] to [netipx.IPSet].
// The resulting map can be used to quickly look up the IPSet for a given [Tag].
// It is intended for internal use in a [PolicyManager].
func resolveTagOwners(p *Policy, users types.Users, nodes views.Slice[types.NodeView]) (map[Tag]*netipx.IPSet, error) {
	if p == nil {
		return make(map[Tag]*netipx.IPSet), nil
	}

	if len(p.TagOwners) == 0 {
		return make(map[Tag]*netipx.IPSet), nil
	}

	ret := make(map[Tag]*netipx.IPSet)

	tagOwners, err := flattenTagOwners(p.TagOwners)
	if err != nil {
		return nil, err
	}

	for tag, owners := range tagOwners {
		var ips netipx.IPSetBuilder

		for _, owner := range owners {
			switch o := owner.(type) {
			case *Tag:
				// After flattening, Tag types should not appear in the owners list.
				// If they do, skip them as they represent already-resolved references.

			case Alias:
				// If it does not resolve, that means the tag is not associated with any IP addresses.
				resolved, _ := o.Resolve(p, users, nodes)
				if resolved != nil {
					for _, pref := range resolved.Prefixes() {
						ips.AddPrefix(pref)
					}
				}

			default:
				// Should never happen - after flattening, all owners should be Alias types
				return nil, fmt.Errorf("%w: %v", ErrInvalidTagOwner, owner)
			}
		}

		ipSet, err := ips.IPSet()
		if err != nil {
			return nil, fmt.Errorf("building tag owner IP set for %s: %w", tag, err)
		}

		ret[tag] = ipSet
	}

	return ret, nil
}

// IPPoolFor returns the nodeAttrs ipPool a new node is numbered from, or
// nil when no grant names it. A target that fails to resolve is logged and
// treated as no pool, so a stale user in the policy does not block
// registration.
func (pm *PolicyManager) IPPoolFor(node types.NodeView) []netip.Prefix {
	if pm == nil {
		return nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pools, err := pm.pol.ipPoolFor(pm.users, node)
	if err != nil {
		log.Warn().Err(err).Str("node", node.Hostname()).Msg("resolving ipPool for a new node")

		return nil
	}

	return pools
}

// NodeCapMap returns the policy-derived CapMap for the given node, or
// nil when the node has no nodeAttrs entries that target it. The
// returned map is a defensive clone — caller mutations cannot reach
// the manager-owned cache.
func (pm *PolicyManager) NodeCapMap(id types.NodeID) tailcfg.NodeCapMap {
	if pm == nil {
		return nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	src := pm.nodeAttrsMap[id]
	if len(src) == 0 {
		return nil
	}

	out := make(tailcfg.NodeCapMap, len(src))
	maps.Copy(out, src)

	return out
}

// NodeCapMaps returns a snapshot of the per-node policy CapMap. The
// mapper calls this once per request to amortise lock acquisitions
// over a peer-loop instead of taking the lock per peer. The returned
// map is a fresh container; the inner [tailcfg.NodeCapMap] values are
// shared with the manager and must be treated as read-only.
func (pm *PolicyManager) NodeCapMaps() map[types.NodeID]tailcfg.NodeCapMap {
	if pm == nil {
		return nil
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	out := make(map[types.NodeID]tailcfg.NodeCapMap, len(pm.nodeAttrsMap))
	maps.Copy(out, pm.nodeAttrsMap)

	return out
}

// NodesWithChangedCapMap returns the IDs of nodes whose nodeAttrs
// CapMap shifted across one or more [PolicyManager.updateLocked] calls
// since the last drain. The buffer drains on return. The mapper calls
// this once per [state.State.ReloadPolicy] to decide which nodes need
// a [change.SelfUpdate].
//
// [PolicyManager.refreshNodeAttrsLocked] APPENDS to the buffer; the drain
// returns the union of every change since the previous read. A concurrent
// [PolicyManager.SetUsers]/[PolicyManager.SetNodes] between
// [PolicyManager.SetPolicy] and a drain cannot silently lose the
// policy-reload diff.
func (pm *PolicyManager) NodesWithChangedCapMap() []types.NodeID {
	if pm == nil {
		return nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	out := pm.nodeAttrsChanged
	pm.nodeAttrsChanged = nil

	return out
}

// peerPositionsLocked runs the pair scan that fits the policy: the global
// filter serves every node unless via grants or autogroup:self need a
// filter per node (compileFilterRules skips via grants).
func (pm *PolicyManager) peerPositionsLocked(nodes views.Slice[types.NodeView], routes []nodeRoutes) [][]int32 {
	if !pm.needsPerNodeFilter {
		return pm.globalPeerListsLocked(nodes, routes)
	}

	return pm.perNodePeerListsLocked(nodes, routes)
}

// globalPeerListsLocked scans every node pair under the global filter and
// returns each node's peers, indexed by the node's position in nodes.
func (pm *PolicyManager) globalPeerListsLocked(
	nodes views.Slice[types.NodeView],
	routes []nodeRoutes,
) [][]int32 {
	peers := make([][]int32, nodes.Len())

	for i := range nodes.Len() {
		nodeI, ri := nodes.At(i), routes[i]

		for j := i + 1; j < nodes.Len(); j++ {
			nodeJ := nodes.At(j)
			if nodeI.ID() == nodeJ.ID() {
				continue
			}

			if pm.globalPeersLocked(nodeI, nodeJ, ri, routes[j]) {
				peers[i] = append(peers[i], int32(j))
				peers[j] = append(peers[j], int32(i))
			}
		}
	}

	return peers
}

// perNodePeerListsLocked does the same scan for autogroup:self and via
// grants, where each node has its own filter.
func (pm *PolicyManager) perNodePeerListsLocked(
	nodes views.Slice[types.NodeView],
	routes []nodeRoutes,
) [][]int32 {
	// Pre-compute per-node matchers using unreduced compiled rules
	// We need unreduced rules to determine peer relationships correctly.
	// Reduced rules only show destinations where the node is the target,
	// but peer relationships require the full bidirectional access rules.
	nodeMatchers := make([][]matcher.Match, nodes.Len())
	for i := range nodes.Len() {
		nodeMatchers[i] = matcher.MatchesFromFilterRules(pm.filterRulesForNodeLocked(nodes.At(i)))
	}

	peers := make([][]int32, nodes.Len())

	// Check each node pair for peer relationships.
	// Start j at i+1 to avoid checking the same pair twice and creating duplicates.
	for i := range nodes.Len() {
		nodeI, mi, ri := nodes.At(i), nodeMatchers[i], routes[i]

		for j := i + 1; j < nodes.Len(); j++ {
			nodeJ := nodes.At(j)

			if perNodePeers(nodeI, nodeJ, mi, nodeMatchers[j], ri, routes[j]) {
				peers[i] = append(peers[i], int32(j))
				peers[j] = append(peers[j], int32(i))
			}
		}
	}

	return peers
}

// globalPeersLocked reports whether a and b see each other under the
// global filter: either may reach the other.
func (pm *PolicyManager) globalPeersLocked(a, b types.NodeView, ra, rb nodeRoutes) bool {
	return a.CanAccessWithRoutes(pm.matchers, b, ra.subnet, rb.subnet, rb.isExit) ||
		b.CanAccessWithRoutes(pm.matchers, a, rb.subnet, ra.subnet, ra.isExit)
}

// matchersForNodeLocked derives a node's unreduced matchers from the
// compiled grants, cached until the next recompile. The lock must be held.
func (pm *PolicyManager) matchersForNodeLocked(node types.NodeView) []matcher.Match {
	if cached, ok := pm.matchersForNodeMap.Load(node.ID()); ok {
		return cached
	}

	// For autogroup:self or via grants, derive matchers from
	// the stored compiled grants for this specific node.
	unreduced := pm.filterRulesForNodeLocked(node)
	matchers := matcher.MatchesFromFilterRules(unreduced)
	pm.matchersForNodeMap.Store(node.ID(), matchers)

	return matchers
}

// refreshAutoApproversLocked recomputes the route and service auto-approver
// maps and the exit node set, updating pm's cached hashes in place, and
// reports whether each changed since the last call. The lock must be held.
func (pm *PolicyManager) refreshAutoApproversLocked() (bool, bool, error) {
	autoMap, exitSet, err := resolveAutoApprovers(pm.pol, pm.users, pm.nodes)
	if err != nil {
		return false, false, fmt.Errorf("resolving auto approvers map: %w", err)
	}

	pm.autoApproveServices, err = resolveServiceAutoApprovers(pm.pol, pm.users, pm.nodes)
	if err != nil {
		return false, false, fmt.Errorf("resolving service auto approvers: %w", err)
	}

	autoApproveMapHash := deephash.Hash(&autoMap)

	autoApproveChanged := autoApproveMapHash != pm.autoApproveMapHash
	if autoApproveChanged {
		log.Debug().
			Str("autoApprove.hash.old", pm.autoApproveMapHash.String()[:8]).
			Str("autoApprove.hash.new", autoApproveMapHash.String()[:8]).
			Int("autoApprovers.old", len(pm.autoApproveMap)).
			Int("autoApprovers.new", len(autoMap)).
			Msg("Auto-approvers hash changed")
	}

	pm.autoApproveMap = autoMap
	pm.autoApproveMapHash = autoApproveMapHash

	exitSetHash := deephash.Hash(&exitSet)

	exitSetChanged := exitSetHash != pm.exitSetHash
	if exitSetChanged {
		log.Debug().
			Str("exitSet.hash.old", pm.exitSetHash.String()[:8]).
			Str("exitSet.hash.new", exitSetHash.String()[:8]).
			Msg("Exit node set hash changed")
	}

	pm.exitSet = exitSet
	pm.exitSetHash = exitSetHash

	return autoApproveChanged, exitSetChanged, nil
}

// updateLocked updates the filter rules based on the current policy and nodes.
// It must be called with the lock held.
func (pm *PolicyManager) updateLocked() (bool, error) {
	// The access model compiles next to the file's grants, so it is
	// attached before every compile; rules without a file still need a
	// policy to hang off.
	if pm.pol == nil && hasAccessGrants(pm.access) {
		pm.pol = &Policy{}
	}

	if pm.pol == nil && len(pm.vipServices) > 0 {
		pm.pol = &Policy{}
	}

	if pm.pol != nil {
		pm.pol.access = pm.access
		pm.pol.country = pm.country
		pm.pol.recording = pm.sshRecording
		pm.pol.services = servicesByName(pm.vipServices)
	}

	pm.usesSourceAddress = pm.pol.usesSourceAddress()

	// Compile all grants once. Both global and per-node filter
	// rules are derived from these compiled grants.
	pm.compiledGrants = pm.pol.compileGrants(pm.users, pm.nodes)
	pm.userNodeIdx = buildUserNodeIndex(pm.nodes)
	pm.viaGrants, pm.hasViaGrants = resolveViaGrants(pm.pol, pm.users, pm.nodes)
	pm.needsPerNodeFilter = hasPerNodeGrants(pm.compiledGrants)
	pm.viaTargetTags = collectViaTargetTags(pm.compiledGrants)

	relayTargetIPs, err := collectRelayTargetIPs(pm.compiledGrants)
	if err != nil {
		return false, fmt.Errorf("collecting relay target IPs: %w", err)
	}

	pm.relayTargetIPs = relayTargetIPs

	var filter []tailcfg.FilterRule
	if !pm.pol.enforces() {
		filter = tailcfg.FilterAllowAll

		// An open tailnet still needs the ingress capability on the
		// funnel nodes' filters; allow-all carries no capabilities.
		if funnel := pm.pol.funnelFilterRules(pm.users, pm.nodes); len(funnel) > 0 {
			filter = append(slices.Clone(tailcfg.FilterAllowAll), funnel...)
		}
	} else {
		filter = globalFilterRules(pm.compiledGrants)
	}

	// Hash both the compiled filter AND the policy content together.
	// This ensures filterHash changes when policy changes, even for autogroup:self
	// where the compiled filter is always empty. This eliminates the need for
	// a separate policyHash field.
	filterHash := deephash.Hash(&filterAndPolicy{
		Filter:   filter,
		Policy:   pm.pol,
		Access:   pm.access,
		Services: pm.vipServices,
	})

	filterChanged := filterHash != pm.filterHash
	if filterChanged {
		log.Debug().
			Str("filter.hash.old", pm.filterHash.String()[:8]).
			Str("filter.hash.new", filterHash.String()[:8]).
			Int("filter.rules", len(pm.filter)).
			Int("filter.rules.new", len(filter)).
			Msg("Policy filter hash changed")
	}

	pm.filter = filter

	pm.filterHash = filterHash
	if filterChanged {
		pm.matchers = matcher.MatchesFromFilterRules(pm.filter)
	}

	// Order matters, tags might be used in autoapprovers, so we need to ensure
	// that the map for tag owners is resolved before resolving autoapprovers.
	// TODO(kradalby): Order might not matter after #2417
	tagMap, err := resolveTagOwners(pm.pol, pm.users, pm.nodes)
	if err != nil {
		return false, fmt.Errorf("resolving tag owners map: %w", err)
	}

	tagOwnerMapHash := deephash.Hash(&tagMap)

	tagOwnerChanged := tagOwnerMapHash != pm.tagOwnerMapHash
	if tagOwnerChanged {
		log.Debug().
			Str("tagOwner.hash.old", pm.tagOwnerMapHash.String()[:8]).
			Str("tagOwner.hash.new", tagOwnerMapHash.String()[:8]).
			Int("tagOwners.old", len(pm.tagOwnerMap)).
			Int("tagOwners.new", len(tagMap)).
			Msg("Tag owner hash changed")
	}

	pm.tagOwnerMap = tagMap
	pm.tagOwnerMapHash = tagOwnerMapHash

	autoApproveChanged, exitSetChanged, err := pm.refreshAutoApproversLocked()
	if err != nil {
		return false, err
	}

	// Recompile per-node nodeAttrs CapMap and append the diff to
	// pm.nodeAttrsChanged. The drain (NodesWithChangedCapMap) returns
	// the accumulated union of every change since the last drain;
	// SetUsers/SetNodes appending between SetPolicy and the drain
	// cannot lose the policy-reload diff.
	err = pm.refreshNodeAttrsLocked()
	if err != nil {
		return false, err
	}

	// Determine if we need to send updates to nodes
	// filterChanged now includes policy content changes (via combined hash),
	// so it will detect changes even for autogroup:self where compiled filter is empty
	needsUpdate := filterChanged || tagOwnerChanged || autoApproveChanged || exitSetChanged

	// Only clear caches if we're actually going to send updates
	// This prevents clearing caches when nothing changed, which would leave nodes
	// with stale filters until they reconnect. This is critical for autogroup:self
	// where even reloading the same policy would clear caches but not send updates.
	if needsUpdate {
		// Clear the SSH policy map to ensure it's recalculated with the new policy.
		// TODO(kradalby): This could potentially be optimized by only clearing the
		// policies for nodes that have changed. Particularly if the only difference is
		// that nodes has been added or removed.
		pm.sshPolicyMap.Clear()
		pm.filterRulesMap.Clear()
		pm.matchersForNodeMap.Clear()
	}

	// If nothing changed, no need to update nodes
	if !needsUpdate {
		log.Trace().
			Msg("Policy evaluation detected no changes - all hashes match")

		return false, nil
	}

	log.Debug().
		Bool("filter.changed", filterChanged).
		Bool("tagOwners.changed", tagOwnerChanged).
		Bool("autoApprovers.changed", autoApproveChanged).
		Bool("exitNodes.changed", exitSetChanged).
		Msg("Policy changes require node updates")

	return true, nil
}

// filterRulesForNodeLocked returns the unreduced compiled filter rules
// for a node, combining pre-compiled global rules with per-node self
// and via rules from the stored compiled grants.
func (pm *PolicyManager) filterRulesForNodeLocked(
	node types.NodeView,
) []tailcfg.FilterRule {
	return filterRulesForNode(
		pm.compiledGrants, node, pm.userNodeIdx,
	)
}

// filterForNodeLocked returns the filter rules for a specific node,
// already reduced to only include rules relevant to that node.
//
// Fast path (!needsPerNodeFilter): reduces global filter per-node.
// Slow path (needsPerNodeFilter): combines global + self + via rules
// from the stored compiled grants, then reduces.
//
// Both paths derive from the same compiledGrants, ensuring there is
// no divergence between global and per-node filter output.
//
// Lock-free version for internal use when the lock is already held.
func (pm *PolicyManager) filterForNodeLocked(
	node types.NodeView,
) []tailcfg.FilterRule {
	if pm == nil {
		return nil
	}

	if rules, ok := pm.filterRulesMap.Load(node.ID()); ok {
		return rules
	}

	var unreduced []tailcfg.FilterRule
	if !pm.needsPerNodeFilter {
		unreduced = pm.filter
	} else {
		unreduced = pm.filterRulesForNodeLocked(node)
	}

	reduced := policyutil.ReduceFilterRules(node, unreduced)
	pm.filterRulesMap.Store(node.ID(), reduced)

	return reduced
}

func (pm *PolicyManager) nodesHavePolicyAffectingChanges(newNodes views.Slice[types.NodeView]) bool {
	if pm.nodes.Len() != newNodes.Len() {
		return true
	}

	oldNodes := nodeIDViewMap(pm.nodes)

	for _, newNode := range newNodes.All() {
		oldNode, exists := oldNodes[newNode.ID()]
		if !exists {
			return true
		}

		if newNode.HasPolicyChange(oldNode) {
			return true
		}

		if pm.usesSourceAddress && newNode.SourceAddr() != oldNode.SourceAddr() {
			return true
		}

		// Via grants and autogroup:self compile filter rules per-node
		// that depend on the node's route state (SubnetRoutes, ExitRoutes).
		// Route changes are policy-affecting in this context because they
		// alter which filter rules get generated for the via-designated node.
		if pm.needsPerNodeFilter && newNode.HasNetworkChanges(oldNode) {
			return true
		}
	}

	return false
}

// userMatchesOwner checks if a user matches a tag owner entry.
// This is used as a fallback when the node's IP is not in the [PolicyManager.tagOwnerMap].
func (pm *PolicyManager) userMatchesOwner(user types.UserView, owner Owner) bool {
	switch o := owner.(type) {
	case *Username:
		if o == nil {
			return false
		}
		// Resolve the username to find the user it refers to
		resolvedUser, err := o.resolveUser(pm.users)
		if err != nil {
			return false
		}

		return user.ID() == resolvedUser.ID

	case *Group:
		if o == nil || pm.pol == nil {
			return false
		}
		// Resolve the group to get usernames
		usernames, ok := pm.pol.Groups[*o]
		if !ok {
			return false
		}
		// Check if the user matches any username in the group
		for _, uname := range usernames {
			resolvedUser, err := uname.resolveUser(pm.users)
			if err != nil {
				continue
			}

			if user.ID() == resolvedUser.ID {
				return true
			}
		}

		return false

	default:
		return false
	}
}

// collectRemovedAutogroupSelfUsers adds the owners of nodes present in
// oldNodeMap but absent from newNodeMap to affected. Only non-tagged nodes
// affect autogroup:self.
func collectRemovedAutogroupSelfUsers(
	oldNodeMap, newNodeMap map[types.NodeID]types.NodeView,
	affected map[types.UserID]struct{},
) {
	for nodeID, oldNode := range oldNodeMap {
		if _, exists := newNodeMap[nodeID]; !exists && !oldNode.IsTagged() {
			affected[oldNode.TypedUserID()] = struct{}{}
		}
	}
}

// collectAddedAutogroupSelfUsers adds the owners of nodes present in
// newNodeMap but absent from oldNodeMap to affected. Only non-tagged nodes
// affect autogroup:self.
func collectAddedAutogroupSelfUsers(
	oldNodeMap, newNodeMap map[types.NodeID]types.NodeView,
	affected map[types.UserID]struct{},
) {
	for nodeID, newNode := range newNodeMap {
		if _, exists := oldNodeMap[nodeID]; !exists && !newNode.IsTagged() {
			affected[newNode.TypedUserID()] = struct{}{}
		}
	}
}

// collectModifiedAutogroupSelfUsers adds the owners affected by user, tag,
// or IP changes on nodes present in both maps.
func collectModifiedAutogroupSelfUsers(
	oldNodeMap, newNodeMap map[types.NodeID]types.NodeView,
	affected map[types.UserID]struct{},
) {
	for nodeID, newNode := range newNodeMap {
		oldNode, exists := oldNodeMap[nodeID]
		if !exists {
			continue
		}

		// Check if tag status changed — this affects the user's autogroup:self device set.
		// Use the non-tagged version to get the user ID safely.
		if oldNode.IsTagged() != newNode.IsTagged() {
			if !oldNode.IsTagged() {
				// Was untagged, now tagged: user lost a device
				affected[oldNode.TypedUserID()] = struct{}{}
			} else {
				// Was tagged, now untagged: user gained a device
				affected[newNode.TypedUserID()] = struct{}{}
			}

			continue
		}

		// Skip tagged nodes for remaining checks — they don't participate in autogroup:self
		if newNode.IsTagged() {
			continue
		}

		// Check if user changed (both versions are non-tagged here)
		if oldNode.TypedUserID() != newNode.TypedUserID() {
			affected[oldNode.TypedUserID()] = struct{}{}
			affected[newNode.TypedUserID()] = struct{}{}
		}

		// Check if IPs changed.
		if !slices.Equal(oldNode.IPs(), newNode.IPs()) {
			affected[newNode.TypedUserID()] = struct{}{}
		}
	}
}

// invalidateAutogroupSelfCache intelligently clears only the cache entries that need to be
// invalidated when using autogroup:self policies. This is much more efficient than clearing
// the entire cache.
func (pm *PolicyManager) invalidateAutogroupSelfCache(oldNodes, newNodes views.Slice[types.NodeView]) {
	// Build maps for efficient lookup
	oldNodeMap := nodeIDViewMap(oldNodes)
	newNodeMap := nodeIDViewMap(newNodes)

	// Track which users are affected by changes.
	// Tagged nodes don't participate in autogroup:self (identity is tag-based),
	// so we skip them when collecting affected users, except when tag status changes
	// (which affects the user's device set).
	//
	// Ownership is keyed on TypedUserID (the UserID field), not the User
	// association view: the NodeStore holds nodes by value with User as a
	// *User pointer, and not every write path hydrates that association. A
	// non-tagged node always has UserID set, so it is the reliable owner key.
	affectedUsers := make(map[types.UserID]struct{})

	collectRemovedAutogroupSelfUsers(oldNodeMap, newNodeMap, affectedUsers)
	collectAddedAutogroupSelfUsers(oldNodeMap, newNodeMap, affectedUsers)
	collectModifiedAutogroupSelfUsers(oldNodeMap, newNodeMap, affectedUsers)

	pm.clearAutogroupSelfCacheForAffectedUsers(oldNodeMap, newNodeMap, affectedUsers)

	if len(affectedUsers) > 0 {
		log.Debug().
			Int("affected_users", len(affectedUsers)).
			Int("remaining_cache_entries", pm.filterRulesMap.Size()).
			Msg("Selectively cleared autogroup:self cache for affected users")
	}
}

// clearAutogroupSelfCacheForAffectedUsers clears cache entries for affected
// users only. For autogroup:self, all nodes belonging to affected users must
// be cleared because autogroup:self rules depend on the entire user's device
// set. Entries for nodes absent from both maps are always cleared.
func (pm *PolicyManager) clearAutogroupSelfCacheForAffectedUsers(
	oldNodeMap, newNodeMap map[types.NodeID]types.NodeView,
	affectedUsers map[types.UserID]struct{},
) {
	pm.filterRulesMap.Range(func(nodeID types.NodeID, _ []tailcfg.FilterRule) bool {
		// Find the user for this cached node using the already-built indexes.
		node, ok := newNodeMap[nodeID]
		if !ok {
			node, ok = oldNodeMap[nodeID]
		}

		// Node not found in either old or new list, clear it.
		if !ok {
			pm.filterRulesMap.Delete(nodeID)
			pm.matchersForNodeMap.Delete(nodeID)

			return true
		}

		// Tagged nodes don't participate in autogroup:self, so their cache
		// doesn't need user-based invalidation; leave nodeUserID at zero.
		var nodeUserID types.UserID
		if !node.IsTagged() {
			nodeUserID = node.TypedUserID()
		}

		// If the owning user is affected, clear this cache entry.
		if _, affected := affectedUsers[nodeUserID]; affected {
			pm.filterRulesMap.Delete(nodeID)
			pm.matchersForNodeMap.Delete(nodeID)
		}

		return true
	})
}

// invalidateNodeCache invalidates cache entries based on what changed.
func (pm *PolicyManager) invalidateNodeCache(newNodes views.Slice[types.NodeView]) {
	if pm.needsPerNodeFilter {
		// For autogroup:self or via grants, a node's filter depends
		// on its peers. When any node changes, invalidate affected
		// users' caches.
		pm.invalidateAutogroupSelfCache(pm.nodes, newNodes)
	} else {
		// For global policies, a node's filter depends only on its
		// own properties. Only invalidate changed nodes.
		pm.invalidateGlobalPolicyCache(newNodes)
	}
}

// invalidateGlobalPolicyCache invalidates only nodes whose properties affecting
// [policyutil.ReduceFilterRules] changed. For global policies, each node's filter is independent.
func (pm *PolicyManager) invalidateGlobalPolicyCache(newNodes views.Slice[types.NodeView]) {
	oldNodeMap := nodeIDViewMap(pm.nodes)
	newNodeMap := nodeIDViewMap(newNodes)

	// Invalidate nodes whose properties changed
	for nodeID, newNode := range newNodeMap {
		oldNode, existed := oldNodeMap[nodeID]
		if !existed {
			// New node - no cache entry yet, will be lazily calculated
			continue
		}

		if newNode.HasNetworkChanges(oldNode) {
			pm.filterRulesMap.Delete(nodeID)
			pm.matchersForNodeMap.Delete(nodeID)
		}
	}

	// Remove deleted nodes from cache
	pm.filterRulesMap.Range(func(nodeID types.NodeID, _ []tailcfg.FilterRule) bool {
		if _, exists := newNodeMap[nodeID]; !exists {
			pm.filterRulesMap.Delete(nodeID)
		}

		return true
	})

	pm.matchersForNodeMap.Range(func(nodeID types.NodeID, _ []matcher.Match) bool {
		if _, exists := newNodeMap[nodeID]; !exists {
			pm.matchersForNodeMap.Delete(nodeID)
		}

		return true
	})
}

// refreshNodeAttrsLocked recompiles the per-node nodeAttrs CapMap and
// appends the IDs whose CapMap differs from the previous snapshot
// (including newly-targeted nodes and nodes that lost all attrs) to
// pm.nodeAttrsChanged. Append, not overwrite: a concurrent
// SetUsers/SetNodes between SetPolicy and a NodesWithChangedCapMap
// drain cannot clobber the policy-reload diff.
//
// Caller must hold pm.mu.
func (pm *PolicyManager) refreshNodeAttrsLocked() error {
	// Fast path for the common steady-state shape: tailnet has no
	// nodeAttrs entries and never had any. Skip the compile + per-node
	// hash walk entirely. As soon as the operator adds a nodeAttrs
	// entry pm.nodeAttrsHashes becomes non-empty and the gate opens.
	// Role caps (is-admin, is-owner) ride on the same map, so the gate
	// also stays shut only while no user holds a role that stamps them.
	if pm.pol != nil &&
		len(pm.pol.NodeAttrs) == 0 &&
		!pm.pol.RandomizeClientPort &&
		len(pm.nodeAttrsHashes) == 0 &&
		!usersHaveAdmin(pm.users) &&
		!NodesHaveGlobalExitNode(pm.nodes) &&
		len(pm.vipServices) == 0 {
		return nil
	}

	newMap, err := pm.pol.compileNodeAttrs(pm.users, pm.nodes)
	if err != nil {
		return fmt.Errorf("compiling nodeAttrs: %w", err)
	}

	stampRoleCaps(pm.users, pm.nodes, newMap)
	stampServiceCaps(pm.vipServices, pm.nodes, serviceReachable(pm.pol.enforces(), pm.matchers), newMap)

	newHashes := make(map[types.NodeID]deephash.Sum, len(newMap))
	for id, capMap := range newMap {
		newHashes[id] = deephash.Hash(&capMap)
	}

	// Walk the union of old and new node IDs and emit the delta.
	seen := make(map[types.NodeID]struct{}, len(newHashes)+len(pm.nodeAttrsHashes))

	var changed []types.NodeID

	for id, h := range newHashes {
		seen[id] = struct{}{}
		if pm.nodeAttrsHashes[id] != h {
			changed = append(changed, id)
		}
	}

	for id := range pm.nodeAttrsHashes {
		if _, ok := seen[id]; ok {
			continue
		}
		// Node lost all nodeAttrs since the last update.
		changed = append(changed, id)
	}

	pm.nodeAttrsMap = newMap
	pm.nodeAttrsHashes = newHashes
	pm.nodeAttrsChanged = append(pm.nodeAttrsChanged, changed...)

	return nil
}

// findNodePairLocked looks up the node views for srcNodeID and dstNodeID
// among the currently known nodes. Callers must hold pm.mu.
func (pm *PolicyManager) findNodePairLocked(srcNodeID, dstNodeID types.NodeID) (types.NodeView, types.NodeView) {
	var srcNode, dstNode types.NodeView

	for _, n := range pm.nodes.All() {
		nid := n.ID()
		if nid == srcNodeID {
			srcNode = n
		}

		if nid == dstNodeID {
			dstNode = n
		}

		if srcNode.Valid() && dstNode.Valid() {
			break
		}
	}

	return srcNode, dstNode
}

// sshCheckPeriodForRule reports the check period for rule when it is a
// check rule matching the (srcNode, dstNode) pair, and whether it matched.
func (pm *PolicyManager) sshCheckPeriodForRule(
	rule SSH,
	srcNode, dstNode types.NodeView,
) (time.Duration, bool) {
	if rule.Action != SSHActionCheck {
		return 0, false
	}

	// Resolve sources and check if src node matches.
	srcIPs, err := rule.Sources.Resolve(pm.pol, pm.users, pm.nodes)
	if err != nil || srcIPs == nil {
		return 0, false
	}

	if !slices.ContainsFunc(srcNode.IPs(), srcIPs.Contains) {
		return 0, false
	}

	// Check if dst node matches any destination.
	for _, dst := range rule.Destinations {
		if ag, isAG := dst.(*AutoGroup); isAG && ag.Is(AutoGroupSelf) {
			if sshAutogroupSelfMatches(srcNode, dstNode) {
				return checkPeriodFromRule(rule), true
			}

			continue
		}

		dstIPs, err := dst.Resolve(pm.pol, pm.users, pm.nodes)
		if err != nil || dstIPs == nil {
			continue
		}

		if slices.ContainsFunc(dstNode.IPs(), dstIPs.Contains) {
			return checkPeriodFromRule(rule), true
		}
	}

	return 0, false
}

// sshAutogroupSelfMatches reports whether srcNode and dstNode belong to the
// same non-tagged user, matching an autogroup:self SSH check destination.
//
// User().Valid() guards the User().ID() dereference: the NodeStore can hold
// a non-tagged node with UserID set but the User association unhydrated
// (nil), and IsTagged() alone does not cover that. Mirrors filter.go's
// autogroup:self guard. Without it, a tailnet client on the Noise
// SSH-check path crashes the server (nil deref).
func sshAutogroupSelfMatches(srcNode, dstNode types.NodeView) bool {
	return !srcNode.IsTagged() && !dstNode.IsTagged() &&
		srcNode.User().Valid() && dstNode.User().Valid() &&
		srcNode.User().ID() == dstNode.User().ID()
}
