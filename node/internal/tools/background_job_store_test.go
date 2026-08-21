package tools

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackgroundJobStoreMigratesRecoveryColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background_jobs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE background_jobs (
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
);
INSERT INTO background_jobs(job_id, session_id, tool_name, status, started_at)
VALUES ('job-legacy-running', 'agent-legacy', 'bash_run', 'running', 100);`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenBackgroundJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reg, err := newBackgroundJobRegistryWithStore(store, "agent-legacy")
	if err != nil {
		t.Fatal(err)
	}
	job, ok := reg.get("job-legacy-running")
	if !ok || job.status != jobStatusUnknown || job.recoveryReason != "node_restart_orphan" {
		t.Fatalf("legacy recovery job=%+v ok=%v", job, ok)
	}
}

func TestBackgroundJobStorePersistsTerminalJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background_jobs.db")
	store, err := OpenBackgroundJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	job := &backgroundJob{
		id:         "job-persisted",
		sessionID:  "agent-a",
		toolName:   "bash_run",
		toolCallID: "call-persisted",
		status:     jobStatusSucceeded,
		result:     "done",
		startedAt:  100,
		finishedAt: 200,
		done:       make(chan struct{}),
	}
	if err := store.save(job); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenBackgroundJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reg, err := newBackgroundJobRegistryWithStore(reopened, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := reg.get("job-persisted")
	if !ok {
		t.Fatal("persisted job was not restored")
	}
	if restored.status != jobStatusSucceeded || restored.result != "done" {
		t.Fatalf("restored job=%+v", restored)
	}
}

func TestBackgroundJobStoreMarksRunningAsUnknownAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background_jobs.db")
	store, err := OpenBackgroundJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	job := &backgroundJob{
		id:        "job-interrupted",
		sessionID: "agent-a",
		toolName:  "bash_run",
		status:    jobStatusRunning,
		startedAt: 100,
		done:      make(chan struct{}),
		remoteRecovery: &RemoteProcessRecovery{
			TargetID: "prod", JobToken: "job-interrupted", PIDFile: remoteJobPIDFile("job-interrupted"),
		},
	}
	if err := store.save(job); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenBackgroundJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reg, err := newBackgroundJobRegistryWithStore(reopened, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := reg.get("job-interrupted")
	if !ok {
		t.Fatal("interrupted job was not restored")
	}
	if restored.status != jobStatusUnknown {
		t.Fatalf("status=%q want unknown", restored.status)
	}
	if restored.recoveryReason != "node_restart_orphan" || restored.recoveredAt == 0 {
		t.Fatalf("recovery metadata=%+v", restored)
	}
	if restored.remoteRecovery == nil || restored.remoteRecovery.TargetID != "prod" {
		t.Fatalf("remote recovery=%+v", restored.remoteRecovery)
	}
	status := restored.statusText()
	if !strings.Contains(status, "status=unknown") || !strings.Contains(status, "recovery_reason=node_restart_orphan") {
		t.Fatalf("status=%q", status)
	}
	if !strings.Contains(restored.result, "Node restarted") {
		t.Fatalf("result=%q does not explain restart", restored.result)
	}
}

func TestBackgroundJobStoreRestoresOnlySessionJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "background_jobs.db")
	store, err := OpenBackgroundJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, job := range []*backgroundJob{
		{id: "job-a", sessionID: "agent-a", status: jobStatusSucceeded, done: make(chan struct{})},
		{id: "job-b", sessionID: "agent-b", status: jobStatusSucceeded, done: make(chan struct{})},
	} {
		if err := store.save(job); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := newBackgroundJobRegistryWithStore(store, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.get("job-a"); !ok {
		t.Fatal("agent-a job missing")
	}
	if _, ok := reg.get("job-b"); ok {
		t.Fatal("agent-b job leaked into agent-a registry")
	}
}
