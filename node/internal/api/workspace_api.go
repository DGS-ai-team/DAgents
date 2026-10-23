package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

func fileRevision(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// ifNoneMatchMatches implements If-None-Match's weak entity-tag comparison.
// Commas inside a quoted opaque tag are not list separators; the wildcard is
// valid only as the complete field value.
func ifNoneMatchMatches(values []string, currentETag string) bool {
	if len(values) == 0 {
		return false
	}
	raw := strings.Join(values, ",")
	if strings.Trim(raw, " \t") == "*" {
		return true
	}
	currentOpaque, ok := entityTagOpaque(currentETag)
	if !ok {
		return false
	}

	matched := false
	for i := 0; i < len(raw); {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == ',') {
			i++
		}
		if i == len(raw) {
			break
		}
		if strings.HasPrefix(raw[i:], "W/") {
			i += 2
		}
		if i == len(raw) || raw[i] != '"' {
			return false
		}
		i++
		start := i
		for i < len(raw) && raw[i] != '"' {
			c := raw[i]
			if c < 0x21 || c == 0x7f {
				return false
			}
			i++
		}
		if i == len(raw) {
			return false
		}
		opaque := raw[start:i]
		i++
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
			i++
		}
		if i < len(raw) && raw[i] != ',' {
			return false
		}
		if opaque == currentOpaque {
			// If-None-Match always uses weak comparison, so the W/ prefix
			// does not participate in matching.
			matched = true
		}
		if i < len(raw) {
			i++
		}
	}
	return matched
}

