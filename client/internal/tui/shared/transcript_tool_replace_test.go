package shared

import "testing"

func TestTranscript_ReplaceToolCallLines(t *testing.T) {
	t.Parallel()

	tr := NewTranscript(0)
	tr.Add("[tool-pending|blk-1] ▶ 调用 bash_run")
	tr.Add("[tool-call-code|blk-1] ls -la")
	tr.ReplaceToolCallLines("blk-1", []string{
		"[tool-pending|blk-1] ▶ 调用 read_file",
	})
	lines := tr.Lines()
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	if lines[0] != "[tool-pending|blk-1] ▶ 调用 read_file" {
		t.Fatalf("line = %q", lines[0])
	}
}
