package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func staged(t *testing.T, s *Server, path, mode, kind, expected string, data []byte) (string, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"path": path, "mode": mode, "kind": kind, "expected_revision": expected, "size": len(data)})
	w := workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes", body, map[string]string{"X-Dagents-Native-Workspace": "1"})
	if w.Code != 200 {
		t.Fatalf("prepare: %d %s", w.Code, w.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	stage := out["staging_path"].(string)
	if err := os.WriteFile(stage, data, 0600); err != nil {
		t.Fatal(err)
	}
	return stage, out
}
func commitStaged(t *testing.T, s *Server, id string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	sum := sha256.Sum256(data)
	body, _ := json.Marshal(map[string]any{"size": len(data), "sha256": hex.EncodeToString(sum[:])})
	return workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes/"+id+"/commit", body, map[string]string{"X-Dagents-Native-Workspace": "1"})
}

func TestWorkspaceStagedWritesCreateReplaceConflictAndIdempotency(t *testing.T) {
	s, root := workspaceTestServer(t)
	t.Cleanup(func() {
		if s.workspaceWrites != nil {
			s.workspaceWrites.close()
		}
	})
	data := []byte("large import payload")
	_, prep := staged(t, s, "folder/file.bin", "create", "import", "", data)
	id := prep["operation_id"].(string)
	w := commitStaged(t, s, id, data)
	if w.Code != 200 {
		t.Fatalf("create commit %d %s", w.Code, w.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "folder", "file.bin")); string(got) != string(data) {
		t.Fatalf("content=%q", got)
	}
	if w = commitStaged(t, s, id, data); w.Code != 200 {
		t.Fatalf("idempotent commit %d %s", w.Code, w.Body)
	}
	sum := sha256.Sum256(data)
	_, prep = staged(t, s, "folder/file.bin", "replace", "text", hex.EncodeToString(sum[:]), []byte("updated"))
	id = prep["operation_id"].(string)
	if w = commitStaged(t, s, id, []byte("updated")); w.Code != 200 {
		t.Fatalf("replace commit %d %s", w.Code, w.Body)
	}
	_, prep = staged(t, s, "folder/file.bin", "create", "text", "", []byte("must not overwrite"))
	if w = commitStaged(t, s, prep["operation_id"].(string), []byte("must not overwrite")); w.Code != 412 {
		t.Fatalf("create overwrite status=%d body=%s", w.Code, w.Body)
	}
}

func TestWorkspacePrepareReturnsAbsoluteStagePathForRelativeRuntimeRoot(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := os.MkdirTemp(cwd, "workspace-write-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	relativeRoot, err := filepath.Rel(cwd, workspace)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := workspaceTestServerWithRuntimeRoot(t, relativeRoot)
	stage, prep := staged(t, s, "portable.txt", "create", "text", "", []byte("portable"))
	stagingRoot := prep["staging_root"].(string)
	if !filepath.IsAbs(stage) || !filepath.IsAbs(stagingRoot) {
		t.Fatalf("staging response is not absolute: path=%q root=%q", stage, stagingRoot)
	}
	canonical, err := filepath.EvalSymlinks(stagingRoot)
	if err != nil {
		t.Fatalf("staging root unavailable from caller cwd: %v", err)
	}
	if filepath.Clean(filepath.Dir(stage)) != filepath.Clean(canonical) {
		t.Fatalf("stage path %q is outside canonical root %q", stage, canonical)
	}
	if w := commitStaged(t, s, prep["operation_id"].(string), []byte("portable")); w.Code != 200 {
		t.Fatalf("commit relative runtime workspace: %d %s", w.Code, w.Body)
	}
}

func TestWorkspaceWriteRejectsStagingManagerInitializationFailure(t *testing.T) {
	s, _ := workspaceTestServer(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.workspaceWrites = newWorkspaceWriteManager(filepath.Join(blocker, "writes"))
	body, _ := json.Marshal(map[string]any{"path": "file.txt", "mode": "create", "kind": "text", "size": 0})
	w := workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes", body, map[string]string{"X-Dagents-Native-Workspace": "1"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("staging init failure status=%d body=%s", w.Code, w.Body)
	}
}

func TestWorkspaceStagedWritesCancelDigestAndMarker(t *testing.T) {
	s, root := workspaceTestServer(t)
	data := []byte("payload")
	stage, prep := staged(t, s, "cancelled.txt", "create", "text", "", data)
	id := prep["operation_id"].(string)
	w := commitStaged(t, s, id, []byte("bad"))
	if w.Code < 400 {
		t.Fatalf("bad digest accepted: %d", w.Code)
	}
	w = workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes/"+id+"/cancel", nil, map[string]string{"X-Dagents-Native-Workspace": "1"})
	if w.Code != 200 {
		t.Fatalf("cancel %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("staging file remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "cancelled.txt")); !os.IsNotExist(err) {
		t.Fatalf("cancel published target: %v", err)
	}
	bad := workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes", []byte(`{"path":"x","mode":"create","kind":"text","size":0}`), nil)
	if bad.Code != http.StatusForbidden {
		t.Fatalf("missing marker status=%d", bad.Code)
	}
}

func TestWorkspaceStagedWritesRejectsStagingHardlinkSwap(t *testing.T) {
	s, root := workspaceTestServer(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("same"), 0600); err != nil {
		t.Fatal(err)
	}
	stage, prep := staged(t, s, "target", "create", "text", "", []byte("same"))
	if err := os.Remove(stage); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(victim, stage); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	w := commitStaged(t, s, prep["operation_id"].(string), []byte("same"))
	if w.Code < 400 {
		t.Fatalf("hardlink swap accepted: %d %s", w.Code, w.Body)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "same" {
		t.Fatalf("victim changed: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "target")); !os.IsNotExist(err) {
		t.Fatalf("target published: %v", err)
	}
}

func TestWorkspaceStagedWritesNeverRecreatesDeletedReplaceTarget(t *testing.T) {
	s, root := workspaceTestServer(t)
	target := filepath.Join(root, "gone.txt")
	if err := os.WriteFile(target, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("before"))
	_, prep := staged(t, s, "gone.txt", "replace", "text", hex.EncodeToString(sum[:]), []byte("after"))
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	w := commitStaged(t, s, prep["operation_id"].(string), []byte("after"))
	if w.Code != 412 {
		t.Fatalf("deleted target status=%d body=%s", w.Code, w.Body)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("deleted target recreated: %v", err)
	}
}

func TestWorkspaceStagedWritePrepareRejectsUnsafePaths(t *testing.T) {
	s, root := workspaceTestServer(t)
	outside := filepath.Join(t.TempDir(), "outside")
	_ = os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "escape")); err != nil && runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	paths := []string{"../outside", ".dagents/private", "escape/outside", "C:\\secret"}
	if runtime.GOOS == "windows" {
		paths = append(paths, ".dagents./private", ".dagents /private")
	}
	for _, path := range paths {
		body, _ := json.Marshal(map[string]any{"path": path, "mode": "create", "kind": "text", "size": 1})
		w := workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes", body, map[string]string{"X-Dagents-Native-Workspace": "1"})
		if w.Code < 400 {
			t.Errorf("unsafe path %q accepted: %d", path, w.Code)
		}
	}
}

func TestWorkspaceStagedWriteQuotaReleasesCompletedOperations(t *testing.T) {
	s, _ := workspaceTestServer(t)
	data := []byte("x")
	_, active := staged(t, s, "still-active", "create", "text", "", data)
	firstID := ""
	for i := 0; i < workspaceCompletedResultLimit+1; i++ {
		name := fmt.Sprintf("batch-%02d", i)
		_, prep := staged(t, s, name, "create", "text", "", data)
		if i == 0 {
			firstID = prep["operation_id"].(string)
		}
		if w := commitStaged(t, s, prep["operation_id"].(string), data); w.Code != 200 {
			t.Fatalf("commit %d: %d %s", i, w.Code, w.Body)
		}
	}
	if s.workspaceWrites.activeCount() != 1 || s.workspaceWrites.used != 1 || len(s.workspaceWrites.ops) != workspaceCompletedResultLimit+1 {
		t.Fatalf("completed operations still consume capacity: active=%d bytes=%d", s.workspaceWrites.activeCount(), s.workspaceWrites.used)
	}
	if w := commitStaged(t, s, firstID, data); w.Code != http.StatusNotFound {
		t.Fatalf("evicted result status=%d body=%s", w.Code, w.Body)
	}
	if got, err := os.ReadFile(filepath.Join(s.workspaceWrites.ops[active["operation_id"].(string)].root, "batch-00")); err != nil || string(got) != "x" {
		t.Fatalf("evicted operation changed published target: %q %v", got, err)
	}
	w := workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes/"+active["operation_id"].(string)+"/cancel", nil, map[string]string{"X-Dagents-Native-Workspace": "1"})
	if w.Code != 200 {
		t.Fatalf("active operation was evicted: %d %s", w.Code, w.Body)
	}
}

func TestWorkspaceListHidesOnlyNodeTemporaryWrites(t *testing.T) {
	s, root := workspaceTestServer(t)
	_ = os.WriteFile(filepath.Join(root, ".dagents-write-internal"), []byte("partial"), 0600)
	_ = os.WriteFile(filepath.Join(root, ".notes"), []byte("user"), 0600)
	_ = os.Mkdir(filepath.Join(root, "nested"), 0700)
	_ = os.WriteFile(filepath.Join(root, "nested", "note.txt"), []byte("user"), 0600)
	w := workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace", nil, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), ".dagents-write-internal") || !strings.Contains(w.Body.String(), ".notes") {
		t.Fatalf("workspace listing exposed/hid wrong entries: %d %s", w.Code, w.Body)
	}
	w = workspaceRequest(s, http.MethodGet, "/v1/agents/agt-one/workspace?path=nested", nil, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "note.txt") {
		t.Fatalf("nested workspace listing failed: %d %s", w.Code, w.Body)
	}
}

func TestWorkspaceWriteRestartCleanupOnlyRemovesProductOperations(t *testing.T) {
	dir := t.TempDir()
	op := filepath.Join(dir, "0123456789abcdef0123456789abcdef0123456789abcdef")
	unrelated := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(op, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = newWorkspaceWriteManager(dir)
	if _, err := os.Stat(op); !os.IsNotExist(err) {
		t.Fatalf("stale op remains: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}

func TestWorkspaceWriteParentReplacementCannotRedirectIntoPrivateDir(t *testing.T) {
	_, root := workspaceTestServer(t)
	private := filepath.Join(root, ".dagents", "agt-one")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	parentPath := filepath.Join(root, "parent")
	if err := os.Mkdir(parentPath, 0755); err != nil {
		t.Fatal(err)
	}
	held, err := openWorkspaceWriteParent(root, "parent/file.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	defer held.close()
	if err = os.Rename(parentPath, filepath.Join(root, "parent-moved")); err != nil {
		t.Fatalf("could not replace held parent directory: %v", err)
	}
	if err = os.Symlink(private, parentPath); err != nil {
		t.Fatalf("could not create private-directory reparse point: %v", err)
	}
	if err = verifyWorkspaceWriteParent(root, "parent/file.txt", held); err == nil {
		t.Fatal("parent replacement passed identity verification")
	}
	if _, err = os.Stat(filepath.Join(private, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("private target appeared after parent replacement: %v", err)
	}
}

func TestWorkspaceStagedCommitRejectsArchivedAgent(t *testing.T) {
	s, root := workspaceTestServer(t)
	data := []byte("x")
	_, prep := staged(t, s, "archive.txt", "create", "text", "", data)
	rec, err := s.agents.Get(t.Context(), "agt-one")
	if err != nil {
		t.Fatal(err)
	}
	rec.Archived = true
	if err = s.agents.Save(t.Context(), *rec); err != nil {
		t.Fatal(err)
	}
	w := commitStaged(t, s, prep["operation_id"].(string), data)
	if w.Code != 409 {
		t.Fatalf("archived Agent commit status=%d %s", w.Code, w.Body)
	}
	w = workspaceRequest(s, http.MethodPost, "/v1/agents/agt-one/workspace/writes/"+prep["operation_id"].(string)+"/cancel", nil, map[string]string{"X-Dagents-Native-Workspace": "1"})
	if w.Code != 200 {
		t.Fatalf("cancel after failed archived commit status=%d %s", w.Code, w.Body)
	}
	if _, err = os.Stat(filepath.Join(root, "archive.txt")); !os.IsNotExist(err) {
		t.Fatalf("archived Agent wrote target: %v", err)
	}
}

func TestWorkspaceStagedCommitWaitCanBeCancelled(t *testing.T) {
	s, root := workspaceTestServer(t)
	data := []byte("x")
	_, prep := staged(t, s, "busy.txt", "create", "text", "", data)
	lease, err := s.workspaceCoord.TryAcquire(t.Context(), filepath.Join(root, "busy.txt"), "test-holder")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	old := workspaceLockWait
	workspaceLockWait = 40 * time.Millisecond
	defer func() { workspaceLockWait = old }()
	w := commitStaged(t, s, prep["operation_id"].(string), data)
	if w.Code != 409 {
		t.Fatalf("busy commit status=%d %s", w.Code, w.Body)
	}
	if _, err = os.Stat(filepath.Join(root, "busy.txt")); !os.IsNotExist(err) {
		t.Fatalf("busy commit published target: %v", err)
	}
}

func TestWorkspaceStagedOperationTTLAndCloseCleanup(t *testing.T) {
	s, _ := workspaceTestServer(t)
	stage, prep := staged(t, s, "expired.txt", "create", "text", "", []byte("x"))
	id := prep["operation_id"].(string)
	s.workspaceWrites.mu.Lock()
	s.workspaceWrites.ops[id].expires = time.Now().Add(-time.Second)
	s.workspaceWrites.mu.Unlock()
	w := commitStaged(t, s, id, []byte("x"))
	if w.Code != 410 {
		t.Fatalf("expired operation status=%d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("expired staging file remains: %v", err)
	}
	stage, prep = staged(t, s, "close.txt", "create", "text", "", []byte("x"))
	_ = prep
	s.workspaceWrites.close()
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("Close left staging file: %v", err)
	}
}
