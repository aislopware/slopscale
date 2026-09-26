package state

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
)

// Traffic report errors; the report handler answers them with the status
// in brackets, which is what the agent acts on.
var (
	// ErrTrafficUnauthenticated: the credential is not a current identity
	// token of a node (401; the agent fetches a new token).
	ErrTrafficUnauthenticated = errors.New("traffic report credential rejected")
	// ErrTrafficForbidden: the node may not report (403).
	ErrTrafficForbidden = errors.New("traffic report refused")
	// ErrTrafficReportInvalid: the report breaks the contract (400).
	ErrTrafficReportInvalid = errors.New("invalid traffic report")
	// ErrTrafficReportTooLarge: more entries than a report may carry (413).
	ErrTrafficReportTooLarge = errors.New("traffic report too large")
	// ErrTrafficReporterNotFound: the node never reported (404).
	ErrTrafficReporterNotFound = errors.New("traffic reporter not found")
)

const (
	// trafficResolverFreshness is how long after its last report a
	// gateway's resolver stays in the DNS of its exit node users. Agents
	// report every minute; the users also keep the gateway's own exit
	// node resolver, so an agent that died costs them nothing but its
	// answers until it is dropped.
	trafficResolverFreshness = 90 * time.Second

	// trafficReportInterval is how often the server asks agents to report.
	trafficReportInterval = 60

	// trafficClockSkew is how far into the future a bucket may start: an
	// agent whose clock runs ahead further is refused, so the operator
	// sees the clock problem instead of traffic landing in the future.
	trafficClockSkew = 5 * time.Minute

	// trafficKeepPerHour and trafficKeepPerDay are how many destinations
	// and names each node keeps per gateway and bucket; the rest is
	// folded into one remainder row once the bucket closes.
	trafficKeepPerHour = 500
	trafficKeepPerDay  = 1000

	// maxTrafficName bounds host and question names: a DNS name is at
	// most 253 characters.
	maxTrafficName = 253

	// maxTrafficInstance bounds the agent's instance id.
	maxTrafficInstance = 64

	// maxTrafficVersion bounds the agent's version string.
	maxTrafficVersion = 64

	// maxTrafficStatusError bounds each collector's error text.
	maxTrafficStatusError = 512

	// maxTrafficDNSListen bounds the resolver addresses a report lists.
	maxTrafficDNSListen = 8

	// dnsPort is the only port a gateway resolver is used on: a client
	// sends plain DNS to port 53.
	dnsPort = 53
)

// TrafficSettings returns the traffic monitor settings in force.
func (s *State) TrafficSettings() types.TrafficSettings {
	if p := s.trafficSettings.Load(); p != nil {
		return *p
	}

	return types.DefaultTrafficSettings()
}

// PatchTrafficSettings changes the settings through patch, validates,
// stores and applies them, all under the traffic lock so two changes at
// once cannot lose each other's fields. Turning the DNS log on or off
// changes the DNS of the gateways' exit node users, so it returns the
// change to publish.
func (s *State) PatchTrafficSettings(
	patch func(*types.TrafficSettings),
) (types.TrafficSettings, change.Change, error) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	settings := s.TrafficSettings()
	patch(&settings)

	err := settings.Validate()
	if err != nil {
		return types.TrafficSettings{}, change.Change{}, err
	}

	err = s.db.SaveTrafficSettings(settings)
	if err != nil {
		return types.TrafficSettings{}, change.Change{}, fmt.Errorf("saving traffic settings: %w", err)
	}

	s.trafficSettings.Store(&settings)

	c, err := s.applyTrafficResolversLocked(time.Now())

	return settings, c, err
}

