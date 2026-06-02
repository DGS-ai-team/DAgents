package triggers

import (
	"fmt"
	"strings"
	"time"
)

// CalendarKind 为结构化 schedule.kind。
type CalendarKind string

const (
	CalendarDaily   CalendarKind = "daily"
	CalendarWeekly  CalendarKind = "weekly"
	CalendarMonthly CalendarKind = "monthly"
)

// ScheduleSpec 为 condition.schedule 的结构化日历调度（方案 A）。
type ScheduleSpec struct {
	Kind    CalendarKind
	Weekday int // weekly：0=周日 … 6=周六
	Day     int // monthly：正数日期；负数表示倒数（-1=最后一天）
	Hour    int
	Minute  int
}

// ParseScheduleSpec 从 condition["schedule"] 解析日历调度。

// 异常：缺失/未知 kind/字段非法时返回 error。
func ParseScheduleSpec(condition map[string]any) (ScheduleSpec, error) {
	raw, ok := condition["schedule"].(map[string]any)
	if !ok || len(raw) == 0 {
		return ScheduleSpec{}, fmt.Errorf("schedule object is required")
	}
	kind := CalendarKind(strings.ToLower(strings.TrimSpace(fmt.Sprint(raw["kind"]))))
	switch kind {
	case CalendarDaily, CalendarWeekly, CalendarMonthly:
	default:
		return ScheduleSpec{}, fmt.Errorf("unsupported schedule kind: %q", kind)
	}
	spec := ScheduleSpec{
		Kind:   kind,
		Hour:   intFromAny(raw["hour"]),
		Minute: intFromAny(raw["minute"]),
	}
	if spec.Hour < 0 || spec.Hour > 23 || spec.Minute < 0 || spec.Minute > 59 {
		return ScheduleSpec{}, fmt.Errorf("invalid schedule time hour=%d minute=%d", spec.Hour, spec.Minute)
	}
	switch kind {
	case CalendarWeekly:
		spec.Weekday = intFromAny(raw["weekday"])
		if spec.Weekday < 0 || spec.Weekday > 6 {
			return ScheduleSpec{}, fmt.Errorf("weekday must be 0-6 (0=Sunday)")
		}
	case CalendarMonthly:
		spec.Day = intFromAny(raw["day"])
		if spec.Day == 0 || spec.Day < -31 || spec.Day > 31 {
			return ScheduleSpec{}, fmt.Errorf("day must be 1-31, or negative for end-of-month (-1=last day)")
		}
	}
	return spec, nil
}

// ConditionCmd 返回 condition 内可选 cmd 门控脚本（空表示无门控）。
func ConditionCmd(condition map[string]any) string {
	if condition == nil {
		return ""
	}
	raw, ok := condition["cmd"]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

// NextCalendarFire 计算 strictly after `after` 的下一次日历触发时刻（主机本地时区）。
func NextCalendarFire(spec ScheduleSpec, after time.Time) time.Time {
	loc := time.Local
	after = after.In(loc)
	switch spec.Kind {
	case CalendarDaily:
		return nextDailyFire(spec, after, loc)
	case CalendarWeekly:
		return nextWeeklyFire(spec, after, loc)
	case CalendarMonthly:
		return nextMonthlyFire(spec, after, loc)
	default:
		return after.Add(24 * time.Hour)
	}
}

func nextDailyFire(spec ScheduleSpec, after time.Time, loc *time.Location) time.Time {
	candidate := time.Date(after.Year(), after.Month(), after.Day(), spec.Hour, spec.Minute, 0, 0, loc)
	if !candidate.After(after) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}

func nextWeeklyFire(spec ScheduleSpec, after time.Time, loc *time.Location) time.Time {
	cursor := time.Date(after.Year(), after.Month(), after.Day(), spec.Hour, spec.Minute, 0, 0, loc)
	for i := 0; i < 8; i++ {
		if int(cursor.Weekday()) == spec.Weekday && cursor.After(after) {
			return cursor
		}
		cursor = cursor.Add(24 * time.Hour)
	}
	return cursor
}

func nextMonthlyFire(spec ScheduleSpec, after time.Time, loc *time.Location) time.Time {
	year, month := after.Year(), after.Month()
	for i := 0; i < 24; i++ {
		day, ok := resolveMonthDay(year, month, spec.Day)
		if ok {
			candidate := time.Date(year, month, day, spec.Hour, spec.Minute, 0, 0, loc)
			if candidate.After(after) {
				return candidate
			}
		}
		month++
		if month > 12 {
			month = 1
			year++
		}
	}
	return after.Add(31 * 24 * time.Hour)
}

// CalendarPeriod 返回相邻两次日历触发的间隔，用于漏触发判定。
func CalendarPeriod(spec ScheduleSpec, anchor time.Time) time.Duration {
	next := NextCalendarFire(spec, anchor.Add(-time.Second))
	if !next.After(anchor) {
		next = NextCalendarFire(spec, next)
	}
	if next.After(anchor) {
		return next.Sub(anchor)
	}
	switch spec.Kind {
	case CalendarDaily:
		return 24 * time.Hour
	case CalendarWeekly:
		return 7 * 24 * time.Hour
	default:
		return 31 * 24 * time.Hour
	}
}

// resolveMonthDay 解析 monthly day；正数超出当月天数则 ok=false（跳月）。
func resolveMonthDay(year int, month time.Month, day int) (int, bool) {
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.Local).Day()
	if day < 0 {
		resolved := daysInMonth + day + 1
		if resolved < 1 || resolved > daysInMonth {
			return 0, false
		}
		return resolved, true
	}
	if day > daysInMonth {
		return 0, false
	}
	return day, true
}

