package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func workspaceTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfg := &config.Config{NodeID: "node-test", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	db, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.triggerSched.Stop()
	t.Cleanup(func() { srv.sessions.Stop() })
	srv.agents = db
	srv.workspaceCoord = workspacecoord.New()
	if err := db.Save(context.Background(), store.AgentRecord{AgentID: "agt-one", DisplayName: "One", ConfigSnapshot: []byte(`{"workspace":{"mode":"private"}}`)}); err != nil {
		t.Fatal(err)
	}
	root, err := agentruntime.EnsureWorkspace(cfg.RuntimeDir(), "agt-one", agentruntime.WorkspaceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return srv, root
}

func workspaceRequest(s *Server, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestWorkspaceLocationReturnsAuthoritativeAgentRoot(t *testing.T) {
	s, root := workspaceTestServer(t)
	w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/location", nil, map[string]string{"X-Dagents-Native-Workspace": "1"})
	if w.Code != http.StatusOK {
		t.Fatalf("location status=%d body=%s", w.Code, w.Body)
	}
	var got struct {
		AgentID string `json:"agent_id"`
		Root    string `json:"root"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentID != "agt-one" || filepath.Clean(got.Root) != filepath.Clean(want) {
		t.Fatalf("location=%+v want agent=agt-one root=%q", got, want)
	}
	missing := workspaceRequest(s, http.MethodGet, "/v1/agents/unknown/workspace/location", nil, map[string]string{"X-Dagents-Native-Workspace": "1"})
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown agent status=%d body=%s", missing.Code, missing.Body)
	}
}

func TestWorkspaceLocationIsUnavailableThroughGenericWebViewProxy(t *testing.T) {
	s, _ := workspaceTestServer(t)
	w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/location", nil, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("location without native bridge header status=%d body=%s", w.Code, w.Body)
	}
}

func TestWorkspaceHTTPCRUDAndRevisions(t *testing.T) {
	s, root := workspaceTestServer(t)
	w := workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path=folder%2Fnote.bin", []byte{0, 1, 2, 255}, map[string]string{"If-None-Match": "*"})
	if w.Code != 200 {
		t.Fatalf("create status %d: %s", w.Code, w.Body)
	}
	rev := w.Header().Get("ETag")
	if rev == "" {
		t.Fatal("missing ETag")
	}
	w = workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace?path=folder", nil, nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"file_ref"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"revision"`)) {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(root, "folder", "note.bin"), 0751); err != nil {
			t.Fatal(err)
		}
		if w = workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path=folder%2Fnote.bin", []byte("mode"), map[string]string{"If-Match": rev}); w.Code != 200 {
			t.Fatalf("mode update status=%d body=%s", w.Code, w.Body)
		}
		rev = w.Header().Get("ETag")
		if st, err := os.Stat(filepath.Join(root, "folder", "note.bin")); err != nil || st.Mode().Perm() != 0751 {
			t.Fatalf("mode not preserved: %v %v", st, err)
		}
	}
	w = workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path=folder%2Fnote.bin", []byte("new"), map[string]string{"If-Match": rev})
	if w.Code != 200 {
		t.Fatalf("update status %d: %s", w.Code, w.Body)
	}
	w = workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path=folder%2Fnote.bin", []byte("stale"), map[string]string{"If-Match": rev})
	if w.Code != 412 {
		t.Fatalf("stale update status=%d body=%s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "folder", "note.bin")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceReadRoutesAreNoLongerRegistered(t *testing.T) {
	s, _ := workspaceTestServer(t)
	file := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path=note.txt", nil, nil)
	if file.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET workspace file status=%d; want 405 from the remaining PUT route", file.Code)
	}
	chunk := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/chunk?path=note.txt&offset=0", nil, nil)
	if chunk.Code != http.StatusNotFound {
		t.Fatalf("GET workspace chunk status=%d; want 404", chunk.Code)
	}
}

