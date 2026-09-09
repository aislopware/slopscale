package derp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/juanfont/headscale/hscontrol/egress"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
	"tailscale.com/envknob"
	"tailscale.com/tailcfg"
)

// maxDERPMapBytes bounds a fetched map: the public one is a few tens of
// kilobytes, so this leaves room without letting a URL stream forever.
const maxDERPMapBytes = 4 << 20

var (
	// ErrFetchFailed is returned when a DERP map URL answers outside 2xx.
	ErrFetchFailed = errors.New("DERP map URL rejected the request")
	// ErrRedirected is returned when a DERP map URL redirects; the map is
	// read from the URL the operator configured only.
	ErrRedirected = errors.New("DERP map URL redirected")
)

// noRedirect keeps the fetch at the configured URL instead of following it
// to wherever it points, which would step past the egress guard's decision
// about the host the operator named.
func noRedirect(*http.Request, []*http.Request) error {
	return ErrRedirected
}

func loadDERPMapFromPath(path string) (*tailcfg.DERPMap, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading DERP map file %q: %w", path, err)
	}

	var derpMap tailcfg.DERPMap

	err = yaml.Unmarshal(b, &derpMap)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling DERP map YAML: %w", err)
	}

	return &derpMap, nil
}

func loadDERPMapFromURL(ctx context.Context, addr url.URL) (*tailcfg.DERPMap, error) {
	ctx, cancel := context.WithTimeout(ctx, types.HTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request for DERP map: %w", err)
	}

	// The URL is operator input: it is dialed through the egress guard, the
	// answer is taken from where it was asked and its size is bounded.
	client := http.Client{
		Timeout:       types.HTTPTimeout,
		Transport:     egress.Transport(),
		CheckRedirect: noRedirect,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching DERP map from %s: %w", addr.Redacted(), err)
	}

	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetching DERP map from %s: %w: %s", addr.Redacted(), ErrFetchFailed, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDERPMapBytes))
	if err != nil {
		return nil, fmt.Errorf("reading DERP map response body: %w", err)
	}

	var derpMap tailcfg.DERPMap

	err = json.Unmarshal(body, &derpMap)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling DERP map JSON: %w", err)
	}

	return &derpMap, nil
}

// mergeDERPMaps naively merges a list of [tailcfg.DERPMap] values into a single
// [tailcfg.DERPMap]: the Regions by ID and the HomeParams region scores.
// If a region or a score exists in two of the given [tailcfg.DERPMap]
// values, the one from the _last_ [tailcfg.DERPMap] will be preserved.
// An empty [tailcfg.DERPMap] list will result in a [tailcfg.DERPMap] with no regions.
func mergeDERPMaps(derpMaps []*tailcfg.DERPMap) *tailcfg.DERPMap {
	result := tailcfg.DERPMap{
		OmitDefaultRegions: false,
		Regions:            map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{},
	}

	for _, derpMap := range derpMaps {
		// Clone each region: copying the pointer would let a later in-place
		// shuffle alias regions shared with the source map or a previously
		// served map, racing concurrent readers.
		for id, region := range derpMap.Regions {
			if cloned := sanitizeRegion(id, region); cloned != nil {
				result.Regions[id] = cloned
			}
		}

		if derpMap.HomeParams == nil {
			continue
		}

		// A score scales the region's measured latency when the client
		// picks its home DERP; below 1 prefers the region, above 1 avoids
		// it. The client ignores zero and negative scores, so drop them
		// here rather than send them.
		for id, score := range derpMap.HomeParams.RegionScore {
			if score <= 0 {
				continue
			}

			if result.HomeParams == nil {
				result.HomeParams = &tailcfg.DERPHomeParams{RegionScore: map[tailcfg.DERPRegionID]float64{}}
			}

			result.HomeParams.RegionScore[id] = score
		}
	}

	return &result
}

// GetDERPMap builds the map the config file alone describes: its URLs,
// files and inline map, without the embedded relay. The server builds its
// live map through [State] from the effective settings; this stays for
// callers that only have a config.
func GetDERPMap(ctx context.Context, cfg types.DERPConfig) (*tailcfg.DERPMap, error) {
	sources, err := FetchSources(ctx, cfg.Settings().URLs, cfg.Paths)
	if err != nil {
		return nil, err
	}

	if cfg.DERPMap != nil {
		sources = append([]*tailcfg.DERPMap{cfg.DERPMap}, sources...)
	}

	return Build(sources...), nil
}

// FetchSources loads the maps behind every URL and file path, in that
// order. One unreachable source fails the whole fetch, so a refresh never
// applies a partial map.
func FetchSources(ctx context.Context, urls, paths []string) ([]*tailcfg.DERPMap, error) {
	maps := make([]*tailcfg.DERPMap, 0, len(urls)+len(paths))

	for _, u := range urls {
		addr, err := url.Parse(u)
		if err != nil {
			return nil, fmt.Errorf("parsing DERP map URL %q: %w", u, err)
		}

		derpMap, err := loadDERPMapFromURL(ctx, *addr)
		if err != nil {
			return nil, err
		}

		maps = append(maps, derpMap)
	}

	for _, path := range paths {
		derpMap, err := loadDERPMapFromPath(path)
		if err != nil {
			return nil, err
		}

		maps = append(maps, derpMap)
	}

	return maps, nil
}

