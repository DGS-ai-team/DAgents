package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
)

const (
	workspaceImportLimit          = int64(1 << 30)
	workspaceStageQuota           = int64(2 << 30)
	workspaceStageCount           = 32
	workspaceStageTTL             = 15 * time.Minute
	workspaceCompletedResultLimit = 256
)

var workspaceLockWait = 10 * time.Second

type workspaceWrite struct {
	id, agent, root, rel, mode, kind, expected string
	size                                       int64
	expires                                    time.Time
	identity                                   os.FileInfo
	stageIdentity                              string
	result                                     map[string]any
	completedAt                                time.Time
	committing                                 bool
}
type workspaceWriteManager struct {
	mu      sync.Mutex
	root    string
	initErr error
	ops     map[string]*workspaceWrite
	used    int64
}

func newWorkspaceWriteManager(root string) *workspaceWriteManager {
	absRoot, err := filepath.Abs(root)
	m := &workspaceWriteManager{root: filepath.Clean(absRoot), ops: make(map[string]*workspaceWrite)}
	if err != nil {
		m.initErr = err
		return m
	}
	if err = os.MkdirAll(m.root, 0700); err != nil {
		m.initErr = err
		return m
	}
	if err = os.Chmod(m.root, 0700); err != nil {
		m.initErr = err
		return m
	}
	canonical, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		m.initErr = err
		return m
	}
	m.root, err = filepath.Abs(canonical)
	if err != nil {
		m.initErr = err
		return m
	}
	// A process restart makes every in-flight operation unknown. Reclaim only
	// the product's 192-bit hex operation filenames; leave unrelated files alone.
	if entries, err := os.ReadDir(m.root); err != nil {
		m.initErr = err
		return m
	} else {
		for _, entry := range entries {
			name := entry.Name()
			if len(name) != 48 {
				continue
			}
			if _, e := hex.DecodeString(name); e != nil {
				continue
			}
			info, e := entry.Info()
			if e == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				if e = os.Remove(filepath.Join(m.root, name)); e != nil {
					m.initErr = e
					return m
				}
			}
		}
	}
	return m
}
func (m *workspaceWriteManager) cleanup(now time.Time) {
	for id, op := range m.ops {
		if now.After(op.expires) && !op.committing {
			_ = os.Remove(filepath.Join(m.root, id))
			if op.result == nil {
				m.used -= op.size
			}
			delete(m.ops, id)
		}
	}
}
func (m *workspaceWriteManager) activeCount() int {
	n := 0
	for _, op := range m.ops {
		if op.result == nil {
			n++
		}
	}
	return n
}
func (m *workspaceWriteManager) evictCompletedResults() {
	for {
		count := 0
		var oldestID string
		var oldest time.Time
		for id, op := range m.ops {
			if op.result == nil {
				continue
			}
			count++
			if oldestID == "" || op.completedAt.Before(oldest) {
				oldestID = id
				oldest = op.completedAt
			}
		}
		if count <= workspaceCompletedResultLimit {
			return
		}
		delete(m.ops, oldestID)
	}
}
func (m *workspaceWriteManager) close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.ops {
		_ = os.Remove(filepath.Join(m.root, id))
	}
	m.ops = make(map[string]*workspaceWrite)
	m.used = 0
}
func (s *Server) nativeWorkspaceWrite(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Dagents-Native-Workspace") != "1" {
		writeAPIError(w, http.StatusForbidden, "native_workspace_marker_required", "missing X-Dagents-Native-Workspace invocation marker", nil)
		return false
	}
	if s.workspaceWrites == nil || s.workspaceWrites.initErr != nil {
		detail := "staging unavailable"
		if s.workspaceWrites != nil {
			detail = s.workspaceWrites.initErr.Error()
		}
		writeAPIError(w, 503, "workspace_staging_unavailable", detail, nil)
		return false
	}
	return true
}
func (s *Server) handleWorkspaceWritePrepare(w http.ResponseWriter, r *http.Request) {
	if !s.nativeWorkspaceWrite(w, r) {
		return
	}
	var req struct {
		Path     string `json:"path"`
		Mode     string `json:"mode"`
		Kind     string `json:"kind"`
		Expected string `json:"expected_revision"`
		Size     int64  `json:"size"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	limit := int64(workspaceFileLimit)
	if req.Kind == "import" {
		limit = workspaceImportLimit
	} else if req.Kind != "text" {
		writeAPIError(w, 400, "workspace_write_invalid", "kind must be text or import", nil)
		return
	}
	if req.Size < 0 || req.Size > limit {
		writeAPIError(w, 413, "workspace_file_too_large", "declared file size exceeds limit", nil)
		return
	}
	if req.Mode != "replace" && req.Mode != "create" {
		writeAPIError(w, 400, "workspace_write_invalid", "mode must be replace or create", nil)
		return
	}
	if req.Mode == "replace" {
		req.Expected = strings.Trim(req.Expected, `"`)
		if len(req.Expected) != 64 {
			writeAPIError(w, 400, "workspace_precondition_required", "replace requires a SHA-256 expected_revision", nil)
			return
		}
		if _, e := hex.DecodeString(req.Expected); e != nil {
			writeAPIError(w, 400, "workspace_precondition_required", "expected_revision must be a SHA-256 digest", nil)
			return
		}
	}
	root, ok := s.workspaceRoot(w, r)
	if !ok {
		return
	}
	root, err := canonicalWorkspaceRoot(root)
	if err != nil {
		writeAPIError(w, 400, "workspace_unavailable", err.Error(), nil)
		return
	}
	_, rel, err := resolveWorkspacePath(root, req.Path, false)
	if err != nil {
		writeAPIError(w, 400, "workspace_path_invalid", err.Error(), nil)
		return
	}
	m := s.workspaceWrites
	if m == nil {
		writeAPIError(w, 503, "workspace_write_unavailable", "write staging unavailable", nil)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.cleanup(now)
	if m.activeCount() >= workspaceStageCount || m.used+req.Size > workspaceStageQuota {
		writeAPIError(w, 429, "workspace_staging_full", "workspace staging capacity exhausted", nil)
		return
	}
	if err = os.MkdirAll(m.root, 0700); err != nil {
		writeAPIError(w, 500, "workspace_staging_failed", err.Error(), nil)
		return
	}
	_ = os.Chmod(m.root, 0700)
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		writeAPIError(w, 500, "workspace_staging_failed", err.Error(), nil)
		return
	}
	id := hex.EncodeToString(b)
	stage := filepath.Join(m.root, id)
	f, err := os.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		writeAPIError(w, 500, "workspace_staging_failed", err.Error(), nil)
		return
	}
	info, statErr := f.Stat()
	identity, idErr := workspaceFileIdentity(f)
	_ = f.Close()
	if statErr != nil || idErr != nil {
		_ = os.Remove(stage)
		writeAPIError(w, 500, "workspace_staging_failed", "could not identify staging file", nil)
		return
	}
	op := &workspaceWrite{id: id, agent: r.PathValue("agent_id"), root: root, rel: rel, mode: req.Mode, kind: req.Kind, expected: req.Expected, size: req.Size, expires: now.Add(workspaceStageTTL), identity: info, stageIdentity: identity}
	m.ops[id] = op
	m.used += req.Size
	writeJSON(w, 200, map[string]any{"operation_id": id, "staging_path": stage, "staging_root": m.root, "staging_identity": identity, "expires_at": op.expires.UTC().Format(time.RFC3339Nano)})
}
func (s *Server) handleWorkspaceWriteCancel(w http.ResponseWriter, r *http.Request) {
	if !s.nativeWorkspaceWrite(w, r) {
		return
	}
	m := s.workspaceWrites
	m.mu.Lock()
	defer m.mu.Unlock()
	id := r.PathValue("operation_id")
	op := m.ops[id]
	if op == nil || op.agent != r.PathValue("agent_id") {
		writeAPIError(w, 404, "workspace_operation_not_found", "workspace write operation not found", nil)
		return
	}
	if op.committing {
		writeAPIError(w, 409, "workspace_operation_busy", "workspace write commit is in progress", nil)
		return
	}
	if op.result != nil {
		writeJSON(w, 200, map[string]string{"status": "committed"})
		return
	}
	_ = os.Remove(filepath.Join(m.root, id))
	m.used -= op.size
	delete(m.ops, id)
	writeJSON(w, 200, map[string]string{"status": "cancelled"})
}
func (s *Server) handleWorkspaceWriteCommit(w http.ResponseWriter, r *http.Request) {
	if !s.nativeWorkspaceWrite(w, r) {
		return
	}
	var req struct {
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error(), nil)
		return
	}
	m := s.workspaceWrites
	id := r.PathValue("operation_id")
	m.mu.Lock()
	op := m.ops[id]
	if op == nil || op.agent != r.PathValue("agent_id") {
		m.mu.Unlock()
		writeAPIError(w, 404, "workspace_operation_not_found", "workspace write operation not found", nil)
		return
	}
	if op.result != nil {
		result := op.result
		m.mu.Unlock()
		writeJSON(w, 200, result)
		return
	}
	if op.committing {
		m.mu.Unlock()
		writeAPIError(w, 409, "workspace_operation_busy", "workspace write commit is in progress", nil)
		return
	}
	if time.Now().After(op.expires) {
		m.cleanup(time.Now())
		m.mu.Unlock()
		writeAPIError(w, 410, "workspace_operation_expired", "workspace write operation expired", nil)
		return
	}
	op.committing = true
	m.mu.Unlock()
	committed := false
	var safeParent *workspaceWriteParent
	defer func() {
		if safeParent != nil {
			if !committed {
				safeParent.cleanupCreated()
			}
			safeParent.close()
		}
		if !committed {
			m.mu.Lock()
			if m.ops[id] == op {
				op.committing = false
			}
			m.mu.Unlock()
		}
	}()
	currentRoot, rootOK := s.currentWorkspaceRoot(r.Context(), op.agent)
	if !rootOK || filepath.Clean(currentRoot) != filepath.Clean(op.root) {
		writeAPIError(w, 409, "workspace_changed", "Agent workspace changed; prepare again", nil)
		return
	}
	stage := filepath.Join(m.root, id)
	st, err := os.Lstat(stage)
	if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || !os.SameFile(op.identity, st) {
		writeAPIError(w, 400, "workspace_staging_invalid", "staging file identity changed", nil)
		return
	}
	if req.Size != op.size || st.Size() != op.size {
		writeAPIError(w, 400, "workspace_size_mismatch", "staged file size mismatch", nil)
		return
	}
	f, err := openWorkspaceStageForRead(m.root, id)
	if err != nil {
		writeAPIError(w, 400, "workspace_staging_invalid", err.Error(), nil)
		return
	}
	opened, statErr := f.Stat()
	actualIdentity, identityErr := workspaceFileIdentity(f)
	if statErr != nil || identityErr != nil || actualIdentity != op.stageIdentity || !os.SameFile(op.identity, opened) {
		_ = f.Close()
		writeAPIError(w, 400, "workspace_staging_invalid", "staging file identity changed", nil)
		return
	}
	if len(strings.TrimSpace(req.SHA256)) != 64 {
		_ = f.Close()
		writeAPIError(w, 400, "workspace_digest_mismatch", "sha256 must be a 64 character digest", nil)
		return
	}
	if _, err = hex.DecodeString(req.SHA256); err != nil {
		_ = f.Close()
		writeAPIError(w, 400, "workspace_digest_mismatch", "invalid sha256", nil)
		return
	}
	safeParent, err = openWorkspaceWriteParent(op.root, op.rel, true)
	if err != nil {
		_ = f.Close()
		writeAPIError(w, 400, "workspace_path_invalid", err.Error(), nil)
		return
	}
	tmpBase := ".dagents-write-" + id
	tmp, err := safeParent.dir.OpenFile(tmpBase, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		_ = f.Close()
		writeAPIError(w, 400, "workspace_write_failed", err.Error(), nil)
		return
	}
	defer safeParent.dir.Remove(tmpBase)
	h := sha256.New()
	n, copyErr := copyWithContext(r.Context(), io.MultiWriter(tmp, h), io.LimitReader(f, op.size+1))
	_ = f.Close()
	if copyErr == nil {
		copyErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil || n != op.size || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(strings.TrimSpace(req.SHA256)) {
		writeAPIError(w, 400, "workspace_digest_mismatch", "staged file size or digest mismatch", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), workspaceLockWait)
	defer cancel()
	lease, err := s.workspaceCoord.TryAcquire(ctx, filepath.Join(op.root, filepath.FromSlash(op.rel)), "workspace-write:"+op.agent)
	if err != nil {
		writeAPIError(w, 409, "workspace_busy", "workspace is busy", nil)
		return
	}
	defer lease.Release()
	currentRoot, rootOK = s.currentWorkspaceRoot(r.Context(), op.agent)
	if !rootOK || filepath.Clean(currentRoot) != filepath.Clean(op.root) {
		writeAPIError(w, 409, "workspace_changed", "Agent workspace changed; prepare again", nil)
		return
	}
	if err = verifyWorkspaceWriteParent(op.root, op.rel, safeParent); err != nil {
		writeAPIError(w, 400, "workspace_path_invalid", err.Error(), nil)
		return
	}
	target := filepath.Join(op.root, filepath.FromSlash(op.rel))
	info, statErr := safeParent.dir.Lstat(safeParent.leaf)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		writeAPIError(w, 400, "workspace_read_failed", statErr.Error(), nil)
		return
	}
	if exists && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		writeAPIError(w, 400, "workspace_not_file", "target is not a regular file", nil)
		return
	}
	if op.mode == "create" && exists {
		writeAPIError(w, 412, "workspace_file_exists", "file already exists", nil)
		return
	}
	if op.mode == "replace" {
		if !exists {
			writeAPIError(w, 412, "workspace_revision_mismatch", "file revision changed", nil)
			return
		}
		old, e := openWorkspaceTargetForRead(safeParent.dir, safeParent.leaf)
		if e != nil {
			writeAPIError(w, 400, "workspace_read_failed", e.Error(), nil)
			return
		}
		oldInfo, e := old.Stat()
		if e == nil && (!oldInfo.Mode().IsRegular() || !os.SameFile(info, oldInfo)) {
			e = fmt.Errorf("target file changed")
		}
		oldHash := sha256.New()
		var oldSize int64
		if e == nil {
			oldSize, e = copyWithContext(r.Context(), oldHash, io.LimitReader(old, workspaceImportLimit+1))
		}
		_ = old.Close()
		if e != nil || oldSize > workspaceImportLimit || hex.EncodeToString(oldHash.Sum(nil)) != strings.ToLower(op.expected) {
			writeAPIError(w, 412, "workspace_revision_mismatch", "file revision changed", nil)
			return
		}
	}
	if exists {
		if err = safeParent.dir.Chmod(tmpBase, info.Mode().Perm()); err != nil {
			writeAPIError(w, 400, "workspace_write_failed", err.Error(), nil)
			return
		}
	}
	if err = verifyWorkspaceWriteParent(op.root, op.rel, safeParent); err != nil {
		writeAPIError(w, 400, "workspace_path_invalid", err.Error(), nil)
		return
	}
	if op.mode == "create" {
		if err = safeParent.dir.Link(tmpBase, safeParent.leaf); err != nil {
			if errors.Is(err, os.ErrExist) {
				writeAPIError(w, 412, "workspace_file_exists", "file already exists", nil)
			} else {
				writeAPIError(w, 400, "workspace_write_failed", err.Error(), nil)
			}
			return
		}
		_ = safeParent.dir.Remove(tmpBase)
	} else {
		if err = safeParent.dir.Rename(tmpBase, safeParent.leaf); err != nil {
			writeAPIError(w, 400, "workspace_write_failed", err.Error(), nil)
			return
		}
	}
	result := map[string]any{"path": op.rel, "name": filepath.Base(op.rel), "size": op.size, "revision": strings.ToLower(strings.TrimSpace(req.SHA256)), "file_ref": target}
	m.mu.Lock()
	op.result = result
	op.completedAt = time.Now()
	op.committing = false
	_ = os.Remove(stage)
	m.used -= op.size
	op.size = 0
	m.evictCompletedResults()
	m.mu.Unlock()
	committed = true
	writeJSON(w, 200, result)
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 128*1024)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			n += int64(nw)
			if ew != nil {
				return n, ew
			}
			if nw != nr {
				return n, io.ErrShortWrite
			}
		}
		if er == io.EOF {
			return n, nil
		}
		if er != nil {
			return n, er
		}
	}
}

