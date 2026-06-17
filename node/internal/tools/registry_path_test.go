package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathAbsoluteInsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "inside.txt")
	if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, err := json.Marshal(map[string]string{"path": target})
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "read_file", string(args))
	if err != nil {
		t.Fatal(err)
	}
	if readFileBody(out) != "ok" {
		t.Fatalf("read = %q", out)
	}
}

func TestResolvePathAbsoluteOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	reg, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, err := json.Marshal(map[string]string{"path": target})
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "read_file", string(args))
	if err != nil {
		t.Fatal(err)
	}
	if readFileBody(out) != "outside" {
		t.Fatalf("read = %q", out)
	}
}

func TestResolvePathRelativeEscapeStillDenied(t *testing.T) {
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
