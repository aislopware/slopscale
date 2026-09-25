package state

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/traffic/asn"
	"github.com/rs/zerolog/log"
)

// asnDownloadTimeout bounds one download of the ASN table.
const asnDownloadTimeout = 5 * time.Minute

// ErrASNDisabled is returned by [State.RefreshASN] when no table URL is
// configured.
var ErrASNDisabled = errors.New("no ASN table URL is configured")

// asnSource is where the ASN table comes from, through the egress guard
// like every outbound request the server makes.
func (s *State) asnSource() asn.Source {
	return asn.Source{
		URL:       s.cfg.Traffic.ASNDatabaseURL,
		CachePath: s.cfg.Traffic.ASNCachePath,
		Client:    &http.Client{Transport: egress.Transport(), Timeout: asnDownloadTimeout},
	}
}

// loadASNCache puts the cached table in use, so destinations have names
// before the next download. The table takes tens of megabytes, so it is
// loaded only once a gateway reports.
func (s *State) loadASNCache() {
	if s.cfg.Traffic.ASNDatabaseURL == "" {
		return
	}

	table, err := s.asnSource().LoadCache()
	if err != nil {
		if !errors.Is(err, asn.ErrNoCacheFile) {
			log.Warn().Err(err).Msg("the cached ASN table is unusable; waiting for a download")
		}

		return
	}

	s.asnTable.Store(table)
}

// RefreshASN downloads the ASN table and puts it in use. A failed
// download keeps the table in use, so a network problem never strips
// destinations of their names.
func (s *State) RefreshASN(ctx context.Context) error {
	if s.cfg.Traffic.ASNDatabaseURL == "" {
		return ErrASNDisabled
	}

	table, err := s.asnSource().Fetch(ctx)
	if errors.Is(err, asn.ErrNotModified) {
		if s.asnTable.Load() == nil {
			s.loadASNCache()
		}

		return nil
	}

	if err != nil {
		return err
	}

	s.asnTable.Store(table)

	log.Info().Int("ranges", table.Len()).Msg("ASN table loaded")

	return nil
}

// ASNLookup returns what the ASN table knows about addr.
func (s *State) ASNLookup(addr netip.Addr) (asn.Info, bool) {
	return s.asnTable.Load().Lookup(addr)
}

// ASNName returns the name of a network, empty when unknown.
func (s *State) ASNName(number uint32) string {
	return s.asnTable.Load().Name(number)
}

// ASNRanges is how many ranges the table in use holds, 0 without one.
func (s *State) ASNRanges() int {
	return s.asnTable.Load().Len()
}

// EnsureASN puts the cached table in use when none is, and returns when
// the cached copy was last replaced, zero without one.
func (s *State) EnsureASN() time.Time {
	if s.asnTable.Load() == nil {
		s.loadASNCache()
	}

	return s.asnSource().CachedAt()
}
