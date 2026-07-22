package turn

import "testing"

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
	got := FormatLongTermEntries([]LongTermEntry{{ID: "lt-1", Content: "hello"}})
	want := "- [lt-1] hello"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