// SchedulePeriod 返回 condition 的一个调度周期时长（漏触发补发阈值）。
func SchedulePeriod(condition map[string]any, anchor time.Time) time.Duration {
	kind, err := InferScheduleKind(condition)
	if err != nil {
		return 0
	}
	switch kind {
	case ScheduleInterval:
		sec := intFromAny(condition["interval_seconds"])
		if sec <= 0 {
			return 0
		}
		return time.Duration(sec) * time.Second
	case ScheduleOnce:
		return 365 * 24 * time.Hour
	case ScheduleCalendar:
		spec, err := ParseScheduleSpec(condition)
		if err != nil {
			return 0
		}
		return CalendarPeriod(spec, anchor)
	default:
		return 0
	}
}

// ComputeNextFireTime 从 now 起计算下一次应触发的本地时间。
func ComputeNextFireTime(def Definition, now time.Time) *time.Time {
	if !def.Enabled {
		return nil
	}
	kind, err := InferScheduleKind(def.Condition)
	if err != nil {
		return nil
	}
	loc := time.Local
	now = now.In(loc)
	switch kind {
	case ScheduleInterval:
		interval := intFromAny(def.Condition["interval_seconds"])
		if interval <= 0 {
			return nil
		}
		base := now
		if def.LastFiredAt != nil {
			base = unixFloatToTime(*def.LastFiredAt).In(loc)
		}
		next := base.Add(time.Duration(interval) * time.Second)
		if next.Before(now) {
			next = now.Add(time.Duration(interval) * time.Second)
		}
		return &next
	case ScheduleOnce:
		fireAt := floatFromAny(def.Condition["fire_at"])
		if def.LastFiredAt != nil {
			return nil
		}
		t := unixFloatToTime(fireAt).In(loc)
		if t.Before(now) {
			t = now
		}
		return &t
	case ScheduleCalendar:
		spec, err := ParseScheduleSpec(def.Condition)
		if err != nil {
			return nil
		}
		next := NextCalendarFire(spec, now.Add(-time.Second))
		return &next
	default:
		return nil
	}
}

func unixFloatToTime(v float64) time.Time {
	sec := int64(v)
	nsec := int64((v - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).In(time.Local)
}

func timeToUnixFloat(t time.Time) float64 {
	return float64(t.UnixNano()) / 1e9
}

// DueDecision 为调度 tick 对单条 trigger 的判定。
type DueDecision int

const (
	DueNotReady DueDecision = iota
	DueFire
	DueAdvanceOnly
)

// EvaluateDue 判定是否应触发、是否仅推进 next_fire_at（漏触发超过 1 周期）。

// 规则：
// - interval / once：只要 now >= next_fire_at 即 fire（与 Python due_triggers 对齐）；
// - calendar：now >= next_fire_at 且 (now-next) < 1 个周期 → 补发/准时 fire，否则仅推进。
func EvaluateDue(def Definition, now time.Time) (DueDecision, Definition) {
	if !def.Enabled || def.NextFireAt == nil {
		return DueNotReady, def
	}
	nextAt := unixFloatToTime(*def.NextFireAt)
	if now.Before(nextAt) {
		return DueNotReady, def
	}
	kind, _ := InferScheduleKind(def.Condition)
	if kind == ScheduleInterval || kind == ScheduleOnce {
		return DueFire, def
	}
	period := SchedulePeriod(def.Condition, nextAt)
	if period <= 0 {
		return DueFire, def
	}
	gap := now.Sub(nextAt)
	if gap >= period {
		updated := def.RescheduleNextFire(now)
		return DueAdvanceOnly, updated
	}
	return DueFire, def
}

// RescheduleNextFire 仅重算 next_fire_at（不修改 fire_count / last_fired_at）。
func (d Definition) RescheduleNextFire(now time.Time) Definition {
	kind, _ := InferScheduleKind(d.Condition)
	var next *time.Time
	switch kind {
	case ScheduleCalendar:
		spec, err := ParseScheduleSpec(d.Condition)
		if err == nil {
			t := NextCalendarFire(spec, now)
			next = &t
		}
	default:
		next = ComputeNextFireTime(d, now)
	}
	if next == nil {
		d.NextFireAt = nil
	} else {
		v := timeToUnixFloat(*next)
		d.NextFireAt = &v
	}
	d.UpdatedAt = timeToUnixFloat(now)
	return d
}
