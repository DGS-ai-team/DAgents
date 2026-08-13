package session

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// gatedSideEffectLLM keeps the first model step open so external facts can be
// queued while a turn is genuinely active. Subsequent calls complete quickly,
// allowing the test to verify that the buffered facts wake the model again.
type gatedSideEffectLLM struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (m *gatedSideEffectLLM) StreamChat(ctx context.Context, _ llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	call := m.calls.Add(1)
	if call == 1 {
		close(m.started)
		select {
		case <-m.release:
		case <-ctx.Done():
			return llm.ChatResult{}, ctx.Err()
		}
	}
	content := fmt.Sprintf("turn-%d-complete", call)
	if handler.OnDelta != nil {
		handler.OnDelta(content)
	}
	return llm.ChatResult{Content: content, FinishReason: "stop"}, nil
}

func (m *gatedSideEffectLLM) CompleteText(_ context.Context, _ llm.CompleteRequest) (string, error) {
	return "mock summary", nil
}

func (m *gatedSideEffectLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestActiveTurnQueuesAsyncAndTriggerUntilTurnCompletes(t *testing.T) {
	llmClient := &gatedSideEffectLLM{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.LoadFile("")
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", stream.NewHub(32, logx.Discard()), llmClient, reg, pol, nil, TurnOptions{
		SkillsEnabled:       false,
		CompressionBlocking: 0,
	}, logx.Discard())
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if rt == nil {
		t.Fatal("runtime missing")
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeMessage, "keep this turn open", nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	select {
	case <-llmClient.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first model step did not start")
	}

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "active-job-1", ToolName: "bash_run", ToolCallID: "async-active-1",
		Status: "succeeded", ResultText: "background result",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueTriggerMessage(sess.ID, "active-trigger-1", "trigger result"); err != nil {
		t.Fatal(err)
	}

	// The session consumer is still inside StreamChat. Neither external fact
	// may be visible in history before that active step returns.
	time.Sleep(50 * time.Millisecond)
	rt.mu.Lock()
	activeMessages := append([]llm.Message(nil), rt.messages...)
	rt.mu.Unlock()
	if historyContainsText(activeMessages, "active-job-1") || historyContainsText(activeMessages, "trigger result") {
		t.Fatalf("side effects mutated active history: %+v", activeMessages)
	}

	close(llmClient.release)
	waitForActiveSideEffects(t, rt, 8*time.Second)

	rt.mu.Lock()
	finalMessages := append([]llm.Message(nil), rt.messages...)
	rt.mu.Unlock()
	if !historyContainsText(finalMessages, "active-job-1") {
		t.Fatalf("async callback was not applied: %+v", finalMessages)
	}
	if !historyContainsText(finalMessages, "trigger result") {
		t.Fatalf("trigger was not applied: %+v", finalMessages)
	}
	if got := llmClient.calls.Load(); got < 2 {
		t.Fatalf("expected side effects to resume the model, calls=%d", got)
	}
}

func waitForActiveSideEffects(t *testing.T, rt *runtime, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		messages := append([]llm.Message(nil), rt.messages...)
		rt.mu.Unlock()
		if !rt.sideEffects.HasReady() && rt.queue.Len() == 0 &&
			historyContainsText(messages, "active-job-1") &&
			historyContainsText(messages, "trigger result") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	t.Fatalf("active side effects did not drain: queue=%d ready=%v messages=%s", rt.queue.Len(), rt.sideEffects.HasReady(), summarizeMessages(rt.messages))
}

func historyContainsText(messages []llm.Message, text string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.Content, text) {
			return true
		}
	}
	return false
}

func summarizeMessages(messages []llm.Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		parts = append(parts, msg.Role+":"+strings.ReplaceAll(msg.Content, "\n", "\\n"))
	}
	return strings.Join(parts, " | ")
}
