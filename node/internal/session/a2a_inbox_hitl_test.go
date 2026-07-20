package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// bashDateInboxMock：首轮返回 bash_run(date)，审批续跑后返回合规结论文本。
type bashDateInboxMock struct {
	calls int
}

func (m *bashDateInboxMock) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.calls++
	if m.calls == 1 {
		return llm.ChatResult{
			ToolCalls: []llm.ToolCall{{
				ID:   "call-date-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "bash_run",
					Arguments: `{"command":"date && date -u"}`,
				},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	text := "APPROVED | rule=R-TIME-01 | 已核对系统时间"
	mock := &llm.MockClient{FixedReply: text}
	_, _ = mock.StreamChat(ctx, req, handler)
	return llm.ChatResult{Content: text, FinishReason: "stop"}, nil
}

func (m *bashDateInboxMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *bashDateInboxMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func writeInboxBashAlwaysPolicy(t *testing.T, dir string) *policy.Engine {
	t.Helper()
	policyDir := filepath.Join(dir, "policy")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "tool.approval.txt"), []byte("bash_run=always\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine, err := policy.LoadFromDir(policyDir)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func newInboxHitlManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := writeInboxBashAlwaysPolicy(t, dir)
	mock := &bashDateInboxMock{}
	mgr := NewManager("node-a", hub, mock, reg, pol, nil, TurnOptions{RuntimeDir: dir, SkillsEnabled: false}, logx.Discard())
	t.Cleanup(mgr.Stop)
	return mgr
}

func approvalIDFromHITL(hitl *InboxHITLPause) string {
	if hitl == nil || hitl.Data == nil {
		return ""
	}
	if id, ok := hitl.Data["hitl_id"].(string); ok && strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	if id, ok := hitl.Data["approval_id"].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

func toolCallIDsFromHITL(hitl *InboxHITLPause) []string {
	if hitl == nil || hitl.Data == nil {
		return nil
	}
	if hitl.EventType == "hitl_required" {
		return executeToolCallIDsFromHITLItems(hitl.Data["items"])
	}
	args, _ := hitl.Data["approval_args"].(map[string]any)
	if args == nil {
		return nil
	}
	var ids []string
	for _, item := range toolCallsFromApprovalArgs(args) {
		if id, ok := item["id"].(string); ok && strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func executeToolCallIDsFromHITLItems(raw any) []string {
	items := hitlItemsFromSSE(raw)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(fmt.Sprint(item["hitl_type"])) != "execute_tool" {
			continue
		}
		if id, ok := item["id"].(string); ok && strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func hitlItemsFromSSE(raw any) []map[string]any {
	switch items := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return items
	default:
		return nil
	}
}

func toolCallsFromApprovalArgs(args map[string]any) []map[string]any {
	raw := args["tool_calls"]
	switch calls := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(calls))
		for _, item := range calls {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return calls
	default:
		return nil
	}
}

// TestRunInboxTurn_approvalResumeExecutesBashRun 复现 A2A inbox：bash_run=always 审批后续跑应写入 tool 并终态完成。
func TestRunInboxTurn_approvalResumeExecutesBashRun(t *testing.T) {
	mgr := newInboxHitlManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	taskID := "task-hitl-bash"
	content := "【合规咨询】请查看当前系统时间，执行 date 命令并将输出回复"

	step1, err := mgr.RunInboxTurn(ctx, taskID, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step1.Complete {
		t.Fatalf("expected HITL pause, got complete text=%q", step1.Text)
	}
	if step1.HITL == nil || step1.HITL.Awaiting != "hitl" {
		t.Fatalf("hitl=%+v", step1.HITL)
	}
	approved := toolCallIDsFromHITL(step1.HITL)
	if len(approved) != 1 {
		t.Fatalf("approved ids=%v", approved)
	}

	sessionID := InboxSessionID(taskID)
	view, err := mgr.GetContextView(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if view.PendingToolCallsCount != 1 {
		t.Fatalf("pending=%d want 1", view.PendingToolCallsCount)
	}
	if view.MessagesCount != 3 {
		t.Fatalf("messages=%d want 3 (date + bridge user + assistant tool_calls)", view.MessagesCount)
	}

	resume := map[string]any{
		"type":        "selection",
		"approval_id": approvalIDFromHITL(step1.HITL),
		"approved":    approved,
		"rejected":    []string{},
	}
	step2, err := mgr.RunInboxTurn(ctx, taskID, "", resume)
	if err != nil {
		t.Fatal(err)
	}
	if !step2.Complete {
		t.Fatalf("expected complete, hitl=%+v text=%q", step2.HITL, step2.Text)
	}
	if !strings.Contains(step2.Text, "APPROVED") || !strings.Contains(step2.Text, "R-TIME-01") {
		t.Fatalf("result=%q", step2.Text)
	}

	view, err = mgr.GetContextView(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if view.PendingToolCallsCount != 0 {
		t.Fatalf("pending after complete=%d", view.PendingToolCallsCount)
	}
	hasTool := false
	for _, msg := range view.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "BASH_RESULT") {
			hasTool = true
			break
		}
	}
	if !hasTool {
		raw, _ := json.Marshal(view.Messages)
		t.Fatalf("missing bash tool result in history: %s", raw)
	}
}

// askUserInboxMock：首轮 ask_user_information，续跑后返回合规结论文本。
type askUserInboxMock struct {
	calls int
}

func (m *askUserInboxMock) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.calls++
	if m.calls == 1 {
		return llm.ChatResult{
			ToolCalls: []llm.ToolCall{{
				ID:   "call-ask-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "ask_user_information",
					Arguments: `{"question":"请确认目标部署环境"}`,
				},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	text := "APPROVED | rule=R-ENV-01 | 环境已确认"
	mock := &llm.MockClient{FixedReply: text}
	_, _ = mock.StreamChat(ctx, req, handler)
	return llm.ChatResult{Content: text, FinishReason: "stop"}, nil
}

func (m *askUserInboxMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *askUserInboxMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func newInboxUserInfoManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mock := &askUserInboxMock{}
	mgr := NewManager("node-a", hub, mock, reg, pol, nil, TurnOptions{RuntimeDir: dir, SkillsEnabled: false}, logx.Discard())
	t.Cleanup(mgr.Stop)
	return mgr
}

func TestRunInboxTurn_userInformationResumeCompletes(t *testing.T) {
	mgr := newInboxUserInfoManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	taskID := "task-hitl-ask"
	content := "【合规咨询】部署前需确认目标环境"

	step1, err := mgr.RunInboxTurn(ctx, taskID, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if step1.Complete {
		t.Fatalf("expected user_information pause, complete text=%q", step1.Text)
	}
	if step1.HITL == nil || step1.HITL.Awaiting != "hitl" {
		t.Fatalf("hitl=%+v", step1.HITL)
	}
	if step1.HITL.EventType != "hitl_required" {
		t.Fatalf("event_type=%q", step1.HITL.EventType)
	}

	resume := map[string]any{
		"type":         "user_information",
		"tool_call_id": "call-ask-1",
		"answer":       "production",
	}
	step2, err := mgr.RunInboxTurn(ctx, taskID, "", resume)
	if err != nil {
		t.Fatal(err)
	}
	if !step2.Complete {
		t.Fatalf("expected complete, hitl=%+v", step2.HITL)
	}
	if !strings.Contains(step2.Text, "APPROVED") || !strings.Contains(step2.Text, "R-ENV-01") {
		t.Fatalf("result=%q", step2.Text)
	}
}

func TestRunInboxTurn_approvalRejectSkipsBashExecution(t *testing.T) {
	mgr := newInboxHitlManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	taskID := "task-hitl-reject"
	step1, err := mgr.RunInboxTurn(ctx, taskID, "【合规咨询】请执行 date", nil)
	if err != nil {
		t.Fatal(err)
	}
	if step1.HITL == nil {
		t.Fatal("expected HITL pause")
	}
	rejected := toolCallIDsFromHITL(step1.HITL)
	if len(rejected) != 1 {
		t.Fatalf("call ids=%v", rejected)
	}

	resume := map[string]any{
		"type":        "selection",
		"approval_id": approvalIDFromHITL(step1.HITL),
		"approved":    []string{},
		"rejected":    rejected,
	}
	step2, err := mgr.RunInboxTurn(ctx, taskID, "", resume)
	if err != nil {
		t.Fatal(err)
	}
	if !step2.Complete {
		t.Fatalf("expected complete after reject, hitl=%+v", step2.HITL)
	}

	sessionID := InboxSessionID(taskID)
	view, err := mgr.GetContextView(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range view.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "BASH_RESULT") {
			t.Fatalf("bash should not run when rejected: %q", msg.Content)
		}
	}
}
