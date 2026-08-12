package media

import (
	"bytes"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := solidColorImage(w, h, color.RGBA{R: 120, G: 80, B: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestThumbnailFromFileScalesLargeImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.png")
	writeTestPNG(t, path, 960, 480)

	data, mime, served, err := ThumbnailFromFile(path, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if !served {
		t.Fatal("expected thumbnail to be served")
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q", mime)
	}
	if len(data) == 0 {
		t.Fatal("empty thumbnail")
	}
	out, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := out.Bounds()
	if b.Dx() > ThumbnailMaxEdge || b.Dy() > ThumbnailMaxEdge {
		t.Fatalf("thumbnail too large: %dx%d", b.Dx(), b.Dy())
	}
	if b.Dx() != 480 {
		t.Fatalf("width = %d want 480", b.Dx())
	}
}

func TestThumbnailFromFileSkipsSmallImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.png")
	writeTestPNG(t, path, 200, 100)

	_, _, served, err := ThumbnailFromFile(path, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if served {
		t.Fatal("expected original for small image")
	}
}

func TestArtifactThumbnailURL(t *testing.T) {
	art := Artifact{AgentID: "sess-1", ID: "med_abc"}
	want := "/v1/agents/sess-1/media/med_abc?thumbnail=1"
	if art.ThumbnailURL() != want {
		t.Fatalf("got %q want %q", art.ThumbnailURL(), want)
	}
}
