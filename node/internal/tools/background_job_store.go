package tools

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// backgroundJobStore persists task metadata and terminal results. It does not
// attempt to reattach to a process after Node restarts; running jobs are
// restored as unknown instead of being reported as still running.
// BackgroundJobStore stores background command metadata shared by Node
// registries. It is intentionally separate from Registry ownership so
// per-agent registries can share one SQLite connection safely.
type BackgroundJobStore struct {
	db *sql.DB
}

// OpenBackgroundJobStore opens or creates the persistent background job DB.
func OpenBackgroundJobStore(path string) (*BackgroundJobStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("background job db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create background job db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &BackgroundJobStore{db: db}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS background_jobs (
  job_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  result TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL DEFAULT 0,
  finished_at INTEGER NOT NULL DEFAULT 0,
  auto_degraded INTEGER NOT NULL DEFAULT 0,
  bash_cwd TEXT NOT NULL DEFAULT '',
  bash_timeout INTEGER NOT NULL DEFAULT 0,
  bash_shell_type TEXT NOT NULL DEFAULT '',
  bash_output_encoding TEXT NOT NULL DEFAULT '',
  compress_saved_pct INTEGER NOT NULL DEFAULT 0,
  compress_raw_runes INTEGER NOT NULL DEFAULT 0,
  compress_out_runes INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the SQLite connection.
func (s *BackgroundJobStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *BackgroundJobStore) save(job *backgroundJob) error {
	if s == nil || s.db == nil || job == nil {
		return nil
	}
	job.mu.Lock()
	degraded := 0
	if job.autoDegraded {
		degraded = 1
	}
	savedPct, rawRunes, outRunes := 0, 0, 0
	if job.compressStats != nil {
		savedPct = job.compressStats.SavedPct
		rawRunes = job.compressStats.RawRunes
		outRunes = job.compressStats.OutRunes
	}
	args := []any{
		job.id, job.sessionID, job.toolName, job.toolCallID, job.status, job.result,
		job.startedAt, job.finishedAt, degraded, job.bashCwd, job.bashTimeout,
		job.bashShellType, job.bashOutputEncoding, savedPct, rawRunes, outRunes, time.Now().UnixMilli(),
	}
	job.mu.Unlock()
	_, err := s.db.Exec(`
INSERT INTO background_jobs(
  job_id, session_id, tool_name, tool_call_id, status, result,
  started_at, finished_at, auto_degraded, bash_cwd, bash_timeout,
  bash_shell_type, bash_output_encoding, compress_saved_pct,
  compress_raw_runes, compress_out_runes, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(job_id) DO UPDATE SET
  session_id=excluded.session_id,
  tool_name=excluded.tool_name,
  tool_call_id=excluded.tool_call_id,
  status=excluded.status,
  result=excluded.result,
  started_at=excluded.started_at,
  finished_at=excluded.finished_at,
  auto_degraded=excluded.auto_degraded,
  bash_cwd=excluded.bash_cwd,
  bash_timeout=excluded.bash_timeout,
  bash_shell_type=excluded.bash_shell_type,
  bash_output_encoding=excluded.bash_output_encoding,
  compress_saved_pct=excluded.compress_saved_pct,
  compress_raw_runes=excluded.compress_raw_runes,
  compress_out_runes=excluded.compress_out_runes,
  updated_at=excluded.updated_at`, args...)
	return err
}

func (s *BackgroundJobStore) load(sessionID string) ([]*backgroundJob, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	query := `
SELECT job_id, session_id, tool_name, tool_call_id, status, result,
       started_at, finished_at, auto_degraded, bash_cwd, bash_timeout,
       bash_shell_type, bash_output_encoding, compress_saved_pct,
       compress_raw_runes, compress_out_runes
FROM background_jobs`
	args := []any{}
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		query += " WHERE session_id = ?"
		args = append(args, sessionID)
	}
	query += " ORDER BY started_at ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*backgroundJob
	for rows.Next() {
		var job backgroundJob
		var degraded, savedPct, rawRunes, outRunes int
		if err := rows.Scan(
			&job.id, &job.sessionID, &job.toolName, &job.toolCallID, &job.status, &job.result,
			&job.startedAt, &job.finishedAt, &degraded, &job.bashCwd, &job.bashTimeout,
			&job.bashShellType, &job.bashOutputEncoding, &savedPct, &rawRunes, &outRunes,
		); err != nil {
			return nil, err
		}
		job.autoDegraded = degraded != 0
		if savedPct > 0 || rawRunes > 0 || outRunes > 0 {
			job.compressStats = &OutputCompressStats{SavedPct: savedPct, RawRunes: rawRunes, OutRunes: outRunes}
		}
		job.done = make(chan struct{})
		close(job.done)
		if job.status == jobStatusRunning {
			job.status = jobStatusUnknown
			if strings.TrimSpace(job.result) == "" {
				job.result = "Node restarted before this task completed; the process can no longer be tracked."
			}
			if job.finishedAt == 0 {
				job.finishedAt = time.Now().UnixMilli()
			}
		}
		out = append(out, &job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
