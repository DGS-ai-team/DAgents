package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
)

func TestHandbookNoopRecorderReportsRealWritesOnly(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	r, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	var got []HandbookNoop
	record := func(_ context.Context, noop HandbookNoop) error { got = append(got, noop); return nil }
	ctx := WithHandbookNoopRecorder(WithHandbookMaintenance(WithToolCallID(context.Background(), "call-noop")), record)
	if _, err := r.Execute(ctx, "write_file", `{"path":"handbook/a.md","content":"same"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(ctx, "write_file", `{"path":"handbook/a.md","content":"same"}`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ToolCallID != "call-noop" || got[0].ToolName != "write_file" || got[0].Path != "a.md" || got[0].Digest != handbookfs.Digest([]byte("same")) {
		t.Fatalf("write noops=%+v", got)
	}
	if _, err := r.Execute(ctx, "write_file", `{"path":"handbook/a.md","content":"changed"}`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("changed write recorded noop: %+v", got)
	}
	if _, err := r.Execute(ctx, "search_replace", `{"path":"handbook/a.md","old_string":"changed","new_string":"changed"}`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ToolName != "search_replace" || got[1].Path != "a.md" || got[1].Digest != handbookfs.Digest([]byte("changed")) {
		t.Fatalf("search noop=%+v", got)
	}
	if _, err := r.Execute(context.Background(), "write_file", `{"path":"handbook/a.md","content":"changed"}`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ordinary mode recorded noop: %+v", got)
	}
}

func TestHandbookNoopRecorderErrorPropagates(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	r, err := NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handbook, "a.md"), []byte("same"), 0644); err != nil {
		t.Fatal(err)
	}
	want := errors.New("recorder failed")
	ctx := WithHandbookNoopRecorder(WithHandbookMaintenance(context.Background()), func(context.Context, HandbookNoop) error { return want })
	if _, err := r.Execute(ctx, "write_file", `{"path":"handbook/a.md","content":"same"}`); !errors.Is(err, want) {
		t.Fatalf("write err=%v", err)
	}
	if _, err := r.Execute(ctx, "search_replace", `{"path":"handbook/a.md","old_string":"same","new_string":"same"}`); !errors.Is(err, want) {
		t.Fatalf("search err=%v", err)
	}
}
