package turn

import "testing"

func TestApplyRememberAction(t *testing.T) {
	tests := []struct {
		name          string
		existing      string
		action        string
		actionContent string
		replaceTarget string
		want          string
	}{
		{
			name:          "add to empty",
			existing:      "",
			action:        "add",
			actionContent: "hello",
			want:          "hello",
		},
		{
			name:          "add append",
			existing:      "line one",
			action:        "add",
			actionContent: "line two",
			want:          "line one\n\nline two",
		},
		{
			name:          "replace all",
			existing:      "old content",
			action:        "replace",
			actionContent: "new content",
			replaceTarget: "",
			want:          "new content",
		},
		{
			name:          "replace fragment",
			existing:      "foo bar baz",
			action:        "replace",
			actionContent: "qux",
			replaceTarget: "bar",
			want:          "foo qux baz",
		},
		{
			name:          "empty action content keeps existing",
			existing:      "keep me",
			action:        "add",
			actionContent: "",
			want:          "keep me",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRememberAction(tt.existing, tt.action, tt.actionContent, tt.replaceTarget)
			if got != tt.want {
				t.Fatalf("applyRememberAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeRememberNoConflict(t *testing.T) {
	if got := mergeRememberNoConflict("a", "b"); got != "a\n\nb" {
		t.Fatalf("got %q", got)
	}
	if got := mergeRememberNoConflict("", "b"); got != "b" {
		t.Fatalf("got %q", got)
	}
}
