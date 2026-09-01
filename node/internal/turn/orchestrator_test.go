package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
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

func TestOrchestratorForgetSessionReleasesRuntimeState(t *testing.T) {
	o := NewOrchestrator("agent", t.TempDir(), nil, nil, nil, nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{}, nil)
	const sessionID = "session-cleanup"
	o.RequestModelContextRefresh(sessionID, "skills_load")
	o.SetNextStepFinalSummary(sessionID)
	o.recordToolCall(sessionID, "terminal_command")
	o.setModelContextSnapshot(sessionID, NewModelContextSnapshot("system", nil, 1, "digest"))

	o.ForgetSession(sessionID)

	if got := o.ModelContextSnapshot(sessionID); got != nil {
		t.Fatalf("model context snapshot was retained: %#v", got)
	}
	o.contextMutationMu.Lock()
	_, hasMutation := o.contextMutations[sessionID]
	o.contextMutationMu.Unlock()
	o.summaryMu.Lock()
	_, hasSummary := o.summaryNext[sessionID]
	o.summaryMu.Unlock()
	o.turnUsageMu.Lock()
	_, hasUsage := o.turnUsage[sessionID]
	_, hasUsageLast := o.turnUsageLast[sessionID]
	o.turnUsageMu.Unlock()
	o.ctxMetrics.mu.Lock()
	_, hasMetrics := o.ctxMetrics.data[sessionID]
	o.ctxMetrics.mu.Unlock()
	if hasMutation || hasSummary || hasUsage || hasUsageLast || hasMetrics {
		t.Fatalf("session state was not fully released: mutation=%v summary=%v usage=%v usage_last=%v metrics=%v", hasMutation, hasSummary, hasUsage, hasUsageLast, hasMetrics)
	}
}

func TestToolDefinitions_exposesSkillDiscoveryWithSkillsTools(t *testing.T) {
	root := t.TempDir()
	reg := testRegistry(t)
	catalog := skills.NewCatalog(filepath.Join(root, "skills"), true, 3)

	defaultOrch := NewOrchestrator("agent", root, nil, nil, reg, nil, SkillAccess{Catalog: catalog}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{}, nil)
	found := false
	for _, def := range defaultOrch.ToolDefinitions() {
		if def.Function.Name == "list_available_skills" {
			found = true
		}
	}
	if !found {
		t.Fatal("Skills group must expose list_available_skills when load_skills is visible")
	}

	reg.SetBuiltinEnabledNone()
	restricted := NewOrchestrator("agent", root, nil, nil, reg, nil, SkillAccess{Catalog: catalog}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{}, nil)
	for _, def := range restricted.ToolDefinitions() {
		if def.Function.Name == "list_available_skills" {
			t.Fatal("catalog discovery must not appear without load_skills")
		}
	}
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
	stepIndex := outcome.StepIndex + 1
	if stepIndex <= 1 {
		stepIndex = 2
	}
	for outcome.ScheduleToolResult {
		stepCtx := WithExecutionContext(ctx, TurnExecutionContext{SessionID: sessionID, StepIndex: stepIndex})
		outcome = orch.RunToolMessageTurn(stepCtx, sessionID, history)
		if outcome.Err != nil {
			t.Fatalf("RunToolMessageTurn: %v", outcome.Err)
		}
		if outcome.Pending != nil {
			return
		}
		stepIndex++
	}
}

