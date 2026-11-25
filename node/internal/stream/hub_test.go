package stream

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHubPublishSubscribe(t *testing.T) {
	h := NewHub(16, nil)
	ch := h.Subscribe(0)
	defer h.Unsubscribe(ch)

	h.Publish("sess-1", "assistant", map[string]any{"content": "hi"})
	h.Publish("sess-1", "turn_finished", map[string]any{"finish_reason": "stop"})

	var types []string
	for i := 0; i < 2; i++ {
		ev := <-ch
		types = append(types, ev.Type)
		if ev.Seq != i+1 {
			t.Fatalf("seq = %d, want %d", ev.Seq, i+1)
		}
	}
	if types[0] != "assistant" || types[1] != "turn_finished" {
		t.Fatalf("unexpected types: %v", types)
	}
}

func TestHubReplayAfterSeq(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("s", "assistant", map[string]any{"content": "1"})
	h.Publish("s", "assistant", map[string]any{"content": "2"})

	ch := h.Subscribe(1)
	defer h.Unsubscribe(ch)

	ev := <-ch
	if ev.Seq != 2 {
		t.Fatalf("seq = %d", ev.Seq)
	}
}

func TestHubEphemeralIsNotReplayed(t *testing.T) {
	h := NewHub(16, nil)
	h.PublishEphemeral("s", "typing", map[string]any{"content": "old"})
	ch := h.Subscribe(0)
	defer h.Unsubscribe(ch)

	select {
	case ev := <-ch:
		t.Fatalf("unexpected ephemeral replay: %+v", ev)
	default:
	}
	h.PublishEphemeral("s", "typing", map[string]any{"content": "new"})
	select {
	case ev := <-ch:
		if ev.Type != "typing" || ev.Data["content"] != "new" {
			t.Fatalf("unexpected live event: %+v", ev)
		}
	default:
		t.Fatal("expected live ephemeral event")
	}
}

func TestHubCurrentSeq(t *testing.T) {
	h := NewHub(16, nil)
	if got := h.CurrentSeq(); got != 0 {
		t.Fatalf("initial seq = %d", got)
	}
	h.Publish("s", "assistant", map[string]any{"content": "1"})
	if got := h.CurrentSeq(); got != 1 {
		t.Fatalf("after publish seq = %d", got)
	}
}

func TestCaptureAgentSyncSnapshotIncludesEphemeralAssistantDraft(t *testing.T) {
	h := NewHub(16, nil)
	publishAssistantChunk := func(chunkSeq uint64, content string) {
		h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
			"display_type": "delta", "item_id": "step-1/assistant/content",
			"attempt_id": "turn-1/step-1/attempt/1", "part": "content",
			"chunk_seq": chunkSeq, "content": content, "step_id": "step-1",
		})
	}
	publishAssistantChunk(1, "hello ")
	first := h.CaptureAgentSyncSnapshot("agt-draft")
	publishAssistantChunk(2, "world")
	second := h.CaptureAgentSyncSnapshot("agt-draft")

	if first.CoveredAgentSeq != 0 || first.CoveredStreamSeq != 1 || first.StreamEpoch == "" || len(first.AssistantDrafts) != 1 || first.AssistantDrafts[0].Text != "hello " || first.AssistantDrafts[0].ThroughChunkSeq != 1 || first.AssistantDrafts[0].ThroughAgentSeq != 0 {
		t.Fatalf("first snapshot did not preserve its cursor/draft cut: %+v", first)
	}
	if second.CoveredAgentSeq != 0 || second.CoveredStreamSeq != 2 || len(second.AssistantDrafts) != 1 || second.AssistantDrafts[0].Text != "hello world" || second.AssistantDrafts[0].ThroughChunkSeq != 2 || second.AssistantDrafts[0].ThroughAgentSeq != 0 {
		t.Fatalf("second snapshot did not include the full current draft: %+v", second)
	}
}

