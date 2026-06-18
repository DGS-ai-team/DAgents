package hitl

import (
	"fmt"
	"testing"
)

func TestBuildTriggerSessionApprovalResume(t *testing.T) {
	items := []ToolApprovalItem{{
		CallID:       "c1",
		Name:         "trigger_create",
		ApprovalMode: ApprovalModeTriggerSession,
	}}
	rv := BuildTriggerSessionApprovalResume(
		map[string]any{"approval_id": "ap-1"},
		items,
		map[string]string{"c1": TriggerSessionLatestActive},
		nil,
	)
	targets, _ := rv["trigger_session_targets"].(map[string]string)
	if targets["c1"] != TriggerSessionLatestActive {
		t.Fatalf("targets = %v", rv["trigger_session_targets"])
	}
	if rv["approval_id"] != "ap-1" {
		t.Fatalf("routing = %v", rv)
	}
}

func TestBuildTriggerSessionQuickResumeReject(t *testing.T) {
	items := []ToolApprovalItem{{
		CallID:       "c1",
		Name:         "trigger_create",
		ApprovalMode: ApprovalModeTriggerSession,
	}}
	rv := BuildTriggerSessionQuickResume(map[string]any{}, items, false)
	rejected := toTestStringSlice(rv["rejected"])
	if len(rejected) != 1 || rejected[0] != "c1" {
		t.Fatalf("rejected = %v", rv["rejected"])
	}
}

func toTestStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}