// sanitizeRegion clones a fetched region into a shape the server and the
// clients can rely on: the region and its relays carry the map's id, a
// null relay or one without a name is dropped, and of two relays with the
// same name the first stays. A region without relays is kept; a client
// simply has nothing to measure there.
func sanitizeRegion(id tailcfg.DERPRegionID, region *tailcfg.DERPRegion) *tailcfg.DERPRegion {
	cloned := region.Clone()
	if cloned == nil {
		return nil
	}

	cloned.RegionID = id
	seen := make(map[string]bool, len(cloned.Nodes))
	nodes := cloned.Nodes[:0]

	for _, node := range cloned.Nodes {
		if node == nil || node.Name == "" || seen[node.Name] {
			continue
		}

		seen[node.Name] = true
		node.RegionID = id
		nodes = append(nodes, node)
	}

	cloned.Nodes = nodes

	return cloned
}

// Build merges the maps in order, a later region replacing an earlier one
// with the same ID, and shuffles the relays within each region so clients
// do not all start with the same one.
func Build(maps ...*tailcfg.DERPMap) *tailcfg.DERPMap {
	derpMap := mergeDERPMaps(maps)
	shuffleDERPMap(derpMap)

	return derpMap
}

// HasRelay reports whether the map holds a relay that carries traffic,
// not only STUN-only relays: without one a client that cannot connect
// directly has no path.
func HasRelay(dm *tailcfg.DERPMap) bool {
	if dm == nil {
		return false
	}

	for _, region := range dm.Regions {
		for _, node := range region.Nodes {
			if node != nil && !node.STUNOnly {
				return true
			}
		}
	}

	return false
}

// debugUseDERPIP makes the embedded relay's region carry the server's IP
// instead of its host name, for integration tests whose DNS is unreliable.
var debugUseDERPIP = envknob.Bool("HEADSCALE_DEBUG_DERP_USE_IP")

// EmbeddedRegion is the region the embedded relay is published as: one
// relay at the server URL's host and port, STUN on the settings' port.
func EmbeddedRegion(ctx context.Context, serverURL string, s types.DERPServerSettings) (tailcfg.DERPRegion, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return tailcfg.DERPRegion{}, fmt.Errorf("parsing server URL %q: %w", serverURL, err)
	}

	host, portStr, err := net.SplitHostPort(parsed.Host)

	var port int

	if err != nil {
		host = parsed.Host
		if parsed.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	} else {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return tailcfg.DERPRegion{}, fmt.Errorf("parsing server URL port %q: %w", portStr, err)
		}
	}

	if debugUseDERPIP {
		ips, resolveErr := new(net.Resolver).LookupIPAddr(ctx, host)
		if resolveErr != nil {
			log.Error().Caller().Err(resolveErr).Msgf("failed to resolve DERP hostname %s to IP, using hostname", host)
		} else if len(ips) > 0 {
			ip := ips[0].IP.String()
			log.Info().Caller().Msgf("HEADSCALE_DEBUG_DERP_USE_IP: resolved %s to %s", host, ip)
			host = ip
		}
	}

	_, stunPortStr, err := net.SplitHostPort(s.STUNAddr)
	if err != nil {
		return tailcfg.DERPRegion{}, fmt.Errorf("splitting STUN address %q: %w", s.STUNAddr, err)
	}

	stunPort, err := strconv.Atoi(stunPortStr)
	if err != nil {
		return tailcfg.DERPRegion{}, fmt.Errorf("parsing STUN port %q: %w", stunPortStr, err)
	}

	return tailcfg.DERPRegion{
		RegionID:   s.RegionID,
		RegionCode: s.RegionCode,
		RegionName: s.RegionName,
		Nodes: []*tailcfg.DERPNode{{
			Name:             s.RegionID.String(),
			RegionID:         s.RegionID,
			HostName:         host,
			DERPPort:         port,
			STUNPort:         stunPort,
			IPv4:             s.IPv4,
			IPv6:             s.IPv6,
			InsecureForTests: parsed.Scheme != "https",
		}},
	}, nil
}

func shuffleDERPMap(dm *tailcfg.DERPMap) {
	if dm == nil || len(dm.Regions) == 0 {
		return
	}

	// Collect region IDs and sort them to ensure deterministic iteration order.
	// Map iteration order is non-deterministic in Go, which would cause the
	// shuffle to be non-deterministic even with a fixed seed.
	ids := make([]tailcfg.DERPRegionID, 0, len(dm.Regions))
	for id := range dm.Regions {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	for _, id := range ids {
		region := dm.Regions[id]
		if len(region.Nodes) == 0 {
			continue
		}

		derpRandom().Shuffle(len(region.Nodes), reflect.Swapper(region.Nodes))
	}
}

var crc64Table = crc64.MakeTable(crc64.ISO)

var (
	derpRandomInst *rand.Rand
	derpRandomMu   sync.Mutex
)

func derpRandom() *rand.Rand {
	derpRandomMu.Lock()
	defer derpRandomMu.Unlock()

	if derpRandomInst == nil {
		seed := cmp.Or(viper.GetString("dns.base_domain"), time.Now().String())
		derpRandomInst = rand.New( //nolint:gosec // weak random is fine for DERP scrambling
			rand.NewPCG(crc64.Checksum([]byte(seed), crc64Table), 0),
		)
	}

	return derpRandomInst
}

func resetDerpRandomForTesting() {
	derpRandomMu.Lock()
	defer derpRandomMu.Unlock()

	derpRandomInst = nil
}