type createdWorkspaceDir struct {
	parent *os.Root
	name   string
}
type workspaceWriteParent struct {
	dir     *os.Root
	leaf    string
	roots   []*os.Root
	created []createdWorkspaceDir
}

func (p *workspaceWriteParent) cleanupCreated() {
	for i := len(p.created) - 1; i >= 0; i-- {
		_ = p.created[i].parent.Remove(p.created[i].name)
	}
}
func (p *workspaceWriteParent) close() {
	for i := len(p.roots) - 1; i >= 0; i-- {
		_ = p.roots[i].Close()
	}
}

func openWorkspaceWriteParent(root, rel string, create bool) (*workspaceWriteParent, error) {
	base, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	p := &workspaceWriteParent{roots: []*os.Root{base}, leaf: filepath.Base(filepath.FromSlash(rel))}
	cur := base
	parentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if parentRel == "." {
		p.dir = base
		return p, nil
	}
	curRel := ""
	for _, part := range strings.Split(parentRel, "/") {
		if part == "" || part == "." {
			continue
		}
		if workspacePathComponentInvalid(part) {
			p.cleanupCreated()
			p.close()
			return nil, fmt.Errorf("workspace path contains an unsafe Windows component")
		}
		curRel = filepath.ToSlash(filepath.Join(filepath.FromSlash(curRel), part))
		info, e := cur.Lstat(part)
		if os.IsNotExist(e) && create {
			e = cur.Mkdir(part, 0755)
			if e == nil {
				p.created = append(p.created, createdWorkspaceDir{parent: cur, name: part})
				info, e = cur.Lstat(part)
			} else if errors.Is(e, os.ErrExist) {
				info, e = cur.Lstat(part)
			}
		}
		if e != nil {
			p.cleanupCreated()
			p.close()
			return nil, e
		}
		if !info.IsDir() || workspacePathIsReparse(filepath.Join(root, filepath.FromSlash(curRel)), info) {
			p.cleanupCreated()
			p.close()
			return nil, fmt.Errorf("workspace parent is not a safe directory")
		}
		child, e := cur.OpenRoot(part)
		if e != nil {
			p.cleanupCreated()
			p.close()
			return nil, e
		}
		p.roots = append(p.roots, child)
		opened, e := child.Stat(".")
		if e != nil || !os.SameFile(info, opened) {
			p.cleanupCreated()
			p.close()
			if e != nil {
				return nil, e
			}
			return nil, fmt.Errorf("workspace parent changed during validation")
		}
		if len(p.roots) == 2 {
			privateInfo, pe := base.Lstat(".dagents")
			if pe == nil && os.SameFile(privateInfo, opened) {
				p.cleanupCreated()
				p.close()
				return nil, fmt.Errorf("private Agent state is not accessible")
			}
			if pe != nil && !os.IsNotExist(pe) {
				p.cleanupCreated()
				p.close()
				return nil, pe
			}
		}
		cur = child
	}
	p.dir = cur
	return p, nil
}

func verifyWorkspaceWriteParent(root, rel string, held *workspaceWriteParent) error {
	current, err := openWorkspaceWriteParent(root, rel, false)
	if err != nil {
		return err
	}
	defer current.close()
	a, e1 := held.dir.Stat(".")
	b, e2 := current.dir.Stat(".")
	if e1 != nil {
		return e1
	}
	if e2 != nil {
		return e2
	}
	if !os.SameFile(a, b) {
		return fmt.Errorf("workspace parent changed during write")
	}
	return nil
}

func (s *Server) currentWorkspaceRoot(ctx context.Context, agent string) (string, bool) {
	rec, err := s.agents.Get(ctx, agent)
	if err != nil || rec == nil || rec.Archived {
		return "", false
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return "", false
	}
	root, err := agentruntime.EnsureWorkspace(s.cfg.RuntimeDir(), agent, snap.Workspace)
	if err != nil {
		return "", false
	}
	root, err = canonicalWorkspaceRoot(root)
	return root, err == nil
}

func canonicalWorkspaceRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}