// runMessageTurnInline 测试用：RunHumanMessageTurn + RunToolMessageTurn 内联跑完一条 user 消息 turn。
func runMessageTurnInline(
	t *testing.T,
	orch *Orchestrator,
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	userText string,
	_ func(State),
) (*PendingHITL, int, error) {
	t.Helper()
	outcome := orch.RunHumanMessageTurn(WithExecutionContext(ctx, TurnExecutionContext{SessionID: sessionID, StepIndex: 1}), sessionID, history, llm.UserMessage(userText, llm.UserNameHuman))
	if outcome.Err != nil {
		return outcome.Pending, outcome.StepIndex, outcome.Err
	}
	if outcome.Pending != nil {
		// Direct orchestrator tests do not run the SessionRuntime lifecycle
		// adapter, so publish the event explicitly at this boundary.
		orch.PublishPendingHITL(sessionID, outcome.Pending)
		return outcome.Pending, outcome.StepIndex, nil
	}
	stepIndex := outcome.StepIndex + 1
	for outcome.ScheduleToolResult {
		stepCtx := WithExecutionContext(ctx, TurnExecutionContext{SessionID: sessionID, StepIndex: stepIndex})
		outcome = orch.RunToolMessageTurn(stepCtx, sessionID, history)
		if outcome.Err != nil {
			return outcome.Pending, outcome.StepIndex, outcome.Err
		}
		if outcome.Pending != nil {
			orch.PublishPendingHITL(sessionID, outcome.Pending)
			return outcome.Pending, outcome.StepIndex, nil
		}
		stepIndex++
	}
	return nil, outcome.StepIndex, nil
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
	stepIndex := loopCount
	if stepIndex <= 0 {
		stepIndex = 1
	}
	resumeCtx := WithExecutionContext(ctx, TurnExecutionContext{SessionID: sessionID, StepIndex: stepIndex})
	outcome := orch.ContinueAfterResume(resumeCtx, sessionID, history, resume, pending)
	drainToolResultSteps(t, orch, ctx, sessionID, history, outcome)
}

func testOrchestrator(t *testing.T, hub *stream.Hub, client llm.Client) *Orchestrator {
	t.Helper()
	reg := testRegistry(t)
	pol, _ := policy.LoadFile("")
	return NewOrchestrator("a1", t.TempDir(), hub, client, reg, pol, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(t.TempDir())}, logx.Discard())
}

type flakyRetryExecutor struct {
	calls int
}

type cacheReportingClient struct{}

func (cacheReportingClient) StreamChat(_ context.Context, _ llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	if handler.OnDelta != nil {
		handler.OnDelta("cache-aware")
	}
	if handler.OnUsage != nil {
		handler.OnUsage(llm.Usage{
			PromptTokens:          100,
			CompletionTokens:      8,
			TotalTokens:           108,
			PromptCacheHitTokens:  80,
			PromptCacheMissTokens: 20,
			CompletionTokensDetails: &llm.CompletionTokensDetails{
				ReasoningTokens: 3,
			},
		})
	}
	return llm.ChatResult{Content: "cache-aware", FinishReason: "stop"}, nil
}

func (cacheReportingClient) CompleteText(_ context.Context, _ llm.CompleteRequest) (string, error) {
	return "summary", nil
}

