package hitl

import "testing"

func TestParseOptionSelectionSingle(t *testing.T) {
	req := &UserInformationRequest{
		Options: []UserInformationOption{
			{ID: "prod", Label: "生产"},
			{ID: "dev", Label: "开发"},
		},
	}
	selected, ok := parseOptionSelection("2", req)
	if !ok || !selected["dev"] || selected["prod"] {
		t.Fatalf("selected = %v ok=%v", selected, ok)
	}
}

func TestParseOptionSelectionMultiple(t *testing.T) {
	req := &UserInformationRequest{
		AllowMultiple: true,
		Options: []UserInformationOption{
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C"},
		},
	}
	selected, ok := parseOptionSelection("1,3", req)
	if !ok || !selected["a"] || !selected["c"] || selected["b"] {
		t.Fatalf("selected = %v ok=%v", selected, ok)
	}
}

func TestParseOptionSelectionFreeText(t *testing.T) {
	req := &UserInformationRequest{
		Options: []UserInformationOption{{ID: "a", Label: "A"}},
	}
	if _, ok := parseOptionSelection("自定义答案", req); ok {
		t.Fatal("free text should not parse as selection")
	}
}
