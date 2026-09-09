package autonomy

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func profile(id string) Profile {
	return Profile{AgentID: id, Responsibility: "review", WakeIntervalSeconds: 60, MaxToolRounds: 3, DreamingEnabled: true, DreamingTime: "02:30", Timezone: "UTC"}
}

func TestStorePersistsProfilesExperiencesAndTodosWithIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autonomy.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := profile("agent-a")
	if err := s.PutProfile(p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitDreamingExperience("agent-a", 0, "learned", time.Now()); err != nil {
		t.Fatal(err)
	}
	todo, err := s.CreateTodo("agent-a", "follow up")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTodoCAS("agent-a", todo.ID, todo.Revision, "follow up", "completed"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := reopened.GetProfile("agent-a"); !ok || p.Revision != 1 {
		t.Fatalf("profile=%+v ok=%v", p, ok)
	}
	if _, ok := reopened.GetExperience("other"); ok {
		t.Fatal("experience isolation failed")
	}
	e, ok := reopened.GetExperience("agent-a")
	if !ok || e.Revision != 1 || e.AgentID != "agent-a" {
		t.Fatalf("experience=%+v ok=%v", e, ok)
	}
	if _, err := reopened.SubmitDreamingExperience("agent-a", 1, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.GetTodo("agent-a", todo.ID)
	if !ok || got.Status != "completed" || got.Revision != 2 {
		t.Fatalf("todo=%+v ok=%v", got, ok)
	}
	if _, err := reopened.UpdateTodoCAS("other", todo.ID, 2, "x", "pending"); err == nil {
		t.Fatal("cross-agent todo update accepted")
	}
}

func TestStoreValidationAndAtomicFailure(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	bad := profile("bad")
	bad.MaxToolRounds = 0
	if err := s.PutProfile(bad, 0); err == nil {
		t.Fatal("invalid rounds accepted")
	}
	bad = profile("bad")
	bad.DreamingTime = "25:00"
	if err := s.PutProfile(bad, 0); err == nil {
		t.Fatal("invalid time accepted")
	}
	bad = profile("bad")
	bad.Timezone = "No/Such"
	if err := s.PutProfile(bad, 0); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	p := profile("agent-a")
	if err := s.PutProfile(p, 0); err != nil {
		t.Fatal(err)
	}
	block := filepath.Join(t.TempDir(), "block")
	if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(block, "state.json")
	p.Responsibility = "changed"
	if err := s.PutProfile(p, 1); err == nil {
		t.Fatal("save failure accepted")
	}
	stored, _ := s.GetProfile("agent-a")
	if stored.Responsibility != "review" {
		t.Fatalf("memory polluted=%+v", stored)
	}
}

func TestTodoCASRejectsStaleRevision(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "autonomy.json"))
	todo, err := s.CreateTodo("agent-a", "task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTodoCAS("agent-a", todo.ID, todo.Revision+1, "x", "pending"); err != ErrConflict {
		t.Fatalf("err=%v", err)
	}
}

func TestExperienceCASAndTodoDelete(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "autonomy.json"))
	e, err := s.SubmitDreamingExperience("agent-a", 0, "one", time.Now())
	if err != nil || e.Revision != 1 {
		t.Fatalf("experience=%+v err=%v", e, err)
	}
	if _, err := s.SubmitDreamingExperience("agent-a", 0, "stale", time.Now()); err != ErrConflict {
		t.Fatalf("stale experience err=%v", err)
	}
	if _, err := s.SubmitDreamingExperience("agent-a", 1, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	todo, err := s.CreateTodo("agent-a", "remove")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTodoCAS("agent-a", todo.ID, todo.Revision+1); err != ErrConflict {
		t.Fatalf("stale delete err=%v", err)
	}
	if err := s.DeleteTodoCAS("agent-a", todo.ID, todo.Revision); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetTodo("agent-a", todo.ID); ok {
		t.Fatal("todo remains")
	}
}