// loadTraffic reads the settings, the fold marks and the reporters when
// the server starts. The resolvers are recomputed from them, with the
// start as their last report so a restart does not take every resolver
// out of the exit node users' DNS until the gateways report again.
func (s *State) loadTraffic() error {
	settings, err := s.db.LoadTrafficSettings()
	if err != nil {
		return err
	}

	s.trafficSettings.Store(&settings)

	marks, err := s.db.LoadTrafficFoldMarks()
	if err != nil {
		return err
	}

	reporters, err := s.db.ListTrafficReporters()
	if err != nil {
		return err
	}

	s.trafficFoldMu.Lock()
	s.trafficFoldMarks = marks
	s.trafficFoldMu.Unlock()

	for i := range s.trafficDirty {
		s.trafficDirty[i].Store(math.MaxInt64)
	}

	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	s.trafficBoot = time.Now()
	s.trafficReporters = make(map[types.NodeID]types.TrafficReporter, len(reporters))

	for _, r := range reporters {
		s.trafficReporters[r.NodeID] = r
	}

	_, err = s.applyTrafficResolversLocked(s.trafficBoot)

	return err
}

// TrafficInUse reports whether any gateway has reported, which is when
// the monitor needs the ASN table.
func (s *State) TrafficInUse() bool {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	return len(s.trafficReporters) > 0
}

// TrafficReporters returns the gateways that have reported, in node ID
// order, with the resolvers their exit node users are pointed at now.
func (s *State) TrafficReporters() ([]types.TrafficReporter, []types.TrafficResolver) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	reporters := slices.Collect(maps.Values(s.trafficReporters))
	slices.SortFunc(reporters, func(a, b types.TrafficReporter) int { return cmp.Compare(a.NodeID, b.NodeID) })

	return reporters, slices.Clone(s.trafficResolvers)
}

// TrafficReporterStale reports whether a gateway has missed enough
// reports that its resolver is out of its exit node users' DNS.
func TrafficReporterStale(r types.TrafficReporter, now time.Time) bool {
	return now.Sub(r.LastReportAt) > trafficResolverFreshness
}

// DeleteTrafficReporter forgets a gateway, taking its resolver out of the
// exit node users' DNS. What it reported stays until the retention
// removes it.
func (s *State) DeleteTrafficReporter(id types.NodeID) (change.Change, error) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	err := s.db.DeleteTrafficReporter(id)
	if err != nil {
		if errors.Is(err, hsdb.ErrTrafficReporterNotFound) {
			return change.Change{}, ErrTrafficReporterNotFound
		}

		return change.Change{}, err
	}

	delete(s.trafficReporters, id)

	return s.applyTrafficResolversLocked(time.Now())
}

// SetTrafficResolverApproval lets the nodes using a gateway as their exit
// node use its resolver, or stops them. An approved resolver is used only
// while the DNS log is on and the gateway reports it working.
func (s *State) SetTrafficResolverApproval(
	id types.NodeID,
	approved bool,
) (types.TrafficReporter, change.Change, error) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	reporter, ok := s.trafficReporters[id]
	if !ok {
		return types.TrafficReporter{}, change.Change{}, ErrTrafficReporterNotFound
	}

	var at *time.Time

	if approved {
		at = new(reporter.ResolverApprovedAt)
		if at.IsZero() {
			*at = time.Now().UTC()
		}
	}

	err := s.db.SetTrafficResolverApproval(id, at)
	if err != nil {
		if errors.Is(err, hsdb.ErrTrafficReporterNotFound) {
			return types.TrafficReporter{}, change.Change{}, ErrTrafficReporterNotFound
		}

		return types.TrafficReporter{}, change.Change{}, err
	}

	reporter.ResolverApprovedAt = time.Time{}
	if at != nil {
		reporter.ResolverApprovedAt = *at
	}

	s.trafficReporters[id] = reporter

	c, err := s.applyTrafficResolversLocked(time.Now())

	return reporter, c, err
}

// TrafficTick drops the resolvers of gateways that stopped reporting or
// no longer qualify; the scheduler calls it every 15 seconds.
func (s *State) TrafficTick(now time.Time) (change.Change, error) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	return s.applyTrafficResolversLocked(now)
}

