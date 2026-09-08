package goals

import (
	"testing"
	"time"
)

func TestParseWorkScheduleContract(t *testing.T) {
	if _, err := LoadScheduleLocation("Not/AZone"); err == nil {
		t.Fatal("accepted unknown timezone")
	}
	if got, err := ParseWorkSchedule(""); err != nil || got != nil {
		t.Fatalf("empty=%v %v", got, err)
	}
	dailyParsed, err := ParseWorkSchedule("daily 09:05")
	if err != nil || dailyParsed.Hour != 9 || dailyParsed.Minute != 5 || dailyParsed.Kind != "daily" {
		t.Fatalf("daily parse=%+v err=%v", dailyParsed, err)
	}
	for _, raw := range []string{"weekly MON,WED,FRI 18:30"} {
		if got, err := ParseWorkSchedule(raw); err != nil || got == nil {
			t.Fatalf("%q: %v", raw, err)
		}
	}
	for _, raw := range []string{"daily 9:05", "daily 24:00", "MON 09:00", "weekly MON MON 09:00", "weekly FOO 09:00"} {
		if _, err := ParseWorkSchedule(raw); err == nil {
			t.Fatalf("accepted invalid %q", raw)
		}
	}
}

func TestWorkScheduleNextStrictAndDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	daily, _ := ParseWorkSchedule("daily 09:00")
	after := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC) // 09:00 local
	next, ok := daily.NextOccurrence(after, loc)
	if !ok || !next.After(after) || next.In(loc).Hour() != 9 {
		t.Fatalf("next=%v", next)
	}
	dst, _ := ParseWorkSchedule("daily 02:30")
	gap, ok := dst.NextOccurrence(time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC), loc)
	if !ok || gap.In(loc).Day() != 8 || gap.In(loc).Hour() != 3 || gap.In(loc).Minute() != 0 {
		t.Fatalf("gap=%v local=%v", gap, gap.In(loc))
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	bg, ok := dst.NextOccurrence(time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC), berlin)
	if !ok || bg.In(berlin).Hour() != 3 || bg.In(berlin).Minute() != 0 {
		t.Fatalf("berlin gap=%v local=%v", bg, bg.In(berlin))
	}
	lh, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Fatal(err)
	}
	half, _ := ParseWorkSchedule("daily 02:15")
	hg, ok := half.NextOccurrence(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), lh)
	if !ok || hg.In(lh).Hour() != 2 || hg.In(lh).Minute() != 30 {
		t.Fatalf("Lord Howe gap=%v local=%v", hg, hg.In(lh))
	}
	fallback, _ := ParseWorkSchedule("daily 01:30")
	first, ok := fallback.NextOccurrence(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), loc)
	if !ok || !first.Equal(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)) {
		t.Fatalf("fallback first=%v", first)
	}
	second, ok := fallback.NextOccurrence(time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC), loc)
	if !ok || second.In(loc).Day() != 2 || second.In(loc).Hour() != 1 || second.In(loc).Minute() != 30 {
		t.Fatalf("fallback repeated=%v", second.In(loc))
	}
}

func TestWeeklyScheduleSkipsUnconfiguredWeekdays(t *testing.T) {
	loc, err := LoadScheduleLocation("UTC")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseWorkSchedule("weekly MON 09:05")
	if err != nil {
		t.Fatal(err)
	}
	after := time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC) // Tuesday
	next, ok := s.NextOccurrence(after, loc)
	if !ok || next.Weekday() != time.Monday || next.Day() != 14 || next.Hour() != 9 || next.Minute() != 5 {
		t.Fatalf("next=%v", next)
	}
}

func TestWorkScheduleCoalescesMissedOccurrences(t *testing.T) {
	loc, _ := time.LoadLocation("UTC")
	s, _ := ParseWorkSchedule("daily 09:00")
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	got, ok := s.Coalesce(time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC), now, loc)
	if !ok || !got.Coalesced || !got.DueAt.Equal(now) || !got.Next.After(now) {
		t.Fatalf("coalesce=%+v", got)
	}
}
