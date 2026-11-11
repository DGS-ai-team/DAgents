package turn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestOrchestratorRetriesTransientModelFailureInsideStep(t *testing.T) {
	client := &transientModelFailureMock{}
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
	orch.SetModelRetryLimit(1)
	var commands []TurnCommand
	orch.SetLifecycleCommandSink(func(_ string, command TurnCommand) error {
		commands = append(commands, command)
		return nil
	})

	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-1", &history, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.Calls() != 2 {
		t.Fatalf("provider calls = %d, want 2", client.Calls())
	}
	if len(history) != 2 || history[1].Content != "recovered" {
		t.Fatalf("history = %+v", history)
	}
	if countLifecycleCommands(commands, CommandModelRequestRetrying) != 1 {
		t.Fatalf("retry commands = %+v", commands)
	}
	if countLifecycleCommands(commands, CommandModelRequestStarted) != 2 {
		t.Fatalf("start commands = %+v", commands)
	}
}

func TestOrchestratorRetriesHTTP503TwiceThenSucceeds(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&requests, 1)
		if count <= 2 {
			http.Error(w, "temporary upstream outage", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	client := retryTestOpenAIClient{OpenAIClient: llm.NewOpenAIClient(llm.OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})}
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
	var commands []TurnCommand
	orch.SetLifecycleCommandSink(func(_ string, command TurnCommand) error {
		commands = append(commands, command)
		return nil
	})
	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-503", &history, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("provider requests = %d, want 3", got)
	}
	if len(history) != 2 || history[1].Content != "ok" {
		t.Fatalf("history = %+v", history)
	}
	var retryDelays []int64
	for _, command := range commands {
		if command.Type == CommandModelRequestRetrying {
			retryDelays = append(retryDelays, command.RetryAfterMS)
		}
	}
	if len(retryDelays) != 2 {
		t.Fatalf("retry delays = %v, want two retry events", retryDelays)
	}
	if retryDelays[0] < 2000 || retryDelays[0] > 2500 {
		t.Errorf("first retry delay = %dms, want 2000–2500ms", retryDelays[0])
	}
	if retryDelays[1] < 4000 || retryDelays[1] > 5000 {
		t.Errorf("second retry delay = %dms, want 4000–5000ms", retryDelays[1])
	}
}

func TestOrchestratorDoesNotRetryHTTP401Or402(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&requests, 1)
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, "private provider details")
			}))
			defer server.Close()
			client := retryTestOpenAIClient{OpenAIClient: llm.NewOpenAIClient(llm.OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})}
			orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
			var history []llm.Message
			_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-http-error", &history, "hello", nil)
			if err == nil {
				t.Fatal("expected terminal provider error")
			}
			if got := atomic.LoadInt32(&requests); got != 1 {
				t.Fatalf("provider requests = %d, want 1", got)
			}
		})
	}
}

func TestOrchestratorClassifies403InsufficientQuotaWithoutRetry(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":{"message":"insufficient quota","type":"insufficient_quota","code":"insufficient_quota"}}`)
	}))
	defer server.Close()
	client := retryTestOpenAIClient{OpenAIClient: llm.NewOpenAIClient(llm.OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})}
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
	var commands []TurnCommand
	orch.SetLifecycleCommandSink(func(_ string, command TurnCommand) error {
		commands = append(commands, command)
		return nil
	})
	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-403-quota", &history, "hello", nil)
	if err == nil {
		t.Fatal("expected quota failure")
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("provider requests = %d, want one request without retry", got)
	}
	failureFound := false
	for _, command := range commands {
		if command.Type == CommandModelRequestFailed {
			failureFound = true
			if command.ErrorKind != "provider_quota" || command.Retryable {
				t.Fatalf("failure classification = kind %q retryable %v, want provider_quota and false", command.ErrorKind, command.Retryable)
			}
		}
		if command.Type == CommandModelRequestRetrying {
			t.Fatal("quota error must not emit a retrying lifecycle command")
		}
	}
	if !failureFound {
		t.Fatal("missing terminal model request failure lifecycle command")
	}
}