func TestLegacyAssistantEventDoesNotUpdateAssistantDraftSnapshot(t *testing.T) {
	for _, eventType := range []string{"assistant", "reasoning"} {
		t.Run(eventType, func(t *testing.T) {
			h := NewHub(16, nil)
			h.Publish("agt-draft", eventType, map[string]any{
				"display_type": "delta", "item_id": "step-1/assistant/content",
				"attempt_id": "turn-1/step-1/attempt/1", "part": "content",
				"chunk_seq": uint64(1), "content": "legacy", "step_id": "step-1",
			})
			if got := h.CaptureAgentSyncSnapshot("agt-draft"); len(got.AssistantDrafts) != 0 {
				t.Fatalf("legacy event unexpectedly populated assistant draft snapshot: %+v", got.AssistantDrafts)
			}
		})
	}
}

func TestEphemeralConversationDeltaUpdatesSnapshotWithoutAdvancingReplayCursor(t *testing.T) {
	h := NewHub(16, nil)
	ev := h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
		"display_type": "delta", "item_id": "step-1/assistant/content",
		"attempt_id": "turn-1/step-1/attempt/1", "turn_id": "turn-1", "step_id": "step-1",
		"part": "content", "chunk_seq": uint64(1), "content": "streamed",
	})
	if ev.Delivery != "ephemeral" || ev.AgentSeq != 0 || h.CurrentAgentSeq("agt-draft") != 0 {
		t.Fatalf("conversation delta advanced reliable cursor: event=%+v current=%d", ev, h.CurrentAgentSeq("agt-draft"))
	}
	snapshot := h.CaptureAgentSyncSnapshot("agt-draft")
	if snapshot.CoveredStreamSeq != ev.Seq || snapshot.CoveredAgentSeq != 0 || len(snapshot.AssistantDrafts) != 1 {
		t.Fatalf("snapshot cut did not cover ephemeral delta: %+v", snapshot)
	}
	draft := snapshot.AssistantDrafts[0]
	if draft.Text != "streamed" || draft.ThroughChunkSeq != 1 || draft.ThroughStreamSeq != ev.Seq || draft.TurnID != "turn-1" {
		t.Fatalf("draft snapshot missing ephemeral identity/watermark: %+v", draft)
	}
	ch := h.SubscribeAgent(0, "agt-draft")
	defer h.Unsubscribe(ch)
	select {
	case replayed := <-ch:
		t.Fatalf("ephemeral delta was replayed: %+v", replayed)
	default:
	}
}

func TestTerminalConversationPatchRejectsLateAssistantDelta(t *testing.T) {
	h := NewHub(16, nil)
	base := map[string]any{
		"display_type": "delta", "item_id": "step-1/assistant/content",
		"attempt_id": "turn-1/step-1/attempt/1", "turn_id": "turn-1", "step_id": "step-1",
		"part": "content", "content": "answer",
	}
	first := make(map[string]any, len(base)+1)
	for key, value := range base {
		first[key] = value
	}
	first["chunk_seq"] = uint64(1)
	h.PublishEphemeral("agt-draft", "conversation_delta", first)
	h.Publish("agt-draft", "conversation_patch", map[string]any{
		"conversation_id": "agt-draft",
		"turn_state":      map[string]any{"terminal": true, "turn_id": "turn-1"},
	})
	late := make(map[string]any, len(base)+1)
	for key, value := range base {
		late[key] = value
	}
	late["chunk_seq"] = uint64(2)
	late["content"] = " late"
	h.PublishEphemeral("agt-draft", "conversation_delta", late)
	if got := h.CaptureAgentSyncSnapshot("agt-draft"); len(got.AssistantDrafts) != 0 {
		t.Fatalf("late delta resurrected a finalized draft: %+v", got.AssistantDrafts)
	}
}

