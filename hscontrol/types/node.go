package types

import (
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/policy/matcher"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/juanfont/headscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog"
	"go4.org/netipx"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
	"tailscale.com/util/dnsname"
)

var (
	ErrNodeAddressesInvalid = errors.New("parsing node addresses")
	ErrHostnameTooLong      = errors.New("hostname too long, cannot accept more than 255 ASCII chars")
	ErrNodeHasNoGivenName   = errors.New("node has no given name")
	ErrNodeUserHasNoName    = errors.New("node user has no name")
	ErrCannotRemoveAllTags  = errors.New("cannot remove all tags from node")
	ErrInvalidNodeView      = errors.New("cannot convert invalid NodeView to tailcfg.Node")
)

// RouteFunc is a function that takes a node ID and returns a list of
// [netip.Prefix] values representing the routes for that node.
type RouteFunc func(id NodeID) []netip.Prefix

// nodeAttrDisableIPv4 is the policy nodeAttr key that suppresses the
// node's own IPv4 CGNAT prefix in [tailcfg.Node.Addresses] and
// [tailcfg.Node.AllowedIPs]. Subnet routes the node advertises remain.
// See https://tailscale.com/docs/reference/troubleshooting/network-configuration/cgnat-conflicts.
const nodeAttrDisableIPv4 nodecap.Cap = "disable-ipv4"

// filterIPv4 returns ps with every IPv4 prefix dropped. Used by
// [NodeView.TailNode] when the node carries the disable-ipv4 nodeAttr.
func filterIPv4(ps []netip.Prefix) []netip.Prefix {
	out := ps[:0:0]
	for _, p := range ps {
		if p.Addr().Is4() {
			continue
		}

		out = append(out, p)
	}

	return out
}

// ViaRouteResult describes via grant effects for a viewer-peer pair.
// [ViaRouteResult.UsePrimary] is always a subset of [ViaRouteResult.Include]: it marks which included
// prefixes must additionally defer to HA primary election.
type ViaRouteResult struct {
	// Include contains prefixes this peer should serve to this viewer (via-designated).
	Include []netip.Prefix
	// Exclude contains prefixes steered to OTHER peers (suppress from global primary).
	Exclude []netip.Prefix
	// UsePrimary contains prefixes from [ViaRouteResult.Include] where a regular
	// (non-via) grant also covers the prefix. In these cases HA
	// primary election wins. Only the primary router should get
	// the route in [tailcfg.Node.AllowedIPs]. When a prefix is NOT in [ViaRouteResult.UsePrimary],
	// per-viewer via steering applies.
	UsePrimary []netip.Prefix
}

type (
	NodeID  uint64
	NodeIDs []NodeID
)

func ParseNodeID(s string) (NodeID, error) {
	id, err := strconv.ParseUint(s, util.Base10, 64)
	return NodeID(id), err
}

func MustParseNodeID(s string) NodeID {
	id, err := ParseNodeID(s)
	if err != nil {
		panic(err)
	}

	return id
}

func (id NodeID) StableID() tailcfg.StableNodeID {
	return tailcfg.StableNodeID(strconv.FormatUint(uint64(id), util.Base10))
}

func (id NodeID) NodeID() tailcfg.NodeID {
	//nolint:gosec // NodeID is a database autoincrement value, int64 on SQLite/PostgreSQL, so it fits
	return tailcfg.NodeID(id)
}

func (id NodeID) Uint64() uint64 {
	return uint64(id)
}

func (id NodeID) String() string {
	return strconv.FormatUint(id.Uint64(), util.Base10)
}