func TestWorkspaceHTTPPutStillRejectsStaleRevision(t *testing.T) {
	s, root := workspaceTestServer(t)
	const requestPath = "/v1/agents/agt-one/workspace/file?path=revision.txt"
	created := workspaceRequest(s, http.MethodPut, requestPath, []byte("alpha"), map[string]string{"If-None-Match": "*"})
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body)
	}
	oldETag := created.Header().Get("ETag")
	filePath := filepath.Join(root, "revision.txt")
	fixedTime := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filePath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("omega"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filePath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("test setup did not preserve size/mtime: before=(%d,%s) after=(%d,%s)", before.Size(), before.ModTime(), after.Size(), after.ModTime())
	}
	stale := workspaceRequest(s, http.MethodPut, requestPath, []byte("stale"), map[string]string{"If-Match": oldETag})
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale PUT status=%d body=%s", stale.Code, stale.Body)
	}
	contents, err := os.ReadFile(filePath)
	if err != nil || string(contents) != "omega" {
		t.Fatalf("stale PUT changed file: contents=%q err=%v", contents, err)
	}
}

func TestWorkspaceWritesRejectTraversalPrivateStateAndSymlinks(t *testing.T) {
	s, root := workspaceTestServer(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{"../secret.txt", ".dagents/agt-one/private", "C:%5Csecret", "%5C%5Cserver%5Cshare"}
	for _, path := range paths {
		put := workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path="+path, []byte("x"), map[string]string{"If-None-Match": "*"})
		if put.Code < 400 {
			t.Errorf("PUT accepted unsafe path %q: %d", path, put.Code)
		}
		upload := workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/upload?path="+path, []byte("x"), nil)
		if upload.Code < 400 {
			t.Errorf("upload accepted unsafe path %q: %d", path, upload.Code)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".dagents", "agt-one"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dagents", "agt-one", "private"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, ".dagents", "agt-one"), filepath.Join(root, "private-link")); err == nil {
		w := workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path=private-link%2Fprivate", []byte("overwrite"), map[string]string{"If-Match": fileRevision([]byte("secret"))})
		if w.Code < 400 {
			t.Fatalf("PUT followed link into private state: %d", w.Code)
		}
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "escape")); err == nil {
		for _, method := range []string{http.MethodPut, http.MethodPost} {
			path := "/v1/agents/agt-one/workspace/file?path=escape%2Fsecret.txt"
			if method == http.MethodPost {
				path = "/v1/agents/agt-one/workspace/upload?path=escape%2Fsecret.txt"
			}
			w := workspaceRequest(s, method, path, []byte("overwrite"), map[string]string{"If-None-Match": "*"})
			if w.Code < 400 {
				t.Errorf("%s followed workspace escape: %d", method, w.Code)
			}
		}
	} else if runtime.GOOS != "windows" {
		t.Fatalf("create symlink: %v", err)
	}
}

func TestWorkspaceHTTPDistinctAgentIsolation(t *testing.T) {
	s, root := workspaceTestServer(t)
	if err := os.WriteFile(filepath.Join(root, "only-one.txt"), []byte("one"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.agents.Save(context.Background(), store.AgentRecord{AgentID: "agt-two", DisplayName: "Two", ConfigSnapshot: []byte(`{"workspace":{"mode":"private"}}`)}); err != nil {
		t.Fatal(err)
	}
	w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-two/workspace", nil, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "only-one.txt") {
		t.Fatalf("other Agent workspace listing leaked file: %d %s", w.Code, w.Body)
	}
}

func TestWorkspaceHTTPRejectsOversizeAndIsolatesAgents(t *testing.T) {
	s, root := workspaceTestServer(t)
	w := workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path=huge", bytes.Repeat([]byte{'x'}, workspaceFileLimit+1), map[string]string{"If-None-Match": "*"})
	if w.Code != 413 {
		t.Fatalf("oversize status=%d", w.Code)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dagents", "agt-one"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, ".dagents", "agt-one", "private"), []byte("secret"), 0600)
	w = workspaceRequest(s, http.MethodGet, "/v1/agents/missing/workspace", nil, nil)
	if w.Code != 404 {
		t.Fatalf("unknown agent status=%d", w.Code)
	}
}
