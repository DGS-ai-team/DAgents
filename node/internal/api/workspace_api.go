package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
)

const workspaceFileLimit = 16 << 20

type workspaceEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	IsDir      bool   `json:"is_dir"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
	Revision   string `json:"revision,omitempty"`
	FileRef    string `json:"file_ref,omitempty"`
}

// handleWorkspaceLocation returns the authoritative, resolved workspace root
// for the native Desktop shell. It never returns file contents; callers still
// have to supply a validated workspace-relative path to native file commands.
func (s *Server) handleWorkspaceLocation(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Dagents-Native-Workspace") != "1" {
		writeAPIError(w, http.StatusForbidden, "native_workspace_marker_required", "missing X-Dagents-Native-Workspace invocation marker", nil)
		return
	}
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		writeAPIError(w, 400, "workspace_unavailable", "workspace root cannot be resolved", nil)
		return
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		writeAPIError(w, 400, "workspace_unavailable", "workspace root cannot be resolved", nil)
		return
	}
	writeJSON(w, 200, map[string]string{"agent_id": strings.TrimSpace(r.PathValue("agent_id")), "root": filepath.Clean(abs)})
}

func (s *Server) workspaceRoot(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if s.agents == nil {
		writeAPIError(w, 503, "agents_unavailable", "agents store not configured", nil)
		return "", false
	}
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil {
		writeAPIError(w, 500, "agent_get_failed", err.Error(), nil)
		return "", false
	}
	if rec == nil || rec.Archived {
		s.writeAgentNotFound(w, id)
		return "", false
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, 500, "workspace_config_invalid", err.Error(), nil)
		return "", false
	}
	root, err := agentruntime.EnsureWorkspace(s.cfg.RuntimeDir(), id, snap.Workspace)
	if err != nil {
		writeAPIError(w, 400, "workspace_unavailable", err.Error(), nil)
		return "", false
	}
	return root, true
}

// resolveWorkspacePath rejects non-local names and checks every existing path
// component after resolving links, preventing junction/symlink escapes.
func resolveWorkspacePath(root, rel string, allowEmpty bool) (string, string, error) {
	if rel == "" && allowEmpty {
		rel = "."
	}
	if rel == "" || strings.IndexByte(rel, 0) >= 0 || strings.ContainsAny(rel, `:\\`) || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", "", fmt.Errorf("path must be a local workspace-relative path")
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		// The empty-path listing case is represented internally as ".".
		// It names the workspace root and is not a user-supplied component.
		if part != "." && workspacePathComponentInvalid(part) {
			return "", "", fmt.Errorf("path contains an unsafe Windows component")
		}
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if reservedWorkspacePath(clean) {
		return "", "", fmt.Errorf("private Agent state is not accessible")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(absRoot, clean)
	cur := target
	for {
		_, statErr := os.Lstat(cur)
		if statErr == nil {
			break
		}
		if !os.IsNotExist(statErr) {
			return "", "", statErr
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	resolved, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", "", err
	}
	resolved = filepath.Join(resolved, strings.TrimPrefix(target, cur))
	relCheck, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) || filepath.IsAbs(relCheck) {
		return "", "", fmt.Errorf("path escapes workspace")
	}
	if reservedWorkspacePath(relCheck) {
		return "", "", fmt.Errorf("private Agent state is not accessible")
	}
	return target, filepath.ToSlash(clean), nil
}

func reservedWorkspacePath(rel string) bool {
	clean := filepath.Clean(filepath.FromSlash(rel))
	first := strings.Split(clean, string(os.PathSeparator))[0]
	return strings.EqualFold(first, ".dagents")
}

func (s *Server) handleWorkspaceList(w http.ResponseWriter, r *http.Request) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	_, rel, err := resolveWorkspacePath(root, r.URL.Query().Get("path"), true)
	if err != nil {
		writeAPIError(w, 400, "workspace_path_invalid", err.Error(), nil)
		return
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		writeAPIError(w, 400, "workspace_unavailable", err.Error(), nil)
		return
	}
	defer rootFS.Close()
	info, err := rootFS.Stat(rel)
	if err != nil {
		writeAPIError(w, 404, "workspace_path_not_found", "directory not found", nil)
		return
	}
	if !info.IsDir() {
		writeAPIError(w, 400, "workspace_not_directory", "path is not a directory", nil)
		return
	}
	dir, err := rootFS.Open(rel)
	if err != nil {
		writeAPIError(w, 400, "workspace_read_failed", err.Error(), nil)
		return
	}
	items, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		writeAPIError(w, 400, "workspace_read_failed", err.Error(), nil)
		return
	}
	entries := make([]workspaceEntry, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(item.Name(), ".dagents") || strings.HasPrefix(item.Name(), ".dagents-write-") {
			continue
		}
		cr := filepath.ToSlash(filepath.Join(rel, item.Name()))
		st, e := rootFS.Lstat(cr)
		if e != nil || st.Mode()&os.ModeSymlink != 0 || (!st.IsDir() && !st.Mode().IsRegular()) {
			continue
		}
		ent := workspaceEntry{Name: item.Name(), Path: cr, IsDir: st.IsDir(), Size: st.Size(), ModifiedAt: st.ModTime().UTC().Format(time.RFC3339Nano)}
		if st.Mode().IsRegular() {
			ent.FileRef = filepath.Join(root, filepath.FromSlash(cr))
		}
		entries = append(entries, ent)
	}
	writeJSON(w, 200, map[string]any{"path": rel, "entries": entries})
}