// Node is a Headscale client.
type Node struct {
	ID NodeID

	MachineKey key.MachinePublic
	NodeKey    key.NodePublic
	DiscoKey   key.DiscoPublic

	Endpoints AddrPorts

	Hostinfo *tailcfg.Hostinfo

	IPv4 *netip.Addr
	IPv6 *netip.Addr

	// Hostname represents the name given by the Tailscale
	// client during registration
	Hostname string

	// Givenname represents either:
	// a DNS normalized version of [Node.Hostname]
	// a valid name set by the [User]
	//
	// GivenName is the name used in all DNS related
	// parts of headscale.
	GivenName string

	// UserID identifies the owning user for user-owned nodes.
	// Nil for tagged nodes, which are owned by their tags.
	UserID *uint
	User   *User

	RegisterMethod string

	// Tags is the definitive owner for tagged nodes.
	// When non-empty, the node is "tagged" and tags define its identity.
	// Empty for user-owned nodes.
	// Tags cannot be removed once set (one-way transition).
	Tags Strings

	// When a node has been created with a [PreAuthKey], we need to
	// prevent the preauthkey from being deleted before the node.
	// The preauthkey can define "tags" of the node so we need it
	// around.
	AuthKeyID *uint64 `sql:"DEFAULT:NULL"`
	AuthKey   *PreAuthKey

	Expiry *time.Time

	// LastSeen is when the node was last in contact with
	// headscale. It is best effort and not persisted.
	LastSeen *time.Time

	// ApprovedRoutes is a list of routes that the node is allowed to announce
	// as a subnet router. They are not necessarily the routes that the node
	// announces at the moment.
	// See [Node.Hostinfo]
	ApprovedRoutes Prefixes

	// ApprovedAt is when an administrator (or the registration itself,
	// when device approval is off or the pre-auth key is preauthorized)
	// admitted the node to the tailnet. Nil while the node waits for
	// approval: it then gets no peers and no peer sees it.
	ApprovedAt *time.Time

	// SuspendedAt is set while an administrator has suspended the node.
	// A suspended node stays registered and keeps its addresses, but it
	// gets no peers, no peer sees it and its client is told it is not
	// authorized. Only [State.SetNodeSuspension] writes it; the map
	// request path leaves it alone like [Node.ApprovedAt].
	SuspendedAt *time.Time

	// Posture is the device identity the client last reported over c2n,
	// nil until the server asked. Only [State.CollectPosture] writes it.
	Posture *PostureIdentity

	// Attributes are the custom posture attributes set through the API,
	// in key order, expired ones included until the sweeper drops them.
	// Only the attribute operations on State write them.
	Attributes []NodeAttribute

	// SourceAddr is the address the node's control connection last came
	// from, as the trusted-proxy middleware resolved it. It is runtime
	// state, never stored, and feeds the ip: posture attributes.
	SourceAddr netip.Addr

	// SharedWith lists the users the node has been shared with, in
	// ascending id order. The policy resolves autogroup:shared from it
	// and the map response marks the node as shared to those users'
	// nodes. Only [State.ShareNode] and [State.UnshareNode] write it.
	SharedWith []UserID

	// GlobalExitNode marks an exit node every client is told to prefer:
	// the node carries suggest-exit-node and every node auto-exit-node
	// while at least one global exit node exists. Its exit routes are
	// approved when it is set. Only [State.SetGlobalExitNode] writes it.
	GlobalExitNode bool

	// Ephemeral is set when the client asked to be ephemeral in its
	// register request (a tailscaled with mem: state, a tsnet Server
	// with Ephemeral) rather than through an ephemeral pre-auth key.
	// [Node.IsEphemeral] reads both; only registration writes it.
	Ephemeral bool

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	IsOnline *bool

	// Unhealthy excludes the node from primary route election while
	// online. Written by the HA prober. Runtime-only.
	Unhealthy bool

	// ActiveSessions counts live poll sessions for this node.
	// [State.Connect] increments it and every session release
	// ([State.Disconnect]) decrements it, so the node goes offline
	// exactly when its last session ends, regardless of the order in
	// which overlapping sessions' cleanups run. Never persisted, like
	// SessionEpoch.
	ActiveSessions int

	// SessionEpoch identifies a poll session generation; Connect bumps
	// it. It complements ActiveSessions rather than duplicating it:
	// the epoch is monotonic, which the HA prober needs to detect that
	// a probe target reconnected mid-cycle. A refcount can return to
	// its old value, a generation cannot. poll.go also uses the epoch
	// returned by Connect as a "Connect ran" sentinel for its cleanup,
	// and Disconnect logs it. Runtime-only.
	SessionEpoch uint64
}

type Nodes []*Node

func (nodes Nodes) ViewSlice() views.Slice[NodeView] {
	vs := make([]NodeView, len(nodes))
	for i, n := range nodes {
		vs[i] = n.View()
	}

	return views.SliceOf(vs)
}

// StringID returns the node's id as a decimal string, the form the HTTP APIs
// render it as.
func (node *Node) StringID() string {
	if node == nil {
		return ""
	}

	return node.ID.String()
}

// IsExpired returns whether the node registration has expired.
func (node *Node) IsExpired() bool {
	// If Expiry is not set, the client has not indicated that
	// it wants an expiry time, it is therefore considered
	// to mean "not expired"
	if node.Expiry == nil || node.Expiry.IsZero() {
		return false
	}

	return time.Since(*node.Expiry) > 0
}

// IsApproved reports whether the node has been admitted to the tailnet.
// See [Node.ApprovedAt].
func (node *Node) IsApproved() bool {
	return node.ApprovedAt != nil && !node.ApprovedAt.IsZero()
}

// IsSuspended reports whether an administrator has suspended the node.
// See [Node.SuspendedAt].
func (node *Node) IsSuspended() bool {
	return node.SuspendedAt != nil && !node.SuspendedAt.IsZero()
}

// IsAdmitted reports whether the node takes part in the tailnet: it is
// approved and not suspended. Everything that hands out peers or the
// filter checks this, not [Node.IsApproved] alone.
func (node *Node) IsAdmitted() bool {
	return node.IsApproved() && !node.IsSuspended()
}

// IsSharedWith reports whether the node has been shared with the user.
// The owner is never a sharee; see [Node.SharedWith].
func (node *Node) IsSharedWith(uid UserID) bool {
	return slices.Contains(node.SharedWith, uid)
}

// IsEphemeral returns if the node is registered as an Ephemeral node,
// through an ephemeral pre-auth key or by asking in its register request.
// https://tailscale.com/docs/features/ephemeral-nodes
func (node *Node) IsEphemeral() bool {
	return node.Ephemeral || (node.AuthKey != nil && node.AuthKey.Ephemeral)
}

// IPs returns the node's allocated Tailscale addresses. Order is
// deterministic: IPv4 (if allocated) first, IPv6 second. At most one
// of each family.
func (node *Node) IPs() []netip.Addr {
	return node.appendIPs(nil)
}

// HasIP reports if a node has a given IP address.
func (node *Node) HasIP(i netip.Addr) bool {
	return slices.Contains(node.IPs(), i)
}

// DERPRegion returns the DERP region the node reported as its home, or 0
// when it has not reported one.
func (node *Node) DERPRegion() tailcfg.DERPRegionID {
	if node.Hostinfo == nil || node.Hostinfo.NetInfo == nil {
		return 0
	}

	return node.Hostinfo.NetInfo.PreferredDERP
}

// IsTagged reports if a device is tagged and therefore should not be treated
// as a user-owned device.
// When a node has tags, the tags define its identity (not the user).
func (node *Node) IsTagged() bool {
	return len(node.Tags) > 0
}

// IsUserOwned returns true if node is owned by a user (not tagged).
// Tagged nodes may have a UserID for "created by" tracking, but the tag is the owner.
func (node *Node) IsUserOwned() bool {
	return !node.IsTagged()
}

// HasTag reports if a node has a given tag.
func (node *Node) HasTag(tag string) bool {
	return slices.Contains(node.Tags, tag)
}

