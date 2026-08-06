package browser

import "testing"

func TestToolResultLiftsSummary(t *testing.T) {
	out := toolResultFromResponse(Response{
		OK: true,
		URL: "https://example.com",
		ScreenshotPath: "/tmp/shot.png",
		Detail: map[string]any{
			"task_id": "btask-1",
			"status":  "completed",
			"summary": "标题是 Example",
			"success": true,
			"steps":   3,
		},
	})
	if !out.OK {
		t.Fatal(out)
	}
	if out.ExtractedContent != "标题是 Example" {
		t.Fatalf("extracted = %q", out.ExtractedContent)
	}
	if out.Detail["summary"] != "标题是 Example" {
		t.Fatalf("detail summary missing: %+v", out.Detail)
	}
}
