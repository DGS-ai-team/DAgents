package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/wecom"
)

func TestWeComTools_requireClient(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	defs := reg.Definitions()
	for _, d := range defs {
		if strings.HasPrefix(d.Function.Name, "wecom_") {
			t.Fatalf("unexpected wecom tool without client: %s", d.Function.Name)
		}
	}
}

func TestWeComTools_sendMarkdownAndFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "upload_media"):
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","media_id":"M1"}`))
		default:
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		}
	}))
	defer srv.Close()

	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetWeComClient(wecom.NewClient(srv.URL, "k", srv.Client()))

	var foundMD, foundFile bool
	for _, d := range reg.Definitions() {
		switch d.Function.Name {
		case "wecom_send_markdown":
			foundMD = true
		case "wecom_send_file":
			foundFile = true
		}
	}
	if !foundMD || !foundFile {
		t.Fatalf("defs missing wecom tools md=%v file=%v", foundMD, foundFile)
	}

	out, err := reg.Execute(context.Background(), "wecom_send_markdown", `{"call_purpose":"t","content":"# hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	var md map[string]any
	if err := json.Unmarshal([]byte(out), &md); err != nil || md["ok"] != true {
		t.Fatalf("markdown result=%s err=%v", out, err)
	}

	out, err = reg.Execute(context.Background(), "wecom_send_file", `{"call_purpose":"t","path":"note.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal([]byte(out), &file); err != nil || file["ok"] != true {
		t.Fatalf("file result=%s err=%v", out, err)
	}
	if file["media_id"] != "M1" {
		t.Fatalf("media_id=%v", file["media_id"])
	}
}
