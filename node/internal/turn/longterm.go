package turn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	LongTermScopeGlobal = "global"
	LongTermScopeAgent  = "agent"
)

// ErrLongTermVersionConflict 表示长期记忆在读取与写入之间已被其他操作修改。
var ErrLongTermVersionConflict = errors.New("long-term memory version conflict")

// LongTermEntry 为单条长期记忆。
type LongTermEntry struct {
	ID        string
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LongTermSnapshot 为读取长期记忆时的条目与版本快照（用于乐观锁）。
type LongTermSnapshot struct {
	Scope   string
	Entries []LongTermEntry
	Version time.Time
}

// LongTermStore 持久化 Agent 长期记忆（通常写入 agents.db）。
type LongTermStore interface {
	ReadLongTerm(ctx context.Context) (LongTermSnapshot, error)
	SaveLongTerm(ctx context.Context, entries []LongTermEntry, expectedVersion time.Time) error
}

// LongTermScopeSetter is an optional capability for stores whose scope can be
// changed while the owning Agent runtime stays loaded. A Turn still freezes
// the model-visible prompt; the new scope is used by the next Turn or by an
// explicit memory reload boundary.
type LongTermScopeSetter interface {
	SetLongTermScope(scope string)
}

// FormatLongTermEntries 将条目格式化为注入 prompt 的文本。
func FormatLongTermEntries(entries []LongTermEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		content := strings.TrimSpace(e.Content)
		if content == "" {
			continue
		}
		date := longTermEntryDate(e)
		if date == "" {
			fmt.Fprintf(&b, "- [%s] %s\n", strings.TrimSpace(e.ID), content)
			continue
		}
		fmt.Fprintf(&b, "- [%s] [%s] %s\n", strings.TrimSpace(e.ID), date, content)
	}
	return strings.TrimSpace(b.String())
}

// longTermEntryDate returns the date of the latest version of an entry.
// UpdatedAt is preferred so replacements expose when the remembered fact was
// last changed; legacy entries with only CreatedAt still get a stable date.
func longTermEntryDate(entry LongTermEntry) string {
	when := entry.UpdatedAt
	if when.IsZero() {
		when = entry.CreatedAt
	}
	if when.IsZero() {
		return ""
	}
	return when.UTC().Format("20060102")
}

// ApplyRememberActionToEntries 根据 LLM 给出的 action 更新条目列表。
func ApplyRememberActionToEntries(entries []LongTermEntry, action, actionContent, replaceTarget string) []LongTermEntry {
	now := time.Now().UTC()
	actionContent = strings.TrimSpace(actionContent)
	replaceTarget = strings.TrimSpace(replaceTarget)
	if actionContent == "" {
		return entries
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "replace":
		if replaceTarget == "" {
			return []LongTermEntry{NewLongTermEntry(actionContent, now)}
		}
		out := append([]LongTermEntry(nil), entries...)
		for i, e := range out {
			if strings.TrimSpace(e.ID) == replaceTarget {
				out[i].Content = actionContent
				out[i].UpdatedAt = now
				return out
			}
		}
		for i, e := range out {
			if strings.Contains(e.Content, replaceTarget) {
				out[i].Content = strings.Replace(e.Content, replaceTarget, actionContent, 1)
				out[i].UpdatedAt = now
				return out
			}
		}
		return append(out, NewLongTermEntry(actionContent, now))
	default:
		return append(append([]LongTermEntry(nil), entries...), NewLongTermEntry(actionContent, now))
	}
}

// EntriesFromFormattedConflict 将冲突合并文本解析为条目（HITL keep_both）。
func EntriesFromFormattedConflict(text string) []LongTermEntry {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n\n")
	now := time.Now().UTC()
	out := make([]LongTermEntry, 0, len(parts))
	for _, part := range parts {
		content, entryDate, hasDate := parseLongTermEntryPrefix(part)
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		entry := NewLongTermEntry(content, now)
		if hasDate {
			entry.CreatedAt = entryDate
			entry.UpdatedAt = entryDate
		}
		out = append(out, entry)
	}
	return out
}

// NewLongTermEntry 创建带 ID 的新条目。
func NewLongTermEntry(content string, now time.Time) LongTermEntry {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return LongTermEntry{
		ID:        newLongTermEntryID(),
		Content:   strings.TrimSpace(content),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func parseLongTermEntryPrefix(line string) (string, time.Time, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- [") {
		return line, time.Time{}, false
	}
	close := strings.Index(line, "] ")
	if close < 0 {
		return line, time.Time{}, false
	}
	line = strings.TrimSpace(line[close+2:])
	if len(line) >= 10 && strings.HasPrefix(line, "[") && line[9] == ']' && isYYYYMMDD(line[1:9]) {
		if entryDate, err := time.ParseInLocation("20060102", line[1:9], time.UTC); err == nil {
			return strings.TrimSpace(line[10:]), entryDate, true
		}
	}
	return line, time.Time{}, false
}

func isYYYYMMDD(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func newLongTermEntryID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "lt-" + hex.EncodeToString(b[:])
}
