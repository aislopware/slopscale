package state

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/oschwald/maxminddb-golang/v2"
	"github.com/rs/zerolog/log"
)

// ListPostures returns every posture.
func (s *State) ListPostures() []types.Posture {
	return s.AccessModel().Postures
}

// GetPosture returns one posture.
func (s *State) GetPosture(id types.PostureID) (types.Posture, error) {
	if p, ok := s.AccessModel().Posture(id); ok {
		return p, nil
	}

	return types.Posture{}, types.ErrPostureNotFound
}

// CreatePosture validates and stores a posture. It changes nothing on
// the tailnet until a rule names it.
func (s *State) CreatePosture(p types.Posture) (types.Posture, change.Change, error) {
	p, err := s.normalizePosture(p)
	if err != nil {
		return types.Posture{}, change.Change{}, err
	}

	created, err := s.db.CreatePosture(p)
	if err != nil {
		return types.Posture{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.Posture{}, change.Change{}, err
	}

	log.Info().Uint64("posture.id", uint64(created.ID)).Str("posture.name", created.Name).Msg("Posture created")

	return created, c, nil
}

// UpdatePosture replaces every field of a posture; the rules that name
// it recompile.
func (s *State) UpdatePosture(p types.Posture) (types.Posture, change.Change, error) {
	_, err := s.GetPosture(p.ID)
	if err != nil {
		return types.Posture{}, change.Change{}, err
	}

	p, err = s.normalizePosture(p)
	if err != nil {
		return types.Posture{}, change.Change{}, err
	}

	updated, err := s.db.UpdatePosture(p)
	if err != nil {
		return types.Posture{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.Posture{}, change.Change{}, err
	}

	return updated, c, nil
}

// DeletePosture removes a posture no rule names.
func (s *State) DeletePosture(id types.PostureID) (change.Change, error) {
	p, err := s.GetPosture(id)
	if err != nil {
		return change.Change{}, err
	}

	if rules := s.AccessModel().RulesUsingPosture(id); len(rules) > 0 {
		return change.Change{}, fmt.Errorf("%w: %s", types.ErrPostureInUse, rules[0].Name)
	}

	err = s.db.DeletePosture(id)
	if err != nil {
		return change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Uint64("posture.id", uint64(id)).Str("posture.name", p.Name).Msg("Posture deleted")

	return c, nil
}

// normalizePosture trims and validates a posture and refuses ip:country
// when no GeoIP database can answer it.
func (s *State) normalizePosture(p types.Posture) (types.Posture, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)

	exprs := make([]string, 0, len(p.Expressions))

	for _, e := range p.Expressions {
		if e = strings.TrimSpace(e); e != "" {
			exprs = append(exprs, e)
		}
	}

	p.Expressions = exprs

	err := types.ValidatePosture(p)
	if err != nil {
		return types.Posture{}, err
	}

	if s.geoIP == nil {
		for _, e := range p.Parsed() {
			if e.Attr == types.PostureAttributeIPCountry {
				return types.Posture{}, types.ErrPostureCountryNoGeo
			}
		}
	}

	return p, nil
}

// MatchingPostures lists the postures the node satisfies now.
func (s *State) MatchingPostures(id types.NodeID) ([]types.Posture, error) {
	node, ok := s.GetNodeByID(id)
	if !ok {
		return nil, ErrNodeNotFound
	}

	return s.polMan.MatchingPostures(node), nil
}

// NoteNodeSourceAddr records where the node's control connection comes
// from. The address is runtime state, never persisted. When a posture in
// use reads it and it changed, the policy recompiles.
func (s *State) NoteNodeSourceAddr(id types.NodeID, addr netip.Addr) change.Change {
	if !addr.IsValid() {
		return change.Change{}
	}

	addr = addr.Unmap()
	changed := false

	_, ok := s.nodeStore.UpdateNode(id, func(node *types.Node) {
		if node.SourceAddr != addr {
			node.SourceAddr = addr
			changed = true
		}
	})
	if !ok || !changed || !s.polMan.UsesSourceAddress() {
		return change.Change{}
	}

	c, err := s.updatePolicyManagerNodes()
	if err != nil {
		log.Error().Err(err).Uint64("node.id", id.Uint64()).Msg("recompiling policy after source address change")

		return change.Change{}
	}

	if !c.IsEmpty() {
		c.Reason = "node source address"
	}

	return c
}

// RecompilePostures rebuilds the policy from unchanged inputs, for the
// moments a posture's outcome changes on its own: a schedule boundary.
func (s *State) RecompilePostures() (change.Change, error) {
	changed, err := s.polMan.Recompile()
	if err != nil {
		return change.Change{}, fmt.Errorf("recompiling postures: %w", err)
	}

	if !changed {
		return change.Change{}, nil
	}

	s.nodeStore.RebuildPeerMaps()

	c := change.PolicyChange()
	c.Reason = "posture schedule"

	return c, nil
}

// NextPostureBoundary returns when a scheduled posture in use next opens
// or closes, or the zero time when none is scheduled.
func (s *State) NextPostureBoundary(now time.Time) time.Time {
	return s.polMan.NextScheduleBoundary(now)
}

// openGeoIP opens the configured country database and hands the lookup
// to the policy manager. A missing path is fine: ip:country stays unset.
func (s *State) openGeoIP(path string) error {
	if path == "" {
		return nil
	}

	reader, err := maxminddb.Open(path)
	if err != nil {
		return fmt.Errorf("opening GeoIP database %q: %w", path, err)
	}

	s.geoIP = reader

	_, err = s.polMan.SetCountryLookup(s.countryOf)
	if err != nil {
		return fmt.Errorf("installing GeoIP lookup: %w", err)
	}

	log.Info().Str("path", path).Msg("GeoIP database loaded for ip:country postures")

	return nil
}

// countryOf returns the ISO country code of the address, or "" when the
// database has no entry.
func (s *State) countryOf(addr netip.Addr) string {
	if s.geoIP == nil {
		return ""
	}

	var record struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}

	err := s.geoIP.Lookup(addr).Decode(&record)
	if err != nil {
		return ""
	}

	return record.Country.ISOCode
}

// GeoIPAvailable reports whether ip:country can be looked up.
func (s *State) GeoIPAvailable() bool {
	return s.geoIP != nil
}
