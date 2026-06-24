package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

func TestRecordAppend_disabledOrEmptySessionSkipsWrite(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(false, dir, logx.Discard())
	j.RecordAppend("s1", llm.Message{Role: "user", Content: "x"})
	if files, _ := filepath.Glob(filepath.Join(dir, "*", "*.jsonl")); len(files) != 0 {
		t.Fatalf("expected no files, got %v", files)
	}

	enabled := NewJournal(true, dir, logx.Discard())
	enabled.RecordAppend("", llm.Message{Role: "user", Content: "x"})
	if files, _ := filepath.Glob(filepath.Join(dir, "*", "*.jsonl")); len(files) != 0 {
		t.Fatalf("expected no files for empty session, got %v", files)
	}
}

func TestAppendMessage_writesJSONLLineWithSnapshot(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(true, dir, logx.Discard())
	var history []llm.Message
	j.AppendMessage("sess-a", &history, llm.Message{Role: "user", Content: "hello"})
	if len(history) != 1 || history[0].Content != "hello" {
		t.Fatalf("history = %+v", history)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*", "sess-a.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	line := strings.TrimSpace(readFile(t, files[0]))
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["recorded_at"]; !ok {
		t.Fatalf("missing recorded_at: %+v", obj)
	}
	msg, ok := obj["message"].(map[string]any)
	if !ok {
		t.Fatalf("message = %+v", obj["message"])
	}
	if msg["content"] != "hello" {
		t.Fatalf("content = %v", msg["content"])
	}
}

func TestAppendMessage_recordsAssistantToolCallsReasoningKey(t *testing.T) {
	j := NewJournal(false, t.TempDir(), logx.Discard())
	var history []llm.Message
	j.AppendMessage("sess-reasoning", &history, llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "bash_run",
				Arguments: "{}",
			},
		}},
	})
	payload := messageToJournalPayload(history[0])
	if _, ok := payload["reasoning_content"]; !ok {
		t.Fatalf("journal payload missing reasoning_content: %+v", payload)
	}
}

func TestInsertMessage_prependsAndAppendsJournal(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(true, dir, logx.Discard())
	history := []llm.Message{{Role: "user", Content: "old"}}
	j.InsertMessage("sess-b", &history, 0, llm.Message{Role: "system", Content: "sys"})
	if history[0].Role != "system" || history[1].Content != "old" {
		t.Fatalf("history = %+v", history)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*", "sess-b.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	lines := strings.Split(strings.TrimSpace(readFile(t, files[0])), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
}

func TestJournalFilePath_usesDateSubdir(t *testing.T) {
	path := journalFilePath("/tmp/history", "sess-a")
	wantSuffix := filepath.Join(time.Now().Format("20060102"), "sess-a.jsonl")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Fatalf("path = %q, want suffix %q", path, wantSuffix)
	}
	if strings.Contains(filepath.Dir(path), "sess-a_") {
		t.Fatalf("path should not embed date in filename: %q", path)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
