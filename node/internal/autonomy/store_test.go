package autonomy

import (
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
