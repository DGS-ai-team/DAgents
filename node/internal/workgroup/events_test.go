package workgroup

import (
	"encoding/json"
	"testing"
)

func TestDispatchTimelineEventPublishesAndAcks(t *testing.T) {
	w := NewWorker(Config{NodeID: "node_a"})
	gen := w.Connect()
	var got WSEnvelope
	w.OnTimelineEvent = func(env WSEnvelope) { got = env }

	res, err := w.DispatchEnvelope(WSEnvelope{
		EnvelopeID:           "en_01h00000000000000000000001",
		SchemaVersion:        SchemaVersion,
		Type:                 "timeline.event",
		WorkgroupID:          "wg_01h00000000000000000000001",
		DeliverySeq:          7,
		ConnectionGeneration: gen,
		Payload: map[string]any{
			"event_id":     "ev_01h00000000000000000000001",
			"workgroup_id": "wg_01h00000000000000000000001",
			"seq":          3,
			"type":         "human_message",
			"actor_id":     "node_a",
			"text":         "hello",
		},
		SentAt: "2026-08-11T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Handled || !res.PendingAck || res.AckEnvelope["type"] != "delivery.ack" {
		t.Fatalf("unexpected dispatch result: %+v", res)
	}
	if got.Payload["text"] != "hello" {
		t.Fatalf("timeline callback payload: %+v", got.Payload)
	}
	if err := w.CommitPendingAck(res); err != nil {
		t.Fatal(err)
	}
	if got := w.Session.OfferResumeFor("wg_01h00000000000000000000001").LastAckDeliverySeq; got != 7 {
		t.Fatalf("ack cursor=%d", got)
	}
}

func TestClientSessionHandlesRealtimeFrame(t *testing.T) {
	w := NewWorker(Config{NodeID: "node_a"})
	var got map[string]any
	cs := &ClientSession{
		Worker: w,
		OnRealtime: func(payload map[string]any) {
			got = payload
		},
	}
	raw, err := json.Marshal(map[string]any{
		"type": "workgroup.realtime",
		"payload": map[string]any{
			"workgroup_id": "wg_01h00000000000000000000001",
			"event_type":   "delta",
			"data":         map[string]any{"text": "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.HandleIncomingJSON(raw)
	if err != nil || !res.Handled {
		t.Fatalf("realtime result=%+v err=%v", res, err)
	}
	if got["event_type"] != "delta" {
		t.Fatalf("payload=%+v", got)
	}
}

func TestClientSessionHandlesControlFramesBeforeEnvelopeDispatch(t *testing.T) {
	w := NewWorker(Config{NodeID: "node_a"})
	cs := &ClientSession{Worker: w}

	welcome, err := json.Marshal(map[string]any{
		"type": "session.welcome",
		"payload": map[string]any{
			"connection_generation": int64(7),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.HandleIncomingJSON(welcome)
	if err != nil || res == nil || !res.Handled {
		t.Fatalf("welcome result=%+v err=%v", res, err)
	}
	if got := w.Session.Generation(); got != 7 {
		t.Fatalf("welcome generation=%d", got)
	}

	res, err = cs.HandleIncomingJSON([]byte(`{"type":"resume.complete","payload":{}}`))
	if err != nil || res == nil || !res.Handled {
		t.Fatalf("resume result=%+v err=%v", res, err)
	}
}

func TestClientSessionBuildHelloIncludesProtocolContract(t *testing.T) {
	w := NewWorker(Config{NodeID: "node_a"})
	cs := &ClientSession{Worker: w}
	hello := cs.BuildHello()
	payload, ok := hello["payload"].(map[string]any)
	if !ok {
		t.Fatalf("hello payload type=%T", hello["payload"])
	}
	if payload["protocol_version"] != ProtocolVersion || payload["schema_version"] != SchemaVersion {
		t.Fatalf("hello versions=%+v", payload)
	}
	if payload["node_id"] != "node_a" {
		t.Fatalf("hello identity=%+v", payload)
	}
	capabilities, ok := payload["capabilities"].([]string)
	if !ok || len(capabilities) == 0 {
		t.Fatalf("hello capabilities=%T %v", payload["capabilities"], payload["capabilities"])
	}
	if _, ok := payload["client_time"].(string); !ok {
		t.Fatalf("hello client_time=%T", payload["client_time"])
	}
}
