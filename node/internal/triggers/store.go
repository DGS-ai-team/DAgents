package triggers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

var errTriggerNotFound = errors.New("trigger not found")

// Store 触发器 JSON 持久化（内存索引 + 原子写盘）。
type Store struct {
	path         string
	historyLimit int
	mu           sync.RWMutex
	triggers     map[string]Definition
	history      []FireRecord
	pending      *pendingDelivery
	logger       *slog.Logger
}

// OpenStore 加载或初始化 triggers.json。

// 逻辑：
// 1. 解析 path 并尝试读盘；
// 2. 文件不存在则空表启动；
// 3. history_limit 默认 200。
func OpenStore(path string, historyLimit int) (*Store, error) {
	if historyLimit <= 0 {
		historyLimit = 200
	}
	s := &Store{
		path:         path,
		historyLimit: historyLimit,
		triggers:     make(map[string]Definition),
		pending:      newPendingDelivery(),
		logger:       logx.Discard(),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// SetLogger 注入结构化日志；nil 时丢弃输出（单测默认）。
func (s *Store) SetLogger(logger *slog.Logger) {
	if s == nil {
		return
	}
	s.logger = discardLogger(logger)
}

func (s *Store) ListTriggers() []Definition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Definition, 0, len(s.triggers))
	for _, item := range s.triggers {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].TriggerID < out[j].TriggerID
	})
	return out
}

func (s *Store) GetTrigger(id string) (*Definition, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.triggers[id]
	if !ok {
		return nil, false
	}
	copy := item
	return &copy, true
}

func (s *Store) CreateTrigger(def Definition) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.triggers[def.TriggerID]; exists {
		return Definition{}, fmt.Errorf("trigger already exists: %s", def.TriggerID)
	}
	s.triggers[def.TriggerID] = def
	if err := s.saveLocked(); err != nil {
		return Definition{}, err
	}
	s.logCreated(def)
	return def, nil
}

func (s *Store) UpdateTrigger(id string, patch UpdatePatch, now time.Time) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.triggers[id]
	if !ok {
		return Definition{}, errTriggerNotFound
	}
	if patch.Name != nil {
		current.Name = *patch.Name
	}
	if patch.TaskTemplate != nil {
		current.TaskTemplate = *patch.TaskTemplate
	}
	if patch.Condition != nil {
		if _, err := EnsureScheduleCondition(patch.Condition); err != nil {
			return Definition{}, err
		}
		current.Condition = cloneMap(patch.Condition)
	}
	if patch.TargetAgentID != nil {
		current.TargetAgentID = *patch.TargetAgentID
	}
	if patch.TargetSessionID != nil {
		current.TargetSessionID = copyStringPtr(patch.TargetSessionID)
	}
	if patch.ClientID != nil {
		current.ClientID = copyStringPtr(patch.ClientID)
	}
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	updated := current.WithNextFire(now)
	s.triggers[id] = updated
	if err := s.saveLocked(); err != nil {
		return Definition{}, err
	}
	s.logUpdated(updated)
	return updated, nil
}

func (s *Store) DeleteTrigger(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.triggers[id]; !ok {
		return false
	}
	delete(s.triggers, id)
	_ = s.saveLocked()
	s.logDeleted(id)
	return true
}

// ListEnabledTriggers 返回 enabled 触发器快照（调度 tick 用）。
func (s *Store) ListEnabledTriggers() []Definition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Definition, 0, len(s.triggers))
	for _, item := range s.triggers {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out
}

// ReplaceTrigger 覆盖写入触发器定义（用于仅推进 next_fire_at 等内部调度）。
func (s *Store) ReplaceTrigger(def Definition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.triggers[def.TriggerID]; !ok {
		return errTriggerNotFound
	}
	s.triggers[def.TriggerID] = def
	return s.saveLocked()
}

func (s *Store) MarkFired(id string, firedAt time.Time) (Definition, error) {
	current := float64(firedAt.UnixNano()) / 1e9
	s.mu.Lock()
	defer s.mu.Unlock()
	trigger, ok := s.triggers[id]
	if !ok {
		return Definition{}, errTriggerNotFound
	}
	trigger.FireCount++
	trigger.LastFiredAt = &current
	kind, _ := InferScheduleKind(trigger.Condition)
	if kind == ScheduleOnce {
		trigger.Enabled = false
	}
	updated := trigger.WithNextFire(firedAt)
	s.triggers[id] = updated
	if err := s.saveLocked(); err != nil {
		return Definition{}, err
	}
	return updated, nil
}

// HasPendingDelivery 判断 trigger 是否仍有未 Apply 的 side-effect 投递。
func (s *Store) HasPendingDelivery(triggerID string) bool {
	if s == nil || s.pending == nil {
		return false
	}
	return s.pending.HasPendingDelivery(triggerID)
}

// MarkPendingDelivery 标记 trigger 消息已入队待消费。
func (s *Store) MarkPendingDelivery(triggerID string) {
	if s != nil && s.pending != nil {
		s.pending.MarkPendingDelivery(triggerID)
	}
}

// ClearPendingDelivery 在 side-effect Apply 成功或 ClearSession 丢弃缓冲时清除待消费标记。
func (s *Store) ClearPendingDelivery(triggerID string) {
	if s != nil && s.pending != nil {
		s.pending.ClearPendingDelivery(triggerID)
	}
}

func (s *Store) AddHistory(record FireRecord) FireRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, record)
	if len(s.history) > s.historyLimit {
		s.history = s.history[len(s.history)-s.historyLimit:]
	}
	_ = s.saveLocked()
	return record
}

func (s *Store) ListHistory(triggerID string) []FireRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FireRecord, 0)
	for _, record := range s.history {
		if triggerID == "" || record.TriggerID == triggerID {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].FiredAt > out[j].FiredAt
	})
	return out
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read triggers store: %w", err)
	}
	var payload struct {
		Triggers []Definition `json:"triggers"`
		History  []FireRecord `json:"history"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("parse triggers store: %w", err)
	}
	s.triggers = make(map[string]Definition, len(payload.Triggers))
	now := time.Now()
	for _, item := range payload.Triggers {
		if item.Enabled && item.NextFireAt == nil {
			item = item.WithNextFire(now)
		}
		s.triggers[item.TriggerID] = item
	}
	s.history = payload.History
	if s.history == nil {
		s.history = []FireRecord{}
	}
	return nil
}

func (s *Store) saveLocked() error {
	triggers := make([]Definition, 0, len(s.triggers))
	for _, item := range s.triggers {
		triggers = append(triggers, item)
	}
	sort.Slice(triggers, func(i, j int) bool {
		if triggers[i].CreatedAt != triggers[j].CreatedAt {
			return triggers[i].CreatedAt < triggers[j].CreatedAt
		}
		return triggers[i].TriggerID < triggers[j].TriggerID
	})
	payload := map[string]any{
		"history":  s.history,
		"triggers": triggers,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// IsNotFound 判断 store 层 not found 错误。
func IsNotFound(err error) bool {
	return errors.Is(err, errTriggerNotFound)
}
