package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLongTermRecordCAS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.db")
	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	rec := LongTermRecord{
		Scope:     LongTermScopeAgent,
		AgentID:   "agt-1",
		Entries:   []LongTermEntry{NewLongTermEntry("memory v1", time.Now().UTC())},
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.SaveLongTermRecordOverwrite(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetLongTermRecord(ctx, LongTermScopeAgent, "agt-1")
	if err != nil || got == nil || len(got.Entries) != 1 {
		t.Fatalf("got = %+v err=%v", got, err)
	}
	v1 := got.UpdatedAt

	got.Entries[0].Content = "memory v2"
	ok, err := st.SaveLongTermRecordCAS(ctx, *got, v1)
	if err != nil || !ok {
		t.Fatalf("first CAS = %v err=%v", ok, err)
	}

	ok, err = st.SaveLongTermRecordCAS(ctx, *got, v1)
	if err != nil || ok {
		t.Fatalf("stale CAS should fail: ok=%v err=%v", ok, err)
	}
}

func TestEntriesFromLegacyMarkdown(t *testing.T) {
	entries := EntriesFromLegacyMarkdown("a\n\nb", time.Now().UTC())
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
}

func TestEntriesFromLegacyMarkdownStripsEntryDate(t *testing.T) {
	now := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	entries := EntriesFromLegacyMarkdown("- [lt-1] [20260813] a", now)
	if len(entries) != 1 || entries[0].Content != "a" {
		t.Fatalf("entries = %+v", entries)
	}
	entryDate := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	if !entries[0].CreatedAt.Equal(entryDate) || !entries[0].UpdatedAt.Equal(entryDate) {
		t.Fatalf("entry timestamps = %+v", entries[0])
	}
}