// TypedUserID returns the [Node.UserID] as a typed [UserID] type.
// Returns 0 if [Node.UserID] is nil.
func (node *Node) TypedUserID() UserID {
	if node.UserID == nil {
		return 0
	}

	return UserID(*node.UserID)
}

func (node *Node) RequestTags() []string {
	if node.Hostinfo == nil {
		return []string{}
	}

	return node.Hostinfo.RequestTags
}

func (node *Node) Prefixes() []netip.Prefix {
	ips := node.IPs()
	if len(ips) == 0 {
		return nil
	}

	addrs := make([]netip.Prefix, 0, len(ips))

	for _, nodeAddress := range ips {
		ip := netip.PrefixFrom(nodeAddress, nodeAddress.BitLen())
		addrs = append(addrs, ip)
	}

	return addrs
}

// ExitRoutes returns the node's approved exit routes (0.0.0.0/0
// and/or ::/0). Consumed unconditionally by RoutesForPeer when the
// viewer uses an exit node; excluded from [Node.CanAccessRoute] which only
// handles non-exit routing.
func (node *Node) ExitRoutes() []netip.Prefix {
	var routes []netip.Prefix

	for _, route := range node.AnnouncedRoutes() {
		if tsaddr.IsExitRoute(route) && slices.Contains(node.ApprovedRoutes, route) {
			routes = append(routes, route)
		}
	}

	return routes
}

// IsExitNode reports whether the node has any approved exit routes.
// Approval is required: an advertised-but-unapproved exit route does
// not make the node an exit node.
func (node *Node) IsExitNode() bool {
	return len(node.ExitRoutes()) > 0
}

func (node *Node) IPsAsString() []string {
	ips := node.IPs()
	if len(ips) == 0 {
		return nil
	}

	ret := make([]string, 0, len(ips))

	for _, ip := range ips {
		ret = append(ret, ip.String())
	}

	return ret
}

func (node *Node) InIPSet(set *netipx.IPSet) bool {
	return slices.ContainsFunc(node.IPs(), set.Contains)
}

// AppendToIPSet adds all IP addresses of the node to the given
// [netipx.IPSetBuilder]. For identity-based aliases (tags, users,
// groups, autogroups), both IPv4 and IPv6 must be included to
// match Tailscale's behavior in the [tailcfg.FilterRule] wire format.
func (node *Node) AppendToIPSet(build *netipx.IPSetBuilder) {
	if node.IPv4 != nil {
		build.Add(*node.IPv4)
	}

	if node.IPv6 != nil {
		build.Add(*node.IPv6)
	}
}

// CanAccess reports whether node may reach node2 under the given
// matchers. A node owns two source identities for ACL purposes:
//   - its own IPs (regular peer membership)
//   - any approved subnet routes it advertises (subnet-router-as-src,
//     used for subnet-to-subnet ACLs)
//
// Either identity matching a rule's src, combined with the dst
// matching node2's IPs, node2's approved subnet routes, or "the
// internet" when node2 is an exit node, grants access.
func (node *Node) CanAccess(matchers []matcher.Match, node2 *Node) bool {
	return node.canAccess(matchers, node2, node.SubnetRoutes(), node2.SubnetRoutes(), node2.IsExitNode())
}

// CanAccessRoute determines whether a specific route prefix should be
// visible to this node based on the given matchers.
//
// Unlike [Node.CanAccess], this function intentionally does NOT check
// [matcher.Match.DestsIsTheInternet]. Exit routes (0.0.0.0/0, ::/0) are handled by
// RoutesForPeer (state.go) which adds them unconditionally from
// [Node.ExitRoutes], not through ACL-based route filtering. The
// [matcher.Match.DestsIsTheInternet] check in [Node.CanAccess] exists solely for peer
// visibility determination (should two nodes see each other), which
// is a separate concern from route prefix authorization.
//
// Additionally, autogroup:internet is explicitly skipped during filter
// rule compilation (filter.go), so no matchers ever contain "the
// internet" from internet-targeted ACLs. Wildcard "*" dests produce
// matchers where [matcher.Match.DestsOverlapsPrefixes](0.0.0.0/0) already returns
// true, so the check would be redundant for that case.
func (node *Node) CanAccessRoute(matchers []matcher.Match, route netip.Prefix) bool {
	src := node.IPs()
	subnetRoutes := node.SubnetRoutes()

	for _, matcher := range matchers {
		if matcher.SrcsContainsIPs(src...) && matcher.DestsOverlapsPrefixes(route) {
			return true
		}

		if matcher.SrcsOverlapsPrefixes(route) && matcher.DestsContainsIP(src...) {
			return true
		}

		// A subnet router acts on behalf of its advertised subnets.
		// If the node's approved subnet routes overlap the source set
		// and the route overlaps the destination set, the router needs
		// this route to forward traffic from its local subnet.
		if len(subnetRoutes) > 0 {
			if matcher.SrcsOverlapsPrefixes(subnetRoutes...) &&
				matcher.DestsOverlapsPrefixes(route) {
				return true
			}

			// Reverse: traffic from the route's subnet is destined for
			// this node's subnets; the router needs the route for return
			// traffic.
			if matcher.SrcsOverlapsPrefixes(route) &&
				matcher.DestsOverlapsPrefixes(subnetRoutes...) {
				return true
			}
		}
	}

	return false
}

func (nodes Nodes) FilterByIP(ip netip.Addr) Nodes {
	var found Nodes

	for _, node := range nodes {
		if node.IPv4 != nil && ip == *node.IPv4 {
			found = append(found, node)
			continue
		}

		if node.IPv6 != nil && ip == *node.IPv6 {
			found = append(found, node)
		}
	}

	return found
}

func (nodes Nodes) ContainsNodeKey(nodeKey key.NodePublic) bool {
	for _, node := range nodes {
		if node.NodeKey == nodeKey {
			return true
		}
	}

	return false
}

