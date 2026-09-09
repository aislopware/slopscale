package state

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/derp"
	derpServer "github.com/aislopware/slopscale/hscontrol/derp/server"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

// DERPRegionSource says where a region of the live map came from.
type DERPRegionSource string

const (
	// DERPSourceTailscale is Tailscale's public relay map.
	DERPSourceTailscale DERPRegionSource = "tailscale"
	// DERPSourceURL is another fetched map.
	DERPSourceURL DERPRegionSource = "url"
	// DERPSourceFile is a map file from derp.paths.
	DERPSourceFile DERPRegionSource = "file"
	// DERPSourceCustom is a relay added through the settings.
	DERPSourceCustom DERPRegionSource = "custom"
	// DERPSourceEmbedded is the relay this server runs.
	DERPSourceEmbedded DERPRegionSource = "embedded"
	// DERPSourceConfig is the inline map of a test config.
	DERPSourceConfig DERPRegionSource = "config"
)

// DERPRegionInfo is one region of the live map, for the API.
type DERPRegionInfo struct {
	ID     tailcfg.DERPRegionID
	Code   string
	Name   string
	Nodes  int
	Source DERPRegionSource
}

// DERPStatus is the DERP configuration as the API reports it: what the
// tailnet runs with, where it comes from and what the live map holds.
type DERPStatus struct {
	// Effective is what the server runs with.
	Effective types.DERPSettings
	// FromFile is the config file's derp section, what a reset returns to.
	FromFile types.DERPSettings
	// Overridden reports whether the settings table holds an override.
	Overridden bool
	// Paths are the config file's map files, merged after the URLs.
	Paths []string
	// AutoAddEmbedded is derp.server.automatically_add_embedded_derp_region:
	// off means a file in Paths describes the embedded relay.
	AutoAddEmbedded bool
	// RelayAvailable reports whether the server has a relay key, so the
	// embedded relay can be turned on.
	RelayAvailable bool
	// RelayRunning reports whether the embedded relay is serving.
	RelayRunning bool
	// STUNAddr is the address STUN is bound to while the relay runs.
	STUNAddr string
	// ServerURL is where the embedded relay is reached.
	ServerURL string
	// Regions are the live map's regions in ID order.
	Regions []DERPRegionInfo
	// FetchedAt is when the map sources were last fetched; zero before
	// the first fetch.
	FetchedAt time.Time
	// FetchError is the last failed refresh, empty after a good one.
	FetchError string
}

var (
	// ErrDERPRelayUnavailable is returned when the settings turn the
	// embedded relay on but the server has no key for it.
	ErrDERPRelayUnavailable = errors.New(
		"embedded DERP relay unavailable: derp.server.private_key_path is not set",
	)
	// ErrDERPSourceUnreachable wraps a failed map fetch.
	ErrDERPSourceUnreachable = errors.New("fetching DERP map")
)

// derpState is what the DERP subsystem keeps besides the live map.
type derpState struct {
	override *types.DERPSettings
	relay    *derpServer.DERPServer
	// sources are the last fetched maps, one per URL then per file, so a
	// settings change that touches no source rebuilds without a fetch.
	sources   []*tailcfg.DERPMap
	fetchedAt time.Time
	fetchErr  error
	regions   map[tailcfg.DERPRegionID]DERPRegionSource
	// changed is signalled after every settings change, so the refresh
	// scheduler picks up a new interval.
	changed chan struct{}
}

// EffectiveDERP returns the settings the tailnet runs with.
func (s *State) EffectiveDERP() types.DERPSettings {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	return s.effectiveDERPLocked()
}

func (s *State) effectiveDERPLocked() types.DERPSettings {
	if s.derp.override != nil {
		return s.derp.override.Clone()
	}

	return s.cfg.DERP.Settings()
}

// DERP reports the DERP configuration in force and the live map.
func (s *State) DERP() DERPStatus {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	return s.derpStatusLocked()
}

