package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAllLines_jsonlAndHtml(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{\"a\":1}\n{\"b\":2}"), 0o644); err != nil {
		t.Fatal(err)
	}
	htmlPath := filepath.Join(dir, "page.html")
	if err := os.WriteFile(htmlPath, []byte("<html><body>ok</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	jsonlLines, err := readAllLines(jsonlPath, "utf-8")
	if err != nil {
		t.Fatalf("jsonl: %v", err)
	}
	if len(jsonlLines) != 2 || jsonlLines[0] != "{\"a\":1}" {
		t.Fatalf("jsonl lines = %v", jsonlLines)
	}

	htmlLines, err := readAllLines(htmlPath, "utf-8")
	if err != nil {
		t.Fatalf("html: %v", err)
	}
	if len(htmlLines) != 1 || htmlLines[0] != "<html><body>ok</body></html>" {
		t.Fatalf("html lines = %v", htmlLines)
	}
}
