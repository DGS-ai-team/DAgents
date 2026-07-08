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
