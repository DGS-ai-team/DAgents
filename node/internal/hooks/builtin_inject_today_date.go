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

// InjectTodayDateHook 保留旧 Hook 标识以兼容已有配置和诊断，但不再修改
// durable history。当天日期由 turn 的 request-only ContextInjection 负责。
type InjectTodayDateHook struct {
	cfg InjectTodayDateConfig
	now func() time.Time
}

// NewInjectTodayDateHook 构造内置当天日期注入 Hook。
func NewInjectTodayDateHook(cfg InjectTodayDateConfig) *InjectTodayDateHook {
	return &InjectTodayDateHook{
		cfg: InjectTodayDateConfigOrDefault(cfg),
		now: time.Now,
	}
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

func (h *InjectTodayDateHook) Run(_ context.Context, _ *Context, _ Host) (Result, error) {
	// Do not derive the date from Hook history anymore. A Hook mutation would
	// turn a runtime fact into a durable user message and would also make old
	// dates participate in hydrate, compression and cache prefixes.
	return Result{Action: ActionContinue}, nil
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
