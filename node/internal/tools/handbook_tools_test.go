package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundHandbookNamespaceUsesExistingFileTools(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	r, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body, _ := json.Marshal(map[string]any{"path": "handbook/a/deep/b.md", "content": "v1"})
	if _, err := r.Execute(ctx, "write_file", string(body)); err != nil {
		t.Fatal(err)
	}
	out, err := r.Execute(ctx, "read_file", `{"path":"handbook/a/deep/b.md"}`)
	if err != nil || out == "" {
		t.Fatalf("read handbook: %q %v", out, err)
	}
	if raw, err := os.ReadFile(filepath.Join(handbook, "a", "deep", "b.md")); err != nil || string(raw) != "v1" {
		t.Fatalf("disk=%q err=%v", raw, err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "outside"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(ctx, "read_file", `{"path":"handbook/../outside"}`); err == nil {
		t.Fatal("expected handbook traversal rejection")
	}
}

func TestHandbookNewFileUnderEscapingSymlinkIsRejected(t *testing.T) {
	workspace, handbook, outside := t.TempDir(), t.TempDir(), t.TempDir()
	r, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(handbook, "escape")); err != nil {
		t.Skipf("symlink unavailable in this environment: %v", err)
	}
	if _, err := r.Execute(context.Background(), "write_file", `{"path":"handbook/escape/new.md","content":"must not write"}`); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.md")); !os.IsNotExist(err) {
		t.Fatalf("escaped file exists: %v", err)
	}
}

func TestHandbookInternalSymlinkUsesRealLeaseAndTarget(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	r, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(handbook, "real")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, filepath.Join(handbook, "alias")); err != nil {
		t.Skipf("symlink unavailable in this environment: %v", err)
	}
	resolved, err := r.resolvePath("handbook/alias/file.md")
	if err != nil {
		t.Fatal(err)
	}
	realTargetDir, err := filepath.EvalSymlinks(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realTargetDir, "file.md")
	if resolved != want {
		t.Fatalf("resolved=%q want real target %q", resolved, want)
	}
	if _, err := r.Execute(context.Background(), "write_file", `{"path":"handbook/alias/file.md","content":"ok"}`); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(want); err != nil || string(raw) != "ok" {
		t.Fatalf("real target=%q err=%v", raw, err)
	}
}

func TestHandbookDigestConflictHistoryAndRestore(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	r, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	write := func(content, digest string) error {
		body, _ := json.Marshal(map[string]any{"path": "handbook/guide.md", "content": content, "expected_digest": digest})
		_, e := r.Execute(ctx, "write_file", string(body))
		return e
	}
	if err := write("one", ""); err != nil {
		t.Fatal(err)
	}
	out, err := r.Execute(ctx, "read_file", `{"path":"handbook/guide.md"}`)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "文件摘要: "
	idx := strings.Index(out, marker)
	if idx < 0 {
		t.Fatalf("missing digest receipt: %s", out)
	}
	digest := strings.TrimSpace(strings.SplitN(out[idx+len(marker):], "\n", 2)[0])
	if err := os.WriteFile(filepath.Join(handbook, "guide.md"), []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := write("two", digest); err == nil {
		t.Fatal("expected external edit conflict")
	}
	if err := write("two", ""); err != nil {
		t.Fatal(err)
	}
	history, err := r.HandbookHistory(ctx, "handbook/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history entries=%d want 2", len(history))
	}
	if _, err := r.Execute(ctx, "read_file", `{"path":"handbook/.history/manifest.jsonl"}`); err == nil {
		t.Fatal("expected private history rejection")
	}
	if _, err := r.RestoreHandbook(ctx, "handbook/guide.md", "", history[1].Revision); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(handbook, "guide.md")); err != nil || string(raw) != "external" {
		t.Fatalf("restored=%q err=%v", raw, err)
	}
	history, err = r.HandbookHistory(ctx, "handbook/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("restore must create history entry, got %d", len(history))
	}
}
