package childagent

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type fakeHost struct {
	parentOK   bool
	spawned    []string
	tasks      map[string]string
	childHitl  map[string]bool
	parentHitl bool
}

func (f *fakeHost) ParentSessionActive(parentID string) bool { return f.parentOK }
func (f *fakeHost) SpawnChild(spec SpawnSpec) error {
	f.spawned = append(f.spawned, spec.ChildSessionID)
	return nil
}
func (f *fakeHost) StopChild(string) {}
func (f *fakeHost) EnqueueChildTask(id, content string) error {
	if f.tasks == nil {
		f.tasks = map[string]string{}
	}
	f.tasks[id] = content
	return nil
}
func (f *fakeHost) ChildHasPendingHITL(id string) bool  { return f.childHitl[id] }
func (f *fakeHost) ParentHasPendingHITL(string) bool  { return f.parentHitl }
func (f *fakeHost) DeliverChildResume(string, map[string]any) error  { return nil }
func (f *fakeHost) DeliverParentResume(string, map[string]any) error { return nil }

func TestCreateTemporaryAgentAsync(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	host := &fakeHost{parentOK: true}
	m.BindHost(host)

	out, err := m.HandleCreate(context.Background(), "sess-parent", `{"task":"do work","purpose":"demo","wait":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" || out[0] != '{' {
		t.Fatalf("unexpected output: %s", out)
	}
	if len(host.spawned) != 1 {
		t.Fatalf("expected spawn, got %v", host.spawned)
	}
}

func TestResolveAllowedToolsRejectsParentOnly(t *testing.T) {
	if _, err := resolveAllowedTools([]string{"create_temporary_agent"}); err == nil {
		t.Fatal("expected error for parent-only tool")
	}
}

func TestResolveAllowedToolsDefault(t *testing.T) {
	got, err := resolveAllowedTools(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultChildAllowedTools()
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