// trafficRecheck recomputes the resolvers after a node changed in a way
// that can make a gateway ineligible (tags, routes, approval, suspension,
// expiry), so its users leave its resolver at once rather than at the
// next tick.
func (s *State) trafficRecheck() change.Change {
	c, err := s.TrafficTick(time.Now())
	if err != nil {
		log.Error().Err(err).Msg("updating the traffic resolvers after a node change")

		return change.Change{}
	}

	return c
}

// trafficForgetNode drops a deleted node from the reporters; the database
// removed its row and its traffic by cascade.
func (s *State) trafficForgetNode(id types.NodeID) change.Change {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	delete(s.trafficReporters, id)
	s.trafficIngest.Delete(id)

	c, err := s.applyTrafficResolversLocked(time.Now())
	if err != nil {
		log.Error().Err(err).Msg("updating the traffic resolvers after a node deletion")

		return change.Change{}
	}

	return c
}

// trafficResolversLocked is the resolvers the gateways' exit node users
// should use: with the DNS log on, one address of every gateway whose
// resolver an operator approved, that reported it working within the
// freshness, and that still qualifies as a gateway and is online. Right
// after the server starts, the start counts as a report and the online
// check waits, since the gateways have had no time to report or connect.
func (s *State) trafficResolversLocked(now time.Time) []types.TrafficResolver {
	if !s.TrafficSettings().DNSLogging {
		return nil
	}

	apps := s.AppConnectors()
	booting := now.Sub(s.trafficBoot) < trafficResolverFreshness

	var out []types.TrafficResolver

	for _, r := range s.trafficReporters {
		last := r.LastReportAt
		if last.Before(s.trafficBoot) {
			last = s.trafficBoot
		}

		if r.ResolverApprovedAt.IsZero() || now.Sub(last) > trafficResolverFreshness ||
			!r.Status.DNS.Enabled || r.Status.DNS.Error != "" {
			continue
		}

		node, ok := s.nodeStore.GetNode(r.NodeID)
		if !ok || trafficGatewayRefusal(node, apps) != "" {
			continue
		}

		if !booting && (!node.IsOnline().Valid() || !node.IsOnline().Get()) {
			continue
		}

		if addr, ok := trafficResolverAddr(node, r.DNSListen); ok {
			out = append(out, types.TrafficResolver{
				Node:   r.NodeID,
				Stable: r.NodeID.StableID(),
				Addr:   addr,
				DoH:    trafficGatewayDoH(node),
			})
		}
	}

	slices.SortFunc(out, func(a, b types.TrafficResolver) int { return cmp.Compare(a.Node, b.Node) })

	return out
}

// trafficResolverAddr is the one address clients use for a gateway's
// resolver: port 53 on one of the gateway's own addresses, IPv4 first,
// since some clients have no IPv6 route to the tailnet.
func trafficResolverAddr(node types.NodeView, listen []netip.AddrPort) (netip.Addr, bool) {
	var v6 netip.Addr

	for _, ap := range listen {
		addr := ap.Addr().Unmap()
		if ap.Port() != dnsPort || !slices.Contains(node.IPs(), addr) {
			continue
		}

		if addr.Is4() {
			return addr, true
		}

		if !v6.IsValid() {
			v6 = addr
		}
	}

	return v6, v6.IsValid()
}

