package turn

import (
	"context"
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

func testRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func drainToolResultSteps(
	t *testing.T,
	orch *Orchestrator,
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	start StepOutcome,
) {
	t.Helper()
	outcome := start
	for outcome.ScheduleToolResult {
		outcome = orch.RunToolMessageTurn(ctx, sessionID, history, nil, outcome.LoopCount)
		if outcome.Err != nil {
			t.Fatalf("RunToolMessageTurn: %v", outcome.Err)
		}
		if outcome.Pending != nil {
			return
		}
	}
}

func continueResumeAndDrain(
	t *testing.T,
	orch *Orchestrator,
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	resume map[string]any,
	pending *PendingHITL,
	loopCount int,
) {
	t.Helper()
	outcome := orch.ContinueAfterResume(ctx, sessionID, history, resume, pending, nil, loopCount)
	drainToolResultSteps(t, orch, ctx, sessionID, history, outcome)
}

func testOrchestrator(t *testing.T, hub *stream.Hub, client llm.Client) *Orchestrator {
	t.Helper()
	reg := testRegistry(t)
	pol, _ := policy.LoadFile("")
	return NewOrchestrator("a1", t.TempDir(), hub, client, reg, pol, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, logx.Discard())
}

func TestRunMessageTurn(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	var history []llm.Message
	_, _, err := orch.RunMessageTurn(context.Background(), "sess-1", &history, "hi", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].Content != "hi" {
		t.Fatalf("history = %+v", history)
	}

	deadline := time.After(2 * time.Second)
	var text string
	var gotUsage, gotDone bool
	for !(gotUsage && gotDone) {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "assistant":
				if c, ok := ev.Data["content"].(string); ok {
					text += c
				}
			case "usage":
				gotUsage = true
			case "done":
				gotDone = true
			}
		case <-deadline:
			t.Fatalf("timeout text=%q usage=%v done=%v", text, gotUsage, gotDone)
		}
	}
	if text != "hi" {
		t.Fatalf("text = %q", text)
	}
}

func TestRunMessageTurnToolLoop(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg := testRegistry(t)
	ctx := context.Background()
	_, _ = reg.Execute(ctx, "write_file", `{"path":"hello.txt","content":"file-body"}`)

	orch := NewOrchestrator("a1", t.TempDir(), hub, &llm.MockClient{EnableTools: true}, reg, nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, logx.Discard())
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	var history []llm.Message
	pending, _, err := orch.RunMessageTurn(ctx, "sess-1", &history, "读文件", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pending != nil {
		t.Fatalf("unexpected pending hitl: %+v", pending)
	}

	deadline := time.After(3 * time.Second)
	var gotToolCall, gotToolResult, gotDone bool
	for !gotDone {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "tool_call":
				gotToolCall = true
			case "tool_result":
				gotToolResult = true
				if c, ok := ev.Data["content"].(string); !ok || !strings.Contains(c, "file-body") {
					t.Fatalf("tool result = %+v", ev.Data)
				}
			case "done":
				gotDone = true
			}
		case <-deadline:
			t.Fatalf("timeout call=%v result=%v done=%v", gotToolCall, gotToolResult, gotDone)
		}
	}
}

