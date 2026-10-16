package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type turnFinishFixtureClient struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	first    []llm.ToolCall
	steps    [][]llm.ToolCall
}

func (c *turnFinishFixtureClient) StreamChat(_ context.Context, request llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if len(c.steps) > 0 && len(c.requests) <= len(c.steps) {
		calls := c.steps[len(c.requests)-1]
		return llm.ChatResult{ToolCalls: append([]llm.ToolCall(nil), calls...), FinishReason: "tool_calls"}, nil
	}
	if len(c.requests) == 1 && len(c.steps) == 0 {
		return llm.ChatResult{ToolCalls: append([]llm.ToolCall(nil), c.first...), FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "已整理结果，并说明未完成部分。", FinishReason: "stop"}, nil
}

func (*turnFinishFixtureClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*turnFinishFixtureClient) NormalizeAssistant(existing []llm.Message, message llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, message)
}

func (c *turnFinishFixtureClient) capturedRequests() []llm.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]llm.ChatRequest(nil), c.requests...)
}

func newAgentTurnFixture(t *testing.T, maxSteps int, client *turnFinishFixtureClient) (*session.Manager, *stream.Hub, *store.SQLiteStore, *session.Session, Built) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{NodeID: "node-1", RuntimeRoot: filepath.Join(root, "runtime")}
	cfg.ApplyDefaults()
	built, err := Build(BuildParams{
		NodeCFG: cfg,
		AgentID: "agent-1",
		Snapshot: Snapshot{Defaults: map[string]any{
			"llm":   map[string]any{"max_steps": float64(maxSteps)},
			"tools": map[string]any{"enabled_groups": []any{"fs", "bash"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	availableTools := map[string]bool{}
	for _, definition := range built.Registry.Definitions() {
		availableTools[definition.Function.Name] = true
	}
	if !availableTools["read_file"] || !availableTools["bash_run"] {
		_ = built.Close()
		t.Fatalf("enabled_groups fs,bash did not register expected tools: %v", availableTools)
	}
	st, err := store.Open(filepath.Join(root, "turns.db"))
	if err != nil {
		_ = built.Close()
		t.Fatal(err)
	}
	hub := stream.NewHub(128, logx.Discard())
	manager := session.NewManager("agent-1", hub, client, built.Registry,
		policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever, "bash_run": policy.ModeNever}}),
		st, built.TurnOptions, logx.Discard())
	created, _, err := manager.CreateWithOptionsAndLLM("agent-1", built.TurnOptions, built.Registry, nil, client, "agent-1")
	if err != nil {
		manager.Stop()
		_ = built.Close()
		_ = st.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		manager.Stop()
		_ = built.Close()
		_ = st.Close()
	})
	return manager, hub, st, created, built
}

func awaitTurnFinished(t *testing.T, events <-chan stream.Event) []stream.Event {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	var received []stream.Event
	finished, terminalState := false, false
	for {
		select {
		case event := <-events:
			received = append(received, event)
			if event.Type == "turn_finished" && event.Data["turn_complete"] == true {
				finished = true
			}
			if event.Type == "turn_state" && event.Data["terminal"] == true {
				terminalState = true
			}
			if finished && terminalState {
				return received
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for terminal turn_finished; events=%+v", received)
		}
	}
}

func TestAgentMaxStepsReservesToolFreeFinalSummary(t *testing.T) {
	client := &turnFinishFixtureClient{first: []llm.ToolCall{{
		ID: "read-one", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"input.txt"}`},
	}}}
	manager, hub, st, created, built := newAgentTurnFixture(t, 1, client)
	if !built.TurnOptions.Budget.ReserveFinalSummary {
		t.Fatal("Agent builder did not reserve a final summary step")
	}
	if err := os.WriteFile(filepath.Join(built.WorkspaceRoot, "input.txt"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	events := hub.Subscribe(hub.CurrentSeq())
	defer hub.Unsubscribe(events)
	if _, err := manager.EnqueueMessage(context.Background(), created.ID, "message", "read and summarize", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	seen := awaitTurnFinished(t, events)
	requests := client.capturedRequests()
	if len(requests) != 2 {
		t.Fatalf("model requests=%d want tool step plus summary", len(requests))
	}
	if len(requests[1].Tools) != 0 {
		t.Fatalf("summary step exposed tools: %d", len(requests[1].Tools))
	}
	if !containsText(requests[1].SystemPrompt, "只允许进行一次无工具的最终收尾") {
		t.Fatalf("summary system prompt lacks reserved instruction: %q", requests[1].SystemPrompt)
	}
	var finished stream.Event
	var terminalState stream.Event
	for _, event := range seen {
		if event.Type == "turn_finished" {
			finished = event
		}
		if event.Type == "turn_state" && event.Data["terminal"] == true {
			terminalState = event
		}
	}
	if finished.Type == "" || finished.Data["turn_id"] == "" || finished.Data["turn_generation"] == nil {
		t.Fatalf("turn_finished missing turn identity: %+v", finished)
	}
	if terminalState.Type == "" || terminalState.Data["turn_status"] != string(turn.TurnStatusCompleted) {
		t.Fatalf("missing completed turn_state after summary: %+v", seen)
	}
	stored, err := st.ListTurnEvents(context.Background(), created.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var summaryStep, completed bool
	for _, event := range stored {
		if event.EventType == turn.EventStepStarted {
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["final_summary"] == true {
				summaryStep = true
			}
		}
		completed = completed || event.EventType == turn.EventTurnCompleted
	}
	if !summaryStep || !completed {
		t.Fatalf("durable summary/completion facts missing: %v", eventTypes(stored))
	}
}

func TestFailedLastToolInBatchIsTerminalAndPersisted(t *testing.T) {
	shell := "bash"
	if runtime.GOOS == "windows" {
		shell = "powershell"
	}
	readOK := llm.ToolCall{ID: "read-ok", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"input.txt"}`}}
	bashFailure := llm.ToolCall{ID: "bash-fail", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: fmt.Sprintf(`{"command":"exit 1","shell_type":%q}`, shell)}}
	client := &turnFinishFixtureClient{steps: [][]llm.ToolCall{{readOK, bashFailure}}}
	manager, hub, st, created, built := newAgentTurnFixture(t, 4, client)
	if err := os.WriteFile(filepath.Join(built.WorkspaceRoot, "input.txt"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	events := hub.Subscribe(hub.CurrentSeq())
	defer hub.Unsubscribe(events)
	if _, err := manager.EnqueueMessage(context.Background(), created.ID, "message", "read both", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	seen := awaitTurnFinished(t, events)
	resultStatus := map[string]string{}
	failedShellOutput := ""
	for _, event := range seen {
		if event.Type == "tool_result" {
			id, _ := event.Data["tool_call_id"].(string)
			status, _ := event.Data["status"].(string)
			resultStatus[id] = status
			if id == "bash-fail" {
				failedShellOutput, _ = event.Data["content"].(string)
			}
		}
	}
	if resultStatus["read-ok"] != "succeeded" || resultStatus["bash-fail"] != "failed" {
		t.Fatalf("tool results lack terminal success/failure statuses: %v", resultStatus)
	}
	if !strings.Contains(failedShellOutput, "[BASH_RESULT] exit=1") || !strings.Contains(failedShellOutput, "exit_code=1") {
		t.Fatalf("bash_run did not return a real shell exit-1 result: %q", failedShellOutput)
	}
	if jobs := built.Registry.SessionToolJobCounts(created.ID); jobs.Running != 0 || len(jobs.RunningCallIDs) != 0 {
		t.Fatalf("failed bash left a live sync shell job: %+v", jobs)
	}
	stored, err := st.ListTurnEvents(context.Background(), created.ID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	failedCalls, recordedResults, settled, completed := 0, 0, false, false
	for _, event := range stored {
		switch event.EventType {
		case turn.EventToolExecutionFailed:
			failedCalls++
		case turn.EventToolResultRecorded:
			recordedResults++
		case turn.EventToolBatchSettled:
			settled = true
		case turn.EventTurnCompleted:
			completed = true
		}
	}
	if failedCalls != 1 || recordedResults != 2 || !settled || !completed {
		t.Fatalf("failed batch lifecycle incomplete: failed=%d results=%d settled=%t completed=%t events=%v", failedCalls, recordedResults, settled, completed, eventTypes(stored))
	}
	view, err := manager.GetHydrateView(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundFailedTranscript := false
	for _, entry := range view.Transcript {
		if entry["kind"] != "tool_result" {
			continue
		}
		data, _ := entry["data"].(map[string]any)
		if data["tool_call_id"] == "bash-fail" && data["status"] == "failed" {
			foundFailedTranscript = true
		}
	}
	if !foundFailedTranscript {
		t.Fatalf("hydrated transcript did not retain failed result: %#v", view.Transcript)
	}
}

func TestFailedLastToolInLaterStepContinuesAndPersists(t *testing.T) {
	shell := "bash"
	if runtime.GOOS == "windows" {
		shell = "powershell"
	}
	client := &turnFinishFixtureClient{steps: [][]llm.ToolCall{
		{{ID: "first-step", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"input.txt"}`}}},
		{{ID: "last-step-fail", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: fmt.Sprintf(`{"command":"exit 1","shell_type":%q}`, shell)}}},
	}}
	manager, hub, st, created, built := newAgentTurnFixture(t, 5, client)
	if err := os.WriteFile(filepath.Join(built.WorkspaceRoot, "input.txt"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	events := hub.Subscribe(hub.CurrentSeq())
	defer hub.Unsubscribe(events)
	if _, err := manager.EnqueueMessage(context.Background(), created.ID, "message", "read, then run shell", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	seen := awaitTurnFinished(t, events)
	requests := client.capturedRequests()
	if len(requests) != 3 {
		t.Fatalf("model requests=%d want two tool steps plus final response", len(requests))
	}
	failedResult := false
	failedShellOutput := ""
	for _, event := range seen {
		if event.Type == "tool_result" && event.Data["tool_call_id"] == "last-step-fail" && event.Data["status"] == "failed" {
			failedResult = true
			failedShellOutput, _ = event.Data["content"].(string)
		}
	}
	if !failedResult {
		t.Fatalf("last-step bash failure missing from SSE: %+v", seen)
	}
	if !strings.Contains(failedShellOutput, "[BASH_RESULT] exit=1") || !strings.Contains(failedShellOutput, "exit_code=1") {
		t.Fatalf("last-step bash_run did not return a real shell exit-1 result: %q", failedShellOutput)
	}
	if jobs := built.Registry.SessionToolJobCounts(created.ID); jobs.Running != 0 || len(jobs.RunningCallIDs) != 0 {
		t.Fatalf("last-step failed bash left a live sync shell job: %+v", jobs)
	}
	stored, err := st.ListTurnEvents(context.Background(), created.ID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	failedCalls, recordedResults, settled, completed := 0, 0, 0, false
	for _, event := range stored {
		switch event.EventType {
		case turn.EventToolExecutionFailed:
			failedCalls++
		case turn.EventToolResultRecorded:
			recordedResults++
		case turn.EventToolBatchSettled:
			settled++
		case turn.EventTurnCompleted:
			completed = true
		}
	}
	if failedCalls != 1 || recordedResults != 2 || settled != 2 || !completed {
		t.Fatalf("multi-step failure lifecycle incomplete: failed=%d results=%d settled=%d completed=%t events=%v", failedCalls, recordedResults, settled, completed, eventTypes(stored))
	}
}

func eventTypes(events []turn.TurnEventEnvelope) []turn.EventType {
	types := make([]turn.EventType, len(events))
	for i, event := range events {
		types[i] = event.EventType
	}
	return types
}

func containsText(text, part string) bool {
	return strings.Contains(text, part)
}
