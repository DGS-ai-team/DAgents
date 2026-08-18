package turn

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestBuildApprovalToolItemTriggerCreate(t *testing.T) {
	item := buildApprovalToolItem(llm.ToolCall{
		ID:   "call-1",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "trigger_create",
			Arguments: `{"name":"喝水提醒","schedule":{"kind":"once","fire_at":"2026-06-01T12:00:00Z"}}`,
		},
	}, nil)
	if item["name"] != "trigger_create" {
		t.Fatalf("name = %v", item["name"])
	}
	reason, _ := item["approval_reason"].(string)
	if reason == "" || reason == "<nil>" {
		t.Fatalf("reason = %v", reason)
	}
	if item["risk_level"] != "medium" {
		t.Fatalf("risk = %v", item["risk_level"])
	}
	args, ok := item["arguments"].(map[string]any)
	if !ok || args["name"] != "喝水提醒" {
		t.Fatalf("arguments = %v", item["arguments"])
	}
}

func TestDescribeApprovalMetaBashRun(t *testing.T) {
	reason, risk := describeApprovalMeta("bash_run", map[string]any{"command": "curl wttr.in"})
	if risk != "high" {
		t.Fatalf("risk = %q", risk)
	}
	if reason == "" {
		t.Fatal("empty reason")
	}
}

func TestDescribeApprovalMetaLinuxExec(t *testing.T) {
	reason, risk := describeApprovalMeta("linux_exec", map[string]any{
		"channel_id": "prod-app-01",
		"command":    "systemctl restart api",
	})
	if risk != "high" {
		t.Fatalf("risk = %q", risk)
	}
	if reason == "" || !strings.Contains(reason, "prod-app-01") {
		t.Fatalf("reason = %q", reason)
	}
}