func TestOrchestratorExhausted503ReturnsTerminalError(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		http.Error(w, "temporary outage", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := retryTestOpenAIClient{OpenAIClient: llm.NewOpenAIClient(llm.OpenAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "test-key"})}
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-503-exhausted", &history, "hello", nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("provider requests = %d, want 3", got)
	}
}

func TestOrchestratorDoesNotReplayAfterPartialToolOutput(t *testing.T) {
	client := &partialToolTransientFailure{}
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
	orch.SetModelRetryLimit(2)
	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-partial-tool", &history, "hello", nil)
	if err == nil {
		t.Fatal("expected partial stream failure")
	}
	if client.calls != 1 {
		t.Fatalf("provider requests = %d, want no replay", client.calls)
	}
}

func TestOrchestratorPersistsTextOnlyPartialAssistantAfterProviderFailure(t *testing.T) {
	client := &partialTextTransientFailure{}
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
	var history []llm.Message
	_, _, err := runMessageTurnInline(t, orch, context.Background(), "session-partial-text", &history, "hello", nil)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if len(history) != 2 {
		t.Fatalf("history = %#v, want user plus partial assistant", history)
	}
	partial := history[1]
	if partial.Role != "assistant" || partial.Content != "partial answer" || partial.ReasoningContent != "" || len(partial.ToolCalls) != 0 {
		t.Fatalf("persisted partial message = %#v", partial)
	}
	if partial.Provenance == nil || partial.Provenance.Operation != "partial_response" {
		t.Fatalf("partial provenance = %#v", partial.Provenance)
	}
	if client.calls != 1 {
		t.Fatalf("provider requests = %d, want no replay after output", client.calls)
	}
}

func TestRetryDelayHonorsRetryAfter(t *testing.T) {
	want := 17 * time.Second
	got := retryDelayFor(&llm.ProviderHTTPError{StatusCode: http.StatusServiceUnavailable, RetryAfter: want}, 2)
	if got != want {
		t.Fatalf("retry delay = %s, want Retry-After %s", got, want)
	}
	if !retryAfterTooLong(&llm.ProviderHTTPError{StatusCode: http.StatusServiceUnavailable, RetryAfter: 31 * time.Second}) {
		t.Fatal("long Retry-After should skip automatic retry")
	}
}

type partialToolTransientFailure struct{ calls int }

type partialTextTransientFailure struct{ calls int }

func (c *partialTextTransientFailure) StreamChat(_ context.Context, _ llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	if handler.OnDelta != nil {
		handler.OnDelta("partial answer")
	}
	if handler.OnReasoningDelta != nil {
		handler.OnReasoningDelta("private reasoning")
	}
	if handler.OnToolCallDelta != nil {
		handler.OnToolCallDelta([]llm.ToolCall{{Function: llm.ToolCallFunction{Arguments: `{"command":"unsafe"`}}})
	}
	return llm.ChatResult{}, &llm.ProviderHTTPError{StatusCode: http.StatusServiceUnavailable}
}
func (*partialTextTransientFailure) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*partialTextTransientFailure) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

type retryTestOpenAIClient struct{ *llm.OpenAIClient }

func (retryTestOpenAIClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func (c *partialToolTransientFailure) StreamChat(_ context.Context, _ llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	if handler.OnToolCallDelta != nil {
		handler.OnToolCallDelta([]llm.ToolCall{{Function: llm.ToolCallFunction{Arguments: `{"command":`}}})
	}
	return llm.ChatResult{}, &llm.ProviderHTTPError{StatusCode: http.StatusServiceUnavailable}
}
func (*partialToolTransientFailure) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*partialToolTransientFailure) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestWaitModelRetryStopsPromptlyWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	if err := waitModelRetry(ctx, 2, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want cancellation", err)
	}
	if time.Since(started) > 300*time.Millisecond {
		t.Fatal("retry wait did not stop promptly")
	}
}