func TestRetryLifecycleRejectsLateChunksFromRetiredAttempt(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("agt-draft", "turn_state", map[string]any{
		"command": "model_request_started", "turn_id": "turn-1", "step_id": "step-1", "attempt_id": "attempt-1",
	})
	h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
		"display_type": "delta", "item_id": "step-1/assistant/content", "attempt_id": "attempt-1",
		"turn_id": "turn-1", "step_id": "step-1", "part": "content", "chunk_seq": uint64(1), "content": "old",
	})
	h.Publish("agt-draft", "turn_state", map[string]any{
		"command": "model_request_started", "turn_id": "turn-1", "step_id": "step-1", "attempt_id": "attempt-2",
	})
	h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
		"display_type": "delta", "item_id": "step-1/assistant/content", "attempt_id": "attempt-1",
		"turn_id": "turn-1", "step_id": "step-1", "part": "content", "chunk_seq": uint64(2), "content": "late",
	})
	h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
		"display_type": "delta", "item_id": "step-1/assistant/content", "attempt_id": "attempt-2",
		"turn_id": "turn-1", "step_id": "step-1", "part": "content", "chunk_seq": uint64(1), "content": "new",
	})
	snapshot := h.CaptureAgentSyncSnapshot("agt-draft")
	if len(snapshot.AssistantDrafts) != 1 || snapshot.AssistantDrafts[0].AttemptID != "attempt-2" || snapshot.AssistantDrafts[0].Text != "new" {
		t.Fatalf("snapshot retained stale attempt content: %+v", snapshot.AssistantDrafts)
	}
}

func TestCaptureAgentSyncSnapshotDropsDraftWhenAssistantBecomesCanonical(t *testing.T) {
	h := NewHub(16, nil)
	h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
		"display_type": "delta", "item_id": "step-1/assistant/content",
		"attempt_id": "turn-1/step-1/attempt/1", "part": "content",
		"chunk_seq": uint64(1), "content": "answer", "step_id": "step-1",
	})
	h.Publish("agt-draft", "turn_state", map[string]any{
		"command": "assistant_received", "step_id": "step-1",
	})
	if got := h.CaptureAgentSyncSnapshot("agt-draft"); len(got.AssistantDrafts) != 0 || got.CoveredAgentSeq != 1 {
		t.Fatalf("assistant draft should retire at the canonical lifecycle boundary: %+v", got)
	}
}

func TestCaptureAgentSyncSnapshotDropsFailedAttemptWhenRetryStarts(t *testing.T) {
	h := NewHub(16, nil)
	h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
		"display_type": "delta", "item_id": "step-1/assistant/content",
		"attempt_id": "turn-1/step-1/attempt/1", "part": "content",
		"chunk_seq": uint64(1), "content": "partial", "step_id": "step-1",
	})
	h.Publish("agt-draft", "turn_state", map[string]any{
		"command": "model_request_started", "step_id": "step-1",
	})
	if got := h.CaptureAgentSyncSnapshot("agt-draft"); len(got.AssistantDrafts) != 0 || got.CoveredAgentSeq != 1 {
		t.Fatalf("failed attempt draft should retire when retry starts: %+v", got)
	}
}

func TestCaptureAgentSyncSnapshotClearsDraftAtTerminalAndContextBoundary(t *testing.T) {
	for _, test := range []struct {
		name  string
		type_ string
		data  map[string]any
	}{
		{name: "terminal", type_: "turn_state", data: map[string]any{"terminal": true}},
		{name: "finished", type_: "turn_finished", data: map[string]any{}},
		{name: "context boundary", type_: "runtime/notice", data: map[string]any{"message_kind": "context_boundary"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := NewHub(16, nil)
			h.PublishEphemeral("agt-draft", "conversation_delta", map[string]any{
				"display_type": "delta", "item_id": "step-1/assistant/content",
				"attempt_id": "turn-1/step-1/attempt/1", "turn_id": "turn-1", "part": "content",
				"chunk_seq": uint64(1), "content": "answer", "step_id": "step-1",
			})
			data := test.data
			if test.type_ == "turn_state" || test.type_ == "turn_finished" {
				data = make(map[string]any, len(test.data)+1)
				for key, value := range test.data {
					data[key] = value
				}
				data["turn_id"] = "turn-1"
			}
			h.Publish("agt-draft", test.type_, data)
			if got := h.CaptureAgentSyncSnapshot("agt-draft"); len(got.AssistantDrafts) != 0 {
				t.Fatalf("draft survived %s boundary: %+v", test.name, got.AssistantDrafts)
			}
		})
	}
}

