package shared

import "testing"

func TestMediaHintLinesFromMediaArray(t *testing.T) {
	lines := MediaHintLines(map[string]any{
		"tool_name": "browser_snapshot",
		"media": []any{
			map[string]any{
				"id":    "med_1",
				"url":   "/v1/sessions/s1/media/med_1",
				"label": "browser_snapshot",
			},
		},
	})
	if len(lines) != 1 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0] != "browser_snapshot: /v1/sessions/s1/media/med_1" {
		t.Fatalf("line = %q", lines[0])
	}
}

func TestMediaHintLinesShowImagePathFallback(t *testing.T) {
	lines := MediaHintLines(map[string]any{
		"tool_name": "show_image",
		"content":   "[SHOW_IMAGE]\npath=reports/chart.png\nstatus=ok",
	})
	if len(lines) != 1 || lines[0] != "image path: reports/chart.png" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestUserMediaHintLines(t *testing.T) {
	lines := UserMediaHintLines(map[string]any{
		"media": []any{
			map[string]any{"url": "/v1/sessions/s/media/med_u", "label": "user_upload"},
		},
	})
	if len(lines) != 1 {
		t.Fatalf("lines = %#v", lines)
	}
}
