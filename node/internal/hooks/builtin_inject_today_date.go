package hooks

import (
	"context"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// TodayDateMessagePrefix 为当天日期 human message 固定文案前缀（全角冒号）。
const TodayDateMessagePrefix = "当天日期为："

// TodayDateMessageName 为日期消息的 user name（与 llm.UserNameDate 对齐）。
const TodayDateMessageName = llm.UserNameDate

// InjectTodayDateHook 在 turn.before_step 检查 history，必要时追加当天日期 human message。
type InjectTodayDateHook struct {
	now func() time.Time
}

// NewInjectTodayDateHook 构造内置当天日期注入 Hook。
func NewInjectTodayDateHook() *InjectTodayDateHook {
	return &InjectTodayDateHook{now: time.Now}
}

// SetNow 注入时钟（单测）。
func (h *InjectTodayDateHook) SetNow(fn func() time.Time) {
	if h != nil && fn != nil {
		h.now = fn
	}
}

// Name 返回 Hook 标识。
func (h *InjectTodayDateHook) Name() string { return "builtin.inject_today_date" }

// Phases 返回支持的 phase 列表。
func (h *InjectTodayDateHook) Phases() []Phase { return []Phase{PhaseTurnBeforeStep} }

// Run 若 history 中尚无「当天日期为：YYYYMMDD」则插入一条 user 消息。
// 插入位置：若末条为非日期 user，则插在其前，避免盖住本轮用户问题；否则追加到末尾。
func (h *InjectTodayDateHook) Run(_ context.Context, hc *Context, _ Host) (Result, error) {
	if h == nil {
		return Result{Action: ActionContinue}, nil
	}
	nowFn := h.now
	if nowFn == nil {
		nowFn = time.Now
	}
	today := nowFn().Format("20060102")
	want := FormatTodayDateMessage(today)
	msgs := hc.History
	if HasTodayDateMessage(msgs, today) {
		return Result{Action: ActionContinue}, nil
	}
	idx := len(msgs)
	if idx > 0 && strings.TrimSpace(msgs[idx-1].Role) == "user" {
		if _, ok := ParseTodayDateMessage(llm.MessageTextSummary(msgs[idx-1])); !ok {
			idx = idx - 1
		}
	}
	return Result{
		Action: ActionContinue,
		Mutations: map[string]any{
			MutationHistoryInsert: HistoryInsertMutation{
				Index:   idx,
				Message: llm.UserMessage(want, TodayDateMessageName),
			},
		},
	}, nil
}

// FormatTodayDateMessage 返回「当天日期为：YYYYMMDD」。
func FormatTodayDateMessage(yyyymmdd string) string {
	return TodayDateMessagePrefix + strings.TrimSpace(yyyymmdd)
}

// HasTodayDateMessage 判断 message 清单中是否已有当天日期的 human（user）消息。
func HasTodayDateMessage(msgs []llm.Message, todayYYYYMMDD string) bool {
	want := FormatTodayDateMessage(todayYYYYMMDD)
	for _, m := range msgs {
		if strings.TrimSpace(m.Role) != "user" {
			continue
		}
		if strings.TrimSpace(llm.MessageTextSummary(m)) == want {
			return true
		}
	}
	return false
}

// ParseTodayDateMessage 若 content 为「当天日期为：YYYYMMDD」则返回日期与 true。
func ParseTodayDateMessage(content string) (string, bool) {
	s := strings.TrimSpace(content)
	if !strings.HasPrefix(s, TodayDateMessagePrefix) {
		return "", false
	}
	day := strings.TrimSpace(strings.TrimPrefix(s, TodayDateMessagePrefix))
	if len(day) != 8 {
		return "", false
	}
	for _, r := range day {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return day, true
}
