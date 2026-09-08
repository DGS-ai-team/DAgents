package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

type goalApprovalLLM struct {
	called  chan struct{}
	release chan struct{}
	calls   int
}

func (m *goalApprovalLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}
func (m *goalApprovalLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (m *goalApprovalLLM) StreamChat(ctx context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	m.calls++
	select {
	case m.called <- struct{}{}:
	default:
	}
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4})
	}
	if m.calls > 1 {
		if h.OnDelta != nil {
			h.OnDelta("approved")
		}
		return llm.ChatResult{Content: "approved", FinishReason: "stop"}, nil
	}
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			if h.OnDelta != nil {
				h.OnDelta("approved")
			}
			return llm.ChatResult{Content: "approved", FinishReason: "stop"}, nil
		}
	}
	select {
	case <-m.release:
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "call-goal-approval", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"approved.txt","content":"ok"}`}}}, FinishReason: "tool_calls"}, nil
}

func TestGoalApprovalHTTPHydrateResume(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	fake := &goalApprovalLLM{called: make(chan struct{}, 4), release: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(fake))
	defer srv.Close()
	if srv.agents == nil {
		t.Fatal("agent store unavailable")
	}
	rec := store.AgentRecord{AgentID: "approval-goal-agent", DisplayName: "approval goal", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","workspace":{"mode":"private"},"defaults":{"tools":{"enabled_groups":["fs"]}}}`), RuntimeRevision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := srv.agents.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	setup := doGoalRequest(srv, http.MethodPatch, "/v1/setup/config", []byte(`{"agent":{"name":"approval","description":"approval"},"user":{"preferred_name":"QA"},"onboarding":{"node_profile_completed":true}}`))
	if setup.Code != 200 {
		t.Fatalf("setup=%d", setup.Code)
	}
	body, _ := json.Marshal(map[string]any{"objective": "write a file", "acceptance": "approved", "agent_id": rec.AgentID, "enabled": true, "max_runs": 1, "token_budget": 10000, "turn_token_budget": 5000})
	created := createGoalViaAutonomy(srv, rec.AgentID, body)
	if created.Code != http.StatusOK {
		t.Fatalf("create=%d", created.Code)
	}
	var g struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &g); err != nil || g.ID == "" || g.SessionID == "" {
		t.Fatalf("goal=%s", created.Body.String())
	}
	workspace, ok := srv.sessions.SessionWorkspaceRoot(g.SessionID)
	if !ok || workspace == "" || !filepath.IsAbs(workspace) {
		t.Fatalf("invalid goal workspace=%q", workspace)
	}
	approvedPath := filepath.Join(workspace, "approved.txt")
	_ = os.Remove(approvedPath)
	if x := doGoalRequest(srv, http.MethodPost, "/v1/goals/"+g.ID+"/wake", nil); x.Code != 202 {
		t.Fatalf("wake=%d", x.Code)
	}
	select {
	case <-fake.called:
	case <-time.After(3 * time.Second):
		t.Fatal("model not called")
	}
	close(fake.release)
	var first, second struct {
		TurnState struct {
			TurnID string `json:"turn_id"`
			Phase  string `json:"phase"`
		} `json:"turn_state"`
		PendingHITL map[string]any `json:"pending_hitl"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r := doGoalRequest(srv, http.MethodGet, "/v1/agents/"+g.SessionID+"/hydrate", nil)
		if r.Code == 200 {
			_ = json.Unmarshal(r.Body.Bytes(), &first)
			if first.PendingHITL != nil {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if first.TurnState.TurnID == "" || first.PendingHITL == nil {
		t.Fatalf("pending hydrate=%+v", first)
	}
	if _, err := os.Stat(approvedPath); !os.IsNotExist(err) {
		t.Fatalf("approval tool ran before resume: stat err=%v", err)
	}
	r := doGoalRequest(srv, http.MethodGet, "/v1/agents/"+g.SessionID+"/hydrate", nil)
	if r.Code != 200 {
		t.Fatalf("hydrate2=%d", r.Code)
	}
	if err := json.Unmarshal(r.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.TurnState.TurnID != first.TurnState.TurnID {
		t.Fatalf("runtime replaced: %q -> %q", first.TurnState.TurnID, second.TurnState.TurnID)
	}
	ordinary := doGoalRequest(srv, http.MethodPost, "/v1/messages", []byte(`{"agent_id":"`+g.SessionID+`","content":"ordinary"}`))
	if ordinary.Code != http.StatusConflict {
		t.Fatalf("ordinary=%d %s", ordinary.Code, ordinary.Body.String())
	}
	resume := doGoalRequest(srv, http.MethodPost, "/v1/messages", []byte(`{"agent_id":"`+g.SessionID+`","request_type":"resume","resume_value":{"type":"selection","approved":["call-goal-approval"],"rejected":[]}}`))
	if resume.Code != 200 {
		t.Fatalf("resume=%d %s", resume.Code, resume.Body.String())
	}
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("resume did not continue model")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rs := srv.goalStore.Runs(g.ID)
		if len(rs) == 1 && rs[0].FinishedAt != nil {
			if rs[0].TurnID == "" || rs[0].TokensUsed <= 0 || rs[0].Status != "completed" {
				t.Fatalf("run=%+v", rs[0])
			}
			data, err := os.ReadFile(approvedPath)
			if err != nil || string(data) != "ok" {
				if view, viewErr := srv.sessions.GetHydrateView(g.SessionID); viewErr == nil {
					t.Logf("transcript=%+v", view.Transcript)
				}
				t.Fatalf("approved file=%q err=%v", data, err)
			}
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("run did not finish: %+v", srv.goalStore.Runs(g.ID))
}