func (node *Node) GetFQDN(baseDomain string) (string, error) {
	if node.GivenName == "" {
		return "", fmt.Errorf("creating valid FQDN: %w", ErrNodeHasNoGivenName)
	}

	hostname := node.GivenName

	if baseDomain != "" {
		hostname = node.GivenName + "." + baseDomain + "."
	}

	if len(hostname) > MaxHostnameLength {
		return "", fmt.Errorf(
			"creating valid FQDN (%s): %w",
			hostname,
			ErrHostnameTooLong,
		)
	}

	return hostname, nil
}

// ValidateGivenName reports whether givenName is usable as a node's DNS label:
// a valid DNS label that, combined with baseDomain, yields an FQDN within
// MaxHostnameLength. Admin-facing write paths (e.g. node rename) reject names
// that fail this, since the mapper cannot build a map for a node, or any of
// its peers, whose GetFQDN fails. Derived paths sanitise/coerce instead.
func ValidateGivenName(givenName, baseDomain string) error {
	err := dnsname.ValidLabel(givenName)
	if err != nil {
		return fmt.Errorf("%q is not a valid DNS label: %w", givenName, err)
	}

	// Reuse GetFQDN so the length bound stays identical to what the mapper
	// enforces; a valid 63-char label can still overflow under a long
	// base_domain.
	_, err = (&Node{GivenName: givenName}).GetFQDN(baseDomain)
	if err != nil {
		return err
	}

	return nil
}

// AnnouncedRoutes returns the list of routes the node announces, as
// reported by the client in [tailcfg.Hostinfo.RoutableIPs]. Announcement alone
// does not grant visibility. See [Node.SubnetRoutes] for approval-gated
// access.
func (node *Node) AnnouncedRoutes() []netip.Prefix {
	if node.Hostinfo == nil {
		return nil
	}

	return node.Hostinfo.RoutableIPs
}

// SubnetRoutes returns the list of routes (excluding exit routes) that the node
// announces and are approved. Also used by [Node.CanAccess] and [Node.CanAccessRoute] as part
// of the subnet-router-as-source identity.
//
// IMPORTANT: This method reflects announced-and-approved routes. API
// responses that need the routes actively served by the node must
// populate SubnetRoutes from the primary-route election instead.
func (node *Node) SubnetRoutes() []netip.Prefix {
	var routes []netip.Prefix

	for _, route := range node.AnnouncedRoutes() {
		if tsaddr.IsExitRoute(route) {
			continue
		}

		if slices.Contains(node.ApprovedRoutes, route) {
			routes = append(routes, route)
		}
	}

	return routes
}

// IsSubnetRouter reports if the node has any subnet routes.
func (node *Node) IsSubnetRouter() bool {
	return len(node.SubnetRoutes()) > 0
}

// AllApprovedRoutes returns the combination of [Node.SubnetRoutes] and [Node.ExitRoutes].
func (node *Node) AllApprovedRoutes() []netip.Prefix {
	return append(node.SubnetRoutes(), node.ExitRoutes()...)
}

func (node *Node) String() string {
	return node.Hostname
}

// MarshalZerologObject implements [zerolog.LogObjectMarshaler] for safe logging.
// This method is used with [zerolog.Event.EmbedObject] for flat field embedding
// or [zerolog.Event.Object] for nested logging when multiple nodes are logged.
func (node *Node) MarshalZerologObject(e *zerolog.Event) {
	if node == nil {
		return
	}

	e.Uint64(zf.NodeID, node.ID.Uint64())
	e.Str(zf.NodeName, node.Hostname)
	e.Str(zf.MachineKey, node.MachineKey.ShortString())
	e.Str(zf.NodeKey, node.NodeKey.ShortString())
	e.Bool(zf.NodeIsTagged, node.IsTagged())
	e.Bool(zf.NodeExpired, node.IsExpired())

	if node.IsOnline != nil {
		e.Bool(zf.NodeOnline, *node.IsOnline)
	}

	if len(node.Tags) > 0 {
		e.Strs(zf.NodeTags, node.Tags)
	}

	if node.User != nil {
		e.Str(zf.UserName, node.User.Username())
	} else if node.UserID != nil {
		e.Uint(zf.UserID, *node.UserID)
	}
}

// PeerChangeFromMapRequest takes a [tailcfg.MapRequest] and compares it to the node
// to produce a [tailcfg.PeerChange] struct that can be used to updated the node and
// inform peers about smaller changes to the node.
// When a field is added to this function, remember to also add it to:
// - [Node.ApplyPeerChange]
// - logTracePeerChange in poll.go.
func (node *Node) PeerChangeFromMapRequest(req tailcfg.MapRequest) tailcfg.PeerChange {
	ret := tailcfg.PeerChange{
		//nolint:gosec // NodeID is a database autoincrement value, int64 on SQLite/PostgreSQL, so it fits
		NodeID: tailcfg.NodeID(node.ID),
	}

	if node.NodeKey.String() != req.NodeKey.String() {
		ret.Key = &req.NodeKey
	}

	if node.DiscoKey.String() != req.DiscoKey.String() {
		ret.DiscoKey = &req.DiscoKey
	}

	if req.Hostinfo != nil && req.Hostinfo.NetInfo != nil {
		// Use the new PreferredDERP when there is no stored Hostinfo or
		// NetInfo, or when the stored PreferredDERP has changed.
		if node.Hostinfo == nil || node.Hostinfo.NetInfo == nil ||
			node.Hostinfo.NetInfo.PreferredDERP != req.Hostinfo.NetInfo.PreferredDERP {
			ret.DERPRegion = req.Hostinfo.NetInfo.PreferredDERP
		}
	}

	// Compare endpoints using order-independent comparison
	if EndpointsChanged(node.Endpoints, req.Endpoints) {
		ret.Endpoints = req.Endpoints
	}

	now := time.Now()
	ret.LastSeen = &now

	return ret
}

