package triggers

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestParseScheduleSpecWeekly(t *testing.T) {
	spec, err := ParseScheduleSpec(map[string]any{
		"schedule": map[string]any{
			"kind":    "weekly",
			"weekday": 1,
			"hour":    9,
			"minute":  30,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != CalendarWeekly || spec.Weekday != 1 || spec.Hour != 9 || spec.Minute != 30 {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestParseScheduleSpecRejectsMixedCondition(t *testing.T) {
	if _, err := InferScheduleKind(map[string]any{
		"interval_seconds": 60,
		"schedule":         map[string]any{"kind": "daily", "hour": 8, "minute": 0},
	}); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestResolveMonthDayNegativeLastDay(t *testing.T) {
	day, ok := resolveMonthDay(2024, time.February, -1)
	if !ok || day != 29 {
		t.Fatalf("feb 2024 last day = %d ok=%v", day, ok)
	}
}

func TestResolveMonthDaySkipOverflow(t *testing.T) {
	if _, ok := resolveMonthDay(2024, time.February, 31); ok {
		t.Fatal("expected skip for day 31 in february")
	}
}

func TestNextMonthlyFireSkipsInvalidMonth(t *testing.T) {
	loc := time.Local
	after := time.Date(2024, time.January, 31, 12, 0, 0, 0, loc)
	spec := ScheduleSpec{Kind: CalendarMonthly, Day: 31, Hour: 10, Minute: 0}
	next := NextCalendarFire(spec, after)
	want := time.Date(2024, time.March, 31, 10, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next = %v want %v", next, want)
	}
}

func TestNextMonthlyFireNegativeDay(t *testing.T) {
	loc := time.Local
	after := time.Date(2024, time.January, 15, 12, 0, 0, 0, loc)
	spec := ScheduleSpec{Kind: CalendarMonthly, Day: -1, Hour: 8, Minute: 0}
	next := NextCalendarFire(spec, after)
	want := time.Date(2024, time.January, 31, 8, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next = %v want %v", next, want)
	}
}

func TestEvaluateDueCatchUpWithinPeriod(t *testing.T) {
	loc := time.Local
	nextAt := time.Date(2024, time.May, 1, 10, 0, 0, 0, loc)
	now := nextAt.Add(30 * time.Minute)
	def := Definition{
		Enabled:    true,
		Condition:  map[string]any{"schedule": map[string]any{"kind": "daily", "hour": 10, "minute": 0}},
		NextFireAt: ptrFloat(timeToUnixFloat(nextAt)),
	}
	decision, _ := EvaluateDue(def, now)
	if decision != DueFire {
		t.Fatalf("decision = %d want DueFire", decision)
	}
}

func TestEvaluateDueAdvanceWhenGapExceedsPeriod(t *testing.T) {
	loc := time.Local
	nextAt := time.Date(2024, time.May, 1, 10, 0, 0, 0, loc)
	now := nextAt.Add(48 * time.Hour)
	def := Definition{
		Enabled:    true,
		Condition:  map[string]any{"schedule": map[string]any{"kind": "daily", "hour": 10, "minute": 0}},
		NextFireAt: ptrFloat(timeToUnixFloat(nextAt)),
	}
	decision, updated := EvaluateDue(def, now)
	if decision != DueAdvanceOnly {
		t.Fatalf("decision = %d want DueAdvanceOnly", decision)
	}
	if updated.NextFireAt == nil {
		t.Fatal("expected next_fire_at to be set")
	}
	advanced := unixFloatToTime(*updated.NextFireAt)
	if !advanced.After(now) {
		t.Fatalf("advanced = %v should be after now=%v", advanced, now)
	}
}

func TestNewDefinitionFromCreateCalendar(t *testing.T) {
	now := time.Date(2024, time.May, 1, 8, 0, 0, 0, time.Local)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "weekly",
		TaskTemplate: "tick",
		Condition: map[string]any{
			"schedule": map[string]any{"kind": "weekly", "weekday": 0, "hour": 10, "minute": 0},
			"cmd":      "test -f /tmp/ready",
		},
	}, "agent-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if def.NextFireAt == nil {
		t.Fatal("next_fire_at required")
	}
	if ConditionCmd(def.Condition) != "test -f /tmp/ready" {
		t.Fatalf("cmd = %q", ConditionCmd(def.Condition))
	}
}

type stubCmdGate struct {
	ok     bool
	detail string
	err    error
	called int
	last   string
}

func (s *stubCmdGate) Run(cmd string) (bool, string, error) {
	s.called++
	s.last = cmd
	return s.ok, s.detail, s.err
}

func TestSchedulerCmdGateBlocksFire(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.Local
	past := time.Date(2024, time.May, 1, 10, 0, 0, 0, loc)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "gated",
		TaskTemplate: "run",
		Condition: map[string]any{
			"schedule": map[string]any{"kind": "daily", "hour": 10, "minute": 0},
			"cmd":      "false",
		},
	}, "agent-1", past.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	v := timeToUnixFloat(past)
	def.NextFireAt = &v
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 5)
	gate := &stubCmdGate{ok: false, detail: "exit_code=1"}
	sched.SetCmdGate(gate)
	sched.RunOnceForTest(context.Background(), past.Add(time.Minute))
	if gate.called != 1 || gate.last != "false" {
		t.Fatalf("gate called=%d last=%q", gate.called, gate.last)
	}
	if len(sub.messages) != 0 {
		t.Fatalf("expected no message, got %v", sub.messages)
	}
	got, _ := store.GetTrigger(def.TriggerID)
	if got.NextFireAt == nil || *got.NextFireAt <= v {
		t.Fatalf("next_fire_at should advance: %v", got.NextFireAt)
	}
}

func TestSchedulerCmdGateAllowsFire(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.Local
	past := time.Date(2024, time.May, 1, 10, 0, 0, 0, loc)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "gated",
		TaskTemplate: "run {trigger_name}",
		Condition: map[string]any{
			"schedule": map[string]any{"kind": "daily", "hour": 10, "minute": 0},
			"cmd":      "true",
		},
	}, "agent-1", past.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	v := timeToUnixFloat(past)
	def.NextFireAt = &v
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 5)
	gate := &stubCmdGate{ok: true, detail: "exit 0"}
	sched.SetCmdGate(gate)
	sched.RunOnceForTest(context.Background(), past.Add(time.Minute))
	if len(sub.messages) != 1 {
		t.Fatalf("messages = %v", sub.messages)
	}
}

func TestManualFireSkipsCmdGate(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "t.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(200, 0)
	def, err := NewDefinitionFromCreate(CreateInput{
		Name:         "manual",
		TaskTemplate: "run",
		Condition: map[string]any{
			"interval_seconds": 3600,
			"cmd":              "false",
		},
	}, "agent-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	sub := &fakeSubmitter{}
	sched := NewScheduler(store, sub, 5)
	gate := &stubCmdGate{ok: false}
	sched.SetCmdGate(gate)
	record, err := sched.FireTrigger(def.TriggerID, "agent_tool", nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != FireStatusQueued {
		t.Fatalf("status = %s", record.Status)
	}
	if gate.called != 0 {
		t.Fatalf("manual fire should skip cmd gate, called=%d", gate.called)
	}
}

func ptrFloat(v float64) *float64 {
	return &v
}