func TestRunMessageTurnUserInformationPayload(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	mock := &userInfoMock{}
	orch := testOrchestrator(t, hub, mock)
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	done := make(chan struct{})
	var history []llm.Message
	go func() {
		defer close(done)
		pending, _, _ := orch.RunMessageTurn(context.Background(), "sess-1", &history, "ask me", nil, 0)
		if pending == nil {
			t.Error("expected pending hitl")
		}
	}()

	deadline := time.After(3 * time.Second)
	var pending *PendingHITL
	var gotHitlDone bool
	var gotUserInfo bool
	for !(gotHitlDone && gotUserInfo) {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "done":
				if ev.Data["finish_reason"] != "awaiting_user_information" {
					t.Fatalf("unexpected done finish_reason: %+v", ev.Data)
				}
				if ev.Data["turn_complete"] != false {
					t.Fatalf("turn_complete = %v, want false", ev.Data["turn_complete"])
				}
				if ev.Data["awaiting"] != "user_information" {
					t.Fatalf("awaiting = %v", ev.Data["awaiting"])
				}
				gotHitlDone = true
			case "user_information_required":
				args, ok := ev.Data["user_information_args"].(map[string]any)
				if !ok {
					t.Fatalf("user_information_args missing: %+v", ev.Data)
				}
				if ev.Data["content"] != "Pick one?" {
					t.Fatalf("content = %v", ev.Data["content"])
				}
				if args["tool_call_id"] != "call-ask-1" {
					t.Fatalf("tool_call_id = %v", args["tool_call_id"])
				}
				pending = &PendingHITL{Kind: HITLUserInformation, UserInfo: &llm.ToolCall{ID: "call-ask-1"}}
				gotUserInfo = true
			}
		case <-deadline:
			t.Fatalf("timeout hitl_done=%v user_info=%v", gotHitlDone, gotUserInfo)
		}
	}
	continueResumeAndDrain(t, orch, context.Background(), "sess-1", &history, map[string]any{
		"type":         "user_information",
		"tool_call_id": "call-ask-1",
		"answer":       "A",
	}, pending, 1)
	<-done
}

func TestRunMessageTurnApproval(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	mock := &bashApprovalMock{}
	orch := testOrchestrator(t, hub, mock)
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	done := make(chan struct{})
	var history []llm.Message
	go func() {
		defer close(done)
		_, _, _ = orch.RunMessageTurn(context.Background(), "sess-1", &history, "run echo", nil, 0)
	}()

	deadline := time.After(3 * time.Second)
	var pending *PendingHITL
	for {
		select {
		case ev := <-ch:
			if ev.Type == "approval_required" {
				pending = &PendingHITL{Kind: HITLApproval, ToolCalls: []llm.ToolCall{{ID: "call-bash-1"}}}
				goto resumeApproval
			}
		case <-deadline:
			t.Fatal("timeout waiting approval")
		}
	}
resumeApproval:
	continueResumeAndDrain(t, orch, context.Background(), "sess-1", &history, map[string]any{"type": "approve"}, pending, 1)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "done" {
				<-done
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting done")
		}
	}
}

func TestRunMessageTurnMaxToolLoops(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg := testRegistry(t)
	orch := NewOrchestrator("a1", t.TempDir(), hub, alwaysToolMock{}, reg, nil, SkillAccess{}, 2, nil, nil, logx.Discard())
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var history []llm.Message
		_, _, _ = orch.RunMessageTurn(context.Background(), "sess-1", &history, "loop", nil, 0)
	}()

	deadline := time.After(3 * time.Second)
	var gotError, gotDone bool
	for !(gotError && gotDone) {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "error":
				if msg, ok := ev.Data["message"].(string); !ok || !strings.Contains(msg, "工具调用轮次超过上限") {
					t.Fatalf("error payload = %+v", ev.Data)
				}
				gotError = true
			case "done":
				gotDone = true
			}
		case <-deadline:
			t.Fatalf("timeout error=%v done=%v", gotError, gotDone)
		}
	}
	<-done
}