func TestTerminalChildTurnPreservesParentAssistantDraft(t *testing.T) {
	h := NewHub(16, nil)
	for _, item := range []struct {
		turnID, stepID, attemptID, text string
	}{
		{turnID: "parent-turn", stepID: "parent-step", attemptID: "parent-attempt", text: "parent prefix"},
		{turnID: "child-turn", stepID: "child-step", attemptID: "child-attempt", text: "child prefix"},
	} {
		h.Publish("agt-shared", "turn_state", map[string]any{
			"command": "model_request_started", "turn_id": item.turnID, "step_id": item.stepID, "attempt_id": item.attemptID,
		})
		h.PublishEphemeral("agt-shared", "conversation_delta", map[string]any{
			"display_type": "delta", "item_id": item.stepID + "/assistant/content", "attempt_id": item.attemptID,
			"turn_id": item.turnID, "step_id": item.stepID, "part": "content", "chunk_seq": uint64(1), "content": item.text,
		})
	}
	h.Publish("agt-shared", "turn_state", map[string]any{"terminal": true, "turn_id": "child-turn"})
	h.PublishEphemeral("agt-shared", "conversation_delta", map[string]any{
		"display_type": "delta", "item_id": "parent-step/assistant/content", "attempt_id": "parent-attempt",
		"turn_id": "parent-turn", "step_id": "parent-step", "part": "content", "chunk_seq": uint64(2), "content": " continued",
	})

	snapshot := h.CaptureAgentSyncSnapshot("agt-shared")
	if len(snapshot.AssistantDrafts) != 1 {
		t.Fatalf("child completion cleared unrelated parent drafts: %+v", snapshot.AssistantDrafts)
	}
	draft := snapshot.AssistantDrafts[0]
	if draft.TurnID != "parent-turn" || draft.ItemID != "parent-step/assistant/content" || draft.Text != "parent prefix continued" || draft.ThroughChunkSeq != 2 {
		t.Fatalf("parent draft after child completion = %+v", draft)
	}
}

func TestHubAgentCursorSkipsForeignAndEphemeralEvents(t *testing.T) {
	h := NewHub(16, nil)
	first := h.Publish("agt-a", "assistant", map[string]any{"content": "one"})
	h.PublishEphemeral("agt-a", "execution", map[string]any{"event": "process_output"})
	h.Publish("agt-b", "assistant", map[string]any{"content": "foreign"})
	finished := h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})

	if first.AgentSeq != 1 || finished.AgentSeq != 2 {
		t.Fatalf("agent seq = %d, %d; want 1, 2", first.AgentSeq, finished.AgentSeq)
	}
	if first.StreamEpoch == "" || first.StreamEpoch != finished.StreamEpoch {
		t.Fatalf("stream epoch not stable: %q / %q", first.StreamEpoch, finished.StreamEpoch)
	}
	if first.Delivery != "replayable" {
		t.Fatalf("replayable delivery = %q", first.Delivery)
	}

	sub := h.SubscribeAgentCursor(1, 1, "agt-a")
	defer h.Unsubscribe(sub.Events)
	select {
	case ev := <-sub.Events:
		if ev.Type != "turn_finished" || ev.AgentSeq != 2 {
			t.Fatalf("unexpected agent replay: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent replay")
	}
	select {
	case ev := <-sub.Events:
		t.Fatalf("unexpected extra agent replay: %+v", ev)
	default:
	}
}

