package api

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestLongTermEntryViewDatesUseYYYYMMDD(t *testing.T) {
	date := time.Date(2026, time.August, 13, 23, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	views := longTermEntriesToViews([]store.LongTermEntry{{ID: "lt-1", Content: "hello", CreatedAt: date, UpdatedAt: date}})
	if len(views) != 1 || views[0].CreatedAt != "20260813" || views[0].UpdatedAt != "20260813" {
		t.Fatalf("views = %+v", views)
	}

	entries := longTermViewsToEntries(views)
	if len(entries) != 1 || formatLongTermDate(entries[0].CreatedAt) != "20260813" || formatLongTermDate(entries[0].UpdatedAt) != "20260813" {
		t.Fatalf("entries = %+v", entries)
	}
}
