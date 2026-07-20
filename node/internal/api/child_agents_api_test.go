package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testConfigChildAgentsEnabled(t *testing.T) *config.Config {
	t.Helper()
	// 独立目录：session 后台写盘时，避免 testing.TempDir RemoveAll 竞态失败。
	root, err := os.MkdirTemp("", "dagents-child-api-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	cfg := &config.Config{
		NodeID: "ops-linux-01",
		Agent: config.AgentConfig{
			Role: "compliance",
		},
		FSRoot: filepath.Join(root, "runtime"),
		Compression: config.CompressionConfig{
			SilentTriggerTokens:   80000,
			BlockingTriggerTokens: 100000,
		},
	}
	cfg.ApplyDefaults()
	cfg.ChildAgents.Enabled = true
	return cfg
}

func newChildAgentTestServer(t *testing.T, llmClient llm.Client) *httptest.Server {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(testConfigChildAgentsEnabled(t), nil,
		WithLLM(llmClient), WithTools(reg), WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	// 须在 t.TempDir 清理前停止 session，避免后台仍写 prompt_context 导致 RemoveAll 失败。
	t.Cleanup(func() {
		ts.Close()
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
		time.Sleep(50 * time.Millisecond)
	})
	return ts
}

func createSession(t *testing.T, baseURL string) string {
	t.Helper()
	resp, err := http.Post(baseURL+"/v1/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var created createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.SessionID == "" {
		t.Fatal("empty session_id")
	}
	return created.SessionID
}

// TestChildAgentMockLLME2E 经 HTTP + mock LLM 走通 create(wait=true) 全链路。
func TestChildAgentMockLLME2E(t *testing.T) {
	mock := &llm.ChildAgentFlowMock{FinalReply: "HTTP 联调完成"}
	ts := newChildAgentTestServer(t, mock)
	defer ts.Close()

	parentID := createSession(t, ts.URL)

	streamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/streams?session_id="+parentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()

	msgBody := `{"session_id":"` + parentID + `","request_type":"message","content":"请委派子 Agent 检查 README"}`
	msgResp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	defer msgResp.Body.Close()
	if msgResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(msgResp.Body)
		t.Fatalf("message status=%d body=%s", msgResp.StatusCode, body)
	}

	deadline := time.After(10 * time.Second)
	reader := bufio.NewReader(streamResp.Body)
	var gotCreated, gotCompleted, gotDone bool
	var childID string
	var assistant strings.Builder

	for !(gotCreated && gotCompleted && gotDone) {
		select {
		case <-deadline:
			t.Fatalf("timeout created=%v completed=%v done=%v child=%q assistant=%q",
				gotCreated, gotCompleted, gotDone, childID, assistant.String())
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if gotDone {
				break
			}
			t.Fatal(err)
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var envelope struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			continue
		}
		t.Logf("sse %s", envelope.Type)
		switch envelope.Type {
		case "temporary_agent_created":
			gotCreated = true
			childID, _ = envelope.Data["child_session_id"].(string)
		case "temporary_agent_completed":
			gotCompleted = true
		case "assistant":
			if c, ok := envelope.Data["content"].(string); ok {
				assistant.WriteString(c)
			}
		case "done":
			gotDone = true
		}
	}

	if childID == "" {
		t.Fatal("missing child_session_id")
	}
	if !strings.Contains(assistant.String(), "HTTP 联调完成") {
		t.Fatalf("unexpected assistant: %q", assistant.String())
	}

	// 完成后列表应为空（记录已回收）
	listResp, err := http.Get(ts.URL + "/v1/sessions/" + parentID + "/child-agents")
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	var list childAgentListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected empty active list, got %d", len(list.Items))
	}
}

// TestChildAgentHTTPCancel 经 HTTP 取消进行中的子 Agent。
func TestChildAgentHTTPCancel(t *testing.T) {
	ts := newChildAgentTestServer(t, &sessionDelayedEchoMock{delay: 3 * time.Second})
	defer ts.Close()

	parentID := createSession(t, ts.URL)

	streamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/streams?session_id="+parentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()

	msgBody := `{"session_id":"` + parentID + `","request_type":"message","content":"启动异步子任务"}`
	msgResp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	msgResp.Body.Close()

	deadline := time.After(8 * time.Second)
	reader := bufio.NewReader(streamResp.Body)
	var childID string
	for childID == "" {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for temporary_agent_created")
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		var envelope struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal([]byte(payload), &envelope) == nil && envelope.Type == "temporary_agent_created" {
			childID, _ = envelope.Data["child_session_id"].(string)
		}
	}

	cancelBody := bytes.NewReader([]byte(`{"reason":"http test cancel"}`))
	req, err := http.NewRequest(http.MethodPost,
		ts.URL+"/v1/sessions/"+parentID+"/child-agents/"+childID+"/cancel",
		cancelBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	cancelResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelResp.Body.Close()
	if cancelResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(cancelResp.Body)
		t.Fatalf("cancel status=%d body=%s", cancelResp.StatusCode, body)
	}
	var cancelled childAgentCancelResponse
	if err := json.NewDecoder(cancelResp.Body).Decode(&cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("unexpected status: %+v", cancelled)
	}

	// 父 turn / 子 cancel 可能仍在收尾写盘；等 idle 后再让 t.TempDir 清理。
	waitSessionIdleDeadline(t, ts.URL, parentID, 8*time.Second)
}

// sessionDelayedEchoMock 供 api 包 cancel 测试使用（避免 import cycle）。
type sessionDelayedEchoMock struct {
	delay time.Duration
}

func (d *sessionDelayedEchoMock) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	select {
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	case <-time.After(d.delay):
	}
	if d.isParent(req.Tools) && !d.hasToolResult(req.Messages) {
		args := `{"task":"slow","purpose":"http cancel","wait":false}`
		tc := llm.ToolCall{
			ID: "call-create-async", Type: "function",
			Function: llm.ToolCallFunction{Name: "create_temporary_agent", Arguments: args},
		}
		return llm.ChatResult{ToolCalls: []llm.ToolCall{tc}, FinishReason: "tool_calls"}, nil
	}
	if d.isParent(req.Tools) {
		return llm.ChatResult{Content: "ok", FinishReason: "stop"}, nil
	}
	return (&llm.MockClient{}).StreamChat(ctx, req, handler)
}

func (d *sessionDelayedEchoMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "mock", nil
}

func (d *sessionDelayedEchoMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return (&llm.MockClient{}).NormalizeAssistant(existing, msg)
}

func (d *sessionDelayedEchoMock) isParent(toolDefs []tools.ToolDef) bool {
	for _, td := range toolDefs {
		if td.Function.Name == "create_temporary_agent" {
			return true
		}
	}
	return false
}

func (d *sessionDelayedEchoMock) hasToolResult(messages []llm.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			return true
		}
		if messages[i].Role == "user" {
			return false
		}
	}
	return false
}