func TestDreamingCommitIsPerDayIdempotentAndConflicting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autonomy.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)
	in := DreamingCommitInput{AgentID: "agent-a", LocalDate: "2026-09-09", CommitID: "commit-1", SessionID: "agent-a", Boundary: "opaque-1", ExpectedRevision: 0, Content: "learned", Now: now}
	c, err := s.CommitDreaming(in)
	if err != nil || c.ExperienceRevision != 1 || c.ResetApplied {
		t.Fatalf("commit=%+v err=%v", c, err)
	}
	retry, err := s.CommitDreaming(in)
	if err != nil || retry != c {
		t.Fatalf("retry=%+v err=%v want=%+v", retry, err, c)
	}
	in.Content = "changed"
	if _, err := s.CommitDreaming(in); err != ErrConflict {
		t.Fatalf("changed content err=%v", err)
	}
	if _, err := s.MarkDreamingResetApplied("agent-a", "2026-09-09", "commit-1"); err != nil {
		t.Fatal(err)
	}
	in.Content, in.LocalDate, in.CommitID, in.ExpectedRevision = "learned", "2026-09-10", "commit-2", 1
	if _, err := s.CommitDreaming(in); err != nil {
		t.Fatal(err)
	}
	in.AgentID, in.LocalDate, in.CommitID, in.Boundary, in.ExpectedRevision = "agent-b", "2026-09-09", "commit-b", "opaque-b", 0
	if _, err := s.CommitDreaming(in); err != nil {
		t.Fatal(err)
	}
	c, ok := s.GetDreamingCommit("agent-a", "2026-09-09")
	if !ok || !c.ResetApplied {
		t.Fatalf("reset state=%+v ok=%v", c, ok)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.GetDreamingCommit("agent-a", "2026-09-09"); !ok || got.CommitID != "commit-1" || !got.ResetApplied {
		t.Fatalf("reopened=%+v ok=%v", got, ok)
	}
}

func TestDreamingCommitFailureRollsBackAndPendingIsDiscoverable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autonomy.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)
	s.path = filepath.Dir(path)
	_, err = s.CommitDreaming(DreamingCommitInput{AgentID: "a", LocalDate: "2026-09-09", CommitID: "c", SessionID: "a", Boundary: "b", Content: "x", Now: now})
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, ok := s.GetExperience("a"); ok {
		t.Fatal("experience changed after failed commit")
	}
	if _, ok := s.GetDreamingCommit("a", "2026-09-09"); ok {
		t.Fatal("commit changed after failed commit")
	}
	s.path = path
	if _, err := s.CommitDreaming(DreamingCommitInput{AgentID: "a", LocalDate: "2026-09-09", CommitID: "c", SessionID: "a", Boundary: "b", Content: "x", Now: now}); err != nil {
		t.Fatal(err)
	}
	if pending := s.ListPendingDreamingCommits("a"); len(pending) != 1 {
		t.Fatalf("pending=%+v", pending)
	}
}

func TestDreamingPendingAndConfirmationBoundaries(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)
	if _, err := s.CommitDreaming(DreamingCommitInput{AgentID: "a", LocalDate: "2026-09-09", CommitID: "c", SessionID: "a", Boundary: "b", Content: "x", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitDreaming(DreamingCommitInput{AgentID: "a", LocalDate: "2026-09-10", CommitID: "c2", SessionID: "a", Boundary: "b2", Content: "y", ExpectedRevision: 1, Now: now}); err != ErrConflict {
		t.Fatalf("pending next day err=%v", err)
	}
	if _, err := s.MarkDreamingResetApplied("a", "2026-09-09", "wrong"); err != ErrConflict {
		t.Fatalf("wrong confirmation err=%v", err)
	}
	oldPath := s.path
	s.path = filepath.Dir(oldPath)
	if _, err := s.MarkDreamingResetApplied("a", "2026-09-09", "c"); err == nil {
		t.Fatal("expected confirmation persistence failure")
	}
	if pending := s.ListPendingDreamingCommits("a"); len(pending) != 1 || pending[0].ResetApplied {
		t.Fatalf("pending after failure=%+v", pending)
	}
	s.path = oldPath
	if _, err := s.MarkDreamingResetApplied("a", "2026-09-09", "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkDreamingResetApplied("a", "2026-09-09", "c"); err != nil {
		t.Fatal("repeat confirmation: ", err)
	}
}

func TestDreamingCommitConcurrentSameDateOnlyOneWins(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	results := make(chan error, 2)
	for i := 1; i <= 2; i++ {
		go func(i int) {
			_, e := s.CommitDreaming(DreamingCommitInput{AgentID: "a", LocalDate: "2026-09-09", CommitID: fmt.Sprintf("c%d", i), SessionID: "a", Boundary: fmt.Sprintf("b%d", i), Content: fmt.Sprintf("x%d", i), Now: now})
			results <- e
		}(i)
	}
	var success, conflicts int
	for i := 0; i < 2; i++ {
		if e := <-results; e == nil {
			success++
		} else if e == ErrConflict {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}
