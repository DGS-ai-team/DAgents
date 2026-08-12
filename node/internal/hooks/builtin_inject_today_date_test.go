package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestFormatAndParseTodayDateMessage(t *testing.T) {
	got := FormatTodayDateMessage("20260720")
	if got != "当天日期为：20260720" {
		t.Fatalf("format = %q", got)
	}
	day, ok := ParseTodayDateMessage(got)
	if !ok || day != "20260720" {
		t.Fatalf("parse = %q ok=%v", day, ok)
	}
	if _, ok := ParseTodayDateMessage("当天日期为：2026-07-20"); ok {
		t.Fatal("expected reject dashed date")
	}
}

func TestHasTodayDateMessage(t *testing.T) {
	msgs := []llm.Message{
		llm.UserMessage("hello", llm.UserNameHuman),
		llm.UserMessage(FormatTodayDateMessage("20260719"), TodayDateMessageName),
	}
	if HasTodayDateMessage(msgs, "20260720") {
		t.Fatal("should not match other day")
	}
	msgs = append(msgs, llm.UserMessage(FormatTodayDateMessage("20260720"), TodayDateMessageName))
	if !HasTodayDateMessage(msgs, "20260720") {
		t.Fatal("expected today match")
	}
}

func TestInjectTodayDateHook_insertsBeforeLastUserWhenMissing(t *testing.T) {
	hook := NewInjectTodayDateHook(DefaultInjectTodayDateConfig())
	hook.SetNow(func() time.Time {
		return time.Date(2026, 7, 20, 9, 0, 0, 0, time.Local)
	})
	hc := &Context{
		Phase:   PhaseTurnBeforeStep,
		History: []llm.Message{llm.UserMessage("hi", llm.UserNameHuman)},
	}
	res, err := hook.Run(context.Background(), hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	ins, ok := res.Mutations[MutationHistoryInsert].(HistoryInsertMutation)
	if !ok {
		t.Fatalf("mutations = %+v", res.Mutations)
	}
	if ins.Index != 0 {
		t.Fatalf("index = %d, want 0 (before last user)", ins.Index)
	}
	if ins.Message.Content != "当天日期为：20260720" || ins.Message.Name != TodayDateMessageName {
		t.Fatalf("msg = %+v", ins.Message)
	}
}

func TestInjectTodayDateHook_skipsWhenTodayPresent(t *testing.T) {
	hook := NewInjectTodayDateHook(DefaultInjectTodayDateConfig())
	hook.SetNow(func() time.Time {
		return time.Date(2026, 7, 20, 9, 0, 0, 0, time.Local)
	})
	hc := &Context{
		Phase: PhaseTurnBeforeStep,
		History: []llm.Message{
			llm.UserMessage(FormatTodayDateMessage("20260720"), TodayDateMessageName),
			llm.UserMessage("hi", llm.UserNameHuman),
		},
	}
	res, err := hook.Run(context.Background(), hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Mutations) != 0 {
		t.Fatalf("expected no mutation, got %+v", res.Mutations)
	}
}

func TestInjectTodayDateHook_insertsWhenStaleDate(t *testing.T) {
	hook := NewInjectTodayDateHook(DefaultInjectTodayDateConfig())
	hook.SetNow(func() time.Time {
		return time.Date(2026, 7, 20, 9, 0, 0, 0, time.Local)
	})
	hc := &Context{
		Phase: PhaseTurnBeforeStep,
		History: []llm.Message{
			llm.UserMessage(FormatTodayDateMessage("20260719"), TodayDateMessageName),
			llm.UserMessage("hi", llm.UserNameHuman),
		},
	}
	res, err := hook.Run(context.Background(), hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	ins, ok := res.Mutations[MutationHistoryInsert].(HistoryInsertMutation)
	if !ok || ins.Message.Content != "当天日期为：20260720" || ins.Index != 1 {
		t.Fatalf("mutations = %+v", res.Mutations)
	}
}

func TestInjectTodayDateHook_disabled(t *testing.T) {
	enabled := false
	hook := NewInjectTodayDateHook(InjectTodayDateConfig{Enabled: &enabled})
	hc := &Context{
		Phase:   PhaseTurnBeforeStep,
		History: []llm.Message{llm.UserMessage("hi", llm.UserNameHuman)},
	}
	res, err := hook.Run(context.Background(), hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Mutations) != 0 {
		t.Fatalf("expected no mutation when disabled, got %+v", res.Mutations)
	}
}

func TestRegistry_registersInjectTodayDate(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	names := reg.PhaseHookNames(PhaseTurnBeforeStep)
	found := false
	for _, n := range names {
		if n == "builtin.inject_today_date" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("turn.before_step hooks = %v, want builtin.inject_today_date", names)
	}
}
