package turn

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
)

type rememberLongTermStore struct {
	entries []LongTermEntry
	saved   bool
}

func (s *rememberLongTermStore) ReadLongTerm(context.Context) (LongTermSnapshot, error) {
	return LongTermSnapshot{Entries: append([]LongTermEntry(nil), s.entries...)}, nil
}

func (s *rememberLongTermStore) SaveLongTerm(_ context.Context, entries []LongTermEntry, _ time.Time) error {
	s.entries = append([]LongTermEntry(nil), entries...)
	s.saved = true
	return nil
}

func TestPersistLongTermCAS_doesNotRefreshPromptImmediately(t *testing.T) {
	reader := promptcontext.NewContentReader(promptcontext.Content{LongTerm: "old memory"})
	store := &rememberLongTermStore{}
	orch := &Orchestrator{longTermStore: store, promptCtx: reader}

	if err := orch.persistLongTermCAS(context.Background(), []LongTermEntry{{ID: "lt-new", Content: "new memory"}}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !store.saved {
		t.Fatal("expected long-term store write")
	}
	if got := reader.ReadLongTermMemory(); got != "old memory" {
		t.Fatalf("prompt memory = %q, want old memory until reload", got)
	}
}

func TestReloadLongTermMemory_refreshesPrompt(t *testing.T) {
	reader := promptcontext.NewContentReader(promptcontext.Content{LongTerm: "old memory"})
	store := &rememberLongTermStore{entries: []LongTermEntry{{ID: "lt-new", Content: "new memory"}}}
	orch := &Orchestrator{longTermStore: store, promptCtx: reader}

	orch.ReloadLongTermMemory(context.Background())
	if got := reader.ReadLongTermMemory(); got != "- [lt-new] new memory" {
		t.Fatalf("prompt memory = %q, want reloaded memory", got)
	}
}

func TestApplyRememberActionToEntries(t *testing.T) {
	tests := []struct {
		name          string
		existing      []LongTermEntry
		action        string
		actionContent string
		replaceTarget string
		wantCount     int
		wantContent   string
	}{
		{
			name:          "add to empty",
			action:        "add",
			actionContent: "hello",
			wantCount:     1,
			wantContent:   "hello",
		},
		{
			name: "add append",
			existing: []LongTermEntry{
				{ID: "lt-1", Content: "line one"},
			},
			action:        "add",
			actionContent: "line two",
			wantCount:     2,
			wantContent:   "line two",
		},
		{
			name:          "replace all",
			existing:      []LongTermEntry{{ID: "lt-1", Content: "old"}},
			action:        "replace",
			actionContent: "new content",
			replaceTarget: "",
			wantCount:     1,
			wantContent:   "new content",
		},
		{
			name: "replace by id",
			existing: []LongTermEntry{
				{ID: "lt-abc", Content: "foo bar"},
			},
			action:        "replace",
			actionContent: "qux",
			replaceTarget: "lt-abc",
			wantCount:     1,
			wantContent:   "qux",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyRememberActionToEntries(tt.existing, tt.action, tt.actionContent, tt.replaceTarget)
			if len(got) != tt.wantCount {
				t.Fatalf("entry count = %d, want %d", len(got), tt.wantCount)
			}
			if tt.wantContent != "" && got[len(got)-1].Content != tt.wantContent {
				t.Fatalf("last content = %q, want %q", got[len(got)-1].Content, tt.wantContent)
			}
		})
	}
}

func TestFormatLongTermEntries(t *testing.T) {
	date := time.Date(2026, time.August, 13, 23, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	got := FormatLongTermEntries([]LongTermEntry{{ID: "lt-1", Content: "hello", UpdatedAt: date}})
	want := "- [lt-1] [20260813] hello"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestHasExactLongTermContent(t *testing.T) {
	entries := []LongTermEntry{
		{Content: "  user prefers concise answers  "},
	}
	if !hasExactLongTermContent(entries, "user prefers concise answers") {
		t.Fatal("expected trimmed exact content to match")
	}
	if hasExactLongTermContent(entries, "user prefers detailed answers") {
		t.Fatal("unexpected non-matching content")
	}
}

func TestEntriesFromFormattedConflictPreservesEntryDate(t *testing.T) {
	entries := EntriesFromFormattedConflict("- [lt-old] [20260813] old fact")
	if len(entries) != 1 || entries[0].Content != "old fact" {
		t.Fatalf("entries = %+v", entries)
	}
	expected := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	if !entries[0].CreatedAt.Equal(expected) || !entries[0].UpdatedAt.Equal(expected) {
		t.Fatalf("entry timestamps = %+v", entries[0])
	}
}
