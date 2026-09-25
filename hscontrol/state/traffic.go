package state

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"tailscale.com/net/tsaddr"
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
	// gateway's resolver stays in the clients' DNS. Agents report every
	// minute, so three missed reports take it out.
	trafficResolverFreshness = 3 * time.Minute

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

// SetTrafficSettings validates, stores and applies the settings. Turning
// the DNS log on or off changes every client's DNS, so it returns the
// change to publish.
func (s *State) SetTrafficSettings(settings types.TrafficSettings) (types.TrafficSettings, change.Change, error) {
	err := settings.Validate()
	if err != nil {
		return types.TrafficSettings{}, change.Change{}, err
	}

	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	err = s.db.SaveTrafficSettings(settings)
	if err != nil {
		return types.TrafficSettings{}, change.Change{}, fmt.Errorf("saving traffic settings: %w", err)
	}

	s.trafficSettings.Store(&settings)

	c, err := s.applyTrafficResolversLocked(time.Now())

	return settings, c, err
}

// loadTraffic reads the ASN cache, the settings and the reporters when
// the server starts, and points the clients at the resolvers still fresh.
func (s *State) loadTraffic() error {
	s.loadASNCache()

	settings, err := s.db.LoadTrafficSettings()
	if err != nil {
		return err
	}

	s.trafficSettings.Store(&settings)

	reporters, err := s.db.ListTrafficReporters()
	if err != nil {
		return err
	}

	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	s.trafficReporters = make(map[types.NodeID]types.TrafficReporter, len(reporters))
	for _, r := range reporters {
		s.trafficReporters[r.NodeID] = r
	}

	_, err = s.applyTrafficResolversLocked(time.Now())

	return err
}

// TrafficReporters returns the gateways that have reported, in node ID
// order, with the resolvers the clients are pointed at now.
func (s *State) TrafficReporters() ([]types.TrafficReporter, []netip.Addr) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	reporters := slices.Collect(maps.Values(s.trafficReporters))
	slices.SortFunc(reporters, func(a, b types.TrafficReporter) int { return cmp.Compare(a.NodeID, b.NodeID) })

	return reporters, slices.Clone(s.trafficResolvers)
}

// TrafficReporterStale reports whether a gateway has missed enough
// reports that its resolver is out of the clients' DNS.
func TrafficReporterStale(r types.TrafficReporter, now time.Time) bool {
	return now.Sub(r.LastReportAt) > trafficResolverFreshness
}

// DeleteTrafficReporter forgets a gateway, taking its resolver out of the
// clients' DNS. What it reported stays until the retention removes it.
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

// TrafficTick drops the resolvers of gateways that stopped reporting; the
// scheduler calls it every half minute.
func (s *State) TrafficTick(now time.Time) (change.Change, error) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	return s.applyTrafficResolversLocked(now)
}

// trafficResolversLocked is the resolvers the clients should use: with
// the DNS log on, every address a fresh gateway reports its working
// resolver on, as long as the address is the gateway's own.
func (s *State) trafficResolversLocked(now time.Time) []netip.Addr {
	if !s.TrafficSettings().DNSLogging {
		return nil
	}

	var out []netip.Addr

	for _, r := range s.trafficReporters {
		if TrafficReporterStale(r, now) || !r.Status.DNS.Enabled || r.Status.DNS.Error != "" {
			continue
		}

		node, ok := s.nodeStore.GetNode(r.NodeID)
		if !ok {
			continue
		}

		for _, ap := range r.DNSListen {
			if ap.Port() == dnsPort && slices.Contains(node.IPs(), ap.Addr()) {
				out = append(out, ap.Addr())
			}
		}
	}

	slices.SortFunc(out, netip.Addr.Compare)

	return slices.Compact(out)
}

// applyTrafficResolversLocked moves the clients' DNS and the grant to the
// resolvers when the set changed, and returns the change to publish.
func (s *State) applyTrafficResolversLocked(now time.Time) (change.Change, error) {
	next := s.trafficResolversLocked(now)
	if slices.Equal(next, s.trafficResolvers) {
		return change.Change{}, nil
	}

	s.trafficResolvers = next
	s.cfg.SetTrafficResolvers(next)

	_, err := s.polMan.SetTrafficResolvers(next)
	if err != nil {
		return change.Change{}, fmt.Errorf("granting the traffic resolvers: %w", err)
	}

	return change.DNSConfig().Merge(change.PolicyChange()), nil
}

// IngestTrafficReport checks the agent's identity token, attributes the
// report's traffic to nodes, rolls it up and stores it. It returns what
// the agent should do next and the change to publish when the clients'
// resolvers moved.
func (s *State) IngestTrafficReport(
	token string,
	report traffic.Report,
	now time.Time,
) (traffic.Response, change.Change, error) {
	gateway, err := s.trafficReporterNode(token, now)
	if err != nil {
		return traffic.Response{}, change.Change{}, err
	}

	err = validateTrafficReport(report, now)
	if err != nil {
		return traffic.Response{}, change.Change{}, err
	}

	settings := s.TrafficSettings()
	batch := s.rollUpTraffic(gateway.ID(), report, settings.Retention, now)

	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	stored, _, err := s.db.ApplyTrafficBatch(batch)
	if err != nil {
		return traffic.Response{}, change.Change{}, err
	}

	if s.trafficReporters == nil {
		s.trafficReporters = make(map[types.NodeID]types.TrafficReporter)
	}

	s.trafficReporters[stored.NodeID] = stored

	c, err := s.applyTrafficResolversLocked(now)
	if err != nil {
		return traffic.Response{}, change.Change{}, err
	}

	return traffic.Response{Seq: stored.LastSeq, Config: s.trafficAgentConfig(settings)}, c, nil
}