// trafficGatewayDoH is the gateway's own exit node resolver: the DNS over
// HTTP endpoint of its peer API, which its exit node users ask without
// the monitor. Empty when the gateway announced no peer API port.
func trafficGatewayDoH(node types.NodeView) string {
	hostinfo := node.Hostinfo()
	if !hostinfo.Valid() {
		return ""
	}

	var port4, port6 uint16

	for _, svc := range hostinfo.Services().All() {
		switch svc.Proto {
		case tailcfg.PeerAPI4:
			port4 = svc.Port
		case tailcfg.PeerAPI6:
			port6 = svc.Port
		case tailcfg.TCP, tailcfg.UDP, tailcfg.PeerAPIDNS:
		}
	}

	var v6 netip.AddrPort

	for _, addr := range node.IPs() {
		switch {
		case addr.Is4() && port4 != 0:
			return "http://" + netip.AddrPortFrom(addr, port4).String() + "/dns-query"
		case addr.Is6() && port6 != 0 && !v6.IsValid():
			v6 = netip.AddrPortFrom(addr, port6)
		}
	}

	if v6.IsValid() {
		return "http://" + v6.String() + "/dns-query"
	}

	return ""
}

// applyTrafficResolversLocked moves the exit node users' DNS and the
// grant to the resolvers when the set changed, and returns the change to
// publish. The grant goes first: when the policy cannot compile it, the
// users keep their DNS, which the old grant still admits.
func (s *State) applyTrafficResolversLocked(now time.Time) (change.Change, error) {
	next := s.trafficResolversLocked(now)
	if slices.Equal(next, s.trafficResolvers) {
		return change.Change{}, nil
	}

	addrs := make([]netip.Addr, 0, len(next))
	for _, r := range next {
		addrs = append(addrs, r.Addr)
	}

	slices.SortFunc(addrs, netip.Addr.Compare)

	_, err := s.polMan.SetTrafficResolvers(addrs)
	if err != nil {
		return change.Change{}, fmt.Errorf("granting the traffic resolvers: %w", err)
	}

	prev := s.trafficResolvers
	s.trafficResolvers = next
	s.cfg.SetTrafficResolvers(next)

	var gateways *[]tailcfg.StableNodeID

	if len(next) > 0 {
		stable := make([]tailcfg.StableNodeID, 0, len(next))
		for _, r := range next {
			stable = append(stable, r.Stable)
		}

		gateways = &stable
	}

	s.trafficExitNodes.Store(gateways)

	s.auditTrafficResolvers(prev, next)

	return change.DNSConfig().Merge(change.PolicyChange()), nil
}

// auditTrafficResolvers records a change of the resolver set: it moves
// the DNS of every exit node user of the gateways, so it belongs in the
// audit log whatever caused it.
func (s *State) auditTrafficResolvers(prev, next []types.TrafficResolver) {
	names := func(rs []types.TrafficResolver) []string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.Addr.String())
		}

		return out
	}

	var added, removed []string

	for _, r := range next {
		if !slices.Contains(prev, r) {
			added = append(added, r.Addr.String())
		}
	}

	for _, r := range prev {
		if !slices.Contains(next, r) {
			removed = append(removed, r.Addr.String())
		}
	}

	audit.Record(s, &types.AuditEvent{
		ActorKind:  types.ActorSystem,
		Action:     "traffic.resolvers.change",
		TargetKind: "dns",
		Detail:     map[string]any{"resolvers": names(next), "added": added, "removed": removed},
	})

	log.Info().Strs("resolvers", names(next)).Strs("added", added).Strs("removed", removed).
		Msg("traffic resolvers changed")
}

// AuthenticateTrafficReporter checks the agent's identity token and
// returns the gateway it names, before the report body is read.
func (s *State) AuthenticateTrafficReporter(token string, now time.Time) (types.NodeView, error) {
	signer, err := s.IDTokenSigner()
	if err != nil {
		return types.NodeView{}, err
	}

	claims, err := signer.Verify(token, strings.TrimSuffix(s.cfg.ServerURL, "/"), traffic.Audience, now)
	if err != nil {
		return types.NodeView{}, fmt.Errorf("%w: %w", ErrTrafficUnauthenticated, err)
	}

	node, ok := s.nodeStore.GetNode(types.NodeID(claims.NodeID))
	if !ok {
		return types.NodeView{}, fmt.Errorf("%w: the node no longer exists", ErrTrafficUnauthenticated)
	}

	// A token outlives a key rotation by its lifetime at most; the key
	// check makes a token issued to the old key useless at once.
	if node.NodeKey().String() != claims.Key {
		return types.NodeView{}, fmt.Errorf(
			"%w: the token names a node key the node no longer has",
			ErrTrafficUnauthenticated,
		)
	}

	if reason := trafficGatewayRefusal(node, s.AppConnectors()); reason != "" {
		return types.NodeView{}, fmt.Errorf("%w: %s", ErrTrafficForbidden, reason)
	}

	return node, nil
}