func TestHubAgentCursorReportsHistoryTruncation(t *testing.T) {
	h := NewHub(2, nil)
	h.Publish("agt-a", "assistant", map[string]any{"content": "one"})
	h.Publish("agt-a", "assistant", map[string]any{"content": "two"})
	h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})

	sub := h.SubscribeAgentCursor(0, 0, "agt-a")
	defer h.Unsubscribe(sub.Events)
	if !sub.ResyncRequired {
		t.Fatal("expected resync when requested agent history is truncated")
	}
	if sub.StreamEpoch == "" || sub.CurrentAgentSeq != 3 {
		t.Fatalf("bad subscription cursor: %+v", sub)
	}
}

func TestHubGlobalCursorFromPreviousProcessResyncsAndClamps(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("agt-a", "assistant", map[string]any{"content": "new process history"})

	sub := h.SubscribeAgentCursor(42, 0, "")
	defer h.Unsubscribe(sub.Events)
	if !sub.ResyncRequired {
		t.Fatal("expected resync when global cursor is ahead of this process")
	}

	want := h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})
	select {
	case got := <-sub.Events:
		if got.Seq != want.Seq {
			t.Fatalf("received seq %d, want %d after cursor clamp", got.Seq, want.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("new process events were filtered by the stale cursor")
	}
}

func TestHubAgentCursorFromPreviousProcessResyncsAndClamps(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("agt-a", "assistant", map[string]any{"content": "new process history"})

	sub := h.SubscribeAgentCursor(0, 42, "agt-a")
	defer h.Unsubscribe(sub.Events)
	if !sub.ResyncRequired {
		t.Fatal("expected resync when agent cursor is ahead of this process")
	}

	want := h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})
	select {
	case got := <-sub.Events:
		if got.AgentSeq != want.AgentSeq {
			t.Fatalf("received agent seq %d, want %d after cursor clamp", got.AgentSeq, want.AgentSeq)
		}
	case <-time.After(time.Second):
		t.Fatal("new process agent events were filtered by the stale cursor")
	}
}

func TestHubAgentCursorReportsStaleHighWatermarkAfterRestart(t *testing.T) {
	oldHub := NewHub(16, nil)
	oldHub.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})

	// A fresh Hub represents a restarted Node: its epoch and Agent cursor are
	// new, while the browser still reconnects with the previous cursor.
	newHub := NewHub(16, nil)
	sub := newHub.SubscribeAgentCursor(0, 1, "agt-a")
	defer newHub.Unsubscribe(sub.Events)
	if !sub.ResyncRequired {
		t.Fatalf("expected resync for stale high watermark: %+v", sub)
	}
	if sub.CurrentAgentSeq != 0 {
		t.Fatalf("fresh Hub Agent cursor = %d, want 0", sub.CurrentAgentSeq)
	}
	select {
	case ev := <-sub.Events:
		t.Fatalf("unexpected replay from fresh Hub: %+v", ev)
	default:
	}

	// The same rule applies after the new process has emitted fewer events
	// than the old cursor; otherwise those events would remain invisible.
	newHub.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})
	sub2 := newHub.SubscribeAgentCursor(0, 5, "agt-a")
	defer newHub.Unsubscribe(sub2.Events)
	if !sub2.ResyncRequired {
		t.Fatalf("expected resync for cursor ahead of current: %+v", sub2)
	}
}

func TestHubSubscribeLiveSkipsHistory(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("s", "assistant", map[string]any{"content": "old"})
	ch := h.Subscribe(h.CurrentSeq())
	defer h.Unsubscribe(ch)
	h.Publish("s", "assistant", map[string]any{"content": "new"})
	select {
	case ev := <-ch:
		if ev.Data["content"] != "new" {
			t.Fatalf("unexpected replay/live: %+v", ev.Data)
		}
	default:
		t.Fatal("expected live event")
	}
}

