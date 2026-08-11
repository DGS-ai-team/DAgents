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
