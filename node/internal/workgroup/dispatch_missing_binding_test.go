package workgroup

import "testing"

func TestDispatchToolCommandMissingBindingReturnsToolResult(t *testing.T) {
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	gen := w.Connect()
	env := WSEnvelope{
		EnvelopeID:           "en_missing_binding",
		SchemaVersion:        SchemaVersion,
		Type:                 "tool.command",
		DeliverySeq:          7,
		ConnectionGeneration: gen,
		WorkgroupID:          "wg_01",
		Payload: map[string]any{
			"command_id":         "cmd_missing_01",
			"workgroup_id":       "wg_01",
			"member_id":          "mb_not_provisioned",
			"assign_id":          "as_01",
			"tool_name":          "read_file",
			"arguments_json":     `{"path":"README"}`,
			"payload_hash":       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"lease_epoch":        float64(1),
			"member_generation":  float64(1),
			"member_spec_digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	res, err := w.DispatchEnvelope(env)
	if res == nil {
		t.Fatalf("expected dispatch result, err=%v", err)
	}
	if res.AckEnvelope["type"] != "tool.result" {
		t.Fatalf("expected tool.result to wake Manage waiter, got %+v", res.AckEnvelope)
	}
	payload, _ := res.AckEnvelope["payload"].(map[string]any)
	if payload["command_id"] != "cmd_missing_01" {
		t.Fatalf("payload=%+v", payload)
	}
	if payload["status"] != "failed" {
		t.Fatalf("status=%v", payload["status"])
	}
}

func TestOrphanedRunningJournalReturnsIndeterminateResult(t *testing.T) {
	w := NewWorker(Config{NodeID: "node_b", NodeToolNames: []string{"read_file"}})
	gen := w.Connect()
	dir := t.TempDir()
	_, err := w.HandleProvision(ProvisionRequest{
		ProvisionID:      "pv_orph",
		WorkgroupID:      "wg_orph",
		MemberID:         "mb_orph",
		HomeNodeID:       "node_b",
		MemberSpecDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		LeaseEpoch:       1,
		MemberGeneration: 1,
		ToolAllowNames:   []string{"read_file"},
		WorkspaceRoot:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := w.Bindings.Get("mb_orph")
	_ = w.Journal.Put(JournalEntry{
		CommandID:   "cmd_orph_running",
		PayloadHash: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Status:      "running",
		MemberID:    b.MemberID,
		WorkgroupID: b.WorkgroupID,
		ToolName:    "read_file",
		Executions:  1,
		JournaledAt: "2026-08-01T00:00:00Z",
		UpdatedAt:   "2026-08-01T00:00:00Z",
	})

	env := WSEnvelope{
		EnvelopeID:           "en_orph",
		SchemaVersion:        SchemaVersion,
		Type:                 "tool.command",
		DeliverySeq:          3,
		ConnectionGeneration: gen,
		WorkgroupID:          b.WorkgroupID,
		Payload: map[string]any{
			"command_id":            "cmd_orph_running",
			"workgroup_id":          b.WorkgroupID,
			"member_id":             b.MemberID,
			"assign_id":             "as_orph",
			"tool_name":             "read_file",
			"arguments_json":        `{"path":"README"}`,
			"payload_hash":          "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			"lease_epoch":           float64(1),
			"member_generation":     float64(1),
			"member_spec_digest":    b.MemberSpecDigest,
			"tool_catalog_revision": b.ToolCatalogRevision,
		},
	}
	res, err := w.DispatchEnvelope(env)
	if err != nil {
		t.Fatal(err)
	}
	if res.AckEnvelope["type"] != "tool.result" {
		t.Fatalf("expected tool.result, got %+v", res.AckEnvelope)
	}
	payload, _ := res.AckEnvelope["payload"].(map[string]any)
	if payload["status"] != "indeterminate" {
		t.Fatalf("status=%v payload=%+v", payload["status"], payload)
	}
}
