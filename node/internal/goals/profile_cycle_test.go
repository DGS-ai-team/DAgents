package goals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func testProfile(t *testing.T, s *Store, agent string, now time.Time) {
	t.Helper()
	if _, err := s.SaveProfile(AutoProfile{AgentID: agent, RoleObjective: "role", PlanMode: "one_shot", Enabled: true}, 0, now); err != nil {
		t.Fatal(err)
	}
}

func cycleInput(agent, objective string) CreateInput {
	return CreateInput{AgentID: agent, Objective: objective, Acceptance: "evidence"}
}

func TestManagedCycleIdempotencyAndTerminalRenewal(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-1", now)
	g, err := s.CreateManagedCycle(cycleInput("auto-1", "first"), "req-1", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateManagedCycle(cycleInput("auto-1", "first"), "req-1", 1, now.Add(time.Second), true)
	if err != nil || retry.ID != g.ID {
		t.Fatalf("idempotent retry: %+v %v", retry, err)
	}
	if _, err = s.CreateManagedCycle(cycleInput("auto-1", "changed"), "req-1", 1, now, true); err == nil {
		t.Fatal("expected idempotency conflict")
	}
	if _, err = s.CreateManagedCycle(cycleInput("auto-1", "second"), "req-2", 1, now, true); err == nil {
		t.Fatal("expected active cycle conflict")
	}
	if _, err = s.SetStatus(g.ID, StatusCompleted, now); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProfile("auto-1")
	next, err := s.CreateManagedCycle(cycleInput("auto-1", "second"), "req-2", p.Revision, now.Add(time.Second), false)
	if err != nil {
		t.Fatal(err)
	}
	if next.CycleSequence != 2 || next.Status != StatusPaused {
		t.Fatalf("new cycle=%+v", next)
	}
	p, _ = s.GetProfile("auto-1")
	if p.CurrentGoalID != next.ID {
		t.Fatalf("profile=%+v", p)
	}
}

func TestManagedCycleRequiresEnabledProfileAndExpectedRevision(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	if _, err := s.SaveProfile(AutoProfile{AgentID: "auto-disabled", Enabled: false, PlanMode: "recurring"}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateManagedCycle(cycleInput("auto-disabled", "x"), "req", 1, now, true); err == nil {
		t.Fatal("disabled profile was enabled by cycle create")
	}
	if _, err := s.CreateManagedCycle(cycleInput("auto-disabled", "x"), "req", 99, now, false); err == nil {
		t.Fatal("stale profile revision accepted")
	}
}

func TestUpdateProfileAndCycleCASAndTerminalProtection(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-cas", now)
	g, err := s.CreateManagedCycle(cycleInput("auto-cas", "old"), "cas", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProfile("auto-cas")
	q := p
	q.RoleObjective = "new role"
	outP, outG, err := s.UpdateProfileAndCycle("auto-cas", g.ID, q, p.Revision, g.Revision, CreateInput{Objective: "new", Acceptance: "proof"}, nil, now.Add(time.Second))
	if err != nil || outP.RoleObjective != "new role" || outG.Objective != "new" {
		t.Fatalf("update=%+v %+v %v", outP, outG, err)
	}
	if _, _, err = s.UpdateProfileAndCycle("auto-cas", g.ID, q, p.Revision, g.Revision, CreateInput{Objective: "bad", Acceptance: "proof"}, nil, now); err == nil {
		t.Fatal("stale CAS accepted")
	}
	if _, err = s.SetStatus(g.ID, StatusCompleted, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	p, _ = s.GetProfile("auto-cas")
	done, _ := s.Get(g.ID)
	if _, _, err = s.UpdateProfileAndCycle("auto-cas", g.ID, p, p.Revision, done.Revision, CreateInput{Objective: "mutate", Acceptance: "proof"}, func() *Status { v := StatusPaused; return &v }(), now.Add(3*time.Second)); err == nil {
		t.Fatal("terminal cycle changed")
	}
	after, _ := s.Get(g.ID)
	if after.Objective != done.Objective || after.Status != StatusCompleted {
		t.Fatalf("terminal mutated=%+v", after)
	}
}

func TestUpdateProfileAndCycleWriteFailureAndBusy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	testProfile(t, s, "auto-fail", now)
	g, err := s.CreateManagedCycle(cycleInput("auto-fail", "old"), "fail", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProfile("auto-fail")
	oldP, oldG := p, g
	backup := path + ".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	q := p
	q.RoleObjective = "must-not-save"
	if _, _, err = s.UpdateProfileAndCycle("auto-fail", g.ID, q, p.Revision, g.Revision, CreateInput{Objective: "new", Acceptance: "proof"}, nil, now.Add(time.Second)); err == nil {
		t.Fatal("write failure accepted")
	}
	gotP, _ := s.GetProfile("auto-fail")
	gotG, _ := s.Get(g.ID)
	if !reflect.DeepEqual(gotP, oldP) || !reflect.DeepEqual(gotG, oldG) {
		t.Fatalf("rollback changed profile=%+v goal=%+v", gotP, gotG)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rp, rok := reopened.GetProfile("auto-fail")
	rg, gok := reopened.Get(g.ID)
	if !rok || !gok || !reflect.DeepEqual(rp, oldP) || !reflect.DeepEqual(rg, oldG) {
		t.Fatalf("failed transaction changed disk profile=%+v goal=%+v", rp, rg)
	}

	// Busy run and stale CAS must leave both snapshots unchanged.
	s2, _ := OpenStore("")
	testProfile(t, s2, "auto-busy-cas", now)
	g2, _ := s2.CreateManagedCycle(cycleInput("auto-busy-cas", "old"), "busy", 1, now, true)
	p2, _ := s2.GetProfile("auto-busy-cas")
	if _, _, err = s2.UpdateProfileAndCycle("auto-busy-cas", g2.ID, p2, p2.Revision-1, g2.Revision, CreateInput{Objective: "bad", Acceptance: "proof"}, nil, now); err == nil {
		t.Fatal("stale profile CAS accepted")
	}
	if _, _, err = s2.UpdateProfileAndCycle("auto-busy-cas", g2.ID, p2, p2.Revision, g2.Revision-1, CreateInput{Objective: "bad", Acceptance: "proof"}, nil, now); err == nil {
		t.Fatal("stale goal CAS accepted")
	}
	s2.mu.Lock()
	s2.data.Runs[g2.ID] = []Run{{ID: "run", GoalID: g2.ID, Status: "running", StartedAt: now}}
	s2.mu.Unlock()
	if _, _, err = s2.UpdateProfileAndCycle("auto-busy-cas", g2.ID, p2, p2.Revision, g2.Revision, CreateInput{Objective: "bad", Acceptance: "proof"}, nil, now); err == nil {
		t.Fatal("busy update accepted")
	}
	afterP, _ := s2.GetProfile("auto-busy-cas")
	afterG, _ := s2.Get(g2.ID)
	if !reflect.DeepEqual(afterP, p2) || !reflect.DeepEqual(afterG, g2) {
		t.Fatal("busy update mutated state")
	}
}

func TestProvisionStatusDistinguishesFailedProvisionFromUserPause(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-provision", now)
	g, err := s.CreateManagedCycle(cycleInput("auto-provision", "x"), "p", 1, now, false)
	if err != nil {
		t.Fatal(err)
	}
	g, err = s.SetProvisionStatus(g.ID, "failed", now)
	if err != nil {
		t.Fatal(err)
	}
	if g.StatusReason != "provisioning_failed" {
		t.Fatalf("reason=%q", g.StatusReason)
	}
	g, err = s.SetStatus(g.ID, StatusActive, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	g, err = s.SetProvisionStatus(g.ID, "ready", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	g, err = s.SetStatus(g.ID, StatusPaused, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if g.StatusReason == "provisioning_failed" {
		t.Fatal("user pause retained provisioning reason")
	}
}

func TestApplyAutoActionPausesBusyAndRevokesCurrentIntent(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-action", now)
	g, err := s.CreateManagedCycle(cycleInput("auto-action", "x"), "action", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	intent := ScheduleIntent{ID: "intent", AgentID: "auto-action", GoalID: g.ID, Purpose: "goal", Generation: 1, Decision: FinalDecision{Outcome: OutcomeProgress, NextAction: NextAt, NextWakeAt: func() *time.Time { v := now.Add(time.Hour); return &v }()}, State: IntentPending, ProfileRevision: 2, UpdatedAt: now}
	if _, err = s.UpsertScheduleIntent(intent); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.data.Runs[g.ID] = []Run{{ID: "busy", GoalID: g.ID, Status: "running", StartedAt: now}}
	s.mu.Unlock()
	p, _ := s.GetProfile("auto-action")
	updated, out, err := s.ApplyAutoAction("auto-action", g.ID, "pause_goal", p.Revision, g.Revision, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled != p.Enabled || out.Status != StatusPaused {
		t.Fatalf("action profile=%+v goal=%+v", updated, out)
	}
	got, err := s.GetScheduleIntent(g.ID, "goal")
	if err != nil || got.State != IntentRevoked {
		t.Fatalf("intent=%+v err=%v", got, err)
	}
}

func TestManagedCycleIdempotentRetrySurvivesExpiryAndProfileDisable(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-retry", now)
	expires := now.Add(time.Minute)
	in := cycleInput("auto-retry", "once")
	in.ExpiresAt = &expires
	g, err := s.CreateManagedCycle(in, "retry", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProfile("auto-retry")
	p.Enabled = false
	if _, err = s.SaveProfile(p, p.Revision, now); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateManagedCycle(in, "retry", 1, now.Add(2*time.Hour), true)
	if err != nil || retry.ID != g.ID {
		t.Fatalf("retry after expiry/disable=%+v err=%v", retry, err)
	}
}

func TestManagedCycleConcurrentFirstCreateOnlyOne(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-1", now)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateManagedCycle(cycleInput("auto-1", "same"), "req", 1, now, true)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 2 {
		t.Fatalf("same idempotency request should safely replay, successes=%d", successes)
	}
	if len(s.List()) != 1 {
		t.Fatalf("cycles=%d", len(s.List()))
	}
}

func TestManagedProfileCycleAndUsageSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	now := time.Now().UTC()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	testProfile(t, s, "auto-1", now)
	if _, err = s.RecordUsage("auto-1", 12, 3, 0, now); err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateManagedCycle(cycleInput("auto-1", "persist"), "req-persist", 1, now, false)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := reloaded.GetProfile("auto-1")
	if !ok || p.CurrentGoalID != g.ID {
		t.Fatalf("profile after restart=%+v", p)
	}
	u, ok := reloaded.GetUsage("auto-1")
	if !ok || u.BusinessTokens != 12 || u.MaintenanceTokens != 3 {
		t.Fatalf("usage after restart=%+v", u)
	}
	got, ok := reloaded.Get(g.ID)
	if !ok || got.CycleSequence != 1 || got.Status != StatusPaused {
		t.Fatalf("cycle after restart=%+v", got)
	}
}

func TestManagedCycleRejectsUnknownUsageAndRollsBackFailedWrite(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-1", now)
	if _, err := s.RecordUsage("auto-1", 0, 0, 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateManagedCycle(cycleInput("auto-1", "blocked"), "req", 1, now, true); err != ErrUsageUnknown {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.RecordUsage("auto-1", 0, 0, -1, now); err == nil {
		t.Fatal("expected negative usage rejection")
	}
	// A directory cannot be renamed over by saveLocked; verify the in-memory
	// profile and cycle binding are restored when persistence fails.
	dir := t.TempDir()
	s.path = dir
	if _, err := s.SaveProfile(AutoProfile{AgentID: "auto-1", RoleObjective: "changed"}, 1, now); err == nil {
		t.Fatal("expected persistence failure")
	}
	p, _ := s.GetProfile("auto-1")
	if p.RoleObjective != "role" || p.Revision != 1 {
		t.Fatalf("profile mutated after failed save: %+v", p)
	}
}

func TestGoalsSchemaMigrationBacksUpLegacyAndRejectsFuture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	legacy := `{"goals":{},"runs":{}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.data.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema=%d", s.data.SchemaVersion)
	}
	if _, err := os.Stat(path + ".v1.bak"); err != nil {
		t.Fatalf("legacy backup: %v", err)
	}
	data, _ := json.Marshal(disk{SchemaVersion: CurrentSchemaVersion + 1, Goals: map[string]Goal{}, Runs: map[string][]Run{}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path); err == nil {
		t.Fatal("expected future schema rejection")
	}
}

func TestProfileBindingAndUsageGuards(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	testProfile(t, s, "auto-1", now)
	p, _ := s.GetProfile("auto-1")
	if _, err := s.SaveProfile(AutoProfile{AgentID: "auto-1", CurrentGoalID: "foreign"}, p.Revision, now); err == nil {
		t.Fatal("expected foreign binding rejection")
	}
	if _, err := s.SaveProfile(AutoProfile{AgentID: "auto-1", BusinessTokenBudget: 5, Enabled: true, PlanMode: "one_shot"}, p.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordRunUsage("auto-1", "run-1", 4, 0, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordRunUsage("auto-1", "run-1", 5, 0, 0, now); err == nil {
		t.Fatal("expected differing duplicate receipt conflict")
	}
	if _, err := s.RecordRunUsage("auto-1", "run-1", 4, 0, 0, now); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetUsage("auto-1")
	if u.BusinessTokens != 4 {
		t.Fatalf("duplicate receipt counted: %+v", u)
	}
	if _, err := s.RecordRunUsage("auto-1", "run-2", 2, 0, 0, now); err != nil {
		t.Fatal(err)
	}
	u, _ = s.GetUsage("auto-1")
	if u.BusinessTokens != 6 {
		t.Fatalf("actual over-budget usage was not recorded: %+v", u)
	}
	s.path = t.TempDir()
	if _, err := s.RecordUsage("auto-1", 0, 0, 0, now); err == nil {
		t.Fatal("expected persistence failure")
	}
	u, _ = s.GetUsage("auto-1")
	if u.BusinessTokens != 6 {
		t.Fatalf("usage changed after failed save: %+v", u)
	}
}

func TestMigrateAutoProfilesRequiresExplicitValidAndAvoidsAmbiguousHistory(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	legacy, err := s.CreateManaged(CreateInput{AgentID: "auto-1", Objective: "legacy", Acceptance: "evidence"}, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MigrateAutoProfiles(map[string]bool{"other": true}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetProfile("auto-1"); ok {
		t.Fatal("unvalidated agent was migrated")
	}
	if n, err := s.MigrateAutoProfiles(map[string]bool{"auto-1": true}, now); err != nil || n != 1 {
		t.Fatalf("migration n=%d err=%v", n, err)
	}
	p, ok := s.GetProfile("auto-1")
	if !ok || p.CurrentGoalID != legacy.ID {
		t.Fatalf("profile=%+v", p)
	}
	second, _ := s.CreateManaged(CreateInput{AgentID: "auto-2", Objective: "a", Acceptance: "e"}, now, false)
	_, _ = s.CreateManaged(CreateInput{AgentID: "auto-2", Objective: "b", Acceptance: "e"}, now, false)
	terminalDone, _ := s.CreateManaged(CreateInput{AgentID: "auto-2", Objective: "done", Acceptance: "e"}, now, false)
	_, _ = s.SetStatus(terminalDone.ID, StatusCompleted, now)
	terminalStopped, _ := s.CreateManaged(CreateInput{AgentID: "auto-2", Objective: "stopped", Acceptance: "e"}, now, false)
	_, _ = s.SetStatus(terminalStopped.ID, StatusStopped, now)
	if n, err := s.MigrateAutoProfiles(map[string]bool{"auto-2": true}, now); err != nil || n != 0 {
		t.Fatalf("ambiguous migration n=%d err=%v", n, err)
	}
	if got, _ := s.Get(terminalDone.ID); got.Status != StatusCompleted {
		t.Fatalf("completed cycle changed: %s", got.Status)
	}
	if got, _ := s.Get(terminalStopped.ID); got.Status != StatusStopped {
		t.Fatalf("stopped cycle changed: %s", got.Status)
	}
	_ = second
}

func TestMigrationPersistsAmbiguityAndStartRunHonorsProfileBinding(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	first, _ := s.CreateManaged(CreateInput{AgentID: "amb", Objective: "a", Acceptance: "e"}, now, false)
	second, _ := s.CreateManaged(CreateInput{AgentID: "amb", Objective: "b", Acceptance: "e"}, now, false)
	n, err := s.MigrateAutoProfiles(map[string]bool{"amb": true}, now)
	if err != nil || n != 0 {
		t.Fatalf("migration n=%d err=%v", n, err)
	}
	issues := s.MigrationIssues()
	if len(issues) != 1 || len(issues[0].GoalIDs) != 2 {
		t.Fatalf("issues=%+v", issues)
	}
	got, _ := s.Get(first.ID)
	got2, _ := s.Get(second.ID)
	if got.Status != StatusPaused || got2.Status != StatusPaused {
		t.Fatalf("ambiguous goals=%+v %+v", got, got2)
	}
	if _, err := s.SaveProfile(AutoProfile{AgentID: "amb", Enabled: true, CurrentGoalID: first.ID, PlanMode: "one_shot"}, 0, now); err != nil {
		t.Fatal(err)
	}
	// The profile cannot point at the second cycle through a normal save, and
	// its current cycle is the only one allowed to start.
	if _, err := s.StartRun(second.ID, "wrong", now); err == nil {
		t.Fatal("non-current cycle started")
	}
}

func TestFinishRunAccountsUsageExactlyOnceAndRejectsNegative(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	g, _ := s.Create(CreateInput{AgentID: "a", Objective: "x", Acceptance: "e"}, now)
	r, err := s.StartRun(g.ID, "run", now)
	if err != nil {
		t.Fatal(err)
	}
	r.Status, r.TokensUsed = "completed", 7
	if _, err = s.FinishRun(g.ID, r, now); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetUsage("a")
	if u.BusinessTokens != 7 {
		t.Fatalf("usage=%+v", u)
	}
	if _, err = s.FinishRun(g.ID, r, now); err != nil {
		t.Fatal(err)
	}
	u, _ = s.GetUsage("a")
	if u.BusinessTokens != 7 {
		t.Fatalf("duplicate finish usage=%+v", u)
	}
	g2, _ := s.Create(CreateInput{AgentID: "b", Objective: "x", Acceptance: "e"}, now)
	r2, _ := s.StartRun(g2.ID, "run", now)
	r2.Status, r2.TokensUsed = "completed", -1
	if _, err = s.FinishRun(g2.ID, r2, now); err == nil {
		t.Fatal("negative usage accepted")
	}
}

func TestMigrationUsesPerGoalMaximumAndPreservesTerminalAmbiguity(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	a, _ := s.CreateManaged(CreateInput{AgentID: "mix", Objective: "a", Acceptance: "e"}, now, false)
	b, _ := s.CreateManaged(CreateInput{AgentID: "mix", Objective: "b", Acceptance: "e"}, now, false)
	c, _ := s.CreateManaged(CreateInput{AgentID: "mix", Objective: "c", Acceptance: "e"}, now, false)
	d, _ := s.CreateManaged(CreateInput{AgentID: "mix", Objective: "d", Acceptance: "e"}, now, false)
	s.mu.Lock()
	s.data.Goals[a.ID] = a
	s.data.Goals[b.ID] = b
	s.data.Goals[c.ID] = c
	s.data.Goals[d.ID] = d
	s.mu.Unlock()
	for _, id := range []string{a.ID, b.ID} {
		_, _ = s.SetStatus(id, StatusCompleted, now)
	}
	_, _ = s.SetStatus(c.ID, StatusStopped, now)
	// Only d remains non-terminal, so migration is unambiguous. Seed legacy
	// totals to prove each Goal contributes max(goal total, run total).
	s.mu.Lock()
	ga := s.data.Goals[a.ID]
	ga.TokensUsed = 10
	s.data.Goals[a.ID] = ga
	gb := s.data.Goals[b.ID]
	gb.TokensUsed = 8
	s.data.Goals[b.ID] = gb
	s.data.Runs[a.ID] = []Run{{ID: "ra", TokensUsed: 6, Status: "completed"}}
	s.data.Runs[b.ID] = []Run{{ID: "rb", TokensUsed: 9, Status: "completed"}}
	s.mu.Unlock()
	if n, err := s.MigrateAutoProfiles(map[string]bool{"mix": true}, now); err != nil || n != 1 {
		t.Fatalf("migration n=%d err=%v", n, err)
	}
	u, _ := s.GetUsage("mix")
	if u.BusinessTokens != 19 {
		t.Fatalf("per-goal max usage=%+v", u)
	}
	if len(s.MigrationIssues()) != 0 {
		t.Fatal("unexpected ambiguity issue")
	}
}
