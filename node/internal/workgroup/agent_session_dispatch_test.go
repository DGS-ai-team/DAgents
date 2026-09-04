package workgroup

import (
	"context"
	"testing"
)

type fakeAgentSessionHandler struct {
	opened  AgentSessionOpenRequest
	started AgentTurnStartRequest
	cancel  AgentTurnCancelRequest
}

func (f *fakeAgentSessionHandler) OpenAgentSession(_ context.Context, req AgentSessionOpenRequest) (AgentSessionResult, error) {
	f.opened = req
	return AgentSessionResult{
		WorkgroupID: req.WorkgroupID,
		MemberID:    req.MemberID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		Status:      "ready",
	}, nil
}

func (f *fakeAgentSessionHandler) StartAgentTurn(_ context.Context, req AgentTurnStartRequest) error {
	f.started = req
	return nil
}

func (f *fakeAgentSessionHandler) CancelAgentTurn(_ context.Context, req AgentTurnCancelRequest) error {
	f.cancel = req
	return nil
}

func (f *fakeAgentSessionHandler) ResumeAgentTurn(context.Context, AgentTurnResumeRequest) error {
	return nil
}

func (f *fakeAgentSessionHandler) CloseAgentSession(context.Context, AgentSessionOpenRequest) error {
	return nil
}

func TestAgentSessionDispatchUsesStableIdentityAndDeliveryAck(t *testing.T) {
	fake := &fakeAgentSessionHandler{}
	w := NewWorker(Config{NodeID: "node-a", AgentSessions: fake})
	gen := w.Connect()
	base := WSEnvelope{
		SchemaVersion:        SchemaVersion,
		ConnectionGeneration: gen,
		DeliverySeq:          7,
		WorkgroupID:          "wg_01h00000000000000000000001",
	}
	base.Type = "agent.session.open"
	base.Payload = map[string]any{
		"workgroup_id": base.WorkgroupID,
		"member_id":    "mb_01h00000000000000000000001",
		"agent_id":     "agent-existing",
		"session_id":   "wg:group:member:mb_01h00000000000000000000001",
	}
	res, err := w.DispatchEnvelope(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.AckEnvelope["type"]; got != "agent.session.ready" {
		t.Fatalf("ack type=%v", got)
	}
	if !res.PendingAck || res.AckDeliverySeq != 7 || res.AckWorkgroupID != base.WorkgroupID {
		t.Fatalf("open delivery ack=%+v", res)
	}
	payload := res.AckEnvelope["payload"].(map[string]any)
	if payload["delivery_seq"] != int64(7) {
		t.Fatalf("delivery seq=%v", payload["delivery_seq"])
	}
	if payload["agent_id"] != "agent-existing" || fake.opened.SessionID == "" {
		t.Fatalf("opened=%+v", fake.opened)
	}

	base.Type = "agent.turn.start"
	base.DeliverySeq = 8
	base.Payload = map[string]any{
		"workgroup_id":   base.WorkgroupID,
		"member_id":      fake.opened.MemberID,
		"agent_id":       fake.opened.AgentID,
		"session_id":     fake.opened.SessionID,
		"assign_id":      "as_01h00000000000000000000001",
		"source":         "direct_member",
		"parent_turn_id": "tr_01h00000000000000000000001",
		"child_turn_id":  "tr_01h00000000000000000000002",
		"attempt_id":     "at_01h00000000000000000000001",
		"user_message":   "inspect the repository",
	}
	res, err = w.DispatchEnvelope(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.AckEnvelope["type"]; got != "agent.turn.accepted" {
		t.Fatalf("ack type=%v", got)
	}
	if !res.PendingAck || res.AckDeliverySeq != 8 || res.AckWorkgroupID != base.WorkgroupID {
		t.Fatalf("start delivery ack=%+v", res)
	}
	if fake.started.UserMessage != "inspect the repository" {
		t.Fatalf("started=%+v", fake.started)
	}

	base.Type = "agent.turn.cancel"
	base.DeliverySeq = 9
	base.Payload = map[string]any{
		"workgroup_id":  base.WorkgroupID,
		"member_id":     fake.opened.MemberID,
		"agent_id":      fake.opened.AgentID,
		"session_id":    fake.opened.SessionID,
		"assign_id":     "as_01h00000000000000000000001",
		"child_turn_id": "tr_01h00000000000000000000002",
		"attempt_id":    "at_01h00000000000000000000001",
	}
	res, err = w.DispatchEnvelope(base)
	if err != nil {
		t.Fatal(err)
	}
	if !res.PendingAck || res.AckDeliverySeq != 9 {
		t.Fatalf("cancel delivery ack=%+v", res)
	}

	base.Type = "agent.session.close"
	base.DeliverySeq = 10
	res, err = w.DispatchEnvelope(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.AckEnvelope["type"]; got != "agent.session.closed" {
		t.Fatalf("close ack type=%v", got)
	}
	if !res.PendingAck || res.AckDeliverySeq != 10 {
		t.Fatalf("close delivery ack=%+v", res)
	}
}