// TrafficGatewayRefusal says why node may not report traffic, empty when
// it may.
func (s *State) TrafficGatewayRefusal(node types.NodeView) string {
	return trafficGatewayRefusal(node, s.AppConnectors())
}

// trafficGatewayRefusal says why a node may not report traffic, empty
// when it may. Only a tagged node qualifies: tags are the operator's to
// hand out, while any user can advertise routes or an app connector on
// their own device. And only a gateway sees other nodes' traffic: approved
// exit or subnet routes, or an app connector a configured app selects.
func trafficGatewayRefusal(node types.NodeView, apps []types.AppConnector) string {
	switch {
	case !node.IsTagged():
		return "the machine is not tagged; only tagged gateways may report traffic"
	case !node.IsApproved():
		return "the machine is waiting for approval"
	case node.IsSuspended():
		return "the machine is suspended"
	case node.IsExpired():
		return "the machine's key has expired"
	case node.IsExitNode() || node.IsSubnetRouter():
		return ""
	case runsSelectedConnector(node, apps):
		return ""
	default:
		return "the machine has no approved exit or subnet routes and is not an app connector for any app"
	}
}

// runsSelectedConnector reports whether the node runs the app connector
// service for at least one configured app.
func runsSelectedConnector(node types.NodeView, apps []types.AppConnector) bool {
	hostinfo := node.Hostinfo()
	if !hostinfo.Valid() || !hostinfo.AppConnector().EqualBool(true) {
		return false
	}

	tags := node.Tags().AsSlice()

	return slices.ContainsFunc(apps, func(app types.AppConnector) bool { return app.Selects(tags, true) })
}

// trafficIngestLock is the lock that keeps two reports of one gateway
// from being applied at once, since a report may take several
// transactions.
func (s *State) trafficIngestLock(id types.NodeID) *sync.Mutex {
	lock, _ := s.trafficIngest.LoadOrStore(id, &sync.Mutex{})

	//nolint:forcetypeassert // the map only ever holds *sync.Mutex
	return lock.(*sync.Mutex)
}

// IngestTrafficReport attributes the report of an authenticated gateway
// to nodes, rolls it up and stores it. It returns what the agent should
// do next and the change to publish when the resolvers moved.
func (s *State) IngestTrafficReport(
	gateway types.NodeView,
	report traffic.Report,
	now time.Time,
) (traffic.Response, change.Change, error) {
	err := validateTrafficReport(report, now)
	if err != nil {
		return traffic.Response{}, change.Change{}, err
	}

	settings := s.TrafficSettings()

	lock := s.trafficIngestLock(gateway.ID())
	lock.Lock()
	defer lock.Unlock()

	batch := s.rollUpTraffic(gateway.ID(), report, settings.Retention, now)

	applied, err := s.db.ApplyTrafficBatch(batch)
	if err != nil {
		return traffic.Response{}, change.Change{}, err
	}

	if applied.Applied {
		s.markTrafficDirty(batch)
	}

	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	// A node deleted while its report was written stays deleted: its
	// rows went with it, and it must not come back as a reporter.
	if _, ok := s.nodeStore.GetNode(gateway.ID()); ok {
		if s.trafficReporters == nil {
			s.trafficReporters = make(map[types.NodeID]types.TrafficReporter)
		}

		s.trafficReporters[gateway.ID()] = applied.Reporter
	}

	c, err := s.applyTrafficResolversLocked(now)
	if err != nil {
		return traffic.Response{}, change.Change{}, err
	}

	return traffic.Response{Seq: applied.Seq, Config: s.trafficAgentConfigLocked(settings, gateway.ID())}, c, nil
}

