package hitl

import "testing"

func TestExtractUserInformationRequest(t *testing.T) {
	req := ExtractUserInformationRequest(map[string]any{
		"user_information_args": map[string]any{
			"tool_call_id": "tc-1",
			"question":     "选环境",
			"allow_multiple": true,
			"options": []any{
				map[string]any{"id": "prod", "label": "生产"},
				map[string]any{"id": "dev", "label": "开发"},
			},
		},
	})
	if req == nil || len(req.Options) != 2 || !req.AllowMultiple {
		t.Fatalf("req = %+v", req)
	}
	rv, err := BuildUserInformationResumeFromOptions(req, map[string]bool{"prod": true})
	if err != nil {
		t.Fatal(err)
	}
	if rv["answer"] != "生产" {
		t.Fatalf("answer = %v", rv["answer"])
	}
}

func TestBuildApprovalSelectionResume(t *testing.T) {
	data := map[string]any{
		"approval_args": map[string]any{
			"tool_calls": []any{
				map[string]any{"id": "a", "name": "t1"},
				map[string]any{"id": "b", "name": "t2"},
			},
		},
	}
	rv := BuildApprovalSelectionResume(data, map[string]bool{"a": true})
	approved, _ := rv["approved"].([]string)
	rejected, _ := rv["rejected"].([]string)
	if len(approved) != 1 || len(rejected) != 1 {
		t.Fatalf("approved=%v rejected=%v", approved, rejected)
	}
}
