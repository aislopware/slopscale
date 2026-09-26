package state

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"time"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/traffic/asn"
	"github.com/rs/zerolog/log"
)

// asnDownloadTimeout bounds one download of the ASN table.
const asnDownloadTimeout = 5 * time.Minute

// asnBackfillRows is how many destinations one backfill transaction names,
// so naming a large history never holds the database long.
const asnBackfillRows = 500

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

// BackfillTrafficASN names the destinations stored without a network
// because no ASN table was in use when they were reported, like the first
// reports of a fresh server, which arrive before the table is downloaded.
// It walks them once per table put in use, in address order, so a
// destination no table knows is looked at once, not in a loop.
func (s *State) BackfillTrafficASN(ctx context.Context) error {
	table := s.asnTable.Load()
	if table == nil || s.asnBackfilled.Load() == table {
		return nil
	}

	var cursor string

	for {
		err := ctx.Err()
		if err != nil {
			return err
		}

		dsts, err := s.db.TrafficUnnamedDestinations(cursor, asnBackfillRows)
		if err != nil {
			return err
		}

		if len(dsts) == 0 {
			break
		}

		cursor = dsts[len(dsts)-1]

		networks := make([]hsdb.TrafficDestinationNetwork, 0, len(dsts))

		for _, dst := range dsts {
			addr, parseErr := netip.ParseAddr(dst)
			if parseErr != nil {
				continue
			}

			info, ok := table.Lookup(addr)
			if !ok || info.ASN == 0 {
				continue
			}

			networks = append(networks, hsdb.TrafficDestinationNetwork{Dst: dst, ASN: info.ASN, Country: info.Country})
		}

		_, err = s.db.NameTrafficDestinations(networks)
		if err != nil {
			return err
		}
	}

	s.asnBackfilled.Store(table)

	return nil
}
