package turn

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTurnEventEnvelopeValidatesIdentityAndPayload(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	event := NewTurnEventEnvelope("session-1", EventTurnStarted, now)
	event.TurnID = "turn-1"
	event.SessionSeq = 1
	event.TurnSeq = 1
	event.Payload = json.RawMessage(`{"source":"human"}`)
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}

	event.Payload = json.RawMessage(`{invalid`)
	if err := event.Validate(); err == nil || !strings.Contains(err.Error(), "payload") {
		t.Fatalf("expected invalid payload error, got %v", err)
	}
}

func TestTurnEventEnvelopeRejectsUnknownTypeAndNonContiguousSequence(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	previous := NewTurnEventEnvelope("session-1", EventTurnStarted, now)
	previous.TurnID = "turn-1"
	previous.SessionSeq = 1
	previous.TurnSeq = 1
	if err := previous.Validate(); err != nil {
		t.Fatal(err)
	}

	next := NewTurnEventEnvelope("session-1", EventTurnCompleted, now.Add(time.Second))
	next.TurnID = "turn-1"
	next.SessionSeq = 3
	next.TurnSeq = 2
	if err := next.ValidateAfter(previous); err == nil || !strings.Contains(err.Error(), "contiguous") {
		t.Fatalf("expected sequence error, got %v", err)
	}

	unknown := NewTurnEventEnvelope("session-1", EventType("unknown.event"), now)
	unknown.SessionSeq = 2
	unknown.TurnSeq = 2
	if err := unknown.Validate(); err == nil || !strings.Contains(err.Error(), "unknown event type") {
		t.Fatalf("expected unknown type error, got %v", err)
	}
}

func TestTurnEventEnvelopeValidateAfterRequiresSameTurn(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	previous := NewTurnEventEnvelope("session-1", EventTurnStarted, now)
	previous.TurnID = "turn-1"
	previous.SessionSeq = 1
	previous.TurnSeq = 1
	next := NewTurnEventEnvelope("session-1", EventTurnCompleted, now.Add(time.Second))
	next.TurnID = "turn-2"
	next.SessionSeq = 2
	next.TurnSeq = 2
	if err := next.ValidateAfter(previous); err == nil || !strings.Contains(err.Error(), "turn sequence") {
		t.Fatalf("expected turn boundary error, got %v", err)
	}
}
