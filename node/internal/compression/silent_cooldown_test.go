package compression

import (
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestShouldStartSilentPendingDedup(t *testing.T) {
	t.Parallel()

	c := NewCoordinator(nil, 50, 100)
	c.mu.Lock()
	c.readyCompressions["sess"] = readyCompression{Content: "summary"}
	c.mu.Unlock()

	if c.shouldStartSilent("sess", sampleMessages()) {
		t.Fatal("expected false when ready compression pending")
	}
}

func TestShouldStartSilentCooldownBlocks(t *testing.T) {
	prevDuration := SilentCooldownDuration
	prevGrowth := SilentCooldownTokenGrowth
	t.Cleanup(func() {
		SilentCooldownDuration = prevDuration
		SilentCooldownTokenGrowth = prevGrowth
	})
	SilentCooldownDuration = time.Minute
	SilentCooldownTokenGrowth = 1_000_000

	msgs := sampleMessages()
	current := llm.EstimateMessageTokens(msgs)

	c := NewCoordinator(nil, 50, 100)
	c.mu.Lock()
	c.silentCooldown = map[string]silentCooldownState{
		"sess": {
			lastAppliedAt:     time.Now(),
			lastAppliedTokens: current - 10,
		},
	}
	c.mu.Unlock()

	if c.shouldStartSilent("sess", msgs) {
		t.Fatal("expected cooldown to block silent restart")
	}
}

func TestShouldStartSilentCooldownAllowsAfterTokenGrowth(t *testing.T) {
	prevDuration := SilentCooldownDuration
	prevGrowth := SilentCooldownTokenGrowth
	t.Cleanup(func() {
		SilentCooldownDuration = prevDuration
		SilentCooldownTokenGrowth = prevGrowth
	})
	SilentCooldownDuration = time.Hour
	SilentCooldownTokenGrowth = 50

	msgs := sampleMessages()
	current := llm.EstimateMessageTokens(msgs)

	c := NewCoordinator(nil, 50, 100)
	c.mu.Lock()
	c.silentCooldown = map[string]silentCooldownState{
		"sess": {
			lastAppliedAt:     time.Now(),
			lastAppliedTokens: current - SilentCooldownTokenGrowth,
		},
	}
	c.mu.Unlock()

	if !c.shouldStartSilent("sess", msgs) {
		t.Fatal("expected token growth to lift cooldown")
	}
}

func TestSilentCooldownSkipsRepeatSidecar(t *testing.T) {
	prevDuration := SilentCooldownDuration
	prevGrowth := SilentCooldownTokenGrowth
	t.Cleanup(func() {
		SilentCooldownDuration = prevDuration
		SilentCooldownTokenGrowth = prevGrowth
	})
	SilentCooldownDuration = time.Minute
	SilentCooldownTokenGrowth = 1_000_000

	client := &countingLLM{}
	coord := NewCoordinator(client, 10, 100_000)
	msgs := sampleMessages()
	prefix := testSidecarPrefix()

	coord.MaybeHandle(t.Context(), "sess-cool", "agent-1", nil, &msgs, prefix)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.streamCalls.Load() >= 1 && len(msgs) == 2 {
			break
		}
		coord.MaybeHandle(t.Context(), "sess-cool", "agent-1", nil, &msgs, prefix)
		time.Sleep(10 * time.Millisecond)
	}
	if client.streamCalls.Load() != 1 {
		t.Fatalf("first silent apply expected 1 sidecar call, got %d", client.streamCalls.Load())
	}
	if len(msgs) != 2 {
		t.Fatalf("expected compressed messages, got %d", len(msgs))
	}

	// 仍高于 silent 阈值，但冷却期内不应再开侧车。
	msgs = append(msgs, llm.Message{Role: "user", Content: strings.Repeat("x", 40)})
	coord.MaybeHandle(t.Context(), "sess-cool", "agent-1", nil, &msgs, prefix)
	if client.streamCalls.Load() != 1 {
		t.Fatalf("cooldown should block second silent sidecar, got %d calls", client.streamCalls.Load())
	}
}