func (s *State) derpStatusLocked() DERPStatus {
	st := DERPStatus{
		Effective:       s.effectiveDERPLocked(),
		FromFile:        s.cfg.DERP.Settings(),
		Overridden:      s.derp.override != nil,
		Paths:           slices.Clone(s.cfg.DERP.Paths),
		AutoAddEmbedded: s.cfg.DERP.AutomaticallyAddEmbeddedDerpRegion,
		RelayAvailable:  s.derp.relay != nil,
		ServerURL:       s.cfg.ServerURL,
		Regions:         []DERPRegionInfo{},
		FetchedAt:       s.derp.fetchedAt,
	}

	if st.Paths == nil {
		st.Paths = []string{}
	}

	if s.derp.relay != nil {
		st.RelayRunning = s.derp.relay.Enabled()
		st.STUNAddr = s.derp.relay.STUNAddr()
	}

	if s.derp.fetchErr != nil {
		st.FetchError = s.derp.fetchErr.Error()
	}

	if dm := s.derpMap.Load(); dm != nil {
		for id, region := range dm.Regions {
			st.Regions = append(st.Regions, DERPRegionInfo{
				ID:     id,
				Code:   region.RegionCode,
				Name:   region.RegionName,
				Nodes:  len(region.Nodes),
				Source: s.derp.regions[id],
			})
		}

		slices.SortFunc(st.Regions, func(a, b DERPRegionInfo) int { return int(a.ID - b.ID) })
	}

	return st
}

// DERPChanged is signalled after every settings change, so a refresh
// scheduler can pick up the new interval.
func (s *State) DERPChanged() <-chan struct{} {
	return s.derp.changed
}

// SetDERPRelay hands the state the embedded relay, created by the app
// with the server's relay key. Nil means the server has no relay key
// and the settings cannot turn the relay on.
func (s *State) SetDERPRelay(relay *derpServer.DERPServer) {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	s.derp.relay = relay
}

// DERPRelay returns the embedded relay, nil without a relay key.
func (s *State) DERPRelay() *derpServer.DERPServer {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	return s.derp.relay
}

// LoadDERPMap fetches the map sources and builds the live map from the
// effective settings, at startup. A source that cannot be fetched does
// not keep the server from starting: the map is built from the local
// regions, the failure shows in [State.DERP] and the scheduler refetches
// until a fetch succeeds. A map with no relay at all fails the load. A
// stored override that turns the embedded relay on while the server has
// no relay key is served without the embedded region, with a warning.
func (s *State) LoadDERPMap(ctx context.Context) error {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	settings := s.effectiveDERPLocked()

	if settings.Server.Enabled && s.derp.relay == nil && s.derp.override != nil {
		log.Warn().Msg("the stored DERP settings turn the embedded relay on, but the server has no relay key")
	}

	sources, fetchErr := s.fetchDERPSourcesLocked(ctx, settings)
	if fetchErr != nil {
		log.Warn().
			Err(fetchErr).
			Msg("DERP map sources could not be fetched, serving the local regions until a fetch succeeds")

		sources = fetchedSources{}
	}

	err := s.applyDERPRelayLocked(ctx, settings.Server)
	if err != nil {
		return err
	}

	built, err := s.buildDERPLocked(ctx, settings, sources)
	if err != nil {
		if fetchErr != nil {
			return fmt.Errorf("%w (after %w)", err, fetchErr)
		}

		return err
	}

	s.publishDERPLocked(built)
	// publish clears the error; a failed startup fetch stays visible so
	// the status reports it and the scheduler keeps retrying.
	s.derp.fetchErr = fetchErr

	return nil
}

// DERPFetchFailed reports whether the last fetch of the map sources
// failed, so the scheduler retries even while automatic updates are off.
func (s *State) DERPFetchFailed() bool {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	return s.derp.fetchErr != nil
}

// RefreshDERPMap refetches the map sources and rebuilds the live map. It
// returns an error rather than applying a partial map when a fetch fails,
// and reports whether the map changed.
func (s *State) RefreshDERPMap(ctx context.Context) (bool, error) {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	settings := s.effectiveDERPLocked()

	sources, err := s.fetchDERPSourcesLocked(ctx, settings)
	if err != nil {
		return false, err
	}

	built, err := s.buildDERPLocked(ctx, settings, sources)
	if err != nil {
		s.derp.fetchErr = err

		return false, err
	}

	before := s.derpMap.Load()
	s.publishDERPLocked(built)

	return !derpMapsEqual(before, built.derpMap), nil
}

// SetDERP replaces the runtime DERP settings. The settings are normalized
// and validated, new map sources fetched (unchanged ones are reused, so a
// source that is down does not block a change elsewhere), the embedded
// relay brought to the settings, the map built, the settings stored and
// only then the map published and pushed to every client. A step that
// fails leaves the previous settings, relay and map in force.
func (s *State) SetDERP(ctx context.Context, settings types.DERPSettings) (DERPStatus, change.Change, error) {
	settings = settings.Normalize()

	err := settings.Validate()
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	if settings.Server.Enabled && s.derp.relay == nil {
		return DERPStatus{}, change.Change{}, ErrDERPRelayUnavailable
	}

	sources, err := s.sourcesForLocked(ctx, settings)
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	err = s.commitDERPLocked(ctx, settings, sources, func() error {
		saveErr := s.db.SaveDERPSettings(settings)
		if saveErr != nil {
			return fmt.Errorf("saving derp settings: %w", saveErr)
		}

		s.derp.override = &settings

		return nil
	})
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	return s.derpStatusLocked(), change.DERPMap(), nil
}

