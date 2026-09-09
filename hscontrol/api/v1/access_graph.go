package apiv1

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/danielgtaylor/huma/v2"
	"go4.org/netipx"
	"tailscale.com/tailcfg"
)

func init() {
	registrations = append(registrations, registerAccessGraph)
}

// AccessGraph is who can reach what, read off the packet filter and the
// SSH policy the server hands each machine: one edge per ordered pair of
// machines that the policy opens, with the ports and the SSH logins. It
// is computed on demand from the same rules the clients enforce, so it
// says what the tailnet does, not what the policy file seems to say.
type AccessGraph struct {
	Nodes []AccessGraphNode `json:"nodes" nullable:"false"`
	Edges []AccessGraphEdge `json:"edges" nullable:"false"`
	// Reachable lists the edges where the focused node is the source.
	Reachable []AccessGraphEdge `json:"reachable" nullable:"false"`
	// ReachedBy lists the edges where the focused node is the destination.
	ReachedBy []AccessGraphEdge `json:"reachedBy" nullable:"false"`
	// Enforcing is false while the tailnet has no packet filter, in
	// which case every admitted machine reaches every other and the
	// edges carry "*".
	Enforcing bool `doc:"Whether a packet filter is in force; without one every machine reaches every other." json:"enforcing"` //nolint:lll // struct tag
}

// AccessGraphNode is a machine in the graph.
type AccessGraphNode struct {
	ID     string   `format:"uint64"                             json:"id"`
	Name   string   `json:"name"`
	User   string   `doc:"The owner's login, empty when tagged." json:"user"`
	Tags   []string `json:"tags"                                 nullable:"false"`
	Online bool     `json:"online"`
	// Routes are the approved subnet routes the machine serves, which
	// an edge may target instead of the machine itself.
	Routes []string `json:"routes" nullable:"false"`
}

// AccessGraphEdge is what one machine may do to another.
type AccessGraphEdge struct {
	Src string `format:"uint64" json:"src"`
	Dst string `format:"uint64" json:"dst"`
	// Ports are the open ports the way the policy writes them: 22,
	// tcp:22, udp:1-100; "*" is everything. Empty when only SSH or a
	// capability is open.
	Ports []string `json:"ports" nullable:"false"`
	// Routes are the destination's subnet routes the source may reach,
	// when the rule targets a route rather than the machine.
	Routes []string `json:"routes" nullable:"false"`
	// SSHUsers are the logins the source may SSH in as; "*" is any.
	// Empty when SSH is not open.
	SSHUsers []string `json:"sshUsers" nullable:"false"`
	// SSHCheck reports that the SSH rule asks for a fresh sign-in first.
	SSHCheck bool `json:"sshCheck"`
	// Capabilities are the application capabilities granted, such as
	// tailscale.com/cap/ingress.
	Capabilities []string `json:"capabilities" nullable:"false"`
}

type (
	accessGraphInput struct {
		// Node narrows the graph to the edges the node is a source or a
		// destination of.
		Node string `doc:"A node id; only edges from or to it." format:"uint64" query:"node"`
	}
	accessGraphOutput struct {
		Body AccessGraph
	}
)

// edgeKey identifies an edge under construction.
type edgeKey struct {
	src, dst types.NodeID
}

// graphBuilder accumulates edges while the rules are walked.
type graphBuilder struct {
	nodes    []types.NodeView
	byID     map[types.NodeID]types.NodeView
	edges    map[edgeKey]*AccessGraphEdge
	ipSets   map[string]*netipx.IPSet
	focus    types.NodeID
	enforces bool
}

func (g *graphBuilder) edge(src, dst types.NodeID) *AccessGraphEdge {
	key := edgeKey{src, dst}

	e, ok := g.edges[key]
	if !ok {
		e = &AccessGraphEdge{
			Src:          formatID(src.Uint64()),
			Dst:          formatID(dst.Uint64()),
			Ports:        []string{},
			Routes:       []string{},
			SSHUsers:     []string{},
			Capabilities: []string{},
		}
		g.edges[key] = e
	}

	return e
}

// ipSet parses a filter address expression once.
func (g *graphBuilder) ipSet(expr string) *netipx.IPSet {
	set, ok := g.ipSets[expr]
	if ok {
		return set
	}

	set, err := util.ParseIPSet(expr, nil)
	if err != nil {
		set = nil
	}

	g.ipSets[expr] = set

	return set
}

// sources returns the machines whose addresses fall in any of the
// expressions.
func (g *graphBuilder) sources(exprs []string) []types.NodeID {
	var out []types.NodeID

	for _, n := range g.nodes {
		for _, expr := range exprs {
			set := g.ipSet(expr)
			if set != nil && n.InIPSet(set) {
				out = append(out, n.ID())

				break
			}
		}
	}

	return out
}

