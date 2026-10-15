package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
	w = workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path=folder%2Fnote.bin", nil, nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), []byte{0, 1, 2, 255}) || w.Header().Get("ETag") != rev {
		t.Fatalf("read status=%d body=%v etag=%s", w.Code, w.Body.Bytes(), w.Header().Get("ETag"))
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

func TestWorkspaceHTTPRejectsTraversalPrivateStateAndLinks(t *testing.T) {
	s, root := workspaceTestServer(t)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", ".dagents/agt-one/history/log", "C:%5Csecret", "%5C%5Cserver%5Cshare"} {
		w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path="+path, nil, nil)
		if w.Code < 400 {
			t.Errorf("path %q accepted", path)
		}
	}
	for _, path := range []string{".DAGENTS/agt-one/private", "%2eDAGENTS/agt-one/private"} {
		w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path="+path, nil, nil)
		if w.Code < 400 {
			t.Errorf("reserved path %q accepted", path)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".dagents", "agt-one"), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, ".dagents", "agt-one", "private"), []byte("secret"), 0600)
	if err := os.Symlink(filepath.Join(root, ".dagents", "agt-one"), filepath.Join(root, "private-link")); err == nil {
		w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path=private-link%2Fprivate", nil, nil)
		if w.Code < 400 {
			t.Fatalf("link into private namespace accepted: %d", w.Code)
		}
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(filepath.Dir(outside), link); err == nil {
		w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path=escape%2Fsecret", nil, nil)
		if w.Code < 400 {
			t.Fatalf("symlink escape accepted: %d", w.Code)
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
	w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-two/workspace/file?path=only-one.txt", nil, nil)
	if w.Code != 404 {
		t.Fatalf("other Agent saw private workspace file: %d %s", w.Code, w.Body)
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