// trafficReporterNode is the gateway the identity token names, when the
// token is current and the gateway may report.
func (s *State) trafficReporterNode(token string, now time.Time) (types.NodeView, error) {
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

	switch {
	case !node.IsApproved():
		return types.NodeView{}, fmt.Errorf("%w: the device is waiting for approval", ErrTrafficForbidden)
	case node.IsSuspended():
		return types.NodeView{}, fmt.Errorf("%w: the device is suspended", ErrTrafficForbidden)
	case node.IsExpired():
		return types.NodeView{}, fmt.Errorf("%w: the device's key has expired", ErrTrafficForbidden)
	case !isTrafficGateway(node):
		return types.NodeView{}, fmt.Errorf(
			"%w: the device is no exit node, subnet router or app connector", ErrTrafficForbidden,
		)
	}

	return node, nil
}

// isTrafficGateway reports whether other nodes' traffic can cross the
// node: it serves approved exit or subnet routes, or runs an app
// connector. Nothing else sees another node's traffic, so nothing else
// may report it.
func isTrafficGateway(node types.NodeView) bool {
	if node.IsExitNode() || node.IsSubnetRouter() {
		return true
	}

	hostinfo := node.Hostinfo()

	return hostinfo.Valid() && hostinfo.AppConnector().EqualBool(true)
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

// trafficAgentConfig is the configuration handed to every agent.
func (s *State) trafficAgentConfig(settings types.TrafficSettings) traffic.Config {
	return traffic.Config{
		SNI:            settings.SNI,
		DNS:            settings.DNSLogging,
		Upstreams:      s.trafficUpstreams(),
		ReportInterval: trafficReportInterval,
	}
}

// trafficUpstreams are the tailnet's global nameservers an agent's
// resolver can forward to: plain addresses and DoH URLs, never a tailnet
// address, which could be a gateway resolver and loop.
func (s *State) trafficUpstreams() []string {
	var out []string

	for _, ns := range s.cfg.EffectiveDNS().Nameservers.Global {
		u, err := url.Parse(ns)
		if err == nil && u.Scheme == "https" && u.Host != "" {
			out = append(out, ns)

			continue
		}

		addr, err := netip.ParseAddr(ns)
		if err != nil {
			ap, apErr := netip.ParseAddrPort(ns)
			if apErr != nil {
				continue
			}

			addr = ap.Addr()
		}

		if isTailnetAddr(addr) {
			continue
		}

		out = append(out, ns)
	}

	return out
}

func isTailnetAddr(addr netip.Addr) bool {
	addr = addr.Unmap()

	return tsaddr.CGNATRange().Contains(addr) || tsaddr.TailscaleULARange().Contains(addr)
}

// TrafficResolutionFor picks the finest resolution, no finer than
// minimum, whose rows still cover start and that splits the range into
// at most maxPoints buckets.
func (s *State) TrafficResolutionFor(start, end, now time.Time, minimum int64, maxPoints int) int64 {
	retention := s.TrafficSettings().Retention

	for _, res := range []int64{types.TrafficMinute, types.TrafficHour} {
		if res < minimum {
			continue
		}

		covered := !start.Before(now.Add(-retention.Of(res)))
		points := end.Sub(start) / (time.Duration(res) * time.Second)

		if covered && points <= time.Duration(maxPoints) {
			return res
		}
	}

	return types.TrafficDay
}

// TrafficMaintenance deletes what the retention no longer keeps and
// folds each closed bucket's smaller destinations and names, the
// scheduler's hourly job.
func (s *State) TrafficMaintenance(now time.Time) error {
	settings := s.TrafficSettings()

	_, err := s.db.PruneTraffic(now, settings.Retention)
	if err != nil {
		return err
	}

	// Only closed buckets are folded; the last few are looked at again
	// in case a late report added to them after the previous fold.
	hour := now.Truncate(time.Hour)

	_, err = s.db.FoldTraffic(types.TrafficHour, hour.Add(-3*time.Hour), hour, trafficKeepPerHour)
	if err != nil {
		return err
	}

	day := time.Unix(now.Unix()-now.Unix()%types.TrafficDay, 0)

	_, err = s.db.FoldTraffic(types.TrafficDay, day.AddDate(0, 0, -3), day, trafficKeepPerDay)

	return err
}

// Traffic reads for the API.

// TrafficSeries sums the totals per bucket.
func (s *State) TrafficSeries(f types.TrafficFilter) ([]types.TrafficPoint, error) {
	return s.db.TrafficSeries(f)
}

// TrafficTopNodes sums the totals per node, or per gateway.
func (s *State) TrafficTopNodes(f types.TrafficFilter, byReporter bool) ([]types.TrafficNodeSum, error) {
	return s.db.TrafficTopNodes(f, byReporter)
}

// TrafficSum sums the totals in the filter.
func (s *State) TrafficSum(f types.TrafficFilter) (types.TrafficCounts, error) {
	return s.db.TrafficSum(f)
}

// TrafficDestinations sums the destinations by group.
func (s *State) TrafficDestinations(
	f types.TrafficFilter,
	group types.TrafficGroup,
) ([]types.TrafficDestinationSum, error) {
	return s.db.TrafficDestinations(f, group)
}

// TrafficDestinationRows reads the destination rows as stored.
func (s *State) TrafficDestinationRows(f types.TrafficFilter) ([]types.TrafficDestination, error) {
	return s.db.TrafficDestinationRows(f)
}

// TrafficNames sums the DNS questions by name or node.
func (s *State) TrafficNames(f types.TrafficFilter, group types.TrafficGroup) ([]types.TrafficNameSum, error) {
	return s.db.TrafficNames(f, group)
}
