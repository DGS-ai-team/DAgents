package manage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type bashDateComplianceMock struct {
	calls int
}

func (m *bashDateComplianceMock) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.calls++
	if m.calls == 1 {
		return llm.ChatResult{
			ToolCalls: []llm.ToolCall{{
				ID:   "call-date-exec",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "bash_run",
					Arguments: `{"command":"date && date -u"}`,
				},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	text := "APPROVED | rule=R-TIME-01 | mock time check ok"
	mock := &llm.MockClient{FixedReply: text}
	_, _ = mock.StreamChat(ctx, req, handler)
	return llm.ChatResult{Content: text, FinishReason: "stop"}, nil
}

func (m *bashDateComplianceMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *bashDateComplianceMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

// TestComplianceExecutor_hitlRelayExecutesBashRun 端到端：requires_input → caller_input 续跑 → bash 完成 → Task completed。
func TestComplianceExecutor_hitlRelayExecutesBashRun(t *testing.T) {
	cfg, mgr := writeComplianceHitlFixtures(t)

	var mu sync.Mutex
	callerResume := map[string]any(nil)
	var replyBodies []map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/ack"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "t-hitl"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reply"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			replyBodies = append(replyBodies, body)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "t-hitl", "status": body["status"]})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/caller_input"):
			mu.Lock()
			resume := callerResume
			mu.Unlock()
			ready := resume != nil
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready":        ready,
				"resume_value": resume,
			})
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()
	cfg.Manage.URL = srv.URL

	go func() {
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		callerResume = map[string]any{
			"type":        "selection",
			"approval_id": "appr-test",
			"approved":    []any{"call-date-exec"},
			"rejected":    []any{},
		}
		mu.Unlock()
	}()

	ex := NewComplianceExecutor(cfg, mgr, nil)
	content := "【合规咨询】请查看当前系统时间，执行 date 命令并将输出回复"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := ex.HandleTask(ctx, InboxTask{TaskID: "t-hitl", FromAgentID: "node-b", Content: content}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(replyBodies) < 2 {
		t.Fatalf("reply bodies=%d", len(replyBodies))
	}
	last := replyBodies[len(replyBodies)-1]
	if last["status"] != "completed" {
		t.Fatalf("final status=%q bodies=%v", last["status"], replyBodies)
	}
	if !strings.Contains(last["result_text"], "APPROVED") {
		t.Fatalf("result=%q", last["result_text"])
	}
	if replyBodies[0]["status"] != "requires_input" {
		t.Fatalf("first reply status=%q", replyBodies[0]["status"])
	}
}

func writeComplianceHitlFixtures(t *testing.T) (*config.Config, *session.Manager) {
	t.Helper()
	dir := t.TempDir()
	promptDir := filepath.Join(dir, "prompt_context")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "custom.md"), []byte(sampleComplianceCustom), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	cfg := &config.Config{
		AgentID: "compliance-a",
		Agent: config.AgentConfig{
			Role: "compliance",
		},
		FSRoot: dir,
		Manage: config.ManageConfig{Enabled: true},
	}
	policyDir := filepath.Join(cfg.FSRoot, "policy")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "tool.approval.txt"), []byte("bash_run=always\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pol, err := policy.LoadFromDir(policyDir)
	if err != nil {
		t.Fatal(err)
	}
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(cfg.FSRoot, 30)
	if err != nil {
		t.Fatal(err)
	}
	mock := &bashDateComplianceMock{}
	mgr := session.NewManager(cfg.AgentID, hub, mock, reg, pol, nil, session.TurnOptions{
		RuntimeDir:    cfg.FSRoot,
		SkillsEnabled: false,
	}, logx.Discard())
	t.Cleanup(mgr.Stop)
	return cfg, mgr
}
