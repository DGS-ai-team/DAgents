package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadImage_success(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetMultimodalEnabled(true)
	imgPath := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(imgPath, []byte("fake-png"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := WithToolCallID(context.Background(), "call-read-image-1")
	out, err := reg.Execute(ctx, "read_image", `{"path":"pic.png","call_purpose":"inspect"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[READ_IMAGE]") || !strings.Contains(out, "status=ok") {
		t.Fatalf("out = %q", out)
	}
	payload := reg.TakeReadImageVisionForCall("call-read-image-1")
	if payload == nil {
		t.Fatal("expected vision payload")
	}
	if payload.RelPath != "pic.png" {
		t.Fatalf("path = %q", payload.RelPath)
	}
	if !strings.HasPrefix(payload.DataURL, "data:image/png;base64,") {
		t.Fatalf("data url = %q", payload.DataURL)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(payload.DataURL, "data:image/png;base64,"))
	if err != nil || string(decoded) != "fake-png" {
		t.Fatalf("decoded = %q err=%v", decoded, err)
	}
}

func TestReadImage_rejectsTextFile(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetMultimodalEnabled(true)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "read_image", `{"path":"a.txt","call_purpose":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status=error") {
		t.Fatalf("out = %q", out)
	}
}

func TestReadImage_rejectsOutsideSandbox(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetMultimodalEnabled(true)
	out, err := reg.Execute(context.Background(), "read_image", `{"path":"../outside.png","call_purpose":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status=error") {
		t.Fatalf("out = %q", out)
	}
}
