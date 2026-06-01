package compression

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type countingLLM struct {
	calls atomic.Int32
}

func (c *countingLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{}, nil
}

func (c *countingLLM) CompleteText(_ context.Context, _ llm.CompleteRequest) (string, error) {
	c.calls.Add(1)
	return "任务目标：t\n重要结论：c\n修改过的文件和资源：无\n下一步动作：n", nil
}

func longUserMessage() string {
	return strings.Repeat("word ", 120)
}

func sampleMessages() []llm.Message {
	return []llm.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: longUserMessage()},
		{Role: "assistant", Content: "收到"},
		{Role: "user", Content: "继续"},
		{Role: "assistant", Content: "好的"},
	}
}

func TestShouldCompressBlockingPriority(t *testing.T) {
	msgs := sampleMessages()
	total := estimateTokens(msgs)
	d := shouldCompress(msgs, 10, total)
	if !d.Should || d.TriggerLevel != "blocking" {
		t.Fatalf("decision = %+v total=%d", d, total)
	}
}

func TestBlockingCompressionApplies(t *testing.T) {
	client := &countingLLM{}
	coord := NewCoordinator(client, 0, 50)
	hub := stream.NewHub(16, nil)
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	msgs := sampleMessages()
	coord.MaybeHandle(context.Background(), "sess-1", "agent-1", hub, &msgs)
	if len(msgs) != 2 {
		t.Fatalf("expected compressed len 2 (summary + last assistant), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || !strings.Contains(msgs[0].Content, "任务目标") {
		t.Fatalf("replacement = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "好的" {
		t.Fatalf("tail assistant = %+v", msgs[1])
	}
	if client.calls.Load() != 1 {
		t.Fatalf("CompleteText calls = %d", client.calls.Load())
	}

	var gotStart, gotEndApplied bool
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
			}
		case <-deadline:
			t.Fatalf("timeout start=%v end_applied=%v", gotStart, gotEndApplied)
		}
	}
}

func TestSilentCompressionAsyncApply(t *testing.T) {
	client := &countingLLM{}
	coord := NewCoordinator(client, 50, 0)
	msgs := sampleMessages()
	coord.MaybeHandle(context.Background(), "sess-2", "agent-1", nil, &msgs)
	if len(msgs) == 2 {
		t.Fatal("silent should not apply synchronously on first handle")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.calls.Load() >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if client.calls.Load() < 1 {
		t.Fatalf("silent should start LLM call, got %d", client.calls.Load())
	}

	for time.Now().Before(deadline) {
		coord.MaybeHandle(context.Background(), "sess-2", "agent-1", nil, &msgs)
		if len(msgs) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("silent result not applied, len=%d", len(msgs))
}

func TestStalePendingDiscarded(t *testing.T) {
	client := &countingLLM{}
	coord := NewCoordinator(client, 50, 0)
	msgs := sampleMessages()
	coord.MaybeHandle(context.Background(), "sess-3", "agent-1", nil, &msgs)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && client.calls.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}

	msgs[1].Content = "modified within compressed range"
	coord.MaybeHandle(context.Background(), "sess-3", "agent-1", nil, &msgs)
	if len(msgs) == 2 {
		t.Fatal("stale compression should not apply after slice content changed")
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
