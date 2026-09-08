package goals

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

// WorkSchedule is the deliberately small machine contract used by the
// recurring controller. An empty schedule means that the profile is not
// arranged; it is not interpreted as an interval or an always-on schedule.
type WorkSchedule struct {
	Kind   string
	Days   []time.Weekday
	Hour   int
	Minute int
}

// LoadScheduleLocation makes timezone availability an explicit validation
// step for controllers; unknown IANA names are errors, never silently UTC.
func LoadScheduleLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("timezone is required")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}
	return loc, nil
}

var clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)
var weeklyPattern = regexp.MustCompile(`^([A-Z]{3}(?:,[A-Z]{3})*) ([0-9]{2}:[0-9]{2})$`)

// ParseWorkSchedule accepts exactly "daily HH:MM" or
// "weekly MON,TUE HH:MM". Empty input returns an unscheduled profile.
func ParseWorkSchedule(raw string) (*WorkSchedule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "daily ") {
		parts := strings.Split(raw, " ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid work_schedule")
		}
		h, m, ok := parseClock(parts[1])
		if !ok {
			return nil, fmt.Errorf("invalid work_schedule time")
		}
		return &WorkSchedule{Kind: "daily", Hour: h, Minute: m}, nil
	}
	if !strings.HasPrefix(raw, "weekly ") {
		return nil, fmt.Errorf("invalid work_schedule")
	}
	raw = strings.TrimPrefix(raw, "weekly ")
	m := weeklyPattern.FindStringSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("invalid work_schedule")
	}
	h, min, ok := parseClock(m[2])
	if !ok {
		return nil, fmt.Errorf("invalid work_schedule time")
	}
	seen := map[time.Weekday]bool{}
	for _, token := range strings.Split(m[1], ",") {
		day, ok := parseWeekday(token)
		if !ok || seen[day] {
			return nil, fmt.Errorf("invalid work_schedule day")
		}
		seen[day] = true
	}
	days := make([]time.Weekday, 0, len(seen))
	for day := range seen {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i] < days[j] })
	return &WorkSchedule{Kind: "weekly", Days: days, Hour: h, Minute: min}, nil
}

func parseClock(v string) (int, int, bool) {
	m := clockPattern.FindStringSubmatch(v)
	if m == nil {
		return 0, 0, false
	}
	var h, min int
	fmt.Sscanf(m[1], "%02d", &h)
	fmt.Sscanf(m[2], "%02d", &min)
	return h, min, true
}
func parseWeekday(v string) (time.Weekday, bool) {
	switch v {
	case "SUN":
		return time.Sunday, true
	case "MON":
		return time.Monday, true
	case "TUE":
		return time.Tuesday, true
	case "WED":
		return time.Wednesday, true
	case "THU":
		return time.Thursday, true
	case "FRI":
		return time.Friday, true
	case "SAT":
		return time.Saturday, true
	}
	return time.Sunday, false
}

func (s WorkSchedule) matches(day time.Weekday) bool {
	if s.Kind == "daily" {
		return true
	}
	for _, d := range s.Days {
		if d == day {
			return true
		}
	}
	return false
}

// NextOccurrence returns the first scheduled instant strictly after afterUTC.
// Calendar calculations happen in loc and the returned value is UTC. A local
// wall time skipped by DST is moved to the first valid instant of that day.
func (s WorkSchedule) NextOccurrence(afterUTC time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil || (s.Kind != "daily" && s.Kind != "weekly") || s.Hour < 0 || s.Hour > 23 || s.Minute < 0 || s.Minute > 59 {
		return time.Time{}, false
	}
	local := afterUTC.In(loc)
	for n := 0; n <= 370; n++ {
		date := local.AddDate(0, 0, n)
		if !s.matches(date.Weekday()) {
			continue
		}
		candidate := firstValidOnDate(date, loc, s.Hour, s.Minute)
		if candidate.UTC().After(afterUTC.UTC()) {
			return candidate.UTC(), true
		}
	}
	return time.Time{}, false
}

func firstValidOnDate(date time.Time, loc *time.Location, wantHour, wantMinute int) time.Time {
	// Scan real instants around the local date. This avoids time.Date's
	// implementation-dependent normalization during DST gaps and lets us pick
	// one canonical (earliest UTC) instant when a wall time repeats.
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC).Add(-36 * time.Hour)
	end := start.Add(72 * time.Hour)
	target := wantHour*60 + wantMinute
	var exact, first time.Time
	for u := start; u.Before(end); u = u.Add(time.Minute) {
		local := u.In(loc)
		if local.Year() != date.Year() || local.Month() != date.Month() || local.Day() != date.Day() {
			continue
		}
		minute := local.Hour()*60 + local.Minute()
		if minute < target {
			continue
		}
		if first.IsZero() {
			first = u
		}
		if minute == target && exact.IsZero() {
			exact = u
		}
	}
	if !exact.IsZero() {
		return exact.In(loc)
	}
	return first.In(loc)
}

// CoalescedOccurrence describes recovery after downtime. Missed occurrences
// are represented by one immediate check, while Next remains the next future
// scheduled instant; no historical occurrences are replayed.
type CoalescedOccurrence struct {
	DueAt     time.Time
	Next      time.Time
	Coalesced bool
}

func (s WorkSchedule) Coalesce(cursorUTC, nowUTC time.Time, loc *time.Location) (CoalescedOccurrence, bool) {
	if loc == nil {
		return CoalescedOccurrence{}, false
	}
	next, ok := s.NextOccurrence(cursorUTC, loc)
	if !ok {
		return CoalescedOccurrence{}, false
	}
	if !next.After(nowUTC.UTC()) {
		future, found := s.NextOccurrence(nowUTC.UTC(), loc)
		if !found {
			return CoalescedOccurrence{}, false
		}
		return CoalescedOccurrence{DueAt: nowUTC.UTC(), Next: future, Coalesced: true}, true
	}
	return CoalescedOccurrence{DueAt: next, Next: next, Coalesced: false}, true
}
