package turn

import (
	"encoding/json"
	"testing"
	"time"
)

func maintenanceEvents(types ...EventType) []TurnEventEnvelope {
	now := time.Now().UTC()
	result := make([]TurnEventEnvelope, len(types))
	for i, typ := range types {
		e := NewTurnEventEnvelope("session-1", typ, now.Add(time.Duration(i)*time.Millisecond))
		e.AgentID, e.TurnID = "agent-1", "turn-1"
		e.SessionSeq, e.TurnSeq = uint64(i+1), uint64(i+1)
		if typ != EventTurnStarted {
			e.StepID = "step-1"
		}
		e.EventVersion = 1
		e.Payload = []byte(`{"generation":1}`)
		if typ == EventModelRequestStarted {
			e.Payload = []byte(`{"generation":1,"request_digest":"request"}`)
		}
		if typ == EventModelUsageRecorded {
			e.Payload, _ = json.Marshal(map[string]any{"generation": 1, "usage": StepUsage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}})
		}
		result[i] = e
	}
	return result
}

func TestReconcileMaintenanceTurnCompletedWithKnownUsage(t *testing.T) {
	events := maintenanceEvents(EventTurnStarted, EventStepStarted, EventModelRequestStarted, EventModelRequestCompleted, EventModelUsageRecorded, EventAssistantMessageRecorded, EventStepCompleted, EventTurnCompleted)
	for i := range events {
		events[i].SessionSeq += 49
	}
	got, err := ReconcileMaintenanceTurn(events, "agent-1", "session-1", "turn-1")
	if err != nil || !got.Completed || !got.UsageKnown || got.Usage.TotalTokens != 3 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestReconcileMaintenanceTurnFailsClosed(t *testing.T) {
	base := maintenanceEvents(EventTurnStarted, EventStepStarted, EventModelRequestStarted, EventModelRequestCompleted, EventAssistantMessageRecorded, EventStepCompleted, EventTurnCompleted)
	for _, tc := range []struct {
		name string
		edit func([]TurnEventEnvelope) []TurnEventEnvelope
	}{
		{"missing usage", func(e []TurnEventEnvelope) []TurnEventEnvelope { return e }},
		{"missing terminal", func(e []TurnEventEnvelope) []TurnEventEnvelope { return e[:len(e)-1] }},
		{"sequence gap", func(e []TurnEventEnvelope) []TurnEventEnvelope { e[2].SessionSeq++; return e }},
		{"wrong agent", func(e []TurnEventEnvelope) []TurnEventEnvelope { e[1].AgentID = "other"; return e }},
		{"unsupported version", func(e []TurnEventEnvelope) []TurnEventEnvelope { e[1].EventVersion = 2; return e }},
		{"invalid payload", func(e []TurnEventEnvelope) []TurnEventEnvelope { e[1].Payload = []byte("{"); return e }},
		{"unknown event", func(e []TurnEventEnvelope) []TurnEventEnvelope {
			e[1].EventType = EventType("maintenance.unknown")
			return e
		}},
		{"event after terminal", func(e []TurnEventEnvelope) []TurnEventEnvelope {
			extra := e[0]
			extra.EventType = EventModelRequestStarted
			extra.StepID = "step-1"
			extra.Payload = []byte(`{"generation":1,"request_digest":"late"}`)
			e = append(e, extra)
			e[len(e)-1].SessionSeq = uint64(len(e))
			e[len(e)-1].TurnSeq = uint64(len(e))
			return e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReconcileMaintenanceTurn(tc.edit(append([]TurnEventEnvelope(nil), base...)), "agent-1", "session-1", "turn-1")
			if tc.name == "event after terminal" || tc.name == "sequence gap" || tc.name == "wrong agent" || tc.name == "unsupported version" || tc.name == "invalid payload" || tc.name == "unknown event" {
				if err == nil {
					t.Fatalf("malformed journal accepted: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("malformed journal err=%v", err)
			}
			if got.Completed || got.Reason == "" {
				t.Fatalf("unsafe result=%+v", got)
			}
		})
	}
}

func TestReconcileMaintenanceTurnNonSuccessfulTerminalNeverCompletes(t *testing.T) {
	for _, terminal := range []EventType{EventTurnFailed, EventTurnCancelled} {
		events := maintenanceEvents(EventTurnStarted, EventStepStarted, EventModelRequestStarted, EventModelRequestCompleted, EventModelUsageRecorded, EventAssistantMessageRecorded, EventStepCompleted, terminal)
		got, err := ReconcileMaintenanceTurn(events, "agent-1", "session-1", "turn-1")
		if err != nil {
			t.Fatalf("terminal=%s replay: %v", terminal, err)
		}
		if got.Completed || !got.UsageKnown {
			t.Fatalf("terminal=%s result=%+v", terminal, got)
		}
	}
}

func TestReconcileMaintenanceTurnRejectsUnresolvedToolAndInteraction(t *testing.T) {
	toolEvents := maintenanceEvents(EventTurnStarted, EventStepStarted, EventModelRequestStarted, EventModelRequestCompleted, EventModelUsageRecorded, EventAssistantMessageRecorded, EventToolCallRecorded, EventToolExecutionStarted, EventTurnCompleted)
	toolEvents[5].Payload = []byte(`{"generation":1,"has_tools":true}`)
	toolEvents[6].ToolCallID = "call-1"
	toolEvents[6].Payload = []byte(`{"generation":1,"tool_name":"write_file","arguments_json":"{}"}`)
	toolEvents[7].ToolExecutionID = "exec-1"
	if got, err := ReconcileMaintenanceTurn(toolEvents, "agent-1", "session-1", "turn-1"); err == nil && got.Completed {
		t.Fatalf("incomplete tool completed: %+v", got)
	}

	interactionEvents := maintenanceEvents(EventTurnStarted, EventStepStarted, EventModelRequestStarted, EventModelRequestCompleted, EventModelUsageRecorded, EventAssistantMessageRecorded, EventInteractionRequested)
	interactionEvents[6].InteractionID = "interaction-1"
	interactionEvents[6].Payload = []byte(`{"generation":1,"interaction_kind":"approval"}`)
	if got, err := ReconcileMaintenanceTurn(interactionEvents, "agent-1", "session-1", "turn-1"); err == nil && got.Completed {
		t.Fatalf("unresolved interaction completed: %+v", got)
	}
}
