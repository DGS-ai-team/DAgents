package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

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
