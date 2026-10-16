package history

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestArchiveIdleContextProjectsCanonicalMessagesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	messages := []llm.Message{
		{Role: "user", Content: "read this", FileReferences: []llm.FileReference{{Path: "docs/guide.md", Name: "guide.md"}}},
		{Role: "assistant", Content: "I will inspect it", ReasoningContent: "private chain", ToolCalls: []llm.ToolCall{{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"docs/guide.md"}`}}}},
		{Role: "tool", Name: "read_file", ToolCallID: "call-1", Content: "contents", ToolResultMetadata: &llm.ToolResultMetadata{Status: "error", Error: &llm.ToolResultErrorDetail{Code: "denied", Message: "permission denied"}}},
		{Role: "assistant", Content: "caption", ContentParts: []llm.ContentPart{{Type: "text", Text: "caption"}, {Type: "image_url", ImageURL: &llm.ImageURLPart{URL: "data:image/png;base64,SECRET"}}}},
	}
	path, err := ArchiveIdleContext(root, "session", "reset-1", messages)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || !strings.Contains(path, filepath.Join("history", filepath.Base(filepath.Dir(path)))) {
		t.Fatalf("path = %q", path)
	}
	registry, err := tools.NewRegistry(root, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.CloseBrowser()
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	read, err := registry.Execute(context.Background(), "read_file", mustJSON(t, map[string]any{"path": relative}))
	if err != nil || !strings.Contains(read, "[Tool call: read_file]") {
		t.Fatalf("read_file archive result=%q err=%v", read, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("messages = %d", len(got))
	}
	for _, entry := range got {
		if len(entry) != 2 {
			t.Fatalf("archive entry contains non role/content fields: %#v", entry)
		}
	}
	body, _ := json.Marshal(got)
	for _, want := range []string{"read_file", `\"path\": \"docs/guide.md\"`, "docs/guide.md", "permission denied", "Status: error", "caption", "image attachment omitted"} {
		if !strings.Contains(strings.ToLower(string(body)), strings.ToLower(want)) {
			t.Fatalf("archive missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"SECRET", "private chain", "call-1", "reasoning_content"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("archive contains %q", forbidden)
		}
	}
	if again, err := ArchiveIdleContext(root, "session", "reset-1", messages); err != nil || again != path {
		t.Fatalf("retry path=%q err=%v", again, err)
	}
	if _, err := ArchiveIdleContext(root, "session", "reset-1", []llm.Message{{Role: "user", Content: "changed"}}); err == nil {
		t.Fatal("expected conflicting reset ID to fail")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestArchiveIdleContextRequiresWritableRootAndIDs(t *testing.T) {
	if _, err := ArchiveIdleContext(t.TempDir(), "s", "", nil); err == nil {
		t.Fatal("expected empty reset ID error")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveIdleContext(file, "s", "r", nil); err == nil {
		t.Fatal("expected archive write failure")
	}
}

func TestArchiveIdleContextRetryAcrossDateKeepsOriginalPath(t *testing.T) {
	root := t.TempDir()
	messages := []llm.Message{{Role: "user", Content: "same"}}
	projected, err := json.MarshalIndent(projectArchiveMessages(messages), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	projected = append(projected, '\n')
	oldPath := filepath.Join(root, "history", "2026-10-15", contextArchiveFilename("session", "reset"))
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, projected, 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := ArchiveIdleContext(root, "session", "reset", messages)
	if err != nil {
		t.Fatal(err)
	}
	if path != oldPath {
		t.Fatalf("retry path = %q, want original %q", path, oldPath)
	}
}

func TestContextArchiveFilenameBoundsLongIdentifiers(t *testing.T) {
	name := contextArchiveFilename(strings.Repeat("s", 500), strings.Repeat("r", 500))
	if len(name) > 255 {
		t.Fatalf("filename too long (%d): %q", len(name), name)
	}
	if name != contextArchiveFilename(strings.Repeat("s", 500), strings.Repeat("r", 500)) {
		t.Fatal("filename is not deterministic")
	}
	if name == contextArchiveFilename(strings.Repeat("s", 500), strings.Repeat("r", 499)+"x") {
		t.Fatal("different reset IDs must not collide")
	}
}
