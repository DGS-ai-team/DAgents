package session

import (
	"testing"

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
			ev:   stream.Event{SessionID: "s1", Type: "hitl_required"},
			want: true,
		},
		{
			name: "turn finished stop",
			ev: stream.Event{
				SessionID: "s1",
				Type:      "turn_finished",
				Data:      map[string]any{"finish_reason": "stop", "turn_complete": true},
			},
			want: true,
		},
		{
			name: "turn finished error",
			ev: stream.Event{
				SessionID: "s1",
				Type:      "turn_finished",
				Data:      map[string]any{"finish_reason": "error", "turn_complete": true},
			},
			want: false,
		},
		{
			name: "assistant chunk",
			ev:   stream.Event{SessionID: "s1", Type: "assistant"},
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
