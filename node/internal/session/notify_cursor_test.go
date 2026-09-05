package session

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
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