// wanted reports whether an edge between the pair is asked for.
func (g *graphBuilder) wanted(src, dst types.NodeID) bool {
	if src == dst {
		return false
	}

	return g.focus == 0 || src == g.focus || dst == g.focus
}

// addFilter walks the destination's packet filter.
func (g *graphBuilder) addFilter(dst types.NodeView, rules []tailcfg.FilterRule) {
	dstRoutes := dst.SubnetRoutes()

	for _, rule := range rules {
		srcs := g.sources(rule.SrcIPs)
		if len(srcs) == 0 {
			continue
		}

		ports, routes := g.destinations(dst, dstRoutes, rule)
		caps := capabilityNames(dst, rule)

		if len(ports) == 0 && len(routes) == 0 && len(caps) == 0 {
			continue
		}

		for _, src := range srcs {
			if !g.wanted(src, dst.ID()) {
				continue
			}

			e := g.edge(src, dst.ID())
			e.Ports = mergeSorted(e.Ports, ports)
			e.Routes = mergeSorted(e.Routes, routes)
			e.Capabilities = mergeSorted(e.Capabilities, caps)
		}
	}
}

// destinations splits a rule's destinations into the ports open on the
// machine itself and the routes it serves that the rule reaches.
func (g *graphBuilder) destinations(
	dst types.NodeView, dstRoutes []netip.Prefix, rule tailcfg.FilterRule,
) ([]string, []string) {
	var ports, routes []string

	for _, d := range rule.DstPorts {
		set := g.ipSet(d.IP)
		if set == nil {
			continue
		}

		if dst.InIPSet(set) {
			ports = append(ports, portSpecs(rule.IPProto, d.Ports)...)

			continue
		}

		for _, r := range dstRoutes {
			if set.OverlapsPrefix(r) {
				routes = append(routes, r.String())
			}
		}
	}

	return ports, routes
}

// capabilityNames lists the capabilities a rule grants on the machine.
func capabilityNames(dst types.NodeView, rule tailcfg.FilterRule) []string {
	var out []string

	for _, cg := range rule.CapGrant {
		hit := false

		for _, p := range cg.Dsts {
			if slices.ContainsFunc(dst.IPs(), p.Contains) {
				hit = true

				break
			}
		}

		if !hit {
			continue
		}

		for _, c := range cg.Caps {
			out = append(out, string(c))
		}

		for c := range cg.CapMap {
			out = append(out, string(c))
		}
	}

	return out
}

// portSpecs renders a port range under each protocol of the rule.
func portSpecs(protos []int, r tailcfg.PortRange) []string {
	rng := "*"

	if r.First != 0 || r.Last != tailcfg.PortRangeAny.Last {
		if r.First == r.Last {
			rng = strconv.Itoa(int(r.First))
		} else {
			rng = fmt.Sprintf("%d-%d", r.First, r.Last)
		}
	}

	// A rule without a protocol is written "22" in the policy, and reads
	// the same way here.
	if len(protos) == 0 {
		return []string{rng}
	}

	out := make([]string, 0, len(protos))

	for _, p := range protos {
		name := protoName(p)
		if name == "icmp" || name == "ipv6-icmp" {
			out = append(out, name)

			continue
		}

		out = append(out, name+":"+rng)
	}

	return out
}

// The IP protocol numbers the policy names.
const (
	protoICMP   = 1
	protoTCP    = 6
	protoUDP    = 17
	protoICMPv6 = 58
	protoSCTP   = 132
)

// protoName names the common IP protocols the way the policy does.
func protoName(p int) string {
	switch p {
	case protoICMP:
		return "icmp"
	case protoTCP:
		return "tcp"
	case protoUDP:
		return "udp"
	case protoICMPv6:
		return "ipv6-icmp"
	case protoSCTP:
		return "sctp"
	default:
		return "proto-" + strconv.Itoa(p)
	}
}

// addSSH walks the destination's SSH policy.
func (g *graphBuilder) addSSH(dst types.NodeView, pol *tailcfg.SSHPolicy) {
	if pol == nil {
		return
	}

	for _, rule := range pol.Rules {
		if rule == nil || rule.Action == nil || rule.Action.Reject {
			continue
		}

		srcs := g.sshSources(rule.Principals)
		users := sshUserNames(rule.SSHUsers)

		for _, src := range srcs {
			if !g.wanted(src, dst.ID()) {
				continue
			}

			e := g.edge(src, dst.ID())
			e.SSHUsers = mergeSorted(e.SSHUsers, users)

			if rule.Action.HoldAndDelegate != "" {
				e.SSHCheck = true
			}
		}
	}
}

