package compression

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type countingLLM struct {
	streamCalls       atomic.Int32
	completeTextCalls atomic.Int32
	lastReq           llm.ChatRequest
}

func (c *countingLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.streamCalls.Add(1)
	c.lastReq = req
	content := "[示例母任务]\n[示例子任务]\n任务目标：t；阶段性总结论：c；修改过的文件和资源：无"
	if handler.OnDelta != nil {
		handler.OnDelta(content)
	}
	if handler.OnUsage != nil {
		handler.OnUsage(llm.Usage{
			PromptTokens:          1000,
			CompletionTokens:      400,
			TotalTokens:           1400,
			PromptCacheHitTokens:  800,
			PromptCacheMissTokens: 200,
		})
	}
	return llm.ChatResult{Content: content}, nil
}

func (c *countingLLM) CompleteText(_ context.Context, _ llm.CompleteRequest) (string, error) {
	c.completeTextCalls.Add(1)
	return "", fmt.Errorf("CompleteText should not be used for compression")
}

func (c *countingLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func testSidecarPrefix() SidecarPrefix {
	return SidecarPrefix{
		SystemPrompt: "agent-system",
		Tools:        sampleSidecarTools(),
	}
}

func longUserMessage() string {
	return strings.Repeat("word ", 120)
}

func sampleMessages() []llm.Message {
	return []llm.Message{
		{Role: "user", Content: longUserMessage()},
		{Role: "assistant", Content: "收到"},
		{Role: "user", Content: "继续"},
		{Role: "assistant", Content: "好的"},
	}
}

func TestShouldCompressBlockingPriority(t *testing.T) {
	msgs := sampleMessages()
	total := llm.EstimateMessageTokens(msgs)
	d := evaluateCompression(msgs, 10, total).Decision
	if !d.Should || d.TriggerLevel != "blocking" {
		t.Fatalf("decision = %+v total=%d", d, total)
	}
}

func TestBlockingCompressionApplies(t *testing.T) {
	client := &countingLLM{}
	coord := NewCoordinator(client, 0, 50)
	coord.SetRawMessageHistoryEnabled(true)
	hub := stream.NewHub(16, nil)
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	msgs := sampleMessages()
	prefix := testSidecarPrefix()
	coord.MaybeHandle(context.Background(), "sess-1", "agent-1", hub, &msgs, prefix)
	if len(msgs) != 2 {
		t.Fatalf("expected compressed len 2 (summary + last assistant), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Name != llm.UserNameCompression || !strings.Contains(msgs[0].Content, "阶段性总结论") {
		t.Fatalf("replacement = %+v", msgs[0])
	}
	if !strings.Contains(msgs[0].Content, "Node 已将原始消息记录到 <runtime_root>/history/") {
		t.Fatalf("expected journal footer, got %q", msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "好的" {
		t.Fatalf("tail assistant = %+v", msgs[1])
	}
	if client.streamCalls.Load() != 1 {
		t.Fatalf("StreamChat calls = %d", client.streamCalls.Load())
	}
	if client.completeTextCalls.Load() != 0 {
		t.Fatalf("CompleteText calls = %d", client.completeTextCalls.Load())
	}
	if client.lastReq.SystemPrompt != prefix.SystemPrompt {
		t.Fatalf("system = %q", client.lastReq.SystemPrompt)
	}
	if len(client.lastReq.Tools) != len(prefix.Tools) {
		t.Fatalf("tools = %+v", client.lastReq.Tools)
	}

	var gotStart, gotEndApplied, gotMetrics bool
	deadline := time.After(2 * time.Second)
	for !(gotStart && gotEndApplied) {
		select {
		case ev := <-ch:
			if ev.Type != "context_compression_blocking" {
				continue
			}
			phase, _ := ev.Data["phase"].(string)
			status, _ := ev.Data["status"].(string)
			if phase == "start" {
				gotStart = true
			}
			if phase == "end" && status == "applied" {
				gotEndApplied = true
				if ev.Data["prompt_tokens"] != nil && ev.Data["prompt_cache_hit_tokens"] != nil {
					gotMetrics = true
				}
			}
		case <-deadline:
			t.Fatalf("timeout start=%v end_applied=%v metrics=%v", gotStart, gotEndApplied, gotMetrics)
		}
	}
	if !gotMetrics {
		t.Fatal("expected compression metrics in applied SSE event")
	}

	snap, ok := coord.LastCompression("sess-1")
	if !ok {
		t.Fatal("expected last compression snapshot")
	}
	if snap.PromptTokens != 1000 || snap.PromptCacheHitTokens != 800 {
		t.Fatalf("last compression = %+v", snap)
	}
}

func TestSilentCompressionAsyncApply(t *testing.T) {
	gate := make(chan struct{})
	client := &gateLLM{release: gate}
	coord := NewCoordinator(client, 50, 0)
	msgs := sampleMessages()
	prefix := testSidecarPrefix()
	coord.MaybeHandle(context.Background(), "sess-2", "agent-1", nil, &msgs, prefix)
	if len(msgs) == 2 {
		t.Fatal("silent should not apply synchronously on first handle")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !coord.hasRunningTask("sess-2") {
		time.Sleep(5 * time.Millisecond)
	}
	if !coord.hasRunningTask("sess-2") {
		t.Fatal("silent compression task should be running")
	}

	close(gate)
	for time.Now().Before(deadline) {
		coord.MaybeHandle(context.Background(), "sess-2", "agent-1", nil, &msgs, prefix)
		if len(msgs) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("silent result not applied, len=%d", len(msgs))
}

func TestStalePendingDiscarded(t *testing.T) {
	gate := make(chan struct{})
	client := &gateLLM{release: gate}
	coord := NewCoordinator(client, 50, 0)
	msgs := sampleMessages()
	prefix := testSidecarPrefix()
	coord.MaybeHandle(context.Background(), "sess-3", "agent-1", nil, &msgs, prefix)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !coord.hasRunningTask("sess-3") {
		time.Sleep(5 * time.Millisecond)
	}
	if !coord.hasRunningTask("sess-3") {
		t.Fatal("expected silent compression task to be running")
	}
	close(gate)
	waitReadyCompression(t, coord, "sess-3")

	before := len(msgs)
	msgs[1].Content = "modified within compressed range"
	out := coord.applyReadyCompression("sess-3", "agent-1", nil, &msgs)
	if out.status != "stale" {
		t.Fatalf("status = %q, want stale", out.status)
	}
	if len(msgs) != before {
		t.Fatalf("stale compression should not apply after slice content changed, len=%d want %d", len(msgs), before)
	}
}

func waitReadyCompression(t *testing.T, coord *Coordinator, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		coord.reapFinishedTask(sessionID)
		coord.mu.Lock()
		_, ok := coord.readyCompressions[sessionID]
		coord.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timeout waiting ready compression")
}

func TestBlockingCompressionMergeNextUser(t *testing.T) {
	client := &countingLLM{}
	coord := NewCoordinator(client, 0, 50)
	msgs := []llm.Message{
		{Role: "user", Content: longUserMessage()},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "follow up"},
	}
	coord.MaybeHandle(context.Background(), "sess-merge", "agent-1", nil, &msgs, testSidecarPrefix())
	if len(msgs) != 1 {
		t.Fatalf("expected merged single user, got %d: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0].Content, "阶段性总结论") || !strings.Contains(msgs[0].Content, "follow up") {
		t.Fatalf("merged content = %q", msgs[0].Content)
	}
}

func TestEnabled(t *testing.T) {
	if NewCoordinator(&countingLLM{}, 0, 0).Enabled() {
		t.Fatal("both zero should disable")
	}
	if !NewCoordinator(&countingLLM{}, 100, 0).Enabled() {
		t.Fatal("silent should enable")
	}
}

func TestForceBlockingIgnoresThreshold(t *testing.T) {
	client := &countingLLM{}
	coord := NewCoordinator(client, 0, 0)
	if coord.Enabled() {
		t.Fatal("auto compression should be disabled")
	}
	msgs := sampleMessages()
	result := coord.ForceBlocking(context.Background(), "sess-force", "agent-1", nil, &msgs, testSidecarPrefix())
	if result.Status != "applied" {
		t.Fatalf("status = %q result = %+v msgs=%d", result.Status, result, len(msgs))
	}
	if len(msgs) != 2 {
		t.Fatalf("expected compressed len 2, got %d: %+v", len(msgs), msgs)
	}
	if client.streamCalls.Load() != 1 {
		t.Fatalf("StreamChat calls = %d", client.streamCalls.Load())
	}
}

func TestForceBlockingNoop(t *testing.T) {
	coord := NewCoordinator(&countingLLM{}, 0, 0)
	msgs := []llm.Message{{Role: "user", Content: "only user"}}
	result := coord.ForceBlocking(context.Background(), "sess-noop", "agent-1", nil, &msgs, testSidecarPrefix())
	if result.Status != "noop" {
		t.Fatalf("status = %q", result.Status)
	}
}

type gateLLM struct {
	countingLLM
	release chan struct{}
}

func (g *gateLLM) StreamChat(ctx context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	select {
	case <-g.release:
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
	return g.countingLLM.StreamChat(ctx, req, handler)
}

func TestForceBlockingInProgressDuringSilent(t *testing.T) {
	gate := make(chan struct{})
	client := &gateLLM{release: gate}
	coord := NewCoordinator(client, 50, 0)
	msgs := sampleMessages()
	prefix := testSidecarPrefix()
	coord.MaybeHandle(context.Background(), "sess-dup", "agent-1", nil, &msgs, prefix)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !coord.hasRunningTask("sess-dup") {
		time.Sleep(5 * time.Millisecond)
	}
	if !coord.hasRunningTask("sess-dup") {
		t.Fatal("expected silent compression task to be running")
	}

	result := coord.ForceBlocking(context.Background(), "sess-dup", "agent-1", nil, &msgs, prefix)
	if result.Status != "in_progress" || result.TriggerLevel != "silent" {
		t.Fatalf("result = %+v", result)
	}
	if result.CompressedMessageCount <= 0 {
		t.Fatalf("expected compressed_message_count, got %+v", result)
	}

	close(gate)
	coord.waitTask("sess-dup")
}

func TestForceBlockingInProgressDuplicateManual(t *testing.T) {
	gate := make(chan struct{})
	client := &gateLLM{release: gate}
	coord := NewCoordinator(client, 0, 0)
	msgs := sampleMessages()
	msgsCopy := append([]llm.Message(nil), msgs...)
	prefix := testSidecarPrefix()

	done := make(chan ForceResult, 1)
	go func() {
		done <- coord.ForceBlocking(context.Background(), "sess-manual", "agent-1", nil, &msgs, prefix)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !coord.hasRunningTask("sess-manual") {
		time.Sleep(5 * time.Millisecond)
	}
	if !coord.hasRunningTask("sess-manual") {
		t.Fatal("expected manual compression task to be running")
	}

	dup := coord.ForceBlocking(context.Background(), "sess-manual", "agent-1", nil, &msgsCopy, prefix)
	if dup.Status != "in_progress" || dup.TriggerLevel != "blocking" {
		t.Fatalf("duplicate result = %+v", dup)
	}

	close(gate)
	first := <-done
	if first.Status != "applied" {
		t.Fatalf("first result = %+v", first)
	}
}
