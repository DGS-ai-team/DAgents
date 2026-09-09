package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/events"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// eventCheckpointLLM makes the first managed run checkpoint an event wait.
// Subsequent odd calls do the same, allowing the test to prove that the event
// probe started a second managed Run rather than merely creating an intent.
type eventCheckpointLLM struct{ calls atomic.Int32 }

func (f *eventCheckpointLLM) NormalizeAssistant(existing []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, m)
}
func (f *eventCheckpointLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (f *eventCheckpointLLM) StreamChat(_ context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	call := f.calls.Add(1)
	if call%2 == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("checkpoint-%d", call), Type: "function", Function: llm.ToolCallFunction{
			Name:      "goal_checkpoint",
			Arguments: `{"summary":"wait for file","decision":{"outcome":"progress","summary":"wait","next_action":"event","reason":"observe","expected_progress":"file changes","event":{"source_id":"files","filter":{"equals":{"files":1}}}}}`,
		}}}, FinishReason: "tool_calls"}, nil
	}
	if h.OnDelta != nil {
		h.OnDelta("observed")
	}
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	return llm.ChatResult{Content: "observed", FinishReason: "stop"}, nil
}

func TestGoalCheckpointEventChainThroughAPIProbe(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	cfg.Onboarding.NodeProfileCompleted = true
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err = settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	settings.Close()
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	agentID := "event-chain-agent"
	if err = as.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = as.SaveAgentPolicy(context.Background(), store.AgentPolicyRecord{AgentID: agentID, Tools: map[string]string{"goal_checkpoint": "never"}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.WriteFile(filepath.Join(root, "a"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	as.Close()
	srv := NewServer(cfg, nil, WithLLM(&eventCheckpointLLM{}))
	defer srv.Close()
	srv.triggerSched.Stop()
	if err = srv.eventStore.RegisterSource(events.SourceRegistration{SourceID: "files", OwnerAgentID: agentID, Revision: 1, Root: root, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	created := createGoalViaAutonomy(srv, agentID, []byte(`{"objective":"watch","acceptance":"file changes","agent_id":"event-chain-agent","enabled":true}`))
	if created.Code != http.StatusOK {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var goal struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(created.Body.Bytes(), &goal); err != nil || goal.ID == "" {
		t.Fatalf("goal=%s err=%v", created.Body.String(), err)
	}
	wake := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wake, httptest.NewRequest(http.MethodPost, "/v1/goals/"+goal.ID+"/wake", nil))
	if wake.Code != http.StatusAccepted {
		t.Fatalf("wake=%d %s", wake.Code, wake.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	var intent goals.ScheduleIntent
	for {
		var intentErr error
		intent, intentErr = srv.goalStore.GetScheduleIntent(goal.ID, "goal")
		if intentErr == nil && intent.State == "pending" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("checkpoint did not finalize to pending event intent: err=%v intent=%+v", intentErr, intent)
		}
		time.Sleep(20 * time.Millisecond)
	}
	firstRuns := srv.goalStore.Runs(goal.ID)
	if len(firstRuns) != 1 || firstRuns[0].FinishedAt == nil {
		t.Fatalf("first managed run not finalized: runs=%+v", firstRuns)
	}
	if intent.Decision.NextAction != "event" || intent.Decision.Event == nil || intent.Decision.Event.SourceID != "files" {
		t.Fatalf("intent decision=%+v", intent.Decision)
	}
	// Project the pending intent and establish the probe's initial digest.
	srv.triggerSched.RunOnceForTest(context.Background(), time.Now().UTC())
	if err = os.WriteFile(filepath.Join(root, "a"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	srv.triggerSched.RunOnceForTest(context.Background(), time.Now().UTC())
	deadline = time.Now().Add(5 * time.Second)
	for len(srv.goalStore.Runs(goal.ID)) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(srv.goalStore.Runs(goal.ID)); got < 2 {
		t.Fatalf("event probe did not trigger managed Run; runs=%d", got)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		runs := srv.goalStore.Runs(goal.ID)
		if len(runs) == 2 && runs[1].FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second managed run was not finalized: runs=%+v", runs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A second tick without another source change must not replay the event.
	srv.triggerSched.RunOnceForTest(context.Background(), time.Now().UTC())
	if got := len(srv.goalStore.Runs(goal.ID)); got != 2 {
		t.Fatalf("unchanged event probe replayed run: runs=%d", got)
	}
}
