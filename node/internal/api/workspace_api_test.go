package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func workspaceTestServer(t *testing.T) (*Server, string) {
	return workspaceTestServerWithRuntimeRoot(t, t.TempDir())
}

func workspaceTestServerWithRuntimeRoot(t *testing.T, runtimeRoot string) (*Server, string) {
	t.Helper()
	cfg := &config.Config{NodeID: "node-test", RuntimeRoot: runtimeRoot}
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

func TestLegacyWorkspaceBinaryRoutesAreNoLongerRegistered(t *testing.T) {
	s, _ := workspaceTestServer(t)
	file := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/file?path=note.txt", nil, nil)
	if file.Code != http.StatusNotFound {
		t.Fatalf("GET legacy workspace file status=%d; want 404", file.Code)
	}
	chunk := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace/chunk?path=note.txt&offset=0", nil, nil)
	if chunk.Code != http.StatusNotFound {
		t.Fatalf("GET legacy workspace chunk status=%d; want 404", chunk.Code)
	}
	put := workspaceRequest(s, http.MethodPut, "/v1/agents/agt-one/workspace/file?path=note.txt", []byte("legacy"), nil)
	if put.Code != http.StatusNotFound {
		t.Fatalf("PUT legacy workspace file status=%d; want 404", put.Code)
	}
	upload := workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/upload", []byte("legacy"), nil)
	if upload.Code != http.StatusNotFound {
		t.Fatalf("POST legacy workspace upload status=%d; want 404", upload.Code)
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
