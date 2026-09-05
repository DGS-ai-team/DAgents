package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllToolDefinitionsRequireCallPurpose(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range reg.Definitions() {
		params := def.Function.Parameters
		req, ok := params["required"].([]string)
		if !ok {
			t.Fatalf("tool %q required is not []string", def.Function.Name)
		}
		found := false
		for _, name := range req {
			if name == CallPurposeKey {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tool %q missing required %q", def.Function.Name, CallPurposeKey)
		}
	}
}

func TestParseToolCallArgumentsStripsCallPurpose(t *testing.T) {
	cleaned := ParseToolCallArguments(`{"call_purpose":"probe port","command":"echo ok"}`)
	if strings.Contains(cleaned, "call_purpose") {
		t.Fatalf("cleaned should omit call_purpose: %q", cleaned)
	}
	if !strings.Contains(cleaned, "command") {
		t.Fatalf("cleaned should keep command: %q", cleaned)
	}
}

func TestReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"a/b.txt","content":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "read_file", `{"path":"a/b.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "文件总行数: 1") || readFileBody(out) != "hello" {
		t.Fatalf("read = %q err=%v", out, err)
	}
}

func TestPathEscapeDenied(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Execute(context.Background(), "read_file", `{"path":"../outside.txt"}`)
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestBashRun(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "bash_run", `{"command":"echo ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[BASH_RESULT]") || !strings.Contains(out, "ok") {
		t.Fatalf("out = %q", out)
	}
}

func TestSearchReplace(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"x.txt","content":"foo bar foo"}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "search_replace", `{"path":"x.txt","old_string":"foo","new_string":"baz","replace_all":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "成功: 是") || !strings.Contains(out, "替换次数: 2") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "@@ 共 2 处相同替换") || !strings.Contains(out, "-foo") || !strings.Contains(out, "+baz") {
		t.Fatalf("expected compact preview, out = %q", out)
	}
	read, err := reg.Execute(ctx, "read_file", `{"path":"x.txt"}`)
	if err != nil || readFileBody(read) != "baz bar baz" {
		t.Fatalf("read = %q err=%v", read, err)
	}
}

func TestSearchReplace_singleMatchOmitsPreview(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"a.txt","content":"hello world"}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "search_replace", `{"path":"a.txt","old_string":"world","new_string":"there"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "成功: 是\n替换次数: 1"
	if out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
}

func TestSearchReplace_multilineIncludesPreview(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"b.txt","content":"line1\nline2\nline3"}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "search_replace", `{"path":"b.txt","old_string":"line2","new_string":"line2a\nline2b"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "成功: 是") || !strings.Contains(out, "替换次数: 1") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "---\n") || !strings.Contains(out, "-line2") || !strings.Contains(out, "+line2a") {
		t.Fatalf("expected multiline preview, out = %q", out)
	}
	if strings.Contains(out, "-line3") {
		t.Fatalf("should not include unrelated lines, out = %q", out)
	}
}

func TestSearchReplace_failKeepsDiagnostics(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = reg.Execute(ctx, "write_file", `{"path":"c.txt","content":"aa\nbb\naa"}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "search_replace", `{"path":"c.txt","old_string":"aa","new_string":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "成功: 否") || !strings.Contains(out, "匹配 2 处") || !strings.Contains(out, "匹配行:") {
		t.Fatalf("out = %q", out)
	}
}

func TestResolveWorkspaceRootCreatesDir(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "dagents-test-root")
	_ = os.RemoveAll(dir)
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	if reg.workspaceRoot != dir {
		t.Fatalf("workspaceRoot = %q", reg.workspaceRoot)
	}
}
