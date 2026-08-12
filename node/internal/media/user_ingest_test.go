package media

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestPersistUserMessageImages_roundTrip(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry("sess-u1", dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{0x89, 0x50, 0x4e, 0x47}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	msg, err := llm.BuildUserMessage("see this", []llm.ContentPart{{
		Type:     "image_url",
		ImageURL: &llm.ImageURLPart{URL: dataURL},
	}}, llm.UserNameHuman)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := PersistUserMessageImages(reg, msg)
	if err != nil {
		t.Fatal(err)
	}
	var ref string
	for _, part := range stored.ContentParts {
		if part.Type == "image_url" && part.ImageURL != nil {
			ref = part.ImageURL.URL
			break
		}
	}
	if !IsRefURL(ref) {
		t.Fatalf("expected media ref, got %q parts=%+v", ref, stored.ContentParts)
	}
	id, _ := ParseRefURL(ref)
	rel := UserUploadRelPath("sess-u1", id, ".png")
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("file missing: %v", err)
	}
	expanded := ExpandMessagesForLLM([]llm.Message{stored}, reg)
	var got string
	for _, part := range expanded[0].ContentParts {
		if part.Type == "image_url" && part.ImageURL != nil {
			got = part.ImageURL.URL
			break
		}
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("expand failed: %q", got)
	}
}

func TestUserMediaFromMessage_hydrateRef(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry("sess-u2", dir)
	if err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	stored, err := PersistUserMessageImages(reg, llm.Message{
		Role: "user",
		ContentParts: []llm.ContentPart{{
			Type:     "image_url",
			ImageURL: &llm.ImageURLPart{URL: dataURL},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	images, items := UserMediaFromMessage(stored, reg)
	if len(images) != 1 || len(items) != 1 {
		t.Fatalf("images=%v items=%v", images, items)
	}
	if !strings.HasPrefix(images[0], "/v1/agents/sess-u2/media/") {
		t.Fatalf("url=%q", images[0])
	}
}

func TestEnsureArtifact_coldLoad(t *testing.T) {
	dir := t.TempDir()
	reg1, err := NewRegistry("sess-cold", dir)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := PersistUserMessageImages(reg1, llm.Message{
		Role: "user",
		ContentParts: []llm.ContentPart{{
			Type:     "image_url",
			ImageURL: &llm.ImageURLPart{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := ParseRefURL(stored.ContentParts[0].ImageURL.URL)

	reg2, err := NewRegistry("sess-cold", dir)
	if err != nil {
		t.Fatal(err)
	}
	art, err := reg2.EnsureArtifact(id)
	if err != nil {
		t.Fatal(err)
	}
	if art.ID != id {
		t.Fatalf("id=%q want %q", art.ID, id)
	}
}
