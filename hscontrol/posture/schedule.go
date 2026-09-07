package posture

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Schedule is a weekly window in a time zone: the posture holds on the
// listed days between Start and End. An End before Start wraps past
// midnight.
type Schedule struct {
	// Days are lowercase three-letter weekdays: mon, tue, wed, thu, fri,
	// sat, sun.
	Days []string `json:"days"`
	// Start and End are "HH:MM" in Timezone.
	Start string `json:"start"`
	End   string `json:"end"`
	// Timezone is an IANA zone name; empty means UTC.
	Timezone string `json:"timezone,omitempty"`
}

// Errors returned by [Schedule.Validate].
var (
	ErrScheduleDays     = errors.New("schedule needs at least one day of mon, tue, wed, thu, fri, sat, sun")
	ErrScheduleTime     = errors.New("schedule times are HH:MM")
	ErrScheduleTimezone = errors.New("unknown schedule time zone")
)

var weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

const (
	minutesPerHour = 60
	daysPerWeek    = 7
)

// Validate checks the days, times and zone.
func (s Schedule) Validate() error {
	if len(s.Days) == 0 {
		return ErrScheduleDays
	}

	for _, d := range s.Days {
		if !slices.Contains(weekdays, strings.ToLower(d)) {
			return fmt.Errorf("%w, got %q", ErrScheduleDays, d)
		}
	}

	for _, t := range []string{s.Start, s.End} {
		_, err := parseClock(t)
		if err != nil {
			return err
		}
	}

	_, err := s.location()

	return err
}

// parseClock returns minutes since midnight.
func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%w, got %q", ErrScheduleTime, s)
	}

	return t.Hour()*minutesPerHour + t.Minute(), nil
}

// Active reports whether now falls in the window. An invalid schedule is
// never active.
func (s Schedule) Active(now time.Time) bool {
	loc, err := s.location()
	if err != nil {
		return false
	}

	start, err := parseClock(s.Start)
	if err != nil {
		return false
	}

	end, err := parseClock(s.End)
	if err != nil {
		return false
	}

	local := now.In(loc)
	minute := local.Hour()*minutesPerHour + local.Minute()

	if start <= end {
		return s.onDay(local.Weekday()) && minute >= start && minute < end
	}

	// The window wraps midnight: the evening part belongs to the day it
	// starts on, the morning part to the day before.
	if minute >= start {
		return s.onDay(local.Weekday())
	}

	if minute < end {
		return s.onDay((local.Weekday() + daysPerWeek - 1) % daysPerWeek)
	}

	return false
}

// NextBoundary returns the next instant after now at which the window
// may open or close, so the caller can re-evaluate then. The zero time
// means the schedule is invalid.
func (s Schedule) NextBoundary(now time.Time) time.Time {
	loc, err := s.location()
	if err != nil {
		return time.Time{}
	}

	start, err := parseClock(s.Start)
	if err != nil {
		return time.Time{}
	}

	end, err := parseClock(s.End)
	if err != nil {
		return time.Time{}
	}

	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	var next time.Time

	// Today and tomorrow cover every wrap; the day change itself is a
	// boundary for the day list.
	for dayOffset := range 2 {
		day := midnight.AddDate(0, 0, dayOffset)

		for _, minute := range []int{0, start, end} {
			candidate := day.Add(time.Duration(minute) * time.Minute)
			if candidate.After(now) && (next.IsZero() || candidate.Before(next)) {
				next = candidate
			}
		}
	}

	return next
}

func (s Schedule) location() (*time.Location, error) {
	if s.Timezone == "" {
		return time.UTC, nil
	}

	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrScheduleTimezone, s.Timezone)
	}

	return loc, nil
}

func (s Schedule) onDay(d time.Weekday) bool {
	for _, day := range s.Days {
		if strings.EqualFold(day, weekdays[d]) {
			return true
		}
	}

	return false
}
