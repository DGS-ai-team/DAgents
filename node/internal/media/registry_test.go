package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryRegisterAndOpen(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imgPath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry("sess-abc", dir)
	if err != nil {
		t.Fatal(err)
	}
	art, err := reg.RegisterFromRelPath(RegisterOpts{
		RelPath: "shot.png",
		Source:  "browser_snapshot",
		Label:   "browser_snapshot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art.MIME != "image/png" {
		t.Fatalf("mime = %q", art.MIME)
	}
	if art.PublicURL() != "/v1/sessions/sess-abc/media/"+art.ID {
		t.Fatalf("url = %q", art.PublicURL())
	}
	got, abs, err := reg.OpenFile(art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != art.ID {
		t.Fatalf("id mismatch")
	}
	if abs != imgPath {
		t.Fatalf("abs = %q want %q", abs, imgPath)
	}
}

func TestRegistryRegisterExternalAbsolutePath(t *testing.T) {
	fsRoot := t.TempDir()
	externalDir := t.TempDir()
	imgPath := filepath.Join(externalDir, "remote.png")
	if err := os.WriteFile(imgPath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry("sess-ext", fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	art, err := reg.RegisterFromPath(RegisterOpts{
		Path:   imgPath,
		Source: "show_image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art.AbsPath == "" {
		t.Fatal("expected external AbsPath")
	}
	_, abs, err := reg.OpenFile(art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if abs != imgPath {
		t.Fatalf("abs=%q want %q", abs, imgPath)
	}
}

func TestRegistryRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry("sess-x", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.RegisterFromRelPath(RegisterOpts{RelPath: "../etc/passwd"}); err == nil {
		t.Fatal("expected error for traversal")
	}
}

func TestRegistryRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry("sess-x", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.RegisterFromRelPath(RegisterOpts{RelPath: "a.txt"}); err != ErrInvalidImage {
		t.Fatalf("err = %v", err)
	}
}
