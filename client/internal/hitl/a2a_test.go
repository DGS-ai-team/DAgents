package hitl

import "testing"

func TestA2APeerLabel(t *testing.T) {
	if got := A2APeerLabel(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	data := map[string]any{
		"a2a_peer_agent_name": "合规助手",
		"a2a_peer_agent_id":   "compliance-a",
	}
	if got := A2APeerLabel(data); got != "合规助手" {
		t.Fatalf("name = %q", got)
	}
	delete(data, "a2a_peer_agent_name")
	if got := A2APeerLabel(data); got != "compliance-a" {
		t.Fatalf("id fallback = %q", got)
	}
}

func TestA2ARelayApprovedSummary(t *testing.T) {
	got := A2ARelayApprovedSummary("合规助手", true)
	if got != "已审批，由合规助手执行" {
		t.Fatalf("summary = %q", got)
	}
	if A2ARelayApprovedSummary("合规助手", false) != "已拒绝" {
		t.Fatal("expected rejected summary")
	}
}

func TestA2ARelayToolSuffix(t *testing.T) {
	got := A2ARelayToolSuffix(map[string]any{"a2a_peer_agent_name": "Node-A"})
	if got != " from Node-A" {
		t.Fatalf("suffix = %q", got)
	}
	if A2ARelayToolSuffix(map[string]any{}) != " from 对端 Agent" {
		t.Fatal("expected default suffix")
	}
}

func TestApprovalHeaderA2ARelay(t *testing.T) {
	got := ApprovalHeader(map[string]any{
		"a2a_relay":           true,
		"a2a_peer_agent_name": "合规助手",
	})
	if got != "A2A 对端 Agent 请求审批 · 合规助手" {
		t.Fatalf("header = %q", got)
	}
}

func TestIsA2ARelayHITL(t *testing.T) {
	if IsA2ARelayHITL(nil) {
		t.Fatal("nil should be false")
	}
	if IsA2ARelayHITL(map[string]any{}) {
		t.Fatal("empty should be false")
	}
	if !IsA2ARelayHITL(map[string]any{"a2a_relay": true}) {
		t.Fatal("expected true")
	}
}

func TestParseApprovalResumeSelection(t *testing.T) {
	hitl := map[string]any{
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "c1", "name": "bash_run"},
				map[string]any{"id": "c2", "name": "write_file"},
			},
		},
	}
	approved, rejected := ParseApprovalResumeSelection(map[string]any{
		"type":     "selection",
		"approved": []any{"c1"},
		"rejected": []any{"c2"},
	}, hitl)
	if !approved["c1"] || approved["c2"] {
		t.Fatalf("approved = %v", approved)
	}
	if !rejected["c2"] || rejected["c1"] {
		t.Fatalf("rejected = %v", rejected)
	}

	approved, rejected = ParseApprovalResumeSelection(map[string]any{"type": "approve"}, hitl)
	if len(approved) != 2 || len(rejected) != 0 {
		t.Fatalf("approve all: approved=%v rejected=%v", approved, rejected)
	}
	approved, rejected = ParseApprovalResumeSelection(map[string]any{"type": "reject"}, hitl)
	if len(rejected) != 2 {
		t.Fatalf("reject all: rejected=%v", rejected)
	}
}