// ResetDERP drops the runtime DERP settings so the config file is in
// force again, and pushes the file's map to every client.
func (s *State) ResetDERP(ctx context.Context) (DERPStatus, change.Change, error) {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	settings := s.cfg.DERP.Settings()

	if settings.Server.Enabled && s.derp.relay == nil {
		return DERPStatus{}, change.Change{}, ErrDERPRelayUnavailable
	}

	sources, err := s.sourcesForLocked(ctx, settings)
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	err = s.commitDERPLocked(ctx, settings, sources, func() error {
		deleteErr := s.db.DeleteDERPSettings()
		if deleteErr != nil {
			return fmt.Errorf("deleting derp settings: %w", deleteErr)
		}

		s.derp.override = nil

		return nil
	})
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	return s.derpStatusLocked(), change.DERPMap(), nil
}

// commitDERPLocked brings the relay to the settings, builds the map,
// stores the settings through persist and publishes the map. The relay is
// put back to the previous settings when a later step fails, so the
// running relay never disagrees with the settings in force.
func (s *State) commitDERPLocked(
	ctx context.Context,
	settings types.DERPSettings,
	sources fetchedSources,
	persist func() error,
) error {
	previous := s.effectiveDERPLocked()

	err := s.applyDERPRelayLocked(ctx, settings.Server)
	if err != nil {
		return err
	}

	built, err := s.buildDERPLocked(ctx, settings, sources)
	if err != nil {
		s.rollbackDERPRelayLocked(ctx, previous.Server)

		return err
	}

	err = persist()
	if err != nil {
		s.rollbackDERPRelayLocked(ctx, previous.Server)

		return err
	}

	s.publishDERPLocked(built)
	s.signalDERPChangedLocked()

	return nil
}

func (s *State) rollbackDERPRelayLocked(ctx context.Context, server types.DERPServerSettings) {
	err := s.applyDERPRelayLocked(ctx, server)
	if err != nil {
		log.Error().Err(err).Msg("restoring the embedded DERP relay after a failed settings change")
	}
}

// loadDERP reads the stored override, if any, when the server starts.
func (s *State) loadDERP() error {
	settings, err := s.db.LoadDERPSettings()
	if err != nil {
		return err
	}

	s.derp.override = settings
	s.derp.changed = make(chan struct{}, 1)

	return nil
}

func (s *State) signalDERPChangedLocked() {
	select {
	case s.derp.changed <- struct{}{}:
	default:
	}
}

// fetchedSources are the maps behind a settings' URLs, then the file's
// paths, and when they were fetched.
type fetchedSources struct {
	maps      []*tailcfg.DERPMap
	fetchedAt time.Time
}

// fetchDERPSourcesLocked fetches the settings' URLs and the file's paths.
// A failure is recorded for the status and returned; nothing is kept.
func (s *State) fetchDERPSourcesLocked(ctx context.Context, settings types.DERPSettings) (fetchedSources, error) {
	maps, err := derp.FetchSources(ctx, settings.URLs, s.cfg.DERP.Paths)
	if err != nil {
		s.derp.fetchErr = err

		return fetchedSources{}, fmt.Errorf("%w: %w", ErrDERPSourceUnreachable, err)
	}

	return fetchedSources{maps: maps, fetchedAt: time.Now()}, nil
}

// sourcesForLocked returns the maps a settings change builds from: the
// ones already fetched when the URLs are the same as the settings in
// force, so a change to the relay or the schedule needs no network, and
// a fresh fetch otherwise.
func (s *State) sourcesForLocked(ctx context.Context, settings types.DERPSettings) (fetchedSources, error) {
	current := s.effectiveDERPLocked()
	if s.derp.sources != nil && slices.Equal(settings.URLs, current.URLs) {
		return fetchedSources{maps: s.derp.sources, fetchedAt: s.derp.fetchedAt}, nil
	}

	return s.fetchDERPSourcesLocked(ctx, settings)
}

// applyDERPRelayLocked brings the embedded relay to the settings; without
// a relay there is nothing to do.
func (s *State) applyDERPRelayLocked(_ context.Context, server types.DERPServerSettings) error {
	if s.derp.relay == nil {
		return nil
	}

	// The relay's STUN listener outlives the request and runs on its
	// own context, cancelled when the relay is turned off.
	//nolint:contextcheck // see above
	return s.derp.relay.Apply(server)
}