// markTrafficDirty moves the fold marks back to the oldest hour and day
// the batch wrote, so the next maintenance folds a bucket a late report
// added to after it was folded.
func (s *State) markTrafficDirty(batch types.TrafficBatch) {
	for _, d := range batch.Destinations {
		s.markTrafficDirtySlot(trafficDirtySlot(d.Resolution), d.Bucket)
	}

	for _, d := range batch.DNS {
		s.markTrafficDirtySlot(trafficDirtySlot(d.Resolution), d.Bucket)
	}
}

// trafficDirtySlot is the index of a foldable resolution in
// State.trafficDirty.
func trafficDirtySlot(resolution int64) int {
	if resolution == types.TrafficDay {
		return 1
	}

	return 0
}

// validateTrafficReport checks what the whole report must honour; a
// single bad entry is dropped later rather than refusing the report.
func validateTrafficReport(r traffic.Report, now time.Time) error {
	switch {
	case len(r.Flows) > traffic.MaxFlowsPerReport || len(r.Queries) > traffic.MaxQueriesPerReport:
		return fmt.Errorf("%w: %d flows and %d queries, at most %d and %d",
			ErrTrafficReportTooLarge, len(r.Flows), len(r.Queries),
			traffic.MaxFlowsPerReport, traffic.MaxQueriesPerReport)
	case r.Instance == "" || len(r.Instance) > maxTrafficInstance:
		return fmt.Errorf("%w: instance must be 1 to %d characters", ErrTrafficReportInvalid, maxTrafficInstance)
	case r.Seq == 0:
		return fmt.Errorf("%w: seq starts at 1", ErrTrafficReportInvalid)
	case len(r.Version) > maxTrafficVersion:
		return fmt.Errorf("%w: version is longer than %d characters", ErrTrafficReportInvalid, maxTrafficVersion)
	case len(r.DNSListen) > maxTrafficDNSListen:
		return fmt.Errorf("%w: more than %d resolver addresses", ErrTrafficReportInvalid, maxTrafficDNSListen)
	}

	latest := now.Add(trafficClockSkew).Unix()

	for _, f := range r.Flows {
		err := checkTrafficBucket(f.Bucket, latest)
		if err != nil {
			return err
		}
	}

	for _, q := range r.Queries {
		err := checkTrafficBucket(q.Bucket, latest)
		if err != nil {
			return err
		}
	}

	return nil
}

func checkTrafficBucket(bucket, latest int64) error {
	switch {
	case bucket%traffic.BucketSeconds != 0:
		return fmt.Errorf(
			"%w: bucket %d is not a multiple of %d",
			ErrTrafficReportInvalid,
			bucket,
			traffic.BucketSeconds,
		)
	case bucket > latest:
		return fmt.Errorf("%w: bucket %s is in the future; check the gateway's clock",
			ErrTrafficReportInvalid, time.Unix(bucket, 0).UTC().Format(time.RFC3339))
	}

	return nil
}

// trafficAgentConfigLocked is the configuration handed to the agent of
// gateway.
func (s *State) trafficAgentConfigLocked(settings types.TrafficSettings, gateway types.NodeID) traffic.Config {
	usable, _ := s.splitTrafficUpstreams()

	return traffic.Config{
		SNI:            settings.SNI,
		DNS:            settings.DNSLogging,
		Upstreams:      usable,
		LogSources:     s.trafficLogSourcesLocked(gateway),
		ReportInterval: trafficReportInterval,
	}
}