// EndpointsChanged compares two endpoint slices and returns true if they differ.
// The comparison is order-independent - endpoints are sorted before comparison.
func EndpointsChanged(oldEndpoints, newEndpoints []netip.AddrPort) bool {
	return !equalUnordered(oldEndpoints, newEndpoints, netip.AddrPort.Compare)
}

// ApplyPeerChange takes a [tailcfg.PeerChange] struct and updates the node.
func (node *Node) ApplyPeerChange(change *tailcfg.PeerChange) {
	if change.Key != nil {
		node.NodeKey = *change.Key
	}

	if change.DiscoKey != nil {
		node.DiscoKey = *change.DiscoKey
	}

	if change.Online != nil {
		node.IsOnline = change.Online
	}

	if change.Endpoints != nil {
		node.Endpoints = change.Endpoints
	}

	// This might technically not be useful as we replace
	// the whole hostinfo blob when it has changed.
	if change.DERPRegion != 0 {
		// [NodeStore] publishes snapshots that share the *Hostinfo /
		// *NetInfo pointers, so writing PreferredDERP in place would race
		// readers of an already-published snapshot. Clone to fresh pointers
		// and assign, leaving the previous snapshot untouched.
		hi := node.Hostinfo.Clone()
		if hi == nil {
			hi = &tailcfg.Hostinfo{}
		}

		if hi.NetInfo == nil {
			hi.NetInfo = &tailcfg.NetInfo{}
		}

		hi.NetInfo.PreferredDERP = change.DERPRegion
		node.Hostinfo = hi
	}

	node.LastSeen = change.LastSeen
}

func (nodes Nodes) String() string {
	temp := make([]string, len(nodes))

	for index, node := range nodes {
		temp[index] = node.Hostname
	}

	return fmt.Sprintf("[ %s ](%d)", strings.Join(temp, ", "), len(temp))
}

func (nodes Nodes) IDMap() map[NodeID]*Node {
	ret := map[NodeID]*Node{}

	for _, node := range nodes {
		ret[node.ID] = node
	}

	return ret
}

func (nodes Nodes) DebugString() string {
	var sb strings.Builder
	sb.WriteString("Nodes:\n")

	for _, node := range nodes {
		sb.WriteString(node.DebugString())
		sb.WriteString("\n")
	}

	return sb.String()
}

func (node *Node) DebugString() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s(%s):\n", node.Hostname, node.ID)

	// Show ownership status
	switch {
	case node.IsTagged():
		fmt.Fprintf(&sb, "\tTagged: %v\n", node.Tags)

		if node.User != nil {
			fmt.Fprintf(&sb, "\tCreated by: %s (%d, %q)\n", node.User.Display(), node.User.ID, node.User.Username())
		}
	case node.User != nil:
		fmt.Fprintf(&sb, "\tUser-owned: %s (%d, %q)\n", node.User.Display(), node.User.ID, node.User.Username())
	default:
		fmt.Fprintf(&sb, "\tOrphaned: no user or tags\n")
	}

	fmt.Fprintf(&sb, "\tIPs: %v\n", node.IPs())
	fmt.Fprintf(&sb, "\tApprovedRoutes: %v\n", node.ApprovedRoutes)
	fmt.Fprintf(&sb, "\tAnnouncedRoutes: %v\n", node.AnnouncedRoutes())
	fmt.Fprintf(&sb, "\tSubnetRoutes: %v\n", node.SubnetRoutes())
	fmt.Fprintf(&sb, "\tExitRoutes: %v\n", node.ExitRoutes())
	sb.WriteString("\n")

	return sb.String()
}

// appendIPs appends the node's addresses to dst. Callers on the peer
// visibility path pass a stack array to keep the check allocation free.
func (node *Node) appendIPs(dst []netip.Addr) []netip.Addr {
	if node.IPv4 != nil {
		dst = append(dst, *node.IPv4)
	}

	if node.IPv6 != nil {
		dst = append(dst, *node.IPv6)
	}

	return dst
}

// canAccess is [Node.CanAccess] with the snapshot-stable route data supplied by
// the caller. The peer-map build precomputes each node's SubnetRoutes and
// exit-node status once and passes them here, so the O(n^2) pair scan does not
// recompute them for every pair.
func (node *Node) canAccess(
	matchers []matcher.Match,
	node2 *Node,
	srcRoutes, dstRoutes []netip.Prefix,
	dstIsExit bool,
) bool {
	var srcBuf, dstBuf [2]netip.Addr

	src := node.appendIPs(srcBuf[:0])
	allowedIPs := node2.appendIPs(dstBuf[:0])

	for _, m := range matchers {
		srcMatchesIP := m.SrcsContainsIPs(src...)
		srcMatchesRoutes := len(srcRoutes) > 0 && m.SrcsOverlapsPrefixes(srcRoutes...)

		if !srcMatchesIP && !srcMatchesRoutes {
			continue
		}

		if m.DestsContainsIP(allowedIPs...) {
			return true
		}

		if len(dstRoutes) > 0 && m.DestsOverlapsPrefixes(dstRoutes...) {
			return true
		}

		if dstIsExit && m.DestsIsTheInternet() {
			return true
		}
	}

	return false
}

// MarshalZerologObject implements [zerolog.LogObjectMarshaler] for [NodeView].
// This delegates to the underlying [Node]'s implementation.
func (nv NodeView) MarshalZerologObject(e *zerolog.Event) {
	if !nv.Valid() {
		return
	}

	nv.ж.MarshalZerologObject(e)
}

