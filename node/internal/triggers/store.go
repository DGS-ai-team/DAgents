package triggers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/google/uuid"
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
	// A durable pending delivery cannot be proven to have reached the runtime
	// after restart. Fail closed: pause it and require explicit recovery.
	s.mu.Lock()
	dirty := false
	for id, d := range s.triggers {
		if d.PendingDeliveryID != nil && *d.PendingDeliveryID != "" && !d.RecoveryRequired {
			d.Enabled = false
			d.RecoveryRequired = true
			d.RecoveryReason = "pending delivery requires recovery after restart"
			s.triggers[id] = d
			dirty = true
		}
	}
	if dirty {
		_ = s.saveLocked()
	}
	s.mu.Unlock()
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
		if ConditionCmd(patch.Condition) != "" {
			return Definition{}, fmt.Errorf("condition.cmd is not supported")
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
		if *patch.Enabled && current.RecoveryRequired {
			return Definition{}, fmt.Errorf("trigger requires recovery before enabling")
		}
		current.Enabled = *patch.Enabled
	}
	if patch.SessionTargetMode != nil {
		if !ValidSessionTargetMode(*patch.SessionTargetMode) {
			return Definition{}, fmt.Errorf("invalid session_target_mode: %s", *patch.SessionTargetMode)
		}
		current.SessionTargetMode = *patch.SessionTargetMode
	}
	updated := current.WithNextFire(now)
	s.triggers[id] = updated
	if err := s.saveLocked(); err != nil {
		return Definition{}, err
	}
	s.logUpdated(updated)
	return updated, nil
}

// UpdateManagedConfig changes only scheduler fields owned by a managed Goal.
// It preserves delivery/claim state and the Goal's exact next wake time.
func (s *Store) UpdateManagedConfig(id string, interval int, enabled bool, task string, nextFireAt *float64, now time.Time) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.triggers[id]
	if !ok {
		return Definition{}, errTriggerNotFound
	}
	if interval < 60 {
		return Definition{}, fmt.Errorf("managed interval must be >= 60 seconds")
	}
	if enabled && cur.RecoveryRequired {
		return Definition{}, fmt.Errorf("trigger requires recovery before enabling")
	}
	old := cur
	cur.Condition = map[string]any{"interval_seconds": interval}
	cur.Enabled = enabled
	cur.TaskTemplate = task
	if nextFireAt == nil {
		cur.NextFireAt = nil
	} else {
		v := *nextFireAt
		cur.NextFireAt = &v
	}
	cur.UpdatedAt = timeToUnixFloat(now)
	s.triggers[id] = cur
	if err := s.saveLocked(); err != nil {
		s.triggers[id] = old
		return Definition{}, err
	}
	s.logUpdated(cur)
	return cur, nil
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

// UpdateNextFireAt changes only scheduler time, preserving delivery/claim
// fields that may have changed concurrently.
func (s *Store) UpdateNextFireAt(id string, at *float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[id]
	if !ok {
		return errTriggerNotFound
	}
	old := d
	if at == nil {
		d.NextFireAt = nil
	} else {
		v := *at
		d.NextFireAt = &v
	}
	d.UpdatedAt = float64(time.Now().UnixNano()) / 1e9
	s.triggers[id] = d
	if err := s.saveLocked(); err != nil {
		s.triggers[id] = old
		return err
	}
	return nil
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

// ClaimDelivery durably claims one delivery before it is placed in InputBox.
func (s *Store) ClaimDelivery(triggerID, deliveryID, sessionID string) error {
	return s.claimDelivery(triggerID, deliveryID, sessionID, nil)
}

func (s *Store) ClaimDeliveryForOccurrence(triggerID, deliveryID, sessionID string, occurrence *float64) error {
	return s.claimDelivery(triggerID, deliveryID, sessionID, occurrence)
}

func (s *Store) claimDelivery(triggerID, deliveryID, sessionID string, occurrence *float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("delivery store unavailable: %w", err)
	}
	d, ok := s.triggers[triggerID]
	if !ok {
		return errTriggerNotFound
	}
	if d.PendingDeliveryID != nil && *d.PendingDeliveryID != "" {
		return fmt.Errorf("delivery already pending")
	}
	if occurrence != nil && (d.NextFireAt == nil || *d.NextFireAt != *occurrence) {
		return fmt.Errorf("stale trigger occurrence")
	}
	deliveryID, sessionID = strings.TrimSpace(deliveryID), strings.TrimSpace(sessionID)
	if deliveryID == "" || sessionID == "" {
		return fmt.Errorf("delivery identity and session are required")
	}
	old := d
	if occurrence != nil {
		// Reserve the scheduled occurrence in the same durable claim. An old
		// scheduler snapshot can no longer claim it after a fast acknowledgement.
		d = d.WithNextFire(time.Now())
	}
	d.PendingDeliveryID = &deliveryID
	d.PendingSessionID = &sessionID
	s.triggers[triggerID] = d
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		return err
	}
	return nil
}

