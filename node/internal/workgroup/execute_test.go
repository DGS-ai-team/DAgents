package workgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceExecutorReadGlobWrite(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "README"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bindings := NewMemoryBindingStore()
	b := WorkerBinding{
		MemberID:        "mb_01h00000000000000000000002",
		WorkgroupID:     "wg_01h00000000000000000000001",
		HomeNodeID:      "node_b",
		WorkspacePath:   ws,
		ToolAllowNames:  []string{"read_file", "glob_files", "write_file"},
		LeaseEpoch:      1,
		MemberGeneration: 1,
	}
	if err := bindings.Put(b); err != nil {
		t.Fatal(err)
	}
	exec := NewWorkspaceToolExecutor(bindings)

	readOut, err := exec(ToolCommand{
		MemberID:      b.MemberID,
		ToolName:      "read_file",
		ArgumentsJSON: `{"path":"README"}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(readOut, "Demo") {
		t.Fatalf("read_file out=%s", readOut)
	}

	globOut, err := exec(ToolCommand{
		MemberID:      b.MemberID,
		ToolName:      "glob_files",
		ArgumentsJSON: `{"directory":".","glob_pattern":"*"}`,
	})
	if err != nil {
		t.Fatalf("glob_files: %v", err)
	}
	if !strings.Contains(globOut, "README") {
		t.Fatalf("glob_files out=%s", globOut)
	}

	writeOut, err := exec(ToolCommand{
		MemberID:      b.MemberID,
		ToolName:      "write_file",
		ArgumentsJSON: `{"path":"notes/a.txt","content":"hello"}`,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(writeOut, "wrote") {
		t.Fatalf("write_file out=%s", writeOut)
	}
	raw, err := os.ReadFile(filepath.Join(ws, "notes", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello" {
		t.Fatalf("wrote %q", raw)
	}
}

func TestWorkspaceExecutorRejectsEscape(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	bindings := NewMemoryBindingStore()
	b := WorkerBinding{
		MemberID:       "mb_01h00000000000000000000002",
		WorkgroupID:    "wg_01h00000000000000000000001",
		HomeNodeID:     "node_b",
		WorkspacePath:  ws,
		ToolAllowNames: []string{"read_file", "write_file"},
	}
	if err := bindings.Put(b); err != nil {
		t.Fatal(err)
	}
	exec := NewWorkspaceToolExecutor(bindings)
	_, err := exec(ToolCommand{
		MemberID:      b.MemberID,
		ToolName:      "read_file",
		ArgumentsJSON: `{"path":"../outside"}`,
	})
	if err == nil {
		t.Fatal("expected escape error")
	}
	we, ok := err.(*Error)
	if !ok || we.Code != CodeNotAuthorized {
		t.Fatalf("err=%v", err)
	}

	_, err = exec(ToolCommand{
		MemberID:      b.MemberID,
		ToolName:      "bash_run",
		ArgumentsJSON: `{"command":"echo hi"}`,
	})
	if err == nil {
		t.Fatal("expected unsupported tool")
	}
}
