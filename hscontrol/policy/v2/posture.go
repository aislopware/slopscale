package v2

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/posture"
	"github.com/aislopware/slopscale/hscontrol/types"
	"go4.org/netipx"
	"tailscale.com/types/views"
)

// Postures is the policy file's postures section: "posture:name" to the
// expressions a source must all satisfy.
type Postures map[string][]string

// posturePrefix is what every posture name in the file starts with;
// dbPosturePrefix marks a database posture by id in a compiled grant.
const (
	posturePrefix   = "posture:"
	dbPosturePrefix = "posture:#"
)

// dbPostureName is how a compiled access rule names a database posture.
func dbPostureName(id types.PostureID) string {
	return dbPosturePrefix + id.String()
}

// Errors from posture validation.
var (
	ErrPostureName       = errors.New(`posture names start with "posture:"`)
	ErrPostureNoExprs    = errors.New("posture has no expressions")
	ErrPostureUnknown    = errors.New("srcPosture names a posture the postures section does not define")
	ErrPostureExpression = errors.New("invalid posture expression")
)

// validatePostures checks the postures section and every reference to
// it.
func (pol *Policy) validatePostures() []error {
	var errs []error

	for name, exprs := range pol.Postures {
		if !strings.HasPrefix(name, posturePrefix) || len(name) == len(posturePrefix) ||
			strings.HasPrefix(name, dbPosturePrefix) {
			errs = append(errs, fmt.Errorf("%w: %q", ErrPostureName, name))
		}

		if len(exprs) == 0 {
			errs = append(errs, fmt.Errorf("%w: %q", ErrPostureNoExprs, name))
		}

		_, err := posture.ParseAll(exprs)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w in %q: %w", ErrPostureExpression, name, err))
		}
	}

	check := func(where string, names []string) {
		for _, name := range names {
			if _, ok := pol.Postures[name]; !ok {
				errs = append(errs, fmt.Errorf("%w: %s names %q", ErrPostureUnknown, where, name))
			}
		}
	}

	check("defaultSrcPosture", pol.DefaultSrcPosture)

	for i, acl := range pol.ACLs {
		check(fmt.Sprintf("acls[%d]", i), acl.SrcPosture)
	}

	for i, grant := range pol.Grants {
		check(fmt.Sprintf("grants[%d]", i), grant.SrcPosture)
	}

	return errs
}

// compiledPosture is one posture ready to evaluate against a node.
type compiledPosture struct {
	name     string
	exprs    []posture.Expr
	schedule *posture.Schedule
}

// holds reports whether the node's attributes satisfy every expression
// and the schedule, when there is one, is open.
func (cp compiledPosture) holds(attrs map[string]any, now time.Time) bool {
	if cp.schedule != nil && !cp.schedule.Active(now) {
		return false
	}

	return posture.EvalAll(cp.exprs, attrs)
}

// postureContext is what evaluating postures needs beyond the node: the
// time, for schedules, and the country lookup for ip:country.
type postureContext struct {
	now     time.Time
	country func(netip.Addr) string
}

// grantPostures returns the postures the grant's sources must satisfy
// (any one of them) and whether there are any. An explicit empty
// srcPosture turns the default off for that grant.
func (pol *Policy) grantPostures(grant Grant) []compiledPosture {
	names := grant.SrcPosture
	if names == nil {
		names = pol.DefaultSrcPosture
	}

	out := make([]compiledPosture, 0, len(names))

	for _, name := range names {
		if cp, ok := pol.dbPosture(name); ok {
			out = append(out, cp)

			continue
		}

		exprs, err := posture.ParseAll(pol.Postures[name])
		if err != nil {
			// Validation rejected it; never compiled.
			continue
		}

		out = append(out, compiledPosture{name: name, exprs: exprs})
	}

	return out
}

// dbPosture resolves a "posture:#<id>" name to the database posture.
func (pol *Policy) dbPosture(name string) (compiledPosture, bool) {
	rest, ok := strings.CutPrefix(name, dbPosturePrefix)
	if !ok {
		return compiledPosture{}, false
	}

	id, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return compiledPosture{}, false
	}

	p, ok := pol.access.Posture(types.PostureID(id))
	if !ok {
		return compiledPosture{}, false
	}

	return compiledPosture{name: p.Name, exprs: p.Parsed(), schedule: p.Schedule}, true
}

// postureAttributes is the node's attribute map with the ip: attributes
// added from where it connects from.
func postureAttributes(node types.NodeView, ctx postureContext) map[string]any {
	attrs := node.PostureAttributes(ctx.now)

	addr := node.SourceAddr()
	if !addr.IsValid() {
		return attrs
	}

	attrs[types.PostureAttributeIPAddress] = addr.Unmap().String()

	if ctx.country != nil {
		if c := ctx.country(addr); c != "" {
			attrs[types.PostureAttributeIPCountry] = c
		}
	}

	return attrs
}

// satisfiesAny reports whether the node holds at least one posture.
func satisfiesAny(postures []compiledPosture, node types.NodeView, ctx postureContext) bool {
	attrs := postureAttributes(node, ctx)

	for _, p := range postures {
		if p.holds(attrs, ctx.now) {
			return true
		}
	}

	return false
}

