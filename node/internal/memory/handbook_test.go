package memory

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"testing"
)

func handbookCandidate(text string) HandbookCandidate {
	return HandbookCandidate{Content: text, Evidence: []HandbookEvidence{{Source: "turn", Reference: "message-1"}}, Validation: []HandbookValidation{{Name: "format", Passed: true}}, Reason: "observed"}
}

func TestHandbookVersionCASRollbackAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := OpenStore(path, ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitHandbook(context.Background(), "agent-a", handbookCandidate("use the repository formatter"), 0)
	if err != nil || v1.Revision != 1 {
		t.Fatalf("v1=%+v err=%v", v1, err)
	}
	if _, err = s.SubmitHandbook(context.Background(), "agent-a", handbookCandidate("stale"), 0); !errors.Is(err, ErrHandbookConflict) {
		t.Fatalf("stale error=%v", err)
	}
	v2, err := s.SubmitHandbook(context.Background(), "agent-a", handbookCandidate("run focused tests first"), 1)
	if err != nil {
		t.Fatal(err)
	}
	v3, err := s.RollbackHandbook(context.Background(), "agent-a", 1, 2)
	if err != nil || v3.Revision != 3 || v3.Content != v1.Content {
		t.Fatalf("rollback=%+v err=%v", v3, err)
	}
	versions, err := s.ListHandbookVersions(context.Background(), "agent-a")
	if err != nil || len(versions) != 3 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	if got, err := s.GetHandbook(context.Background(), "agent-a"); err != nil || got.Revision != 3 {
		t.Fatalf("current=%+v err=%v", got, err)
	}
	if _, err = s.SubmitHandbook(context.Background(), "agent-b", handbookCandidate("other"), 0); err != nil {
		t.Fatalf("agent-b submit=%v", err)
	}
	if got, err := s.GetHandbook(context.Background(), "agent-a"); err != nil || got.Content != v1.Content {
		t.Fatalf("agent-a crossed scope: %+v err=%v", got, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path, ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetHandbook(context.Background(), "agent-a")
	if err != nil || got.Content != v1.Content {
		t.Fatalf("reopened=%+v err=%v", got, err)
	}
	_ = v2
}

func TestHandbookValidationAndConcurrentCAS(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"), ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.SubmitHandbook(context.Background(), "agent-a", HandbookCandidate{Content: "x"}, 0); !errors.Is(err, ErrHandbookInvalid) {
		t.Fatalf("missing evidence error=%v", err)
	}
	bad := handbookCandidate("x")
	bad.Content = string(make([]byte, 32*1024+1))
	if _, err = s.SubmitHandbook(context.Background(), "agent-a", bad, 0); !errors.Is(err, ErrHandbookInvalid) {
		t.Fatalf("oversize error=%v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.SubmitHandbook(context.Background(), "agent-a", handbookCandidate("same revision"), 0)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	conflicts := 0
	for e := range results {
		if e == nil {
			successes++
		}
		if errors.Is(e, ErrHandbookConflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestHandbookFailedValidationPreservesCurrentAndRejectsGlobal(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"), ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err = s.SubmitHandbook(ctx, "agent-a", handbookCandidate("current"), 0); err != nil {
		t.Fatal(err)
	}
	bad := handbookCandidate("replacement")
	bad.Validation = []HandbookValidation{{Name: "format", Passed: false, Detail: "rejected"}}
	if _, err = s.SubmitHandbook(ctx, "agent-a", bad, 1); !errors.Is(err, ErrHandbookInvalid) {
		t.Fatalf("failed validation error=%v", err)
	}
	got, err := s.GetHandbook(ctx, "agent-a")
	if err != nil || got.Revision != 1 || got.Content != "current" {
		t.Fatalf("current changed: %+v err=%v", got, err)
	}
	if _, err = s.SubmitHandbook(ctx, "agent-a", handbookCandidate("overflow"), -1); !errors.Is(err, ErrHandbookInvalid) {
		t.Fatalf("negative expected=%v", err)
	}
	if _, err = s.SubmitHandbook(ctx, "agent-a", handbookCandidate("overflow"), math.MaxInt64); !errors.Is(err, ErrHandbookInvalid) {
		t.Fatalf("max expected=%v", err)
	}

	global, err := OpenStore(filepath.Join(t.TempDir(), "global.db"), ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	defer global.Close()
	if _, err = global.SubmitHandbook(ctx, "agent-a", handbookCandidate("global"), 0); !errors.Is(err, ErrHandbookInvalid) {
		t.Fatalf("global submit=%v", err)
	}
	if _, err = NewLocalHandbookService(global, "agent-a"); !errors.Is(err, ErrHandbookInvalid) {
		t.Fatalf("global local service=%v", err)
	}
}

func TestLocalHandbookServiceBindsIdentityAndSnapshots(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"), ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	a, err := NewLocalHandbookService(s, " agent-a ")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewLocalHandbookService(s, "agent-b")
	if err != nil {
		t.Fatal(err)
	}
	c := handbookCandidate("bound")
	v, err := a.Submit(ctx, c, 0)
	if err != nil {
		t.Fatal(err)
	}
	v.Evidence[0].Source = "mutated"
	v.Validation[0].Name = "mutated"
	current, err := a.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.Evidence[0].Source != "turn" || current.Validation[0].Name != "format" {
		t.Fatalf("returned snapshot leaked mutation: %+v", current)
	}
	listed, err := a.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed[0].Evidence[0].Source = "mutated again"
	current, err = a.Get(ctx)
	if err != nil || current.Evidence[0].Source != "turn" {
		t.Fatalf("list snapshot leaked mutation: %+v err=%v", current, err)
	}
	if _, err = b.Get(ctx); !errors.Is(err, ErrHandbookNotFound) {
		t.Fatalf("identity crossed: %v", err)
	}
	if _, err = b.Submit(ctx, handbookCandidate("other"), 0); err != nil {
		t.Fatal(err)
	}
	current, err = a.Get(ctx)
	if err != nil || current.Content != "bound" {
		t.Fatalf("agent isolation broken: %+v err=%v", current, err)
	}
}

func TestHandbookSubmitFailureRollsBackCurrentAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	s, err := OpenStore(path, ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = s.SubmitHandbook(ctx, "agent-a", handbookCandidate("v1"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER block_handbook_v2 BEFORE INSERT ON agent_handbook_versions
		WHEN NEW.agent_id='agent-a' AND NEW.revision=2
		BEGIN SELECT RAISE(ABORT, 'blocked handbook insert'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitHandbook(ctx, "agent-a", handbookCandidate("v2"), 1); err == nil {
		t.Fatal("expected insert failure")
	}
	current, err := s.GetHandbook(ctx, "agent-a")
	if err != nil || current.Revision != 1 || current.Content != "v1" {
		t.Fatalf("failed submit changed current: %+v err=%v", current, err)
	}
	versions, err := s.ListHandbookVersions(ctx, "agent-a")
	if err != nil || len(versions) != 1 {
		t.Fatalf("failed submit changed history: %+v err=%v", versions, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER block_handbook_v2`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitHandbook(ctx, "agent-a", handbookCandidate("v2"), 1); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path, ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	current, err = s.GetHandbook(ctx, "agent-a")
	if err != nil || current.Revision != 2 || current.Content != "v2" {
		t.Fatalf("reopened current=%+v err=%v", current, err)
	}
}
