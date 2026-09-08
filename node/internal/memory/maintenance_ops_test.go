package memory

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

func TestApplyMaintenanceOperationAdvancesOnlyAfterConsolidation(t *testing.T) {
	root := t.TempDir()
	service, err := OpenLocalService(filepath.Join(root, "agent.db"), filepath.Join(root, "global.db"), ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	op := MaintenanceOperation{OperationID: "op-apply", AgentID: "agent-1", Scope: ScopeAgent, SourceFingerprint: "journal-1", CandidateFingerprint: "cand-1", ExpectedCursor: 0, NextCursor: 1}
	results, err := service.ApplyMaintenanceOperation(context.Background(), op, []Candidate{{Request: RememberRequest{AgentID: "agent-1", Scope: ScopeAgent, Information: "stable preference"}}}, MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 1, SourceFingerprint: "journal-1", CandidateFingerprint: "cand-1"})
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	cursor, err := service.agent.GetMaintenanceCursor(context.Background(), "agent-1")
	if err != nil || cursor.Sequence != 1 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	entries, err := service.List(context.Background(), ScopeAgent, true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	if _, err := service.ApplyMaintenanceOperation(context.Background(), op, []Candidate{{Request: RememberRequest{AgentID: "agent-1", Scope: ScopeAgent, Information: "different batch"}}}, MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 1, SourceFingerprint: "journal-1"}); err == nil {
		t.Fatal("same operation accepted different candidate batch")
	}
}

func TestMaintenanceStaleAndConcurrentSameOperationHaveNoDuplicateEffects(t *testing.T) {
	root := t.TempDir()
	service, err := OpenLocalService(filepath.Join(root, "agent.db"), filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	candidate := []Candidate{{Request: RememberRequest{AgentID: "agent-1", Scope: ScopeAgent, Information: "atomic maintenance"}}}
	op := MaintenanceOperation{OperationID: "same-op", AgentID: "agent-1", Scope: ScopeAgent, SourceFingerprint: "source", ExpectedCursor: 0, NextCursor: 1}
	next := MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 1, SourceFingerprint: "source"}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := service.ApplyMaintenanceOperation(context.Background(), op, candidate, next)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent apply: %v", e)
		}
	}
	entries, err := service.List(context.Background(), ScopeAgent, true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
	stale := MaintenanceOperation{OperationID: "stale", AgentID: "agent-1", Scope: ScopeAgent, SourceFingerprint: "stale", ExpectedCursor: 0, NextCursor: 2}
	if _, err := service.ApplyMaintenanceOperation(context.Background(), stale, []Candidate{{Request: RememberRequest{AgentID: "agent-1", Scope: ScopeAgent, Information: "must rollback"}}}, MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 2, SourceFingerprint: "stale"}); err == nil {
		t.Fatal("stale operation accepted")
	}
	entries, _ = service.List(context.Background(), ScopeAgent, true)
	if len(entries) != 1 {
		t.Fatalf("stale operation left effects: %d", len(entries))
	}
}

func TestMaintenanceSQLiteFailureRollsBackAndRecoversAfterReopen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent.db")
	service, err := OpenLocalService(path, filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate := []Candidate{{Request: RememberRequest{AgentID: "agent-1", Scope: ScopeAgent, Information: "rollback me"}}}
	op := MaintenanceOperation{OperationID: "fault-op", AgentID: "agent-1", Scope: ScopeAgent, SourceFingerprint: "fault-source", ExpectedCursor: 0, NextCursor: 1}
	next := MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 1, SourceFingerprint: "fault-source"}
	if _, err := service.agent.db.Exec(`CREATE TRIGGER fail_maintenance_cursor BEFORE INSERT ON maintenance_cursors BEGIN SELECT RAISE(ABORT, 'injected cursor failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyMaintenanceOperation(context.Background(), op, candidate, next); err == nil {
		t.Fatal("injected SQL failure was accepted")
	}
	var n int
	for _, table := range []string{"memory_entries", "memory_revisions", "maintenance_operations", "maintenance_cursors"} {
		if err := service.agent.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s retained %d rows after rollback", table, n)
		}
	}
	if _, err := service.agent.db.Exec(`DROP TRIGGER fail_maintenance_cursor`); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalService(path, filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.ApplyMaintenanceOperation(context.Background(), op, candidate, next); err != nil {
		t.Fatalf("retry after reopen: %v", err)
	}
	entries, err := reopened.List(context.Background(), ScopeAgent, true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("recovered entries=%d err=%v", len(entries), err)
	}
}

func TestMaintenanceOperationCursorPersistsAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent.db")
	service, err := OpenLocalService(path, filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyMaintenanceOperation(context.Background(), MaintenanceOperation{OperationID: "op-1", AgentID: "agent-1", Scope: ScopeAgent, SourceFingerprint: "journal-1", ExpectedCursor: 0, NextCursor: 4}, nil, MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 4, SourceFingerprint: "journal-1"}); err != nil {
		t.Fatal(err)
	}
	cursor, err := service.agent.GetMaintenanceCursor(context.Background(), "agent-1")
	if err != nil || cursor.Sequence != 4 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(path, ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.GetMaintenanceOperation(context.Background(), "op-1")
	if err != nil || recovered.Status != "applied" {
		t.Fatalf("recovered operation=%+v err=%v", recovered, err)
	}
}

func TestApplyMaintenanceOperationAllowsEmptyBatchAndAdvancesCursor(t *testing.T) {
	root := t.TempDir()
	service, err := OpenLocalService(filepath.Join(root, "agent.db"), filepath.Join(root, "global.db"), ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	op := MaintenanceOperation{OperationID: "op-empty", AgentID: "agent-1", Scope: ScopeAgent, SourceFingerprint: "journal-empty", ExpectedCursor: 0, NextCursor: 9}
	results, err := service.ApplyMaintenanceOperation(context.Background(), op, nil, MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 9, SourceFingerprint: "journal-empty"})
	if err != nil || len(results) != 0 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	cursor, err := service.agent.GetMaintenanceCursor(context.Background(), "agent-1")
	if err != nil || cursor.Sequence != 9 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
}

func TestMaintenanceOperationConflictAndScopeIsolation(t *testing.T) {
	root := t.TempDir()
	s, err := OpenLocalService(filepath.Join(root, "agent.db"), filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	op := MaintenanceOperation{OperationID: "op-1", AgentID: "agent-1", Scope: ScopeAgent, SourceFingerprint: "j", CandidateFingerprint: "cand", ExpectedCursor: 0, NextCursor: 1}
	if _, err := s.ApplyMaintenanceOperation(context.Background(), op, nil, MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 1, SourceFingerprint: "j"}); err != nil {
		t.Fatal(err)
	}
	op.SourceFingerprint = "different"
	if _, err := s.ApplyMaintenanceOperation(context.Background(), op, nil, MaintenanceCursor{AgentID: "agent-1", Scope: ScopeAgent, Sequence: 1, SourceFingerprint: "different"}); err == nil {
		t.Fatal("different operation payload accepted")
	}
	if _, err := s.ApplyMaintenanceOperation(context.Background(), MaintenanceOperation{OperationID: "global", AgentID: "agent-1", Scope: ScopeGlobal, SourceFingerprint: "j", CandidateFingerprint: "cand", NextCursor: 1}, nil, MaintenanceCursor{}); err == nil {
		t.Fatal("global scope accepted by agent store")
	}
}