// withPostureSources narrows the grant's sources to the nodes that
// satisfy one of its postures. The sources are resolved as written, then
// every node whose addresses they cover is kept only when its posture
// holds; the grant's sources become that node set. A grant without
// postures is returned as is. Sources that are not nodes (hosts, raw
// prefixes, the wildcard's CGNAT range) fall away, because a posture is
// a property of a node.
func (pol *Policy) withPostureSources(
	grant Grant, users types.Users, nodes views.Slice[types.NodeView], ctx postureContext,
) Grant {
	postures := pol.grantPostures(grant)
	if len(postures) == 0 {
		return grant
	}

	covered, err := grant.Sources.Resolve(pol, users, nodes)
	if err != nil && covered == nil {
		return grant
	}

	set := &postureNodes{names: postureNames(postures)}

	for _, node := range nodes.All() {
		if !nodeCoveredBy(covered, node) {
			continue
		}

		if satisfiesAny(postures, node, ctx) {
			set.ids = append(set.ids, node.ID())
		}
	}

	narrowed := grant
	narrowed.Sources = Aliases{set}

	return narrowed
}

func postureNames(postures []compiledPosture) []string {
	names := make([]string, 0, len(postures))
	for _, p := range postures {
		names = append(names, p.name)
	}

	return names
}

func nodeCoveredBy(covered ResolvedAddresses, node types.NodeView) bool {
	if covered == nil {
		return false
	}

	return slices.ContainsFunc(node.IPs(), covered.Contains)
}

// errPostureNodesNotInPolicyFile is returned when the file names the
// internal posture node set, which it cannot.
var errPostureNodesNotInPolicyFile = errors.New("posture node sets cannot be written in the policy file")

// postureNodes is the [Alias] a posture-narrowed source compiles to: the
// nodes, by id, that satisfied a posture at compile time.
type postureNodes struct {
	names []string
	ids   []types.NodeID
}

func (p *postureNodes) Validate() error { return nil }

func (p *postureNodes) UnmarshalJSON([]byte) error { return errPostureNodesNotInPolicyFile }

func (p *postureNodes) String() string { return "posturenodes:" + strings.Join(p.names, ",") }

func (p *postureNodes) Resolve(
	pol *Policy, users types.Users, nodes views.Slice[types.NodeView],
) (ResolvedAddresses, error) {
	return newResolvedAddresses(p.resolve(pol, users, nodes))
}

func (p *postureNodes) resolve(_ *Policy, _ types.Users, nodes views.Slice[types.NodeView]) (*netipx.IPSet, error) {
	var ips netipx.IPSetBuilder

	for _, node := range nodes.All() {
		if slices.Contains(p.ids, node.ID()) {
			node.AppendToIPSet(&ips)
		}
	}

	ipset, err := ips.IPSet()
	if err != nil {
		return nil, fmt.Errorf("building IP set for %s: %w", p, err)
	}

	return ipset, nil
}

// usesSourceAddress reports whether any posture in use reads an ip:
// attribute.
func (pol *Policy) usesSourceAddress() bool {
	if pol == nil {
		return false
	}

	for _, exprs := range pol.Postures {
		parsed, err := posture.ParseAll(exprs)
		if err == nil && posture.UsesSourceAddress(parsed) {
			return true
		}
	}

	for _, rule := range pol.access.Rules {
		if !rule.Enabled {
			continue
		}

		for _, id := range rule.PostureIDs {
			p, ok := pol.access.Posture(id)
			if ok && posture.UsesSourceAddress(p.Parsed()) {
				return true
			}
		}
	}

	return false
}

// usesPostures reports whether a grant in use carries a posture, so a
// change in a node's posture inputs can move the filter. A default
// posture applies to every grant that names none, so it counts on its
// own; access rules become grants with their postures attached.
func (pol *Policy) usesPostures() bool {
	if pol == nil {
		return false
	}

	if len(pol.DefaultSrcPosture) > 0 {
		return true
	}

	for _, acl := range pol.ACLs {
		if len(acl.SrcPosture) > 0 {
			return true
		}
	}

	for _, grant := range pol.Grants {
		if len(grant.SrcPosture) > 0 {
			return true
		}
	}

	for _, grant := range accessGrants(pol.access) {
		if len(grant.SrcPosture) > 0 {
			return true
		}
	}

	return false
}

// nextScheduleBoundary returns the next instant a schedule of a posture
// in use opens or closes, or the zero time when none is scheduled.
func (pol *Policy) nextScheduleBoundary(now time.Time) time.Time {
	if pol == nil {
		return time.Time{}
	}

	var next time.Time

	for _, rule := range pol.access.Rules {
		if !rule.Enabled {
			continue
		}

		for _, id := range rule.PostureIDs {
			p, ok := pol.access.Posture(id)
			if !ok || p.Schedule == nil {
				continue
			}

			boundary := p.Schedule.NextBoundary(now)
			if !boundary.IsZero() && (next.IsZero() || boundary.Before(next)) {
				next = boundary
			}
		}
	}

	return next
}

// matchingPostures lists the database postures the node satisfies now,
// for the console and the API.
func (pol *Policy) matchingPostures(node types.NodeView, ctx postureContext) []types.Posture {
	if pol == nil {
		return nil
	}

	attrs := postureAttributes(node, ctx)

	var out []types.Posture

	for _, p := range pol.access.Postures {
		cp := compiledPosture{name: p.Name, exprs: p.Parsed(), schedule: p.Schedule}
		if cp.holds(attrs, ctx.now) {
			out = append(out, p)
		}
	}

	return out
}