func (cacheReportingClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func (e *flakyRetryExecutor) Definitions() []tools.ToolDef {
	return []tools.ToolDef{{Type: "function", Function: tools.FunctionDef{Name: "read_file"}}}
}

func (e *flakyRetryExecutor) Execute(context.Context, string, string) (string, error) {
	e.calls++
	if e.calls == 1 {
		return "", fmt.Errorf("temporary timeout while reading")
	}
	return "read-after-retry", nil
}

func (*flakyRetryExecutor) StartBackground(context.Context, string, string, string, string) (string, error) {
	return "", fmt.Errorf("background unsupported")
}

func (*flakyRetryExecutor) TakeBashCompressStatsForCall(string) map[string]any { return nil }
func (*flakyRetryExecutor) TakeToolResultMediaForCall(string) map[string]any   { return nil }
func (*flakyRetryExecutor) TakeReadImageVisionForCall(string) *tools.ReadImageVisionPayload {
	return nil
}
func (*flakyRetryExecutor) ToolRetryAllowed(name string) bool { return name == "read_file" }

func TestOrchestratorRetriesSafeTransientToolFailureWithoutNewToolCall(t *testing.T) {
	executor := &flakyRetryExecutor{}
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(16, logx.Discard()), &llm.MockClient{}, executor, nil, SkillAccess{}, 4, nil, nil, hooks.RuntimeConfig{}, logx.Discard())
	orch.SetToolRetryLimit(1)
	var lifecycle []CommandType
	orch.SetLifecycleCommandSink(func(_ string, command TurnCommand) error {
		lifecycle = append(lifecycle, command.Type)
		return nil
	})
	history := []llm.Message{}
	err := orch.executeTool(context.Background(), "session-1", &history, llm.ToolCall{
		ID: "call-retry", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{}`},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 2 {
		t.Fatalf("executor calls = %d, want 2", executor.calls)
	}
	if len(history) != 1 || history[0].Content != "read-after-retry" {
		t.Fatalf("history after retry = %#v", history)
	}
	want := []CommandType{CommandToolExecutionStarted, CommandToolExecutionRetrying, CommandToolExecutionCompleted}
	if len(lifecycle) != len(want) {
		t.Fatalf("lifecycle = %#v, want %#v", lifecycle, want)
	}
	for i := range want {
		if lifecycle[i] != want[i] {
			t.Fatalf("lifecycle[%d] = %s, want %s", i, lifecycle[i], want[i])
		}
	}
}

func TestRunMessageTurn(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "sess-1", &history, "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "hi" || history[1].Content != "hi" {
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
			case "turn_finished":
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

func TestOrchestratorEmitsModelAndAssistantLifecycleFacts(t *testing.T) {
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), &llm.MockClient{})
	var commands []TurnCommand
	orch.SetLifecycleCommandSink(func(_ string, command TurnCommand) error {
		commands = append(commands, command)
		return nil
	})
	var history []llm.Message
	if _, _, err := runMessageTurnInline(t, orch, context.Background(), "session-1", &history, "hello", nil); err != nil {
		t.Fatal(err)
	}
	var got []CommandType
	for _, command := range commands {
		got = append(got, command.Type)
	}
	want := []CommandType{CommandTurnSnapshotCreated, CommandModelRequestStarted, CommandModelUsageRecorded, CommandModelResponseCompleted, CommandAssistantReceived}
	if len(got) != len(want) {
		t.Fatalf("lifecycle commands = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lifecycle command %d = %s, want %s", i, got[i], want[i])
		}
	}
	if commands[0].RuntimeDigest == "" || commands[1].RequestDigest == "" {
		t.Fatalf("missing snapshot/request digest: %#v", commands)
	}
}

func TestOrchestratorCarriesProviderCacheUsageIntoLifecycle(t *testing.T) {
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), cacheReportingClient{})
	var commands []TurnCommand
	orch.SetLifecycleCommandSink(func(_ string, command TurnCommand) error {
		commands = append(commands, command)
		return nil
	})
	var history []llm.Message
	if _, _, err := runMessageTurnInline(t, orch, context.Background(), "session-cache", &history, "measure", nil); err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		if command.Type != CommandModelUsageRecorded {
			continue
		}
		if command.Usage.InputTokens != 100 || command.Usage.PromptCacheHitTokens != 80 ||
			command.Usage.PromptCacheMissTokens != 20 || !command.Usage.PromptCacheMetricsObserved ||
			command.Usage.ReasoningTokens != 3 {
			t.Fatalf("lifecycle usage = %+v", command.Usage)
		}
		return
	}
	t.Fatal("model usage lifecycle command was not emitted")
}

func TestRunMessageTurnToolLoop(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg := testRegistry(t)
	ctx := context.Background()
	_, _ = reg.Execute(ctx, "write_file", `{"path":"hello.txt","content":"file-body"}`)

	orch := NewOrchestrator("a1", t.TempDir(), hub, &llm.MockClient{EnableTools: true}, reg, nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(t.TempDir())}, logx.Discard())
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	var history []llm.Message
	pending, _, err := runMessageTurnInline(t, orch, ctx, "sess-1", &history, "读文件", nil)
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
			case "turn_finished":
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
		pending, _, _ := runMessageTurnInline(t, orch, context.Background(), "sess-1", &history, "ask me", nil)
		if pending == nil {
			t.Error("expected pending hitl")
		}
	}()

	deadline := time.After(3 * time.Second)
	var pending *PendingHITL
	var gotUserInfo bool
	for !gotUserInfo {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "turn_finished":
				t.Fatalf("HITL pause must not emit turn_finished: %+v", ev.Data)
			case "hitl_required":
				items, ok := ev.Data["items"].([]any)
				if !ok || len(items) != 1 {
					t.Fatalf("items missing: %+v", ev.Data)
				}
				item, ok := items[0].(map[string]any)
				if !ok {
					t.Fatalf("item invalid: %+v", items[0])
				}
				if item["hitl_type"] != hitlTypeUserInformation {
					t.Fatalf("hitl_type = %v", item["hitl_type"])
				}
				args, ok := item["user_information_args"].(map[string]any)
				if !ok {
					t.Fatalf("user_information_args missing: %+v", item)
				}
				if item["content"] != "Pick one?" {
					t.Fatalf("content = %v", item["content"])
				}
				if args["tool_call_id"] != "call-ask-1" {
					t.Fatalf("tool_call_id = %v", args["tool_call_id"])
				}
				pending = &PendingHITL{Items: []PendingHITLItem{{ToolCall: llm.ToolCall{ID: "call-ask-1"}}}}
				gotUserInfo = true
			}
		case <-deadline:
			t.Fatalf("timeout user_info=%v", gotUserInfo)
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
		_, _, _ = runMessageTurnInline(t, orch, context.Background(), "sess-1", &history, "run echo", nil)
	}()

	deadline := time.After(3 * time.Second)
	var pending *PendingHITL
	for {
		select {
		case ev := <-ch:
			if ev.Type == "hitl_required" {
				pending = &PendingHITL{Items: []PendingHITLItem{{ToolCall: llm.ToolCall{ID: "call-bash-1"}}}}
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
			if ev.Type == "turn_finished" {
				<-done
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting done")
		}
	}
}

func TestProcessToolCallsMixedHITL(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	var history []llm.Message
	pending, state, err := orch.processToolCalls(context.Background(), "sess-1", &history, []llm.ToolCall{
		{
			ID: "call-ask-1", Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "ask_user_information",
				Arguments: `{"question":"Which env?"}`,
			},
		},
		{
			ID: "call-bash-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo ok"}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if state != "awaiting_hitl" {
		t.Fatalf("state = %q, want awaiting_hitl", state)
	}
	if pending == nil || len(pending.Items) != 2 {
		t.Fatalf("pending = %+v", pending)
	}
	orch.PublishPendingHITL("sess-1", pending)

	deadline := time.After(2 * time.Second)
	gotHitl := false
	for !gotHitl {
		select {
		case ev := <-ch:
			if ev.Type == "hitl_required" {
				if len(hitlSSEItems(ev.Data)) != 2 {
					t.Fatalf("hitl items = %+v", ev.Data["items"])
				}
				gotHitl = true
			}
		case <-deadline:
			t.Fatal("timeout waiting hitl_required SSE")
		}
	}

	outcome := orch.ContinueAfterResume(context.Background(), "sess-1", &history, map[string]any{
		"type":         "user_information",
		"tool_call_id": "call-ask-1",
		"answer":       "prod",
	}, pending)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}
	if outcome.ScheduleToolResult {
		t.Fatal("expected partial pending after user_information resume")
	}
	if outcome.Pending == nil || len(outcome.Pending.Items) != 1 {
		t.Fatalf("partial pending = %+v", outcome.Pending)
	}
	if outcome.Pending.Items[0].ToolCall.ID != "call-bash-1" {
		t.Fatalf("remaining pending = %+v", outcome.Pending.Items[0])
	}
	orch.PublishPendingHITL("sess-1", outcome.Pending)
	republishDeadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "hitl_required" {
				continue
			}
			items := hitlSSEItems(ev.Data)
			if len(items) != 1 || items[0]["id"] != "call-bash-1" {
				t.Fatalf("republished hitl items = %+v", items)
			}
			goto republished
		case <-republishDeadline:
			t.Fatal("timeout waiting remaining hitl_required SSE")
		}
	}

republished:
	continueResumeAndDrain(t, orch, context.Background(), "sess-1", &history, map[string]any{"type": "approve"}, outcome.Pending, 1)
}

func hitlSSEItems(data map[string]any) []map[string]any {
	raw := data["items"]
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

func TestPendingHITLLegacyJSON(t *testing.T) {
	var pending PendingHITL
	if err := json.Unmarshal([]byte(`{"kind":"approval","tool_calls":[{"id":"c1","type":"function","function":{"name":"bash_run"}}]}`), &pending); err != nil {
		t.Fatal(err)
	}
	if len(pending.Items) != 1 || pending.Items[0].ToolCall.ID != "c1" {
		t.Fatalf("pending = %+v", pending.Items)
	}
}

func TestRunMessageTurnMaxToolLoops(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg := testRegistry(t)
	hookCfg := hooks.RuntimeConfig{
		Duplicate:  hooks.DuplicateConfig{Enabled: false, WindowSeconds: 1},
		ToolResult: hooks.DefaultToolResultConfig(t.TempDir()),
	}
	orch := NewOrchestrator("a1", t.TempDir(), hub, alwaysToolMock{}, reg, nil, SkillAccess{}, 2, nil, nil, hookCfg, logx.Discard())
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	done := make(chan struct{})
	var turnErr error
	var history []llm.Message
	go func() {
		defer close(done)
		_, _, turnErr = runMessageTurnInline(t, orch, context.Background(), "sess-1", &history, "loop", nil)
	}()

	deadline := time.After(3 * time.Second)
	var gotDone bool
	for !gotDone {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "error":
				t.Fatalf("unexpected error SSE: %+v", ev.Data)
			case "turn_finished":
				if reason, _ := ev.Data["finish_reason"].(string); reason != "stop" {
					t.Fatalf("done finish_reason = %q, want stop", reason)
				}
				gotDone = true
			}
		case <-deadline:
			t.Fatalf("timeout done=%v turnErr=%v", gotDone, turnErr)
		}
	}
	<-done
	if turnErr != nil {
		t.Fatalf("runMessageTurnInline err = %v, want nil (soft tool_result)", turnErr)
	}
	gotSoft := false
	for _, msg := range history {
		if msg.Role == "tool" && strings.Contains(msg.Content, "已超过单轮工具调用次数") {
			gotSoft = true
			break
		}
	}
	if !gotSoft {
		t.Fatalf("history missing soft tool_result; history=%+v", history)
	}
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
	orch := NewOrchestrator("a1", root, hub, &dualReadFileMock{}, reg, pol, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig(t.TempDir())}, logx.Discard())

	var history []llm.Message
	pending, _, err := runMessageTurnInline(t, orch, ctx, "sess-1", &history, "读两个文件", nil)
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

func (m *dualReadFileMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
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

func (m *userInfoMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
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

func (m *bashApprovalMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
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

func (alwaysToolMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

type blockingMock struct{}

func (b *blockingMock) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	<-ctx.Done()
	return llm.ChatResult{}, ctx.Err()
}

func (b *blockingMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (b *blockingMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

type errMock struct{ msg string }

func (e *errMock) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{}, fmt.Errorf("%s", e.msg)
}

func (e *errMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", fmt.Errorf("%s", e.msg)
}

func (e *errMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
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
		_, _, _ = runMessageTurnInline(t, orch, ctx, "sess-1", &history, "hi", nil)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	deadline := time.After(2 * time.Second)
	var finish string
	for finish == "" {
		select {
		case ev := <-ch:
			if ev.Type == "turn_finished" {
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

func TestRunMessageTurnCancelledPersistsPartialAssistant(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := testOrchestrator(t, hub, &partialCancelMock{
		content:   "partial answer",
		reasoning: "partial think",
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var history []llm.Message
	go func() {
		defer close(done)
		_, _, _ = runMessageTurnInline(t, orch, ctx, "sess-1", &history, "hi", nil)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	if len(history) != 2 {
		t.Fatalf("history = %+v", history)
	}
	assistant := history[1]
	if assistant.Role != "assistant" || assistant.Content != "partial answer" {
		t.Fatalf("assistant = %+v", assistant)
	}
	if assistant.ReasoningContent != "partial think" {
		t.Fatalf("reasoning_content = %q", assistant.ReasoningContent)
	}
}

type partialCancelMock struct {
	content   string
	reasoning string
	toolCalls []llm.ToolCall
}

func (m *partialCancelMock) StreamChat(ctx context.Context, _ llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	if handler.OnDelta != nil && m.content != "" {
		handler.OnDelta(m.content)
	}
	if handler.OnReasoningDelta != nil && m.reasoning != "" {
		handler.OnReasoningDelta(m.reasoning)
	}
	<-ctx.Done()
	return llm.ChatResult{
		Content:          m.content,
		ReasoningContent: m.reasoning,
		ToolCalls:        append([]llm.ToolCall(nil), m.toolCalls...),
	}, ctx.Err()
}

func (m *partialCancelMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *partialCancelMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestRunMessageTurnLLMError(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	orch := testOrchestrator(t, hub, &errMock{msg: "boom"})
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "sess-1", &history, "hi", nil)
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
			case "turn_finished":
				gotDone = true
			}
		case <-deadline:
			t.Fatalf("timeout error=%v done=%v", gotError, gotDone)
		}
	}
}
