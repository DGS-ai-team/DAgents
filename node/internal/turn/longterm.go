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
		fmt.Fprintf(&b, "- [%s] %s\n", strings.TrimSpace(e.ID), content)
	}
	return strings.TrimSpace(b.String())
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
		content := strings.TrimSpace(stripLongTermEntryPrefix(part))
		if content == "" {
			continue
		}
		out = append(out, NewLongTermEntry(content, now))
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

func stripLongTermEntryPrefix(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- [") {
		return line
	}
	close := strings.Index(line, "] ")
	if close < 0 {
		return line
	}
	return strings.TrimSpace(line[close+2:])
}

func newLongTermEntryID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "lt-" + hex.EncodeToString(b[:])
}
