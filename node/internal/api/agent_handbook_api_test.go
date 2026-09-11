package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestAgentHandbookHTTPIsolatesFilesAndHonorsConfiguredDirectory(t *testing.T) {
	cfg := &config.Config{NodeID: "handbook-http", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	_ = settings.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}))
	defer srv.Close()
	now := time.Now().UTC()
	for _, id := range []string{"handbook-a", "handbook-b"} {
		if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	root := func(id string) string {
		return filepath.Join(cfg.RuntimeDir(), "agents", id, "workspace", ".dagents", id, "handbook")
	}
	if err := os.MkdirAll(root("handbook-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root("handbook-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root("handbook-a"), "README.md"), []byte("from A"), 0o644); err != nil {
		t.Fatal(err)
	}
	get := func(id, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/agents/"+id+"/handbook"+path, nil)
		srv.Handler().ServeHTTP(w, req)
		return w
	}
	a := get("handbook-a", "?path=README.md")
	if a.Code != 200 || !strings.Contains(a.Body.String(), "from A") {
		t.Fatalf("initial read status=%d body=%s", a.Code, a.Body.String())
	}
	b := get("handbook-b", "?path=README.md")
	if b.Code != 404 {
		t.Fatalf("cross-agent read status=%d body=%s", b.Code, b.Body.String())
	}
	if err := os.WriteFile(filepath.Join(root("handbook-b"), "README.md"), []byte("from B"), 0o644); err != nil {
		t.Fatal(err)
	}
	b = get("handbook-b", "?path="+url.QueryEscape("README.md"))
	if b.Code != 200 || !strings.Contains(b.Body.String(), "from B") || strings.Contains(b.Body.String(), "from A") {
		t.Fatalf("agent B isolation status=%d body=%s", b.Code, b.Body.String())
	}
	if err := os.WriteFile(filepath.Join(root("handbook-a"), "README.md"), []byte("edited externally"), 0o644); err != nil {
		t.Fatal(err)
	}
	a = get("handbook-a", "?path=README.md")
	if !strings.Contains(a.Body.String(), "edited externally") {
		t.Fatalf("external edit not visible: %s", a.Body.String())
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"../secret", 400}, {`C:\secret`, 400}, {"missing.md", 404}} {
		w := get("handbook-a", "?path="+url.QueryEscape(tc.path))
		if w.Code != tc.status {
			t.Fatalf("path %q status=%d want=%d body=%s", tc.path, w.Code, tc.status, w.Body.String())
		}
	}
	patch := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patch, httptest.NewRequest(http.MethodPatch, "/v1/agents/handbook-a", strings.NewReader(`{"handbook":{"directory":"custom"}}`)))
	if patch.Code != 200 {
		t.Fatalf("patch status=%d body=%s", patch.Code, patch.Body.String())
	}
	keep := httptest.NewRecorder()
	srv.Handler().ServeHTTP(keep, httptest.NewRequest(http.MethodPatch, "/v1/agents/handbook-a", strings.NewReader(`{"defaults":{"agent":{"description":"kept"}}}`)))
	if keep.Code != 200 {
		t.Fatalf("follow-up patch status=%d body=%s", keep.Code, keep.Body.String())
	}
	custom := filepath.Join(cfg.RuntimeDir(), "agents", "handbook-a", "workspace", ".dagents", "handbook-a", "custom")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, "new.md"), []byte("new root"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := get("handbook-a", "?path=new.md")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "new root") {
		t.Fatalf("configured directory status=%d body=%s", w.Code, w.Body.String())
	}
	if err := os.WriteFile(filepath.Join(custom, "large.md"), make([]byte, 1024*1024+1), 0o644); err != nil {
		t.Fatal(err)
	}
	w = get("handbook-a", "?path=large.md")
	if w.Code != 400 {
		t.Fatalf("large status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAgentHandbookHTTPHistoryRestoreRoundTrip(t *testing.T) {
	cfg := &config.Config{NodeID: "handbook-history-http", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	_ = settings.Close()
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}))
	defer srv.Close()
	now := time.Now().UTC()
	for _, id := range []string{"history-a", "history-b"} {
		if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(cfg.RuntimeDir(), "agents", "history-a", "workspace", ".dagents", "history-a", "handbook")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := handbookfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "README.md")
	v1, err := service.Write(context.Background(), path, "", []byte("version one"))
	if err != nil {
		t.Fatal(err)
	}
	v2, err := service.Write(context.Background(), path, handbookfs.Digest([]byte("version one")), []byte("version two"))
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, url, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(method, url, strings.NewReader(body)))
		return w
	}
	history := request(http.MethodGet, "/v1/agents/history-a/handbook/history?path=README.md", "")
	if history.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	var historyBody struct {
		History []handbookfs.Entry `json:"history"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &historyBody); err != nil {
		t.Fatal(err)
	}
	if len(historyBody.History) != 2 || historyBody.History[0].Revision != v1.Revision || historyBody.History[1].Revision != v2.Revision {
		t.Fatalf("unexpected history: %+v", historyBody.History)
	}
	current := request(http.MethodGet, "/v1/agents/history-a/handbook?path=README.md", "")
	var currentBody struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &currentBody); err != nil {
		t.Fatal(err)
	}
	restored := request(http.MethodPost, "/v1/agents/history-a/handbook/restore", `{"path":"README.md","revision":2,"expected_digest":"`+currentBody.Digest+`"}`)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restored.Code, restored.Body.String())
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "version one" {
		t.Fatalf("restored content=%q err=%v", content, err)
	}
	entries, err := service.History(context.Background(), path)
	if err != nil || len(entries) != 3 {
		t.Fatalf("restored history=%d err=%v", len(entries), err)
	}
	stale := request(http.MethodPost, "/v1/agents/history-a/handbook/restore", `{"path":"README.md","revision":2,"expected_digest":"stale"}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
	content, _ = os.ReadFile(path)
	entries, _ = service.History(context.Background(), path)
	if string(content) != "version one" || len(entries) != 3 {
		t.Fatalf("stale changed content=%q history=%d", content, len(entries))
	}
	newPath := filepath.Join(root, "new.md")
	newEntry, err := service.Write(context.Background(), newPath, "", []byte("new file"))
	if err != nil {
		t.Fatal(err)
	}
	newDigest := handbookfs.Digest([]byte("new file"))
	deleted := request(http.MethodPost, "/v1/agents/history-a/handbook/restore", `{"path":"new.md","revision":`+fmt.Sprint(newEntry.Revision)+`,"expected_digest":"`+newDigest+`"}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("first-write restore status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("first-write restore left file: %v", err)
	}
	newHistory, err := service.History(context.Background(), newPath)
	if err != nil || len(newHistory) != 2 {
		t.Fatalf("first-write restore history=%d err=%v", len(newHistory), err)
	}
	for _, body := range []string{`{"path":"README.md","revision":0,"expected_digest":"x"}`, `{"path":"README.md","revision":1}`} {
		bad := request(http.MethodPost, "/v1/agents/history-a/handbook/restore", body)
		if bad.Code != http.StatusBadRequest {
			t.Fatalf("invalid restore status=%d body=%s", bad.Code, bad.Body.String())
		}
	}
	private := request(http.MethodGet, "/v1/agents/history-a/handbook?path=.history/manifest.jsonl", "")
	if private.Code != http.StatusBadRequest {
		t.Fatalf("private file status=%d body=%s", private.Code, private.Body.String())
	}
	otherRoot := filepath.Join(cfg.RuntimeDir(), "agents", "history-b", "workspace", ".dagents", "history-b", "handbook")
	otherService, err := handbookfs.New(otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(otherRoot, "README.md")
	if _, err := otherService.Write(context.Background(), otherPath, "", []byte("agent B")); err != nil {
		t.Fatal(err)
	}
	other := request(http.MethodPost, "/v1/agents/history-b/handbook/restore", `{"path":"README.md","revision":2,"expected_digest":"`+handbookfs.Digest([]byte("agent B"))+`"}`)
	if other.Code != http.StatusBadRequest {
		t.Fatalf("cross-agent restore status=%d body=%s", other.Code, other.Body.String())
	}
	_ = v1
}