func TestEventFormatSSE(t *testing.T) {
	ev := Event{
		AgentID: "agt-x",
		Type:    "assistant",
		Seq:     3,
		TS:      "2026-05-27T00:00:00Z",
		Data:    map[string]any{"content": "ok"},
	}
	s := ev.FormatSSE()
	if !strings.Contains(s, "event: assistant") || !strings.Contains(s, "id: 3") {
		t.Fatalf("bad sse: %q", s)
	}
	if strings.Contains(s, "session_id") {
		t.Fatalf("wire SSE must not include session_id: %q", s)
	}
	if !strings.Contains(s, `"agent_id":"agt-x"`) {
		t.Fatalf("expected agent_id in sse: %q", s)
	}
}

func TestHubDisconnectsSlowSubscriberOnOverflowWithoutReordering(t *testing.T) {
	h := NewHub(16, nil)
	ch := h.Subscribe(0)
	for i := 0; i < defaultSubscriberBuffer; i++ {
		h.Publish("agt-1", "assistant", map[string]any{"content": "x"})
	}
	// The next event overflows the queue. It must disconnect this subscriber
	// rather than retrying outside the Hub lock and overtaking later events.
	h.Publish("agt-1", "turn_finished", map[string]any{"finish_reason": "stop", "turn_complete": true})
	h.Publish("agt-1", "assistant", map[string]any{"content": "later"})

	for wantSeq := 1; wantSeq <= defaultSubscriberBuffer; wantSeq++ {
		ev, ok := <-ch
		if !ok || ev.Seq != wantSeq {
			t.Fatalf("queued event %d = %+v, open=%v", wantSeq, ev, ok)
		}
	}
	if ev, ok := <-ch; ok {
		t.Fatalf("overflowing event or later event was delivered after disconnect: %+v", ev)
	}
	h.Unsubscribe(ch) // closed-channel cleanup remains idempotent
	reconnected := h.SubscribeAgentCursor(0, 0, "agt-1")
	defer h.Unsubscribe(reconnected.Events)
	if !reconnected.ResyncRequired {
		t.Fatal("reconnect after an overflow beyond retention must require resync")
	}
}

func TestHubSubscribeAgentFiltersOtherAgents(t *testing.T) {
	h := NewHub(64, nil)
	chA := h.SubscribeAgent(0, "agt-a")
	defer h.Unsubscribe(chA)

	h.Publish("agt-b", "assistant", map[string]any{"content": "noise"})
	h.Publish("agt-b", "turn_finished", map[string]any{"finish_reason": "stop"})
	h.Publish("agt-a", "assistant", map[string]any{"content": "mine"})
	h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop", "turn_complete": true})

	var types []string
	for i := 0; i < 2; i++ {
		select {
		case ev := <-chA:
			if ev.AgentID != "agt-a" {
				t.Fatalf("got other agent event: %+v", ev)
			}
			types = append(types, ev.Type)
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for agt-a event %d", i)
		}
	}
	if types[0] != "assistant" || types[1] != "turn_finished" {
		t.Fatalf("types = %v", types)
	}
	select {
	case ev := <-chA:
		t.Fatalf("unexpected extra event: %+v", ev)
	default:
	}
}

