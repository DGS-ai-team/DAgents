package childagent

import (
	"context"
	"testing"
	"time"

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
	f.spawned = append(f.spawned, spec.ChildAgentID)
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
func (f *fakeHost) ChildHasPendingHITL(id string) bool               { return f.childHitl[id] }
func (f *fakeHost) ParentHasPendingHITL(string) bool                 { return f.parentHitl }
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

func TestResolveAllowedToolsRejectsSkills(t *testing.T) {
	if _, err := resolveAllowedTools([]string{ToolLoadSkills}); err == nil {
		t.Fatal("expected error for skills tool")
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

func TestObserveChildEventPublishesRefreshableProgress(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	agent := newActiveAgent("sess-parent", CreateInput{Purpose: "inspect files", MaxTurns: 8}, "child-1", time.Now().Add(time.Hour))
	agent.ToolCallID = "call-child-1"
	agent.Status = StatusActive
	agent.Progress.Status = StatusActive
	agent.Progress.MaxTurns = 8
	m.mu.Lock()
	m.activeByID[agent.ChildAgentID] = agent
	m.activeIDsByParent[agent.ParentAgentID] = []string{agent.ChildAgentID}
	m.childToParent[agent.ChildAgentID] = agent.ParentAgentID
	m.mu.Unlock()

	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)
	m.ObserveChildEvent(agent.ChildAgentID, "turn_state", map[string]any{
		"phase":      "tool_executing",
		"step_index": 2,
		"tool_executions": []any{map[string]any{
			"tool_call_id": "child-call-2",
			"tool_name":    "bash_run",
			"status":       "running",
		}},
	})

	select {
	case ev := <-ch:
		if ev.Type != EventTemporaryAgentProgress {
			t.Fatalf("event type = %q", ev.Type)
		}
		if ev.Data["tool_call_id"] != "call-child-1" {
			t.Fatalf("parent tool call id = %v", ev.Data["tool_call_id"])
		}
		if ev.Data["phase"] != "tool_executing" || ev.Data["current_tool"] != "bash_run" {
			t.Fatalf("progress payload = %#v", ev.Data)
		}
		if ev.Data["turn_count"] != 2 {
			t.Fatalf("turn count = %v", ev.Data["turn_count"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for child progress event")
	}

	snapshot := agent.ProgressSnapshot()
	if snapshot.Revision != 1 || snapshot.CurrentToolCallID != "child-call-2" {
		t.Fatalf("progress snapshot = %+v", snapshot)
	}
}
