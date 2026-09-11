package childagent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type fakeHost struct {
	mu         sync.Mutex
	parentOK   bool
	spawned    []string
	tasks      map[string]string
	childHitl  map[string]bool
	parentHitl bool
}

type memoryRunRepository struct {
	records []RunRecord
}

func (r *memoryRunRepository) SaveChildRun(_ context.Context, record RunRecord) error {
	for i := range r.records {
		if r.records[i].ChildAgentID == record.ChildAgentID {
			r.records[i] = record
			return nil
		}
	}
	r.records = append(r.records, record)
	return nil
}

func (r *memoryRunRepository) ListChildRuns(_ context.Context, parentID string, _ int) ([]RunRecord, error) {
	out := make([]RunRecord, 0, len(r.records))
	for _, record := range r.records {
		if parentID == "" || record.ParentAgentID == parentID {
			out = append(out, record)
		}
	}
	return out, nil
}

func (f *fakeHost) ParentSessionActive(parentID string) bool { return f.parentOK }
func (f *fakeHost) SpawnChild(spec SpawnSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func (f *fakeHost) spawnedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.spawned...)
}

func TestCreateTemporaryAgentIsSynchronous(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	host := &fakeHost{parentOK: true}
	m.BindHost(host)

	done := make(chan string, 1)
	go func() {
		out, err := m.HandleCreate(context.Background(), "sess-parent", `{"task":"do work","purpose":"demo"}`)
		if err != nil {
			done <- "ERROR: " + err.Error()
			return
		}
		done <- out
	}()
	deadline := time.After(time.Second)
	for len(host.spawnedIDs()) == 0 {
		select {
		case <-deadline:
			t.Fatal("child was not spawned")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	m.OnChildSettled(host.spawnedIDs()[0], "done", 1)
	select {
	case out := <-done:
		if out == "" || out[0] != '{' || !strings.Contains(out, `"kind":"result"`) {
			t.Fatalf("unexpected synchronous output: %s", out)
		}
	case <-time.After(time.Second):
		t.Fatal("create did not wait for terminal child result")
	}
}

func TestCreateTemporaryAgentReturnsTerminalFailure(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	host := &fakeHost{parentOK: true}
	m.BindHost(host)

	done := make(chan string, 1)
	go func() {
		out, err := m.HandleCreate(context.Background(), "sess-parent", `{"task":"fail","purpose":"failure"}`)
		if err != nil {
			done <- "ERROR: " + err.Error()
			return
		}
		done <- out
	}()
	deadline := time.After(time.Second)
	for len(host.spawnedIDs()) == 0 {
		select {
		case <-deadline:
			t.Fatal("child was not spawned")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	m.OnChildFailed(host.spawnedIDs()[0], "provider unavailable", 2)
	select {
	case out := <-done:
		if !strings.Contains(out, `"kind":"result"`) || !strings.Contains(out, `"status":"failed"`) {
			t.Fatalf("unexpected failure result: %s", out)
		}
	case <-time.After(time.Second):
		t.Fatal("create did not return terminal failure")
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

func TestSetRunRepositoryMarksOrphanedRunsInterrupted(t *testing.T) {
	repo := &memoryRunRepository{records: []RunRecord{{
		ChildAgentID:  "child-restarted",
		ParentAgentID: "parent-1",
		Status:        string(StatusActive),
		Phase:         "tool_executing",
		ProgressJSON:  []byte(`{"status":"active","phase":"tool_executing","revision":4}`),
		Revision:      4,
	}}}
	m := NewManager(Config{Enabled: true}, nil, "agent-1", nil)
	m.SetRunRepository(repo)

	if len(repo.records) != 1 || repo.records[0].Status != string(StatusInterrupted) {
		t.Fatalf("recovered record = %#v", repo.records)
	}
	var progress Progress
	if err := json.Unmarshal(repo.records[0].ProgressJSON, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Status != StatusInterrupted || progress.Phase != "interrupted" || progress.PendingApproval {
		t.Fatalf("recovered progress = %#v", progress)
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

func TestObserveChildEventTracksRecentToolActivities(t *testing.T) {
	hub := stream.NewHub(16, nil)
	m := NewManager(Config{Enabled: true}, hub, "agent-1", nil)
	agent := newActiveAgent("sess-parent", CreateInput{Purpose: "inspect files", MaxTurns: 8}, "child-1", time.Now().Add(time.Hour))
	agent.ToolCallID = "call-child-1"
	agent.Status = StatusActive
	agent.Progress.Status = StatusActive
	m.mu.Lock()
	m.activeByID[agent.ChildAgentID] = agent
	m.activeIDsByParent[agent.ParentAgentID] = []string{agent.ChildAgentID}
	m.mu.Unlock()

	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)
	m.ObserveChildEvent(agent.ChildAgentID, "tool_call", map[string]any{
		"tool_calls": []any{map[string]any{
			"id": "child-call-1",
			"function": map[string]any{
				"name":      "bash_run",
				"arguments": `{"command":"Write-Output child-ok"}`,
			},
		}},
	})

	progress := agent.ProgressSnapshot()
	if len(progress.RecentTools) != 1 {
		t.Fatalf("recent tools after call = %#v", progress.RecentTools)
	}
	if activity := progress.RecentTools[0]; activity.ToolName != "bash_run" || activity.Status != "running" || activity.InputSummary != "Write-Output child-ok" {
		t.Fatalf("recent tool after call = %#v", activity)
	}

	m.ObserveChildEvent(agent.ChildAgentID, "tool_result", map[string]any{
		"tool_call_id": "child-call-1",
		"tool_name":    "bash_run",
		"status":       "succeeded",
		"arguments":    map[string]any{"command": "Write-Output child-ok"},
		"content":      "child-ok\nsecond line",
	})

	progress = agent.ProgressSnapshot()
	if len(progress.RecentTools) != 1 {
		t.Fatalf("recent tools after result = %#v", progress.RecentTools)
	}
	activity := progress.RecentTools[0]
	if activity.Status != "succeeded" || activity.OutputPreview != "child-ok" || activity.FinishedAt.IsZero() {
		t.Fatalf("recent tool after result = %#v", activity)
	}

	m.ObserveChildEvent(agent.ChildAgentID, "tool_call", map[string]any{
		"tool_calls": []any{map[string]any{
			"id": "child-call-secret",
			"function": map[string]any{
				"name":      "custom_tool",
				"arguments": `{"api_key":"secret-value","scope":"public"}`,
			},
		}},
	})
	progress = agent.ProgressSnapshot()
	if got := progress.RecentTools[len(progress.RecentTools)-1].InputSummary; got != "api_key=[已隐藏]" {
		t.Fatalf("sensitive activity input = %q", got)
	}

	for i := 1; i <= 9; i++ {
		m.ObserveChildEvent(agent.ChildAgentID, "tool_call", map[string]any{
			"tool_calls": []any{map[string]any{
				"id": "child-call-bounded-" + strconv.Itoa(i),
				"function": map[string]any{
					"name":      "bounded_tool_" + strconv.Itoa(i),
					"arguments": `{"value":"` + strconv.Itoa(i) + `"}`,
				},
			}},
		})
	}
	progress = agent.ProgressSnapshot()
	if len(progress.RecentTools) != maxRecentToolActivities {
		t.Fatalf("recent tools exceeded bound: got %d want %d", len(progress.RecentTools), maxRecentToolActivities)
	}
	if first := progress.RecentTools[0].ToolName; first != "bounded_tool_2" {
		t.Fatalf("oldest recent tool = %q", first)
	}
	if last := progress.RecentTools[len(progress.RecentTools)-1].ToolName; last != "bounded_tool_9" {
		t.Fatalf("newest recent tool = %q", last)
	}

	select {
	case ev := <-ch:
		if ev.Data["recent_tools"] == nil {
			t.Fatalf("progress event missing recent_tools: %#v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for recent tool progress event")
	}
}
