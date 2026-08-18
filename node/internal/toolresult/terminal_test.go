package toolresult

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPackageTerminalSanitizesClientAndPreservesCursorMetadata(t *testing.T) {
	rawOutput := strings.Repeat("INFO repeated log line\n", 2000) + "ERROR: build failed\n" + strings.Repeat("tail\n", 1000)
	raw := `{"terminal_id":"term-1","output":` + mustJSONTerminalString(t, rawOutput) + `,"next_seq":42,"exited":false,"replay_gap":false}`

	client, history, stats := PackageTerminal(raw, 1200)
	if client == raw || strings.Contains(client, "\x1b") || strings.Contains(client, "\r") {
		t.Fatalf("client result should be readable and sanitized: %q", client)
	}
	if stats.Mode != "compressed" || stats.RawBytes <= stats.KeptBytes || stats.OmittedBytes <= 0 {
		t.Fatalf("unexpected compression stats: %+v", stats)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(history), &got); err != nil {
		t.Fatalf("history result is not JSON: %v\n%s", err, history)
	}
	if got["terminal_id"] != "term-1" || got["next_seq"].(float64) != 42 || got["replay_gap"] != false {
		t.Fatalf("cursor metadata was not preserved: %#v", got)
	}
	output, ok := got["output"].(string)
	if !ok || len([]byte(output)) > 1200 {
		t.Fatalf("compressed output length=%d", len([]byte(output)))
	}
	if !strings.Contains(output, "ERROR: build failed") {
		t.Fatalf("important error line was removed: %q", output)
	}
	if got["output_mode"] != "compressed" {
		t.Fatalf("missing output mode: %#v", got["output_mode"])
	}
}

func TestPackageTerminalSanitizesShortOutput(t *testing.T) {
	raw := `{"terminal_id":"term-1","output":"ready\r\n","next_seq":2,"exited":false}`
	client, history, stats := PackageTerminal(raw, 1200)
	if client == raw || history == raw {
		t.Fatalf("short result should remove terminal controls: client=%q history=%q", client, history)
	}
	if !strings.Contains(client, `"output":"ready\n"`) || !strings.Contains(history, `"output":"ready\n"`) {
		t.Fatalf("sanitized output missing: client=%q history=%q", client, history)
	}
	if stats != (TerminalCompressionStats{}) {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestPackageTerminalRemovesWSLProtocolSequencesForClient(t *testing.T) {
	rawOutput := "top -bn1 | head -20\r\n" +
		"\x1b]3008;start=abc;type=shell\x1b\\" +
		"\x1b[?2004h\x1b[?2004l" +
		"\x1b]0;aphrodite@host: /mnt/d/workspace\a" +
		"\x1b[32m\x1b[1maphrodite\x1b[m: \x1b[34m\x1b[1m/mnt/d/workspace\x1b[m$ " +
		"top -bn1 | head -20\r\n" +
		"Tasks: 76 total\r\n" +
		"\x1b[11;1Hdone\x1b[24;1H\x1b[?25h\n"
	raw := `{"terminal_id":"term-1","output":` + mustJSONTerminalString(t, rawOutput) + `,"next_seq":15,"exited":false}`

	client, history, stats := PackageTerminal(raw, 1200)
	for name, value := range map[string]string{"client": client, "history": history} {
		if strings.ContainsAny(value, "\x1b\x07\r") {
			t.Fatalf("%s retained terminal control bytes: %q", name, value)
		}
		if strings.Contains(value, "3008;") || strings.Contains(value, "[?2004") || strings.Contains(value, "[32m") {
			t.Fatalf("%s retained WSL/ANSI sequence: %q", name, value)
		}
		if !strings.Contains(value, "Tasks: 76 total") {
			t.Fatalf("%s lost readable output: %q", name, value)
		}
	}
	if stats != (TerminalCompressionStats{}) {
		t.Fatalf("unexpected stats for short sanitized output: %+v", stats)
	}
}

func TestNormalizeTerminalTextRemovesANSIAndCollapsesRepeats(t *testing.T) {
	text, lines := normalizeTerminalText("\x1b[32mready\x1b[0m\r\nrepeat\nrepeat\nrepeat\n")
	if lines != 5 {
		t.Fatalf("line count=%d", lines)
	}
	if strings.Contains(text, "\x1b[") || strings.Contains(text, "\r") {
		t.Fatalf("control sequence remained: %q", text)
	}
	if !strings.Contains(text, "repeat [repeated 3 times]") {
		t.Fatalf("repeat was not collapsed: %q", text)
	}
}

func TestPackageTerminalInvalidShapePassesThrough(t *testing.T) {
	for _, raw := range []string{"not json", `{"output":123}`, `{"message":"ok"}`} {
		client, history, stats := PackageTerminal(raw, 10)
		if client != raw || history != raw || stats != (TerminalCompressionStats{}) {
			t.Fatalf("invalid shape changed: raw=%q client=%q history=%q stats=%+v", raw, client, history, stats)
		}
	}
}

func mustJSONTerminalString(t *testing.T, value string) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
