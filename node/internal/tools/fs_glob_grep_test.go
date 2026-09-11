package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobFiles(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for _, rel := range []string{"a/x.go", "a/y.txt", "b/z.go"} {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := reg.Execute(ctx, "glob_files", `{"directory":"a","glob_pattern":"*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "total_matches: 1") || !strings.Contains(out, "a/x.go") {
		t.Fatalf("glob single dir: %q", out)
	}

	out2, err := reg.Execute(ctx, "glob_files", `{"directory":".","glob_pattern":"**/*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "total_matches: 2") {
		t.Fatalf("glob recursive: %q", out2)
	}
	if !strings.Contains(out2, "a/x.go") || !strings.Contains(out2, "b/z.go") {
		t.Fatalf("glob paths: %q", out2)
	}
}

func TestGrepFile(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{"alpha", "beta foo", "gamma"}, "\n")
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"s.txt","content":`+mustJSON(content)+`}`)
	if err != nil {
		t.Fatal(err)
	}

	out, err := reg.Execute(ctx, "grep_file", `{"path":"s.txt","pattern":"foo","literal":true,"count_limit":2,"context_lines":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "全文件命中数: 1") || !strings.Contains(out, "beta foo") {
		t.Fatalf("grep_file: %q", out)
	}
}

func TestGrepFileRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "grep_file", `{"path":"sub","pattern":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "glob_files") || !strings.Contains(out, "grep_files") {
		t.Fatalf("hint: %q", out)
	}
}

func TestGrepFiles(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	write := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pkg/a.go", "package pkg\nfunc Alpha() {}\n")
	write("pkg/b.go", "package pkg\n// TODO fix\n")
	write("pkg/read.me", "TODO in readme\n")

	out, err := reg.Execute(ctx, "grep_files", `{"directory":"pkg","pattern":"TODO","literal":true,"glob_pattern":"**/*.go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "全树命中数: 1") {
		t.Fatalf("hit count: %q", out)
	}
	if !strings.Contains(out, "pkg/b.go") || !strings.Contains(out, "TODO fix") {
		t.Fatalf("hit block: %q", out)
	}
	if strings.Contains(out, "read.me") {
		t.Fatalf("should not match non-go: %q", out)
	}
}

func TestExternalHandbookGlobAndGrepUseHandbookNamespace(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"handbook/notes/deep/guide.md","content":"needle in handbook"}`); err != nil {
		t.Fatal(err)
	}
	glob, err := reg.Execute(context.Background(), "glob_files", `{"directory":"handbook","glob_pattern":"**/*.md"}`)
	if err != nil || !strings.Contains(glob, "handbook/notes/deep/guide.md") || strings.Contains(glob, "..") {
		t.Fatalf("external handbook glob=%q err=%v", glob, err)
	}
	grep, err := reg.Execute(context.Background(), "grep_files", `{"directory":"handbook/notes","pattern":"needle","literal":true,"glob_pattern":"**/*.md"}`)
	if err != nil || !strings.Contains(grep, "handbook/notes/deep/guide.md") || !strings.Contains(grep, "needle in handbook") {
		t.Fatalf("external handbook grep=%q err=%v", grep, err)
	}
	read, err := reg.Execute(context.Background(), "read_file", `{"path":"handbook/notes/deep/guide.md"}`)
	if err != nil || !strings.Contains(read, "needle in handbook") {
		t.Fatalf("handbook read after glob/grep=%q err=%v", read, err)
	}
}

func TestHandbookRootSymlinkGlobUsesCanonicalNamespace(t *testing.T) {
	workspace, realHandbook, parent := t.TempDir(), t.TempDir(), t.TempDir()
	link := filepath.Join(parent, "handbook-link")
	if err := os.Symlink(realHandbook, link); err != nil {
		t.Skipf("symlink unavailable in this environment: %v", err)
	}
	reg, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(link); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"handbook/nested/guide.md","content":"canonical"}`); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "glob_files", `{"directory":"handbook","glob_pattern":"**/*.md"}`)
	if err != nil || !strings.Contains(out, "handbook/nested/guide.md") || strings.Contains(out, "..") {
		t.Fatalf("canonical handbook glob=%q err=%v", out, err)
	}
}
