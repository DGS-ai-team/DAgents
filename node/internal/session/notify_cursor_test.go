package session

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestShouldBumpNotifySeq(t *testing.T) {
	tests := []struct {
		name string
		ev   stream.Event
		want bool
	}{
		{
			name: "hitl",
			ev:   stream.Event{AgentID: "s1", Type: "hitl_required"},
			want: true,
		},
		{
			name: "turn finished stop",
			ev: stream.Event{
				AgentID: "s1",
				Type:    "turn_finished",
				Data:    map[string]any{"finish_reason": "stop", "turn_complete": true},
			},
			want: true,
		},
		{
			name: "turn finished error",
			ev: stream.Event{
				AgentID: "s1",
				Type:    "turn_finished",
				Data:    map[string]any{"finish_reason": "error", "turn_complete": true},
			},
			want: false,
		},
		{
			name: "trusted no work",
			ev: stream.Event{
				AgentID: "s1", Type: "turn_finished",
				Data: map[string]any{"finish_reason": "stop", "turn_complete": true, "no_work": true},
			},
			want: false,
		},
		{
			name: "assistant chunk",
			ev:   stream.Event{AgentID: "s1", Type: "assistant"},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldBumpNotifySeq(tc.ev); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestOnStreamEventPublishesNotificationProjection(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	mgr := NewManager("node-1", hub, nil, nil, nil, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	mgr.OnStreamEvent(stream.Event{
		AgentID: "session-1", Type: "turn_finished", Seq: 1, AgentSeq: 1,
		Data: map[string]any{"finish_reason": "stop"}, TS: time.Now().UTC().Format(time.RFC3339Nano),
	})
	select {
	case ev := <-ch:
		if ev.Type != "notification_changed" || ev.AgentID != "session-1" {
			t.Fatalf("notification event = %#v", ev)
		}
		if _, ok := ev.Data["has_unread"]; !ok {
			t.Fatalf("notification projection = %#v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification_changed")
	}
}

func TestCancelledTerminalRefreshesNotificationWithoutBumpingCursor(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	mgr := NewManager("node-1", hub, nil, nil, nil, nil, TurnOptions{}, logx.Discard())
	defer mgr.Stop()
	ch := hub.Subscribe(0)
	defer hub.Unsubscribe(ch)

	mgr.OnStreamEvent(stream.Event{
		AgentID: "session-cancelled", Type: "turn_state", Seq: 9, AgentSeq: 9,
		Data: map[string]any{"phase": "cancelled", "terminal": true},
	})
	select {
	case ev := <-ch:
		if ev.Type != "notification_changed" {
			t.Fatalf("event type=%q, want notification_changed", ev.Type)
		}
		if ev.Data["notify_seq"] != 0 || ev.Data["has_unread"] != false || ev.Data["has_pending_hitl"] != false {
			t.Fatalf("cancel notification projection=%#v, want unchanged cursor and cleared pending state", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancellation notification projection")
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected second event after cancelled terminal: %#v", ev)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestDelayedHITLNotificationCallbackReadsPostCancelProjection(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	call := llm.ToolCall{
		ID: "call-delayed-notification",
		Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"cancel.txt","content":"x"}`,
		},
	}
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: call}}})
	if !mgr.NotificationState(sess.ID).HasPendingHITL {
		t.Fatal("test setup did not create pending HITL")
	}

	ch := mgr.hub.Subscribe(0)
	defer mgr.hub.Unsubscribe(ch)
	for len(ch) > 0 {
		<-ch // Ignore replayed lifecycle setup events.
	}
	mgr.notificationProjectionMu.Lock()
	started := make(chan struct{})
	oldCallbackDone := make(chan struct{})
	go func() {
		close(started)
		mgr.OnStreamEvent(stream.Event{
			AgentID: sess.ID, Type: "hitl_required", Seq: 1, AgentSeq: 1,
		})
		close(oldCallbackDone)
	}()
	<-started
	select {
	case ev := <-ch:
		mgr.notificationProjectionMu.Unlock()
		t.Fatalf("delayed HITL callback published before serialized state read: %#v", ev)
	case <-time.After(30 * time.Millisecond):
	}

	if err := rt.lifecycleCancel(); err != nil {
		mgr.notificationProjectionMu.Unlock()
		t.Fatalf("cancel lifecycle: %v", err)
	}
	if rt.pendingSnapshot() != nil {
		mgr.notificationProjectionMu.Unlock()
		t.Fatal("cancellation did not clear pending HITL before releasing projection lock")
	}
	cancelCallbackDone := make(chan struct{})
	go func() {
		mgr.OnStreamEvent(stream.Event{
			AgentID: sess.ID, Type: "turn_state", Seq: 2, AgentSeq: 2,
			Data: map[string]any{"phase": "cancelled", "terminal": true},
		})
		close(cancelCallbackDone)
	}()
	mgr.notificationProjectionMu.Unlock()

	for _, done := range []<-chan struct{}{oldCallbackDone, cancelCallbackDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("notification callback remained blocked")
		}
	}
	seen := 0
	deadline := time.After(time.Second)
	for seen < 2 {
		select {
		case ev := <-ch:
			if ev.Type != "notification_changed" {
				continue // terminal turn_state itself is also on the Hub
			}
			seen++
			if ev.Data["has_pending_hitl"] != false {
				t.Fatalf("late callback published stale pending projection: %#v", ev.Data)
			}
		case <-deadline:
			t.Fatalf("received %d notification projections, want both serialized callbacks", seen)
		}
	}
}
