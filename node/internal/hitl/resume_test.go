package hitl

import "testing"

func TestParseApprovalResume(t *testing.T) {
	ids := []string{"c1", "c2"}
	plan, err := ParseApprovalResume(map[string]any{"type": "selection", "approved": []any{"c1"}, "rejected": []any{"c2"}}, ids)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsApproved("c1") || plan.IsApproved("c2") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestParseUserInformationResume(t *testing.T) {
	text, err := ParseUserInformationResume(map[string]any{"type": "user_information", "answer": "prod"}, "tc1")
	if err != nil || text == "" {
		t.Fatalf("err=%v text=%q", err, text)
	}
}
