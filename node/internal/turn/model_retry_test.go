package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

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

func TestOrchestratorChecksToolBudgetBeforeSideEffect(t *testing.T) {
	reg := testRegistry(t)
	executor := &countingExecutor{Registry: reg}
	pol := policy.NewDefaultEngine()
	orch := NewOrchestrator("a1", t.TempDir(), stream.NewHub(16, logx.Discard()), alwaysToolMock{}, executor, pol, SkillAccess{}, nil, nil,
		hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.ToolResultConfigOrDefault(hooks.ToolResultConfig{WorkspaceRoot: t.TempDir()})}, logx.Discard())
	orch.SetToolBudgetCheck(func(string) (bool, string) { return false, "max_tool_calls" })

	var history []llm.Message
	outcome := orch.RunHumanMessageTurn(context.Background(), "session-1", &history, llm.UserMessage("run", llm.UserNameHuman))
	if !errors.Is(outcome.Err, ErrBudgetExhausted) {
		t.Fatalf("outcome error = %v", outcome.Err)
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
		return llm.ChatResult{}, fmt.Errorf("temporary upstream timeout")
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