func TestOrchestratorChecksToolBudgetBeforeSideEffect(t *testing.T) {
	reg := testRegistry(t)
	executor := &countingExecutor{Registry: reg}
	pol := policy.NewDefaultEngine()
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(16, logx.Discard()), alwaysToolMock{}, executor, pol, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.ToolResultConfigOrDefault(hooks.ToolResultConfig{WorkspaceRoot: t.TempDir()})}, logx.Discard())
	orch.SetToolBudgetCheck(func(string, string) (bool, string) { return false, "max_tool_calls" })

	var history []llm.Message
	outcome := orch.RunHumanMessageTurn(context.Background(), "session-1", &history, llm.UserMessage("run", llm.UserNameHuman))
	if outcome.Err != nil || !outcome.ScheduleToolResult {
		t.Fatalf("overflow batch should close and enter budget continuation: %+v", outcome)
	}
	if executor.calls != 0 {
		t.Fatalf("tool side effect calls = %d, want 0", executor.calls)
	}
	if len(history) < 3 || history[len(history)-1].Role != "tool" || history[len(history)-1].Content != "ERROR: turn budget exhausted" {
		t.Fatalf("budget history = %+v", history)
	}
}

func TestOrchestratorStopsBeforeModelWhenLifecycleFactFails(t *testing.T) {
	client := &transientModelFailureMock{}
	orch := testOrchestrator(t, stream.NewHub(16, logx.Discard()), client)
	orch.SetLifecycleCommandSink(func(string, TurnCommand) error {
		return errors.New("lifecycle store unavailable")
	})

	var history []llm.Message
	outcome := orch.RunHumanMessageTurn(context.Background(), "session-1", &history, llm.UserMessage("hello", llm.UserNameHuman))
	if outcome.Err == nil || !strings.Contains(outcome.Err.Error(), "lifecycle") {
		t.Fatalf("outcome error = %v, want lifecycle error", outcome.Err)
	}
	if client.Calls() != 0 {
		t.Fatalf("provider calls = %d, want 0", client.Calls())
	}
}

func TestOrchestratorStopsBeforeToolWhenExecutionFactFails(t *testing.T) {
	reg := testRegistry(t)
	executor := &countingExecutor{Registry: reg}
	pol := policy.NewDefaultEngine()
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(16, logx.Discard()), alwaysToolMock{}, executor, pol, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{}, logx.Discard())
	orch.SetLifecycleCommandSink(func(_ string, command TurnCommand) error {
		if command.Type == CommandToolExecutionStarted {
			return errors.New("execution lifecycle store unavailable")
		}
		return nil
	})

	var history []llm.Message
	outcome := orch.RunHumanMessageTurn(context.Background(), "session-1", &history, llm.UserMessage("run", llm.UserNameHuman))
	if outcome.Err == nil || !strings.Contains(outcome.Err.Error(), "execution") {
		t.Fatalf("outcome error = %v, want execution lifecycle error", outcome.Err)
	}
	if executor.calls != 0 {
		t.Fatalf("tool side effect calls = %d, want 0", executor.calls)
	}
	if len(history) == 0 || history[len(history)-1].Role != "tool" {
		t.Fatalf("history = %#v, want a compensating tool result", history)
	}
}

type transientModelFailureMock struct {
	mu    sync.Mutex
	calls int
}

func (m *transientModelFailureMock) StreamChat(_ context.Context, _ llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.mu.Unlock()
	if call == 1 {
		return llm.ChatResult{}, &llm.ProviderHTTPError{StatusCode: 503, Body: "temporary upstream timeout"}
	}
	if handler.OnDelta != nil {
		handler.OnDelta("recovered")
	}
	return llm.ChatResult{Content: "recovered", FinishReason: "stop"}, nil
}

func (m *transientModelFailureMock) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *transientModelFailureMock) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *transientModelFailureMock) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

type countingExecutor struct {
	*tools.Registry
	calls int
}

func (e *countingExecutor) Execute(ctx context.Context, name, arguments string) (string, error) {
	e.calls++
	return e.Registry.Execute(ctx, name, arguments)
}

func countLifecycleCommands(commands []TurnCommand, kind CommandType) int {
	count := 0
	for _, command := range commands {
		if command.Type == kind {
			count++
		}
	}
	return count
}