// sshSources maps the rule's principals to machines: an address, or any.
func (g *graphBuilder) sshSources(principals []*tailcfg.SSHPrincipal) []types.NodeID {
	var out []types.NodeID

	for _, p := range principals {
		if p == nil {
			continue
		}

		if p.Any {
			for _, n := range g.nodes {
				out = append(out, n.ID())
			}

			continue
		}

		if p.NodeIP != "" {
			addr, err := netip.ParseAddr(p.NodeIP)
			if err != nil {
				continue
			}

			for _, n := range g.nodes {
				if slices.Contains(n.IPs(), addr) {
					out = append(out, n.ID())
				}
			}
		}
	}

	slices.Sort(out)

	return slices.Compact(out)
}

// sshUserNames renders the rule's login map: "*" when any login is
// allowed, else the logins that map to something.
func sshUserNames(users map[string]string) []string {
	if len(users) == 0 {
		return nil
	}

	if _, ok := users["*"]; ok {
		return []string{"*"}
	}

	var out []string

	for login, mapped := range users {
		if mapped == "" {
			continue
		}

		out = append(out, login)
	}

	return out
}

func mergeSorted(a, b []string) []string {
	if len(b) == 0 {
		return a
	}

	out := append(slices.Clone(a), b...)
	slices.Sort(out)

	return slices.Compact(out)
}

// accessGraph builds the graph for the admitted machines.
func (b Backend) accessGraph(focus types.NodeID) (AccessGraph, error) {
	var admitted []types.NodeView

	for _, n := range b.State.ListNodes().All() {
		if n.IsAdmitted() {
			admitted = append(admitted, n)
		}
	}

	g := &graphBuilder{
		nodes:    admitted,
		byID:     make(map[types.NodeID]types.NodeView, len(admitted)),
		edges:    map[edgeKey]*AccessGraphEdge{},
		ipSets:   map[string]*netipx.IPSet{},
		focus:    focus,
		enforces: b.State.Enforces(),
	}

	for _, n := range admitted {
		g.byID[n.ID()] = n
	}

	for _, dst := range admitted {
		rules, err := b.State.FilterForNode(dst)
		if err != nil {
			return AccessGraph{}, fmt.Errorf("filter for %s: %w", dst.GivenName(), err)
		}

		g.addFilter(dst, rules)

		pol, err := b.State.SSHPolicy(dst)
		if err != nil {
			return AccessGraph{}, fmt.Errorf("ssh policy for %s: %w", dst.GivenName(), err)
		}

		g.addSSH(dst, pol)
	}

	return g.result(), nil
}

func (g *graphBuilder) result() AccessGraph {
	out := AccessGraph{
		Nodes:     make([]AccessGraphNode, 0, len(g.nodes)),
		Edges:     make([]AccessGraphEdge, 0, len(g.edges)),
		Reachable: make([]AccessGraphEdge, 0),
		ReachedBy: make([]AccessGraphEdge, 0),
		Enforcing: g.enforces,
	}

	for _, n := range g.nodes {
		gn := AccessGraphNode{
			ID:     n.StringID(),
			Name:   n.GivenName(),
			Tags:   nonNilStrings(n.Tags().AsSlice()),
			Online: n.IsOnline().Valid() && n.IsOnline().Get(),
			Routes: nonNilStrings(util.PrefixesToString(n.SubnetRoutes())),
		}

		if !n.IsTagged() && n.User().Valid() {
			gn.User = n.User().Username()
		}

		out.Nodes = append(out.Nodes, gn)
	}

	keys := slices.SortedFunc(maps.Keys(g.edges), func(a, b edgeKey) int {
		if a.src != b.src {
			return cmp.Compare(a.src, b.src)
		}

		return cmp.Compare(a.dst, b.dst)
	})

	focusStr := formatID(g.focus.Uint64())

	for _, k := range keys {
		e := *g.edges[k]
		out.Edges = append(out.Edges, e)

		if g.focus != 0 {
			if e.Src == focusStr {
				out.Reachable = append(out.Reachable, e)
			}

			if e.Dst == focusStr {
				out.ReachedBy = append(out.ReachedBy, e)
			}
		}
	}

	return out
}

func registerAccessGraph(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getAccessGraph",
		Method:      http.MethodGet,
		Path:        "/api/v1/access-graph",
		Summary:     "Access graph",
		Description: "Who can reach what: one edge per ordered pair of machines the packet filter or the " +
			"SSH policy opens, computed from the rules the server hands each machine.",
		Tags:     []string{tagAccessControl},
		Security: bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, in *accessGraphInput) (*accessGraphOutput, error) {
		var focus types.NodeID

		if strings.TrimSpace(in.Node) != "" {
			id, err := parseNodeID(in.Node)
			if err != nil {
				return nil, err
			}

			if _, ok := b.State.GetNodeByID(id); !ok {
				return nil, huma.Error404NotFound("node not found")
			}

			focus = id
		}

		graph, err := b.accessGraph(focus)
		if err != nil {
			return nil, huma.Error500InternalServerError("building access graph", err)
		}

		return &accessGraphOutput{Body: graph}, nil
	})
}