func (s *Store) ClearPendingDeliveryIfMatch(triggerID, deliveryID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[triggerID]
	if !ok || d.PendingDeliveryID == nil || *d.PendingDeliveryID != strings.TrimSpace(deliveryID) {
		return
	}
	old := d
	d.PendingDeliveryID = nil
	d.PendingSessionID = nil
	s.triggers[triggerID] = d
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		return
	}
	if s.pending != nil {
		s.pending.ClearPendingDelivery(triggerID)
	}
}

func (s *Store) IsRecoveryRequired(triggerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.triggers[triggerID]
	return ok && d.RecoveryRequired
}

// IsPendingDelivery reports whether this exact delivery is still the durable
// owner of the trigger. It fences envelopes that were restored after an
// explicit recovery action cleared the old delivery.
func (s *Store) IsPendingDelivery(triggerID, deliveryID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.triggers[triggerID]
	return ok && !d.RecoveryRequired && d.PendingDeliveryID != nil && strings.TrimSpace(*d.PendingDeliveryID) == strings.TrimSpace(deliveryID)
}

// RecoverPendingDelivery discards the old delivery after fencing by identity.
// It deliberately leaves the trigger disabled; enabling is a separate action.
func (s *Store) RecoverPendingDelivery(triggerID, deliveryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[triggerID]
	if !ok {
		return errTriggerNotFound
	}
	if !d.RecoveryRequired || d.PendingDeliveryID == nil || *d.PendingDeliveryID != strings.TrimSpace(deliveryID) {
		return fmt.Errorf("recovery delivery identity mismatch")
	}
	old := d
	oldHistory := append([]FireRecord(nil), s.history...)
	sessionID := d.PendingSessionID
	d.PendingDeliveryID = nil
	d.PendingSessionID = nil
	d.RecoveryRequired = false
	d.RecoveryReason = ""
	d.Enabled = false
	s.triggers[triggerID] = d
	s.history = append(s.history, FireRecord{FireID: uuid.NewString(), TriggerID: triggerID, DeliveryID: strings.TrimSpace(deliveryID), Status: FireStatusSkipped, Reason: "recovery", SessionID: sessionID, Message: "pending delivery discarded during recovery", FiredAt: float64(time.Now().UnixNano()) / 1e9})
	if len(s.history) > s.historyLimit {
		s.history = s.history[len(s.history)-s.historyLimit:]
	}
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		s.history = oldHistory
		return err
	}
	if s.pending != nil {
		s.pending.ClearPendingDelivery(triggerID)
	}
	return nil
}

// ClearPendingDelivery 在 side-effect Apply 成功或 ClearSession 丢弃缓冲时清除待消费标记。
func (s *Store) ClearPendingDelivery(triggerID string) {
	if s != nil {
		s.mu.Lock()
		if d, ok := s.triggers[triggerID]; ok && d.PendingDeliveryID != nil {
			previous := d
			d.PendingDeliveryID = nil
			d.PendingSessionID = nil
			s.triggers[triggerID] = d
			if err := s.saveLocked(); err != nil {
				s.triggers[triggerID] = previous
			} else if s.pending != nil {
				s.pending.ClearPendingDelivery(triggerID)
			}
		} else if s.pending != nil {
			s.pending.ClearPendingDelivery(triggerID)
		}
		s.mu.Unlock()
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
	for _, item := range payload.Triggers {
		if item.Enabled && item.NextFireAt == nil {
			return fmt.Errorf("trigger %q is enabled but next_fire_at is missing", item.TriggerID)
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
