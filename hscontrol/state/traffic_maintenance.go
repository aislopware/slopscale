package state

import (
	"fmt"
	"math"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// TrafficResolutionFor picks the finest resolution, no finer than
// minimum, whose rows still cover start and that splits the range into
// at most maxPoints buckets. A range too long even for daily buckets is
// refused, so no read scans an unbounded number of rows.
func (s *State) TrafficResolutionFor(start, end, now time.Time, minimum int64, maxPoints int) (int64, error) {
	retention := s.TrafficSettings().Retention

	for _, res := range []int64{types.TrafficMinute, types.TrafficHour, types.TrafficDay} {
		if res < minimum {
			continue
		}

		covered := res == types.TrafficDay || !start.Before(now.Add(-retention.Of(res)))
		points := end.Sub(start) / (time.Duration(res) * time.Second)

		if covered && points <= time.Duration(maxPoints) {
			return res, nil
		}
	}

	return 0, fmt.Errorf("%w: the range spans more than %d days; narrow it",
		types.ErrTrafficRangeInvalid, maxPoints)
}

// TrafficMaintenance deletes what the retention no longer keeps and
// folds the smaller destinations and names of every bucket that closed
// since the last fold, or that a late report added to, the scheduler's
// job at startup and every hour. A run that finds another one going
// returns at once.
func (s *State) TrafficMaintenance(now time.Time) error {
	if !s.trafficFoldMu.TryLock() {
		return nil
	}
	defer s.trafficFoldMu.Unlock()

	settings := s.TrafficSettings()

	_, err := s.db.PruneTraffic(now, settings.Retention)
	if err != nil {
		return err
	}

	marks := s.trafficFoldMarks

	for slot, fold := range []struct {
		resolution int64
		keep       int
	}{
		{types.TrafficHour, trafficKeepPerHour},
		{types.TrafficDay, trafficKeepPerDay},
	} {
		dirty := s.trafficDirty[slot].Swap(math.MaxInt64)

		from := min(marks.Of(fold.resolution), dirty)
		oldest := now.Add(-settings.Retention.Of(fold.resolution)).Unix()
		from = max(from, oldest-oldest%fold.resolution)
		to := now.Unix() - now.Unix()%fold.resolution

		if from < to {
			_, err = s.db.FoldTraffic(fold.resolution, time.Unix(from, 0), time.Unix(to, 0), fold.keep)
			if err != nil {
				s.markTrafficDirtySlot(slot, dirty)

				return err
			}
		}

		marks = marks.With(fold.resolution, to)
	}

	err = s.db.SaveTrafficFoldMarks(marks)
	if err != nil {
		return fmt.Errorf("saving the traffic fold marks: %w", err)
	}

	s.trafficFoldMarks = marks

	return nil
}

// markTrafficDirtySlot lowers the dirty mark of one resolution to bucket.
func (s *State) markTrafficDirtySlot(slot int, bucket int64) {
	for {
		current := s.trafficDirty[slot].Load()
		if bucket >= current || s.trafficDirty[slot].CompareAndSwap(current, bucket) {
			return
		}
	}
}

// SetTrafficBootForTest moves the time the server counts as started, which
// is every gateway's last report until it reports again.
func (s *State) SetTrafficBootForTest(at time.Time) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()

	s.trafficBoot = at
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