// Owner returns the owner for display purposes.
// For tagged nodes, returns [TaggedDevices]. For user-owned nodes, returns the user.
// Returns an invalid [UserView] if the node is in an orphaned state (no tags, no user).
// Callers should check .Valid() on the result before accessing fields.
func (nv NodeView) Owner() UserView {
	if nv.IsTagged() {
		return TaggedDevices.View()
	}

	if user := nv.User(); user.Valid() {
		return user
	}

	return UserView{}
}

func (nv NodeView) IPs() []netip.Addr {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.IPs()
}

func (nv NodeView) InIPSet(set *netipx.IPSet) bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.InIPSet(set)
}

func (nv NodeView) CanAccess(matchers []matcher.Match, node2 NodeView) bool {
	if !nv.Valid() || !node2.Valid() {
		return false
	}

	return nv.ж.CanAccess(matchers, node2.ж)
}

// CanAccessWithRoutes is [NodeView.CanAccess] with precomputed route data, used
// by the peer-map build to avoid recomputing each node's routes per pair.
func (nv NodeView) CanAccessWithRoutes(
	matchers []matcher.Match,
	node2 NodeView,
	srcRoutes, dstRoutes []netip.Prefix,
	dstIsExit bool,
) bool {
	if !nv.Valid() || !node2.Valid() {
		return false
	}

	return nv.ж.canAccess(matchers, node2.ж, srcRoutes, dstRoutes, dstIsExit)
}

func (nv NodeView) CanAccessRoute(matchers []matcher.Match, route netip.Prefix) bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.CanAccessRoute(matchers, route)
}

func (nv NodeView) AnnouncedRoutes() []netip.Prefix {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.AnnouncedRoutes()
}

func (nv NodeView) SubnetRoutes() []netip.Prefix {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.SubnetRoutes()
}

func (nv NodeView) IsSubnetRouter() bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsSubnetRouter()
}

func (nv NodeView) AllApprovedRoutes() []netip.Prefix {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.AllApprovedRoutes()
}

func (nv NodeView) AppendToIPSet(build *netipx.IPSetBuilder) {
	if !nv.Valid() {
		return
	}

	nv.ж.AppendToIPSet(build)
}

func (nv NodeView) RequestTagsSlice() views.Slice[string] {
	if !nv.Valid() || !nv.Hostinfo().Valid() {
		return views.Slice[string]{}
	}

	return nv.Hostinfo().RequestTags()
}

// StringID returns the node's id as a decimal string, the form the HTTP APIs
// render it as.
func (nv NodeView) StringID() string {
	if !nv.Valid() {
		return ""
	}

	return nv.ID().String()
}

// IsTagged reports if a device is tagged
// and therefore should not be treated as a
// user owned device.
// Currently, this function only handles tags set
// via CLI ("forced tags" and preauthkeys).
func (nv NodeView) IsTagged() bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsTagged()
}

// IsExpired returns whether the node registration has expired.
func (nv NodeView) IsExpired() bool {
	if !nv.Valid() {
		return true
	}

	return nv.ж.IsExpired()
}

// IsApproved reports whether the node has been admitted to the tailnet.
func (nv NodeView) IsApproved() bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsApproved()
}

// IsSuspended reports whether an administrator has suspended the node.
func (nv NodeView) IsSuspended() bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsSuspended()
}

// IsAdmitted reports whether the node is approved and not suspended.
func (nv NodeView) IsAdmitted() bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsAdmitted()
}

// IsSharedWith reports whether the node has been shared with the user.
func (nv NodeView) IsSharedWith(uid UserID) bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsSharedWith(uid)
}

// IsGlobalExitNode reports whether the node is marked as a global exit
// node; see [Node.GlobalExitNode].
func (nv NodeView) IsGlobalExitNode() bool {
	return nv.Valid() && nv.ж.GlobalExitNode
}

// IsEphemeral returns if the node is registered as an Ephemeral node.
// https://tailscale.com/docs/features/ephemeral-nodes
func (nv NodeView) IsEphemeral() bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsEphemeral()
}

// PeerChangeFromMapRequest takes a [tailcfg.MapRequest] and compares it to the node
// to produce a [tailcfg.PeerChange] struct that can be used to updated the node and
// inform peers about smaller changes to the node.
func (nv NodeView) PeerChangeFromMapRequest(req tailcfg.MapRequest) tailcfg.PeerChange {
	if !nv.Valid() {
		return tailcfg.PeerChange{}
	}

	return nv.ж.PeerChangeFromMapRequest(req)
}

// GetFQDN returns the fully qualified domain name for the node.
func (nv NodeView) GetFQDN(baseDomain string) (string, error) {
	if !nv.Valid() {
		return "", fmt.Errorf("creating valid FQDN: %w", ErrInvalidNodeView)
	}

	return nv.ж.GetFQDN(baseDomain)
}

// ExitRoutes returns a list of both exit routes if the
// node has any exit routes enabled.
// If none are enabled, it will return nil.
func (nv NodeView) ExitRoutes() []netip.Prefix {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.ExitRoutes()
}

func (nv NodeView) IsExitNode() bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.IsExitNode()
}

// RequestTags returns the ACL tags that the node is requesting.
func (nv NodeView) RequestTags() []string {
	if !nv.Valid() || !nv.Hostinfo().Valid() {
		return []string{}
	}

	return nv.Hostinfo().RequestTags().AsSlice()
}

// HasIP reports if a node has a given IP address.
func (nv NodeView) HasIP(i netip.Addr) bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.HasIP(i)
}

// HasTag reports if a node has a given tag.
func (nv NodeView) HasTag(tag string) bool {
	if !nv.Valid() {
		return false
	}

	return nv.ж.HasTag(tag)
}

// TypedUserID returns the [Node.UserID] as a typed [UserID] type.
// Returns 0 if [Node.UserID] is nil or node is invalid.
func (nv NodeView) TypedUserID() UserID {
	if !nv.Valid() {
		return 0
	}

	return nv.ж.TypedUserID()
}