// trafficLogSourcesLocked are the addresses of the nodes whose DNS points
// at gateway's resolver: those that use gateway as their exit node while
// the DNS log uses its resolver. The agent records the questions of these
// nodes only. A client older than capability version 122 (Tailscale 1.86)
// does not say which exit node it uses, so it is never among them.
func (s *State) trafficLogSourcesLocked(gateway types.NodeID) []netip.Addr {
	if !slices.ContainsFunc(s.trafficResolvers, func(r types.TrafficResolver) bool { return r.Node == gateway }) {
		return nil
	}

	stable := gateway.StableID()

	var out []netip.Addr

	for _, node := range s.nodeStore.ListNodes().All() {
		hostinfo := node.Hostinfo()
		if node.ID() == gateway || !hostinfo.Valid() || hostinfo.ExitNodeID() != stable {
			continue
		}

		out = append(out, node.IPs()...)
	}

	slices.SortFunc(out, netip.Addr.Compare)

	return out
}

// noteTrafficExitNodeMove remembers that node id's DNS moved when it
// switched its exit node to or from a gateway whose resolver the DNS log
// uses, for [State.TakeTrafficDNSChange]. It runs on the map request path
// whenever a client reports another exit node, so while the DNS log uses
// no resolver it costs one atomic load.
func (s *State) noteTrafficExitNodeMove(id types.NodeID, from, to tailcfg.StableNodeID) {
	gateways := s.trafficExitNodes.Load()
	if gateways == nil {
		return
	}

	if slices.Contains(*gateways, from) || slices.Contains(*gateways, to) {
		s.trafficDNSMoved.Store(id, struct{}{})
	}
}

// TakeTrafficDNSChange returns, once, the change that sends node id its
// DNS after its map request moved its exit node to or from a gateway
// resolver; empty otherwise. The map request's own change goes to its
// peers too, so this one is separate, for the node alone.
func (s *State) TakeTrafficDNSChange(id types.NodeID) change.Change {
	if _, moved := s.trafficDNSMoved.LoadAndDelete(id); moved {
		return change.SelfDNS(id)
	}

	return change.Change{}
}

// TrafficSkippedUpstreams are the nameservers for exit node users the
// agents cannot forward to: DNS over TLS, which they do not speak, and
// tailnet addresses.
func (s *State) TrafficSkippedUpstreams() []string {
	_, skipped := s.splitTrafficUpstreams()

	return skipped
}

// splitTrafficUpstreams sorts the nameservers an agent's resolver may
// forward to from those it cannot: the global nameservers the operator
// kept for exit node users, which such a user asks without the monitor.
// Without any, the agent forwards to the gateway's own resolvers, as the
// gateway's peer API does for its exit node users. Plain addresses and
// DoH URLs qualify, never a tailnet address, which could be a gateway
// resolver and loop.
func (s *State) splitTrafficUpstreams() ([]string, []string) {
	var usable, skipped []string

	dns := s.cfg.EffectiveDNS()
	if !dns.OverrideLocalDNS {
		return nil, nil
	}

	for _, ns := range dns.Nameservers.UseWithExitNode {
		u, err := url.Parse(ns)
		if err == nil && u.Scheme == "https" && u.Host != "" {
			usable = append(usable, ns)

			continue
		}

		addr, err := netip.ParseAddr(ns)
		if err != nil {
			ap, apErr := netip.ParseAddrPort(ns)
			if apErr != nil {
				skipped = append(skipped, ns)

				continue
			}

			addr = ap.Addr()
		}

		if isTailnetAddr(addr) {
			skipped = append(skipped, ns)

			continue
		}

		usable = append(usable, ns)
	}

	return usable, skipped
}

func isTailnetAddr(addr netip.Addr) bool {
	addr = addr.Unmap()

	return tsaddr.CGNATRange().Contains(addr) || tsaddr.TailscaleULARange().Contains(addr)
}
