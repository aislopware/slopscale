package state

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/juanfont/headscale/hscontrol/derp"
	derpServer "github.com/juanfont/headscale/hscontrol/derp/server"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
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
// effective settings, at startup. A source that cannot be fetched fails
// the load, as does a map with no relay at all.
func (s *State) LoadDERPMap(ctx context.Context) error {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	settings := s.effectiveDERPLocked()

	err := s.fetchDERPSourcesLocked(ctx, settings)
	if err != nil {
		return err
	}

	return s.applyDERPLocked(ctx, settings)
}

// RefreshDERPMap refetches the map sources and rebuilds the live map. It
// returns an error rather than applying a partial map when a fetch fails,
// and reports whether the map changed.
func (s *State) RefreshDERPMap(ctx context.Context) (bool, error) {
	s.derpMu.Lock()
	defer s.derpMu.Unlock()

	settings := s.effectiveDERPLocked()

	err := s.fetchDERPSourcesLocked(ctx, settings)
	if err != nil {
		return false, err
	}

	before := s.derpMap.Load()

	err = s.applyDERPLocked(ctx, settings)
	if err != nil {
		return false, err
	}

	return !derpMapsEqual(before, s.derpMap.Load()), nil
}

// SetDERP replaces the runtime DERP settings. The settings are normalized
// and validated, the map sources fetched, the embedded relay brought to
// the settings, the map rebuilt and stored, and the new map pushed to
// every client. Nothing is stored when a step fails.
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

	err = s.fetchDERPSourcesLocked(ctx, settings)
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	err = s.applyDERPLocked(ctx, settings)
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	err = s.db.SaveDERPSettings(settings)
	if err != nil {
		return DERPStatus{}, change.Change{}, fmt.Errorf("saving derp settings: %w", err)
	}

	s.derp.override = &settings
	s.signalDERPChangedLocked()

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

	err := s.fetchDERPSourcesLocked(ctx, settings)
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	err = s.applyDERPLocked(ctx, settings)
	if err != nil {
		return DERPStatus{}, change.Change{}, err
	}

	err = s.db.DeleteDERPSettings()
	if err != nil {
		return DERPStatus{}, change.Change{}, fmt.Errorf("deleting derp settings: %w", err)
	}

	s.derp.override = nil
	s.signalDERPChangedLocked()

	return s.derpStatusLocked(), change.DERPMap(), nil
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

// fetchDERPSourcesLocked fetches the settings' URLs and the file's paths
// and keeps them for the next build. A failure keeps the previous
// sources and is recorded for the status.
func (s *State) fetchDERPSourcesLocked(ctx context.Context, settings types.DERPSettings) error {
	sources, err := derp.FetchSources(ctx, settings.URLs, s.cfg.DERP.Paths)
	if err != nil {
		s.derp.fetchErr = err

		return fmt.Errorf("%w: %w", ErrDERPSourceUnreachable, err)
	}

	s.derp.sources = sources
	s.derp.fetchedAt = time.Now()
	s.derp.fetchErr = nil

	return nil
}

// applyDERPLocked brings the embedded relay to the settings and builds
// the live map from the fetched sources, the custom regions and the
// embedded region. The relay is left as it was when the map would be
// empty.
func (s *State) applyDERPLocked(ctx context.Context, settings types.DERPSettings) error {
	maps := make([]*tailcfg.DERPMap, 0, len(s.derp.sources)+3)
	sources := make(map[tailcfg.DERPRegionID]DERPRegionSource)

	if s.cfg.DERP.DERPMap != nil {
		maps = append(maps, s.cfg.DERP.DERPMap)
		markDERPSources(sources, s.cfg.DERP.DERPMap, DERPSourceConfig)
	}

	for i, m := range s.derp.sources {
		maps = append(maps, m)

		src := DERPSourceFile
		if i < len(settings.URLs) {
			src = DERPSourceURL
			if settings.URLs[i] == types.TailscaleDERPMapURL {
				src = DERPSourceTailscale
			}
		}

		markDERPSources(sources, m, src)
	}

	custom := settings.RegionsMap()
	maps = append(maps, custom)
	markDERPSources(sources, custom, DERPSourceCustom)

	if settings.Server.Enabled && s.cfg.DERP.AutomaticallyAddEmbeddedDerpRegion {
		region, err := derp.EmbeddedRegion(ctx, s.cfg.ServerURL, settings.Server)
		if err != nil {
			return err
		}

		maps = append(maps, &tailcfg.DERPMap{
			Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{region.RegionID: &region},
		})
		sources[region.RegionID] = DERPSourceEmbedded
	}

	derpMap := derp.Build(maps...)
	if len(derpMap.Regions) == 0 {
		return types.ErrDERPMapEmpty
	}

	if s.derp.relay != nil {
		// The relay's STUN listener outlives this call and runs on its
		// own context, cancelled when the relay is turned off.
		//nolint:contextcheck // see above
		err := s.derp.relay.Apply(settings.Server)
		if err != nil {
			return err
		}
	}

	s.derp.regions = sources
	s.derpMap.Store(derpMap)

	return nil
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

// derpMapsEqual compares two maps region by region, ignoring the order
// relays were shuffled into.
func derpMapsEqual(a, b *tailcfg.DERPMap) bool {
	if a == nil || b == nil {
		return a == b
	}

	if len(a.Regions) != len(b.Regions) {
		return false
	}

	for id, ra := range a.Regions {
		rb, ok := b.Regions[id]
		if !ok || !derpRegionsEqual(ra, rb) {
			return false
		}
	}

	return true
}

func derpRegionsEqual(a, b *tailcfg.DERPRegion) bool {
	if a.RegionCode != b.RegionCode || a.RegionName != b.RegionName || len(a.Nodes) != len(b.Nodes) {
		return false
	}

	byName := make(map[string]*tailcfg.DERPNode, len(a.Nodes))
	for _, n := range a.Nodes {
		byName[n.Name] = n
	}

	for _, n := range b.Nodes {
		other, ok := byName[n.Name]
		if !ok || *other != *n {
			return false
		}
	}

	return true
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
