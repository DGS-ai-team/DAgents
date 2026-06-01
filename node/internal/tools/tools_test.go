package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllToolDefinitionsHaveRequired(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range reg.Definitions() {
		params := def.Function.Parameters
		if params == nil {
			t.Fatalf("tool %q missing parameters", def.Function.Name)
		}
		if _, ok := params["required"]; !ok {
			t.Fatalf("tool %q parameters missing required array", def.Function.Name)
		}
		req, ok := params["required"].([]string)
		if !ok {
			t.Fatalf("tool %q required is not []string: %T", def.Function.Name, params["required"])
		}
		_ = req
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
	read, err := reg.Execute(ctx, "read_file", `{"path":"x.txt"}`)
	if err != nil || readFileBody(read) != "baz bar baz" {
		t.Fatalf("read = %q err=%v", read, err)
	}
}

func TestResolveFSRootCreatesDir(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "dagents-test-root")
	_ = os.RemoveAll(dir)
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	if reg.fsRoot != dir {
		t.Fatalf("fsRoot = %q", reg.fsRoot)
	}
}