// builtDERP is a map ready to publish, with where each region came from.
type builtDERP struct {
	sources fetchedSources
	derpMap *tailcfg.DERPMap
	regions map[tailcfg.DERPRegionID]DERPRegionSource
}

// buildDERPLocked builds the map from the config's inline map, the
// fetched sources, the custom regions and the embedded region, in that
// order. The embedded region is published on the STUN port the relay is
// bound to, which differs from the settings when they asked for port 0.
// A map without a relay that carries traffic is refused.
func (s *State) buildDERPLocked(
	ctx context.Context,
	settings types.DERPSettings,
	sources fetchedSources,
) (builtDERP, error) {
	maps := make([]*tailcfg.DERPMap, 0, len(sources.maps)+3)
	regions := make(map[tailcfg.DERPRegionID]DERPRegionSource)

	if s.cfg.DERP.DERPMap != nil {
		maps = append(maps, s.cfg.DERP.DERPMap)
		markDERPSources(regions, s.cfg.DERP.DERPMap, DERPSourceConfig)
	}

	for i, m := range sources.maps {
		maps = append(maps, m)

		src := DERPSourceFile
		if i < len(settings.URLs) {
			src = DERPSourceURL
			if settings.URLs[i] == types.TailscaleDERPMapURL {
				src = DERPSourceTailscale
			}
		}

		markDERPSources(regions, m, src)
	}

	custom := settings.RegionsMap()
	maps = append(maps, custom)
	markDERPSources(regions, custom, DERPSourceCustom)

	if settings.Server.Enabled && s.derp.relay != nil && s.cfg.DERP.AutomaticallyAddEmbeddedDerpRegion {
		server := settings.Server
		if bound := s.derp.relay.STUNAddr(); bound != "" {
			server.STUNAddr = bound
		}

		region, err := derp.EmbeddedRegion(ctx, s.cfg.ServerURL, server)
		if err != nil {
			return builtDERP{}, err
		}

		maps = append(maps, &tailcfg.DERPMap{
			Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{region.RegionID: &region},
		})
		regions[region.RegionID] = DERPSourceEmbedded
	}

	derpMap := derp.Build(maps...)
	if !derp.HasRelay(derpMap) {
		return builtDERP{}, types.ErrDERPMapEmpty
	}

	for id := range regions {
		if _, ok := derpMap.Regions[id]; !ok {
			delete(regions, id)
		}
	}

	return builtDERP{sources: sources, derpMap: derpMap, regions: regions}, nil
}

// publishDERPLocked makes a built map the live one and remembers the
// sources it came from.
func (s *State) publishDERPLocked(built builtDERP) {
	s.derp.sources = built.sources.maps
	s.derp.fetchedAt = built.sources.fetchedAt
	s.derp.fetchErr = nil
	s.derp.regions = built.regions
	s.derpMap.Store(built.derpMap)
}

func markDERPSources(
	sources map[tailcfg.DERPRegionID]DERPRegionSource,
	dm *tailcfg.DERPMap,
	src DERPRegionSource,
) {
	for id := range dm.Regions {
		sources[id] = src
	}
}

// derpMapsEqual compares two maps as the clients see them, region flags
// and home parameters included, ignoring only the order relays were
// shuffled into.
func derpMapsEqual(a, b *tailcfg.DERPMap) bool {
	if a == nil || b == nil {
		return a == b
	}

	return reflect.DeepEqual(canonicalDERPMap(a), canonicalDERPMap(b))
}

// canonicalDERPMap is a copy with the relays of every region in name
// order and an empty home parameter set folded to nil.
func canonicalDERPMap(dm *tailcfg.DERPMap) *tailcfg.DERPMap {
	out := &tailcfg.DERPMap{
		OmitDefaultRegions: dm.OmitDefaultRegions,
		Regions:            make(map[tailcfg.DERPRegionID]*tailcfg.DERPRegion, len(dm.Regions)),
	}

	for id, region := range dm.Regions {
		cloned := region.Clone()
		slices.SortFunc(cloned.Nodes, func(x, y *tailcfg.DERPNode) int { return strings.Compare(x.Name, y.Name) })
		out.Regions[id] = cloned
	}

	if dm.HomeParams != nil && len(dm.HomeParams.RegionScore) > 0 {
		out.HomeParams = dm.HomeParams.Clone()
	}

	return out
}

// LogDERPMap logs the live map's regions at startup.
func (s *State) LogDERPMap() {
	for _, r := range s.DERP().Regions {
		log.Info().
			Int("region", int(r.ID)).
			Str("code", r.Code).
			Int("relays", r.Nodes).
			Str("source", string(r.Source)).
			Msg("DERP region")
	}
}
