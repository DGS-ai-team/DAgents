package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecShowImage_registersMedia(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "demo.png")
	if err := os.WriteFile(pngPath, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetMediaRegister(func(_ context.Context, toolCallID, relPath, source, label, caption string) (*MediaArtifactRef, error) {
		if toolCallID != "call-show" {
			t.Fatalf("toolCallID=%q", toolCallID)
		}
		if relPath != "demo.png" || source != "show_image" {
			t.Fatalf("relPath=%q source=%q", relPath, source)
		}
		return &MediaArtifactRef{
			ID:    "med_demo",
			Kind:  "image",
			MIME:  "image/png",
			URL:   "/v1/sessions/s1/media/med_demo",
			Label: label,
			Caption: caption,
		}, nil
	})
	ctx := WithToolCallID(WithSession(context.Background(), "s1"), "call-show")
	out, err := reg.Execute(ctx, "show_image", `{"path":"demo.png","caption":"hello","call_purpose":"展示"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[SHOW_IMAGE]") || !strings.Contains(out, "status=ok") {
		t.Fatalf("unexpected output: %q", out)
	}
	extra := reg.TakeToolResultMediaForCall("call-show")
	if extra == nil {
		t.Fatal("expected media extra")
	}
	items, ok := extra["media"].([]map[string]any)
	if !ok || len(items) != 1 || items[0]["id"] != "med_demo" {
		t.Fatalf("media extra: %#v", extra)
	}
}

func TestExecShowImage_rejectsMissingFile(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "show_image", `{"path":"missing.png","call_purpose":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status=error") {
		t.Fatalf("expected error output: %q", out)
	}
}

func TestShowImageInDefinitions(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, def := range reg.Definitions() {
		if def.Function.Name == "show_image" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("show_image missing from Definitions")
	}
}
