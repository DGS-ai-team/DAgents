package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
)

type handbookFile struct {
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size,omitempty"`
	ModifiedAt time.Time `json:"modified_at,omitempty"`
}

func (s *Server) handbookDir(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return "", "", false
	}
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil || rec == nil || rec.Archived {
		s.writeAgentNotFound(w, id)
		return "", "", false
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		writeAPIError(w, 503, "handbook_unavailable", "手册目录不可用", nil)
		return "", "", false
	}
	root, err := agentruntime.HandbookRoot(s.runtimeDir(), id, snap.Workspace, snap.Handbook)
	if err != nil {
		writeAPIError(w, 503, "handbook_unavailable", "手册目录不可用", nil)
		return "", "", false
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		writeAPIError(w, 503, "handbook_unavailable", "手册目录不可用", nil)
		return "", "", false
	}
	return id, root, true
}

func safeHandbookPath(root, rel string) (string, error) {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." {
		return root, nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".history" {
			return "", errors.New("reserved handbook path")
		}
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid handbook path")
	}
	p := filepath.Join(root, rel)
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		i, err := os.Lstat(cur)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil && i.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink handbook path")
		}
	}
	return p, nil
}

func (s *Server) handleGetAgentHandbook(w http.ResponseWriter, r *http.Request) {
	id, root, ok := s.handbookDir(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("path")
	p, pathErr := safeHandbookPath(root, rel)
	if pathErr != nil {
		writeAPIError(w, 400, "invalid_path", "手册路径无效", nil)
		return
	}
	info, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		writeAPIError(w, 404, "handbook_not_found", "手册路径不存在", nil)
		return
	}
	if err != nil {
		writeAPIError(w, 500, "handbook_read_failed", "手册读取失败", nil)
		return
	}
	out := map[string]any{"agent_id": id, "path": rel, "is_dir": info.IsDir(), "modified_at": info.ModTime()}
	if rel == "" || rel == "." {
		out["directory"] = root
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			writeAPIError(w, 400, "handbook_file_invalid", "手册文件不可读取", nil)
			return
		}
		f, e := os.Open(p)
		if e != nil {
			writeAPIError(w, 500, "handbook_read_failed", "手册读取失败", nil)
			return
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
		if e != nil {
			writeAPIError(w, 500, "handbook_read_failed", "手册读取失败", nil)
			return
		}
		if int64(len(b)) > 1024*1024 {
			writeAPIError(w, 400, "handbook_file_invalid", "手册文件不可读取", nil)
			return
		}
		out["content"] = string(b)
		out["digest"] = handbookfs.Digest(b)
		out["size"] = info.Size()
		writeJSON(w, 200, out)
		return
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		writeAPIError(w, 500, "handbook_read_failed", "手册读取失败", nil)
		return
	}
	items := make([]handbookFile, 0, len(entries))
	for _, e := range entries {
		i, e2 := e.Info()
		if e2 != nil {
			continue
		}
		child, _ := filepath.Rel(root, filepath.Join(p, e.Name()))
		items = append(items, handbookFile{Path: filepath.ToSlash(child), IsDir: e.IsDir(), Size: i.Size(), ModifiedAt: i.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	out["items"] = items
	writeJSON(w, 200, out)
}

func (s *Server) handleGetAgentHandbookHistory(w http.ResponseWriter, r *http.Request) {
	id, root, ok := s.handbookDir(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("path")
	p, err := safeHandbookPath(root, rel)
	if err != nil || rel == "" {
		writeAPIError(w, 400, "invalid_path", "手册路径无效", nil)
		return
	}
	h, err := handbookfs.New(root)
	if err != nil {
		writeAPIError(w, 503, "handbook_unavailable", "手册历史不可用", nil)
		return
	}
	entries, err := h.History(r.Context(), p)
	if err != nil {
		writeAPIError(w, 500, "handbook_history_failed", "手册历史读取失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"agent_id": id, "path": rel, "history": entries})
}

type handbookRestoreBody struct {
	Path           string `json:"path"`
	Revision       int64  `json:"revision"`
	ExpectedDigest string `json:"expected_digest"`
}

func (s *Server) handleRestoreAgentHandbook(w http.ResponseWriter, r *http.Request) {
	id, root, ok := s.handbookDir(w, r)
	if !ok {
		return
	}
	var in handbookRestoreBody
	if err := decodeJSON(r, &in); err != nil {
		writeAPIError(w, 400, "invalid_json", "恢复请求无效", nil)
		return
	}
	p, err := safeHandbookPath(root, in.Path)
	if err != nil || strings.TrimSpace(in.Path) == "" {
		writeAPIError(w, 400, "invalid_path", "手册路径无效", nil)
		return
	}
	if strings.TrimSpace(in.ExpectedDigest) == "" || in.Revision <= 0 {
		writeAPIError(w, 400, "invalid_restore_request", "恢复请求缺少有效版本校验", nil)
		return
	}
	h, err := handbookfs.New(root)
	if err != nil {
		writeAPIError(w, 503, "handbook_unavailable", "手册历史不可用", nil)
		return
	}
	entry, err := h.Restore(r.Context(), p, in.ExpectedDigest, in.Revision)
	if errors.Is(err, handbookfs.ErrConflict) {
		writeAPIError(w, 409, "revision_conflict", "文件已被修改，请刷新后重试", nil)
		return
	}
	if err != nil {
		writeAPIError(w, 400, "handbook_restore_failed", "手册恢复失败", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"agent_id": id, "entry": entry})
}