// TailscaleUserID returns the user ID to use in Tailscale protocol.
// Tagged nodes always return [TaggedDevices].ID, user-owned nodes return their actual [Node.UserID].
// Returns 0 for nodes in an orphaned state (no tags, no [Node.UserID]).
func (nv NodeView) TailscaleUserID() tailcfg.UserID {
	if !nv.Valid() {
		return 0
	}

	if nv.IsTagged() {
		//nolint:gosec // TaggedDevicesUserID is a fixed constant far below int64 max, so this always fits
		return tailcfg.UserID(int64(TaggedDevices.ID))
	}

	if !nv.UserID().Valid() {
		return 0
	}

	//nolint:gosec // UserID is a database autoincrement value, int64 on SQLite/PostgreSQL, so it fits
	return tailcfg.UserID(int64(nv.UserID().Get()))
}

// Prefixes returns the node IPs as [netip.Prefix].
func (nv NodeView) Prefixes() []netip.Prefix {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.Prefixes()
}

// IPsAsString returns the node IPs as strings.
func (nv NodeView) IPsAsString() []string {
	if !nv.Valid() {
		return nil
	}

	return nv.ж.IPsAsString()
}

// HasNetworkChanges checks if the node has network-related changes.
// Returns true if IPs, announced routes, or approved routes changed.
// This is primarily used for policy cache invalidation. Route slices
// are compared order-insensitively since clients may re-advertise the
// same routes in a different order.
func (nv NodeView) HasNetworkChanges(other NodeView) bool {
	if !slices.Equal(nv.IPs(), other.IPs()) {
		return true
	}

	if !equalPrefixesUnordered(nv.AnnouncedRoutes(), other.AnnouncedRoutes()) {
		return true
	}

	if !equalPrefixesUnordered(nv.SubnetRoutes(), other.SubnetRoutes()) {
		return true
	}

	if !equalPrefixesUnordered(nv.ExitRoutes(), other.ExitRoutes()) {
		return true
	}

	return false
}

// equalPrefixesUnordered reports whether a and b contain the same
// prefixes, order-independent. Inputs are cloned before sorting so
// callers' slices are not mutated.
func equalPrefixesUnordered(a, b []netip.Prefix) bool {
	return equalUnordered(a, b, netip.Prefix.Compare)
}

// equalUnordered reports whether a and b contain the same elements,
// order-independent. Inputs are cloned before sorting so callers'
// slices are not mutated.
func equalUnordered[E comparable](a, b []E, cmp func(E, E) int) bool {
	if len(a) != len(b) {
		return false
	}

	ac := slices.Clone(a)
	bc := slices.Clone(b)

	slices.SortFunc(ac, cmp)
	slices.SortFunc(bc, cmp)

	return slices.Equal(ac, bc)
}

// HasPolicyChange reports whether the node has changes that affect
// policy evaluation. Includes approved subnet routes because they act
// as source identity in [Node.CanAccess] for subnet-to-subnet ACLs.
func (nv NodeView) HasPolicyChange(other NodeView) bool {
	if nv.UserID() != other.UserID() {
		return true
	}

	if !views.SliceEqual(nv.Tags(), other.Tags()) {
		return true
	}

	if !slices.Equal(nv.IPs(), other.IPs()) {
		return true
	}

	if !equalPrefixesUnordered(nv.SubnetRoutes(), other.SubnetRoutes()) {
		return true
	}

	if !views.SliceEqual(nv.SharedWith(), other.SharedWith()) {
		return true
	}

	if nv.GlobalExitNode() != other.GlobalExitNode() {
		return true
	}

	// The policy's postures read the attribute map, so what feeds it
	// counts: the reported OS and versions, the serials and the custom
	// attributes.
	return !maps.EqualFunc(nv.PostureAttributes(time.Time{}), other.PostureAttributes(time.Time{}), attributeEqual)
}

// attributeEqual compares two attribute values, which are scalars or
// serial number lists.
func attributeEqual(a, b any) bool {
	as, aok := a.([]string)
	bs, bok := b.([]string)

	if aok || bok {
		return aok && bok && slices.Equal(as, bs)
	}

	return a == b
}

// TailNodes converts a slice of [NodeView] values into Tailscale [tailcfg.Node] values.
func TailNodes(
	nodes views.Slice[NodeView],
	capVer tailcfg.CapabilityVersion,
	primaryRouteFunc RouteFunc,
	cfg *Config,
) ([]*tailcfg.Node, error) {
	tNodes := make([]*tailcfg.Node, 0, nodes.Len())

	for _, node := range nodes.All() {
		// nil selfPolicyCaps: this batch builds peer views; the caller
		// sets each peer's CapMap from [policyv2.PeerCapMap].
		tNode, err := node.TailNode(capVer, primaryRouteFunc, cfg, nil)
		if err != nil {
			return nil, err
		}

		tNodes = append(tNodes, tNode)
	}

	return tNodes, nil
}

// TailNode converts a [NodeView] into a Tailscale [tailcfg.Node].
//
// selfPolicyCaps is the per-node CapMap from [policy.PolicyManager.NodeCapMap]
// and is merged into the baseline. Pass it when building the self view of the
// requesting node; pass nil when building peer views (peer-side
// [tailcfg.Node.CapMap] is set by the caller from
// [policyv2.PeerCapMap]).
func (nv NodeView) TailNode(
	capVer tailcfg.CapabilityVersion,
	primaryRouteFunc RouteFunc,
	cfg *Config,
	selfPolicyCaps tailcfg.NodeCapMap,
) (*tailcfg.Node, error) {
	return nv.tailNode(capVer, primaryRouteFunc, cfg, selfPolicyCaps, true)
}

