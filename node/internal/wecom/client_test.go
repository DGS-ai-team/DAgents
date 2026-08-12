package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestNewClientFromConfig(t *testing.T) {
	t.Parallel()
	if NewClientFromConfig(nil) != nil {
		t.Fatal("nil cfg")
	}
	enabled := true
	cfg := &config.Config{WeCom: config.WeComConfig{
		Enabled:    &enabled,
		WebhookKey: "k1",
	}}
	c := NewClientFromConfig(cfg)
	if c == nil || !c.Enabled() {
		t.Fatal("expected client")
	}
}

func TestSendMarkdownV2(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/cgi-bin/webhook/send") {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Fatalf("key=%s", r.URL.Query().Get("key"))
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	c := &Client{apiBase: srv.URL, webhookKey: "test-key", http: srv.Client()}
	if err := c.SendMarkdownV2(context.Background(), "# hello"); err != nil {
		t.Fatal(err)
	}
	if gotBody["msgtype"] != "markdown_v2" {
		t.Fatalf("body=%v", gotBody)
	}
	md, _ := gotBody["markdown_v2"].(map[string]any)
	if md["content"] != "# hello" {
		t.Fatalf("content=%v", md)
	}
}

func TestSendFilePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(path, []byte("hello wecom"), 0o644); err != nil {
		t.Fatal(err)
	}

	var uploadHit, sendHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "upload_media"):
			uploadHit = true
			if r.URL.Query().Get("type") != "file" {
				t.Fatalf("type=%s", r.URL.Query().Get("type"))
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","type":"file","media_id":"MID1"}`))
		case strings.Contains(r.URL.Path, "webhook/send"):
			sendHit = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["msgtype"] != "file" {
				t.Fatalf("body=%v", body)
			}
			file, _ := body["file"].(map[string]any)
			if file["media_id"] != "MID1" {
				t.Fatalf("media_id=%v", file)
			}
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{apiBase: srv.URL, webhookKey: "k", http: srv.Client()}
	id, err := c.SendFilePath(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if id != "MID1" || !uploadHit || !sendHit {
		t.Fatalf("id=%s upload=%v send=%v", id, uploadHit, sendHit)
	}
}

func TestSendMarkdownV2_apiError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()
	c := &Client{apiBase: srv.URL, webhookKey: "bad", http: srv.Client()}
	err := c.SendMarkdownV2(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "93000") {
		t.Fatalf("err=%v", err)
	}
}
