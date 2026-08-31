package tools

import "testing"

func TestExtractToolMediaPaths_showImage(t *testing.T) {
	got, ok := ExtractToolMediaPaths("show_image", "[SHOW_IMAGE]\npath=reports/chart.png\nstatus=ok", map[string]any{
		"path":    "reports/chart.png",
		"caption": "对比图",
	})
	if !ok || got.RelPath != "reports/chart.png" || got.Caption != "对比图" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestExtractToolMediaPaths_browserSnapshot(t *testing.T) {
	content := `{"ok":true,"screenshot_path":".runtime/browser/snap.png"}`
	got, ok := ExtractToolMediaPaths("browser_snapshot", content, nil)
	if !ok || got.RelPath != ".runtime/browser/snap.png" || got.Source != "browser" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestExtractAllToolMediaPaths_browserRunTaskScreenshots(t *testing.T) {
	content := `{"ok":true,"detail":{"status":"completed","screenshot_paths":["/tmp/a.png","/tmp/b.png"],"last_screenshot_path":"/tmp/b.png"}}`
	got := ExtractAllToolMediaPaths("browser_run_task", content, nil)
	if len(got) != 2 {
		t.Fatalf("len=%d got=%+v", len(got), got)
	}
	if got[0].RelPath != "/tmp/a.png" || got[1].RelPath != "/tmp/b.png" {
		t.Fatalf("paths=%+v", got)
	}
}

func TestExtractToolMediaPaths_screenCapture(t *testing.T) {
	content := `{"ok":true,"screenshot_path":"C:\\Temp\\dagents\\capture.jpg"}`
	got, ok := ExtractToolMediaPaths("screen_capture", content, nil)
	if !ok || got.RelPath != `C:\Temp\dagents\capture.jpg` || got.Source != "computer" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestExtractToolMediaPaths_readImageFromContent(t *testing.T) {
	got, ok := ExtractToolMediaPaths("read_image", "[READ_IMAGE]\npath=pic.png\nstatus=ok", nil)
	if !ok || got.RelPath != "pic.png" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestExtractToolMediaPaths_unknownTool(t *testing.T) {
	if _, ok := ExtractToolMediaPaths("bash_run", "ok", nil); ok {
		t.Fatal("expected false")
	}
}
