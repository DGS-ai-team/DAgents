package hitl

import "testing"

func TestParseApprovalResumeSelection(t *testing.T) {
	ids := []string{"c1", "c2"}
	plan, err := ParseApprovalResume(map[string]any{
		"type":     "selection",
		"approved": []any{"c1"},
		"rejected": []any{"c2"},
	}, ids)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsApproved("c1") || plan.IsApproved("c2") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestParseApprovalResumeTriggerSessionTargets(t *testing.T) {
	ids := []string{"t1"}
	plan, err := ParseApprovalResume(map[string]any{
		"type":     "selection",
		"approved": []any{"t1"},
		"rejected": []any{},
		"trigger_session_targets": map[string]any{
			"t1": TriggerSessionNew,
		},
	}, ids)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.TriggerSessionTarget("t1"); got != TriggerSessionNew {
		t.Fatalf("target = %q", got)
	}
}

func TestParseApprovalResumeRejectTriggerTargetsOnRejected(t *testing.T) {
	ids := []string{"t1"}
	_, err := ParseApprovalResume(map[string]any{
		"type":     "selection",
		"approved": []any{},
		"rejected": []any{"t1"},
		"trigger_session_targets": map[string]any{
			"t1": TriggerSessionSame,
		},
	}, ids)
	if err == nil {
		t.Fatal("expected error for target on rejected call")
	}
}

func TestResumeValueKind(t *testing.T) {
	if got := ResumeValueKind(map[string]any{"type": "selection"}); got != "approval" {
		t.Fatalf("selection = %q", got)
	}
	if got := ResumeValueKind(map[string]any{"type": "user_information"}); got != "user_information" {
		t.Fatalf("user_information = %q", got)
	}
}

func TestParseUserInformationResume(t *testing.T) {
	text, err := ParseUserInformationResume(map[string]any{"type": "user_information", "answer": "prod"}, "tc1")
	if err != nil || text == "" {
		t.Fatalf("err=%v text=%q", err, text)
	}
}