// PeerTailNode is [NodeView.TailNode] for a peer entry of another node's
// map: policyCaps still shapes the addresses, but the entry carries no
// CapMap, because the caller replaces it with [policyv2.PeerCapMap] and
// building the baseline map for every peer was pure waste.
func (nv NodeView) PeerTailNode(
	capVer tailcfg.CapabilityVersion,
	primaryRouteFunc RouteFunc,
	cfg *Config,
	policyCaps tailcfg.NodeCapMap,
) (*tailcfg.Node, error) {
	return nv.tailNode(capVer, primaryRouteFunc, cfg, policyCaps, false)
}

func (nv NodeView) tailNode(
	capVer tailcfg.CapabilityVersion,
	primaryRouteFunc RouteFunc,
	cfg *Config,
	selfPolicyCaps tailcfg.NodeCapMap,
	withCapMap bool,
) (*tailcfg.Node, error) {
	if !nv.Valid() {
		return nil, ErrInvalidNodeView
	}

	hostname, err := nv.GetFQDN(cfg.BaseDomain)
	if err != nil {
		return nil, err
	}

	var derp tailcfg.DERPRegionID
	if nv.Hostinfo().Valid() && nv.Hostinfo().NetInfo().Valid() {
		derp = nv.Hostinfo().NetInfo().PreferredDERP()
	}

	var keyExpiry time.Time
	if nv.Expiry().Valid() {
		keyExpiry = nv.Expiry().Get()
	}

	// disable-ipv4 (https://tailscale.com/docs/reference/troubleshooting/network-configuration/cgnat-conflicts)
	// drops the node's own IPv4 CGNAT prefix from Addresses and from
	// the AllowedIPs slot the node's own /32 occupies. Advertised
	// subnet routes -- even IPv4 ones -- survive: routes belong to
	// the routing layer, not the node's identity. Mirrors the SaaS
	// captures in testdata/nodeattrs_results/nodeattrs-attr-c1{5,6}-disable-ipv4*.
	_, ipv4Disabled := selfPolicyCaps[nodeAttrDisableIPv4]

	addresses := nv.Prefixes()
	if ipv4Disabled {
		addresses = filterIPv4(addresses)
	}

	// routeFunc returns ALL routes (subnet + exit) for this node.
	allRoutes := primaryRouteFunc(nv.ID())
	allowedIPs := slices.Concat(addresses, allRoutes)
	slices.SortFunc(allowedIPs, netip.Prefix.Compare)

	// PrimaryRoutes only includes non-exit subnet routes for HA tracking.
	var primaryRoutes []netip.Prefix

	for _, r := range allRoutes {
		if !tsaddr.IsExitRoute(r) {
			primaryRoutes = append(primaryRoutes, r)
		}
	}

	var capMap tailcfg.NodeCapMap
	if withCapMap {
		capMap = selfCapMap(cfg, selfPolicyCaps)
	}

	tNode := tailcfg.Node{
		//nolint:gosec // NodeID is a database autoincrement value, int64 on SQLite/PostgreSQL, so it fits
		ID:       tailcfg.NodeID(nv.ID()),
		StableID: nv.ID().StableID(),
		Name:     hostname,
		Cap:      capVer,
		CapMap:   capMap,

		User: nv.TailscaleUserID(),

		Key:       nv.NodeKey(),
		KeyExpiry: keyExpiry.UTC(),

		Machine:       nv.MachineKey(),
		DiscoKey:      nv.DiscoKey(),
		Addresses:     addresses,
		PrimaryRoutes: primaryRoutes,
		AllowedIPs:    allowedIPs,
		Endpoints:     nv.Endpoints().AsSlice(),
		HomeDERP:      derp,
		Hostinfo:      nv.Hostinfo(),
		Created:       nv.CreatedAt().UTC(),

		Online: nv.IsOnline().Clone(),

		Tags: nv.Tags().AsSlice(),

		MachineAuthorized: nv.IsAdmitted() && !nv.IsExpired(),
		Expired:           nv.IsExpired(),
	}

	// LastSeen is when the node was last online, nil only for a node that
	// never was (tailcfg.Node.LastSeen). Online is authoritative for the
	// client's idea of reachability; a client on the peer-delta path
	// (Tailscale 1.102 on Apple platforms) treats a peer without LastSeen
	// as never seen, so an online peer carries it too.
	if nv.LastSeen().Valid() {
		lastSeen := nv.LastSeen().Get()
		tNode.LastSeen = &lastSeen
	}

	return &tNode, nil
}

// selfCapMap is the CapMap of a node's own entry: the baseline caps every
// node receives, regardless of policy, overlaid with its policy caps, which
// carry the nodeAttrs and the role caps (is-admin, is-owner) the policy
// manager stamps from the owning user's role. Mirrors what Tailscale SaaS
// emits for a default tailnet.
func selfCapMap(cfg *Config, policyCaps tailcfg.NodeCapMap) tailcfg.NodeCapMap {
	capMap := tailcfg.NodeCapMap{
		nodecap.SSH: []tailcfg.RawMessage{},
	}

	// cfg.Taildrop.Enabled gates CapabilityFileSharing.
	if cfg.Taildrop.Enabled {
		capMap[nodecap.FileSharing] = []tailcfg.RawMessage{}
	}

	// default-auto-update is always emitted; the value is a JSON bool
	// reflecting cfg.AutoUpdate.Enabled. Clients read this on first
	// netmap and store the default locally; subsequent control-plane
	// changes are ignored unless the client has not yet opted in or
	// out.
	autoUpdateVal := tailcfg.RawMessage("false")
	if cfg.AutoUpdate.Enabled {
		autoUpdateVal = tailcfg.RawMessage("true")
	}

	capMap[nodecap.DefaultAutoUpdate] = []tailcfg.RawMessage{autoUpdateVal}

	maps.Copy(capMap, policyCaps)

	return capMap
}