func entityTagOpaque(tag string) (string, bool) {
	tag = strings.Trim(tag, " \t")
	tag = strings.TrimPrefix(tag, "W/")
	if len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return "", false
	}
	for i := 1; i < len(tag)-1; i++ {
		if tag[i] < 0x21 || tag[i] == '"' || tag[i] == 0x7f {
			return "", false
		}
	}
	return tag[1 : len(tag)-1], true
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
		if strings.EqualFold(item.Name(), ".dagents") {
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

func (s *Server) workspaceFile(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return "", "", false
	}
	_, rel, err := resolveWorkspacePath(root, r.URL.Query().Get("path"), false)
	if err != nil {
		writeAPIError(w, 400, "workspace_path_invalid", err.Error(), nil)
		return "", "", false
	}
	return root, rel, true
}
func (s *Server) handleWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	root, rel, ok := s.workspaceFile(w, r)
	if !ok {
		return
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		writeAPIError(w, 400, "workspace_unavailable", err.Error(), nil)
		return
	}
	defer rootFS.Close()
	info, err := rootFS.Lstat(rel)
	if err != nil {
		writeAPIError(w, 404, "workspace_file_not_found", "file not found", nil)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		writeAPIError(w, 400, "workspace_not_file", "path is not a regular file", nil)
		return
	}
	f, err := rootFS.Open(rel)
	if err != nil {
		writeAPIError(w, 404, "workspace_file_not_found", "file not found", nil)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		writeAPIError(w, 400, "workspace_not_file", "path is not a regular file", nil)
		return
	}
	if st.Size() > workspaceFileLimit {
		writeAPIError(w, 413, "workspace_file_too_large", "file exceeds 16 MiB", nil)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, workspaceFileLimit+1))
	if err != nil || len(data) > workspaceFileLimit {
		writeAPIError(w, 413, "workspace_file_too_large", "file exceeds 16 MiB", nil)
		return
	}
	etag := `"` + fileRevision(data) + `"`
	w.Header().Set("ETag", etag)
	if ifNoneMatchMatches(r.Header.Values("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func (s *Server) handleWorkspacePut(w http.ResponseWriter, r *http.Request) {
	root, rel, ok := s.workspaceFile(w, r)
	if !ok {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, workspaceFileLimit+1))
	if err != nil || len(data) > workspaceFileLimit {
		writeAPIError(w, 413, "workspace_file_too_large", "file exceeds 16 MiB", nil)
		return
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		writeAPIError(w, 400, "workspace_unavailable", err.Error(), nil)
		return
	}
	defer rootFS.Close()
	lease, err := s.workspaceCoord.TryAcquire(r.Context(), filepath.Join(root, filepath.FromSlash(rel)), "workspace-api:"+r.PathValue("agent_id"))
	if err != nil {
		writeAPIError(w, 409, "workspace_busy", err.Error(), nil)
		return
	}
	defer lease.Release()
	oldInfo, statErr := rootFS.Lstat(rel)
	exists := statErr == nil
	if exists && oldInfo.Mode()&os.ModeSymlink != 0 {
		writeAPIError(w, 400, "workspace_not_file", "symbolic links are not writable", nil)
		return
	}
	if exists && oldInfo.Size() > workspaceFileLimit {
		writeAPIError(w, 413, "workspace_file_too_large", "existing file exceeds 16 MiB", nil)
		return
	}
	if exists && !oldInfo.Mode().IsRegular() {
		writeAPIError(w, 400, "workspace_not_file", "path is not a regular file", nil)
		return
	}
	var old []byte
	var readErr error
	if exists {
		oldFile, openErr := rootFS.Open(rel)
		if openErr != nil {
			readErr = openErr
		} else {
			old, readErr = io.ReadAll(io.LimitReader(oldFile, workspaceFileLimit+1))
			_ = oldFile.Close()
		}
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		writeAPIError(w, 400, "workspace_read_failed", statErr.Error(), nil)
		return
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		writeAPIError(w, 400, "workspace_read_failed", readErr.Error(), nil)
		return
	}
	if tag := strings.Trim(r.Header.Get("If-Match"), `"`); tag != "" {
		if !exists || tag != fileRevision(old) {
			writeAPIError(w, 412, "workspace_revision_mismatch", "file revision changed", nil)
			return
		}
	}
	if r.Header.Get("If-None-Match") == "*" && exists {
		writeAPIError(w, 412, "workspace_file_exists", "file already exists", nil)
		return
	}
	if r.Header.Get("If-Match") == "" && r.Header.Get("If-None-Match") != "*" {
		writeAPIError(w, 428, "workspace_precondition_required", "use If-Match or If-None-Match: *", nil)
		return
	}
	if err = rootFS.MkdirAll(filepath.Dir(filepath.FromSlash(rel)), 0o755); err != nil {
		writeAPIError(w, 400, "workspace_write_failed", err.Error(), nil)
		return
	}
	mode := os.FileMode(0o644)
	if exists {
		mode = oldInfo.Mode().Perm()
	}
	tmpName := ".dagents-write-" + fileRevision(data)[:20] + fmt.Sprintf("-%d", time.Now().UnixNano())
	tmpRel := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(rel)), tmpName))
	tmp, err := rootFS.OpenFile(tmpRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		writeAPIError(w, 400, "workspace_write_failed", err.Error(), nil)
		return
	}
	defer rootFS.Remove(tmpRel)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(mode.Perm())
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		if exists {
			err = rootFS.Rename(tmpRel, rel)
		} else {
			err = rootFS.Link(tmpRel, rel)
			if err == nil {
				err = rootFS.Remove(tmpRel)
			} else if errors.Is(err, os.ErrExist) {
				writeAPIError(w, 412, "workspace_file_exists", "file already exists", nil)
				return
			}
		}
	}
	if err != nil {
		writeAPIError(w, 400, "workspace_write_failed", err.Error(), nil)
		return
	}
	rev := fileRevision(data)
	w.Header().Set("ETag", `"`+rev+`"`)
	writeJSON(w, 200, map[string]any{"path": rel, "name": filepath.Base(rel), "size": len(data), "revision": rev, "file_ref": filepath.Join(root, filepath.FromSlash(rel))})
}
func (s *Server) handleWorkspaceUpload(w http.ResponseWriter, r *http.Request) {
	r.Header.Set("If-None-Match", "*")
	s.handleWorkspacePut(w, r)
}