func TestHubSubscribeAgentIgnoresForeignFlood(t *testing.T) {
	h := NewHub(64, nil)
	chA := h.SubscribeAgent(0, "agt-a")
	defer h.Unsubscribe(chA)

	// 他 Agent 洪峰不应占满 A 的缓冲
	for i := 0; i < 500; i++ {
		h.Publish("agt-b", "assistant", map[string]any{"content": "x"})
	}
	h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop", "turn_complete": true})

	select {
	case ev := <-chA:
		if ev.Type != "turn_finished" || ev.AgentID != "agt-a" {
			t.Fatalf("expected agt-a turn_finished, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agt-a turn_finished blocked/dropped by foreign agent flood")
	}
}

func TestHubConcurrentAgentFanoutPreservesPerAgentOrderAndIsolation(t *testing.T) {
	const agentCount = 20
	const chunksPerAgent = 10
	eventsPerAgent := chunksPerAgent + 1 // replayable turn_state + ephemeral deltas
	totalEvents := agentCount * eventsPerAgent
	h := NewHub(totalEvents+1, nil)
	global := h.Subscribe(0)
	defer h.Unsubscribe(global)
	perAgent := make(map[string]chan Event, agentCount)
	for index := 0; index < agentCount; index++ {
		agentID := fmt.Sprintf("agent-%02d", index+1)
		perAgent[agentID] = h.SubscribeAgent(0, agentID)
		defer h.Unsubscribe(perAgent[agentID])
	}

	start := make(chan struct{})
	var publishers sync.WaitGroup
	for agentID := range perAgent {
		agentID := agentID
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			<-start
			h.Publish(agentID, "turn_state", map[string]any{"turn_id": agentID + "-turn"})
			for chunk := 1; chunk <= chunksPerAgent; chunk++ {
				h.PublishEphemeral(agentID, "conversation_delta", map[string]any{
					"item_id":   fmt.Sprintf("%s/assistant/content", agentID),
					"turn_id":   agentID + "-turn",
					"chunk_seq": uint64(chunk),
					"content":   fmt.Sprintf("%s-%02d", agentID, chunk),
				})
			}
		}()
	}
	close(start)
	publishers.Wait()

	nextAgentSeq := make(map[string]int, agentCount)
	for expectedGlobalSeq := 1; expectedGlobalSeq <= totalEvents; expectedGlobalSeq++ {
		select {
		case event := <-global:
			if event.Seq != expectedGlobalSeq {
				t.Fatalf("global sequence = %d, want %d", event.Seq, expectedGlobalSeq)
			}
			if _, ok := perAgent[event.AgentID]; !ok {
				t.Fatalf("unknown Agent received in global fanout: %q", event.AgentID)
			}
			if event.Delivery == "replayable" {
				nextAgentSeq[event.AgentID]++
				if event.AgentSeq != nextAgentSeq[event.AgentID] {
					t.Fatalf("%s replayable Agent sequence = %d, want %d", event.AgentID, event.AgentSeq, nextAgentSeq[event.AgentID])
				}
			} else if event.Delivery != "ephemeral" || event.AgentSeq != 0 {
				t.Fatalf("unexpected ephemeral sequence contract: %+v", event)
			}
			if event.Data["turn_id"] != event.AgentID+"-turn" {
				t.Fatalf("event identity crossed Agents: %+v", event)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for global event %d/%d", expectedGlobalSeq, totalEvents)
		}
	}

	for agentID, events := range perAgent {
		for expectedEvent := 0; expectedEvent < eventsPerAgent; expectedEvent++ {
			select {
			case event := <-events:
				if event.AgentID != agentID || event.Data["turn_id"] != agentID+"-turn" {
					t.Fatalf("%s subscriber received foreign or mismatched event: %+v", agentID, event)
				}
				if expectedEvent == 0 {
					if event.Type != "turn_state" || event.Delivery != "replayable" || event.AgentSeq != 1 {
						t.Fatalf("%s first event = %+v, want replayable turn_state at Agent sequence 1", agentID, event)
					}
				} else if event.Type != "conversation_delta" || event.Delivery != "ephemeral" || event.AgentSeq != 0 {
					t.Fatalf("%s delta event = %+v, want ephemeral delta with no Agent sequence", agentID, event)
				}
			case <-time.After(time.Second):
				t.Fatalf("timed out waiting for %s event %d/%d", agentID, expectedEvent+1, eventsPerAgent)
			}
		}
		select {
		case event := <-events:
			t.Fatalf("%s subscriber received extra event: %+v", agentID, event)
		default:
		}
	}
}