func TestRunMessageTurnMultiToolParallelOrder(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "b.txt"), []byte("beta"), 0o644)

	polPath := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(polPath, []byte("default: deny\ntools:\n  read_file: auto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pol, err := policy.LoadFile(polPath)
	if err != nil {
		t.Fatal(err)
	}
	orch := NewOrchestrator("a1", root, hub, &dualReadFileMock{}, reg, pol, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, logx.Discard())

	var history []llm.Message
	pending, _, err := orch.RunMessageTurn(ctx, "sess-1", &history, "读两个文件", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pending != nil {
		t.Fatalf("unexpected pending: %+v", pending)
	}
	toolIdx := 0
	for _, msg := range history {
		if msg.Role != "tool" {
			continue
		}
		toolIdx++
		switch toolIdx {
		case 1:
			if !strings.Contains(msg.Content, "alpha") || msg.ToolCallID != "call-read-1" {
				t.Fatalf("first tool = %+v", msg)
			}
		case 2:
			if !strings.Contains(msg.Content, "beta") || msg.ToolCallID != "call-read-2" {
				t.Fatalf("second tool = %+v", msg)
			}
		default:
			t.Fatalf("unexpected extra tool message: %+v", msg)
		}
	}
	if toolIdx != 2 {
		t.Fatalf("expected 2 tool messages, got %d", toolIdx)
	}
}

type dualReadFileMock struct{ calls int }

func (m *dualReadFileMock) StreamChat(_ context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	m.calls++
	if m.calls == 1 {
		return llm.ChatResult{
			ToolCalls: []llm.ToolCall{
				{ID: "call-read-1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`}},
				{ID: "call-read-2", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.txt"}`}},
			},
			FinishReason: "tool_calls",
		}, nil
	}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}

func (m *dualReadFileMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func TestBuildUserInformationPayload(t *testing.T) {
	tc := llm.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "ask_user_information",
			Arguments: `{"question":"Q?","options":[{"id":"a","label":"A"}],"allow_multiple":true}`,
		},
	}
	question, args := buildUserInformationPayload(tc)
	if question != "Q?" {
		t.Fatalf("question = %q", question)
	}
	if args["tool_call_id"] != "call-1" {
		t.Fatalf("tool_call_id = %v", args["tool_call_id"])
	}
}

type userInfoMock struct{ calls int }

func (m *userInfoMock) StreamChat(_ context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	m.calls++
	if m.calls == 1 {
		return llm.ChatResult{
			ToolCalls: []llm.ToolCall{{
				ID:   "call-ask-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "ask_user_information",
					Arguments: `{"question":"Pick one?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`,
				},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	return llm.ChatResult{Content: "thanks", FinishReason: "stop"}, nil
}

func (m *userInfoMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

type bashApprovalMock struct{ calls int }

func (m *bashApprovalMock) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.calls++
	if m.calls == 1 {
		return llm.ChatResult{
			ToolCalls: []llm.ToolCall{{
				ID: "call-bash-1", Type: "function",
				Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo ok"}`},
			}},
			FinishReason: "tool_calls",
		}, nil
	}
	text := "done"
	mock := &llm.MockClient{FixedReply: text}
	_, _ = mock.StreamChat(ctx, req, handler)
	return llm.ChatResult{Content: text, FinishReason: "stop"}, nil
}

func (m *bashApprovalMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

type alwaysToolMock struct{}

func (alwaysToolMock) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{
		ToolCalls: []llm.ToolCall{{
			ID:   "call-loop",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"hello.txt"}`,
			},
		}},
		FinishReason: "tool_calls",
	}, nil
}

func (alwaysToolMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

type blockingMock struct{}

func (b *blockingMock) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	<-ctx.Done()
	return llm.ChatResult{}, ctx.Err()
}

func (b *blockingMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

type errMock struct{ msg string }

func (e *errMock) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{}, fmt.Errorf("%s", e.msg)
}

func (e *errMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", fmt.Errorf("%s", e.msg)
}

func TestRunMessageTurnCancelled(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := testOrchestrator(t, hub, &blockingMock{})
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		var history []llm.Message
		_, _, _ = orch.RunMessageTurn(ctx, "sess-1", &history, "hi", nil, 0)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	deadline := time.After(2 * time.Second)
	var finish string
	for finish == "" {
		select {
		case ev := <-ch:
			if ev.Type == "done" {
				finish, _ = ev.Data["finish_reason"].(string)
			}
		case <-deadline:
			t.Fatal("timeout waiting for done")
		}
	}
	if finish != "cancelled" {
		t.Fatalf("finish_reason = %q", finish)
	}
}

func TestRunMessageTurnLLMError(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := testOrchestrator(t, hub, &errMock{msg: "boom"})
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	var history []llm.Message
	_, _, err := orch.RunMessageTurn(context.Background(), "sess-1", &history, "hi", nil, 0)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}

	deadline := time.After(2 * time.Second)
	var gotError, gotDone bool
	for !(gotError && gotDone) {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "error":
				gotError = true
			case "done":
				gotDone = true
			}
		case <-deadline:
			t.Fatalf("timeout error=%v done=%v", gotError, gotDone)
		}
	}
}
