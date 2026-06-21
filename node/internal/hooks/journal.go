package hooks

import (
	"strings"
	"sync"
)

// ExecutionJournal 记录 TurnID+Phase+HookName 执行痕迹，用于 resume 时跳过重复副作用。
type ExecutionJournal interface {
	HasExecuted(turnID string, phase Phase, hookName string) bool
	MarkExecuted(turnID string, phase Phase, hookName string)
}

// NoopExecutionJournal 不记录、不跳过；默认 fail-open 观测类 hook 使用。
type NoopExecutionJournal struct{}

func (NoopExecutionJournal) HasExecuted(string, Phase, string) bool { return false }
func (NoopExecutionJournal) MarkExecuted(string, Phase, string)     {}

// MemoryExecutionJournal 进程内幂等表（单测与无持久化场景）。
type MemoryExecutionJournal struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

// NewMemoryExecutionJournal 构造内存 journal。
func NewMemoryExecutionJournal() *MemoryExecutionJournal {
	return &MemoryExecutionJournal{keys: make(map[string]struct{})}
}

func (j *MemoryExecutionJournal) HasExecuted(turnID string, phase Phase, hookName string) bool {
	if j == nil {
		return false
	}
	key := executionJournalKey(turnID, phase, hookName)
	j.mu.Lock()
	defer j.mu.Unlock()
	_, ok := j.keys[key]
	return ok
}

func (j *MemoryExecutionJournal) MarkExecuted(turnID string, phase Phase, hookName string) {
	if j == nil {
		return
	}
	key := executionJournalKey(turnID, phase, hookName)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.keys == nil {
		j.keys = make(map[string]struct{})
	}
	j.keys[key] = struct{}{}
}

func executionJournalKey(turnID string, phase Phase, hookName string) string {
	return strings.TrimSpace(turnID) + "\x00" + string(phase) + "\x00" + strings.TrimSpace(hookName)
}
