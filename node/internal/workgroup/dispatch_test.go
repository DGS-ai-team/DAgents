package workgroup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDispatchProvisionAndReadFile(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	gen := w.Connect()
	cs := &ClientSession{Worker: w}

	provEnv := WSEnvelope{
		EnvelopeID:           "en_01h00000000000000000000001",
		SchemaVersion:        SchemaVersion,
		Type:                 "member.provision",
		DeliverySeq:          1,
		ConnectionGeneration: gen,
		WorkgroupID:          "wg_01h00000000000000000000001",
		Payload: map[string]any{
			"provision_id":       "pv_01h00000000000000000000009",
			"workgroup_id":       "wg_01h00000000000000000000001",
			"member_id":          "mb_01h00000000000000000000002",
			"home_node_id":       "node_b",
			"member_spec_digest": "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
			"lease_epoch":        float64(2),
			"member_generation":  float64(1),
			"tool_allow_names":   []any{"read_file"},
			"workspace_root":     filepath.Join(dir, "ws"),
		},
		SentAt: "2026-07-31T00:00:00Z",
	}
	r1, err := w.DispatchEnvelope(provEnv)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Handled || r1.AckEnvelope["type"] != "member.provision_result" {
		t.Fatalf("%+v", r1)
	}
	if err := w.CommitPendingAck(r1); err != nil {
		t.Fatal(err)
	}
	b, _ := w.Bindings.Get("mb_01h00000000000000000000002")
	if b == nil {
		t.Fatal("binding missing")
	}
	if err := os.WriteFile(filepath.Join(b.WorkspacePath, "README"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmdEnv := WSEnvelope{
		EnvelopeID:           "en_01h00000000000000000000002",
		SchemaVersion:        SchemaVersion,
		Type:                 "tool.command",
		DeliverySeq:          2,
		ConnectionGeneration: gen,
		WorkgroupID:          b.WorkgroupID,
		Payload: map[string]any{
			"command_id":            "cmd_01h00000000000000000000008",
			"workgroup_id":          b.WorkgroupID,
			"member_id":             b.MemberID,
			"assign_id":             "as_01h00000000000000000000007",
			"run_id":                "rn_01h00000000000000000000006",
			"turn_id":               "tn_01h00000000000000000000005",
			"tool_call_id":          "call_1",
			"tool_name":             "read_file",
			"arguments_json":        `{"path":"README"}`,
			"payload_hash":          "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			"lease_id":              "ls_01h00000000000000000000004",
			"lease_epoch":           float64(2),
			"member_generation":     float64(1),
			"member_spec_digest":    b.MemberSpecDigest,
			"tool_catalog_revision": b.ToolCatalogRevision,
			"status":                "queued",
			"side_effect_class":     "fs_read",
		},
		SentAt: "2026-07-31T00:00:01Z",
	}
	r2, err := w.DispatchEnvelope(cmdEnv)
	if err != nil {
		t.Fatal(err)
	}
	if r2.AckEnvelope["type"] != "tool.result" {
		t.Fatalf("%+v", r2)
	}
	if err := w.CommitPendingAck(r2); err != nil {
		t.Fatal(err)
	}
	if w.Session.LastAckDeliverySeq != 2 {
		t.Fatalf("ack seq=%d", w.Session.LastAckDeliverySeq)
	}
	if got := w.Session.OfferResumeFor(b.WorkgroupID).LastAckDeliverySeq; got != 2 {
		t.Fatalf("per-wg ack seq=%d", got)
	}

	// 旧 generation 帧被拒
	w.Connect() // gen++
	stale := cmdEnv
	stale.EnvelopeID = "en_01h00000000000000000000003"
	stale.DeliverySeq = 3
	stale.ConnectionGeneration = gen
	stale.Payload["command_id"] = "cmd_01h00000000000000000000009"
	_, err = cs.Worker.DispatchEnvelope(stale)
	if err == nil {
		t.Fatal("expected fencing")
	}
	if err.(*Error).Code != CodeFencingRejected {
		t.Fatalf("code=%v", err)
	}
}

func TestDispatchToolCancelPreventsExecution(t *testing.T) {
	dir := t.TempDir()
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	gen := w.Connect()

	provEnv := WSEnvelope{
		EnvelopeID:           "en_01h00000000000000000000011",
		SchemaVersion:        SchemaVersion,
		Type:                 "member.provision",
		DeliverySeq:          1,
		ConnectionGeneration: gen,
		WorkgroupID:          "wg_01h00000000000000000000011",
		Payload: map[string]any{
			"provision_id":       "pv_01h00000000000000000000011",
			"workgroup_id":       "wg_01h00000000000000000000011",
			"member_id":          "mb_01h00000000000000000000011",
			"home_node_id":       "node_b",
			"member_spec_digest": "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
			"lease_epoch":        float64(2),
			"member_generation":  float64(1),
			"tool_allow_names":   []any{"read_file"},
			"workspace_root":     filepath.Join(dir, "ws"),
		},
		SentAt: "2026-07-31T00:00:00Z",
	}
	r1, err := w.DispatchEnvelope(provEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.CommitPendingAck(r1); err != nil {
		t.Fatal(err)
	}
	b, _ := w.Bindings.Get("mb_01h00000000000000000000011")
	if b == nil {
		t.Fatal("binding missing")
	}
	if err := os.WriteFile(filepath.Join(b.WorkspacePath, "README"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cancelEnv := WSEnvelope{
		EnvelopeID:           "en_01h00000000000000000000012",
		SchemaVersion:        SchemaVersion,
		Type:                 "tool.cancel",
		DeliverySeq:          2,
		ConnectionGeneration: gen,
		WorkgroupID:          b.WorkgroupID,
		Payload: map[string]any{
			"command_id":   "cmd_01h00000000000000000000012",
			"workgroup_id": b.WorkgroupID,
			"assign_id":    "as_01h00000000000000000000012",
			"member_id":    b.MemberID,
			"status":       "canceled",
		},
		SentAt: "2026-07-31T00:00:01Z",
	}
	rCancel, err := w.DispatchEnvelope(cancelEnv)
	if err != nil {
		t.Fatal(err)
	}
	if rCancel.AckEnvelope["type"] != "tool.result" {
		t.Fatalf("%+v", rCancel)
	}
	pl := rCancel.AckEnvelope["payload"].(map[string]any)
	if pl["status"] != "canceled" {
		t.Fatalf("status=%v", pl["status"])
	}
	if err := w.CommitPendingAck(rCancel); err != nil {
		t.Fatal(err)
	}

	cmdEnv := WSEnvelope{
		EnvelopeID:           "en_01h00000000000000000000013",
		SchemaVersion:        SchemaVersion,
		Type:                 "tool.command",
		DeliverySeq:          3,
		ConnectionGeneration: gen,
		WorkgroupID:          b.WorkgroupID,
		Payload: map[string]any{
			"command_id":            "cmd_01h00000000000000000000012",
			"workgroup_id":          b.WorkgroupID,
			"member_id":             b.MemberID,
			"assign_id":             "as_01h00000000000000000000012",
			"run_id":                "rn_01h00000000000000000000012",
			"turn_id":               "tn_01h00000000000000000000012",
			"tool_call_id":          "call_1",
			"tool_name":             "read_file",
			"arguments_json":        `{"path":"README"}`,
			"payload_hash":          "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			"lease_id":              "ls_01h00000000000000000000012",
			"lease_epoch":           float64(2),
			"member_generation":     float64(1),
			"member_spec_digest":    b.MemberSpecDigest,
			"tool_catalog_revision": b.ToolCatalogRevision,
			"status":                "queued",
			"side_effect_class":     "fs_read",
		},
		SentAt: "2026-07-31T00:00:02Z",
	}
	rCmd, err := w.DispatchEnvelope(cmdEnv)
	if err != nil {
		t.Fatal(err)
	}
	if rCmd.AckEnvelope["type"] != "tool.result" {
		t.Fatalf("%+v", rCmd)
	}
	pl2 := rCmd.AckEnvelope["payload"].(map[string]any)
	if pl2["status"] != "canceled" {
		t.Fatalf("expected canceled after pre-cancel, got %v", pl2["status"])
	}
	entry, err := w.Commands.Journal.Get("cmd_01h00000000000000000000012")
	if err != nil || entry == nil {
		t.Fatalf("journal: %v %#v", err, entry)
	}
	if entry.Status != "canceled" || entry.Executions != 0 {
		t.Fatalf("entry=%+v", entry)
	}
}


func TestDispatchProvisionWrongHomeReturnsProvisionResult(t *testing.T) {
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	gen := w.Connect()
	env := WSEnvelope{
		EnvelopeID:           "en_01h00000000000000000000021",
		SchemaVersion:        SchemaVersion,
		Type:                 "member.provision",
		DeliverySeq:          1,
		ConnectionGeneration: gen,
		WorkgroupID:          "wg_01h00000000000000000000021",
		Payload: map[string]any{
			"provision_id":       "pv_01h00000000000000000000021",
			"workgroup_id":       "wg_01h00000000000000000000021",
			"member_id":          "mb_01h00000000000000000000021",
			"home_node_id":       "node_other",
			"member_spec_digest": "sha256:5045f5acc432f3f9fc64c14c1275d4c808f26b02b69acc1cdc60674ef1de238c",
			"lease_epoch":        float64(1),
			"member_generation":  float64(1),
			"tool_allow_names":   []any{"read_file"},
		},
		SentAt: "2026-07-31T00:00:00Z",
	}
	r, err := w.DispatchEnvelope(env)
	if err == nil {
		t.Fatal("expected home mismatch error")
	}
	if r == nil || r.AckEnvelope == nil || r.AckEnvelope["type"] != "member.provision_result" {
		t.Fatalf("want provision_result on failure, got %+v", r)
	}
	payload, _ := r.AckEnvelope["payload"].(map[string]any)
	if payload["status"] != "error" {
		t.Fatalf("status=%v", payload["status"])
	}
	if payload["member_id"] != "mb_01h00000000000000000000021" {
		t.Fatalf("member_id=%v", payload["member_id"])
	}
}
