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
var ErrRevisionConflict = errors.New("revision conflict")

func (s *Store) CreateAuthorized(p Principal, in CreateInput, now time.Time) (Definition, error) {
	if p.Kind != "admin" && p.Kind != "agent" {
		return Definition{}, errTriggerNotFound
	}
	if p.Kind == "agent" {
		if strings.TrimSpace(in.TargetAgentID) == "" {
			in.TargetAgentID = p.AgentID
		}
		if strings.TrimSpace(in.TargetAgentID) != strings.TrimSpace(p.AgentID) {
			return Definition{}, errTriggerNotFound
		}
	}
	def, err := NewDefinitionFromCreate(in, in.TargetAgentID, now)
	if err != nil {
		return Definition{}, err
	}
	def.OwnerAgentID = strings.TrimSpace(in.TargetAgentID)
	def.Controller, def.ControllerID = "user", def.OwnerAgentID
	def.CreatedBy = strings.TrimSpace(p.ID)
	if def.CreatedBy == "" {
		def.CreatedBy = strings.TrimSpace(p.AgentID)
	}
	created, err := s.CreateTrigger(def)
	if err != nil {
		s.logAuthorization(p, def, "create", "denied", err.Error())
		return Definition{}, err
	}
	s.logAuthorization(p, created, "create", "allowed", "")
	return created, nil
}

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

func cloneDefinition(in Definition) Definition {
	out := in
	out.Condition = cloneMap(in.Condition)
	cloneFloat := func(v *float64) *float64 {
		if v == nil {
			return nil
		}
		x := *v
		return &x
	}
	out.NextFireAt = cloneFloat(in.NextFireAt)
	out.LastFiredAt = cloneFloat(in.LastFiredAt)
	if in.TargetSessionID != nil {
		v := *in.TargetSessionID
		out.TargetSessionID = &v
	}
	if in.ClientID != nil {
		v := *in.ClientID
		out.ClientID = &v
	}
	if in.PendingDeliveryID != nil {
		v := *in.PendingDeliveryID
		out.PendingDeliveryID = &v
	}
	if in.PendingSessionID != nil {
		v := *in.PendingSessionID
		out.PendingSessionID = &v
	}
	out.PendingOccurrence = cloneFloat(in.PendingOccurrence)
	out.PendingConditionReason = in.PendingConditionReason
	out.PendingConditionContent = in.PendingConditionContent
	out.PendingConditionApproved = in.PendingConditionApproved
	out.PendingConditionPayload = cloneMap(in.PendingConditionPayload)
	return out
}

func cloneFireRecord(in FireRecord) FireRecord {
	out := in
	if in.SessionID != nil {
		v := *in.SessionID
		out.SessionID = &v
	}
	if in.ClientID != nil {
		v := *in.ClientID
		out.ClientID = &v
	}
	if in.Payload != nil {
		out.Payload = cloneAny(in.Payload).(map[string]any)
	}
	return out
}

func cloneAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, v := range x {
			m[k] = cloneAny(v)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, v := range x {
			a[i] = cloneAny(v)
		}
		return a
	default:
		return v
	}
}

type Principal struct{ Kind, ID, AgentID string }

func (s *Store) ListAuthorized(p Principal) []Definition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Definition, 0, len(s.triggers))
	for _, d := range s.triggers {
		if p.Kind == "admin" || p.canOwn(d) {
			out = append(out, cloneDefinition(d))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].TriggerID < out[j].TriggerID
	})
	return out
}

// ValidateOwners disables triggers whose owner/controller cannot be proven.
func (s *Store) ValidateOwners(validAgents map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := make(map[string]Definition, len(s.triggers))
	for id, d := range s.triggers {
		old[id] = d
	}
	dirty := false
	for id, d := range s.triggers {
		reason := ""
		if strings.TrimSpace(d.OwnerAgentID) == "" || !validAgents[strings.TrimSpace(d.OwnerAgentID)] {
			reason = "trigger owner is unavailable"
		}
		if reason == "" && (strings.TrimSpace(d.TargetAgentID) == "" || !validAgents[strings.TrimSpace(d.TargetAgentID)]) {
			reason = "trigger target agent is unavailable"
		}
		if d.Controller != "user" && d.Controller != "auto" {
			reason = "trigger controller is invalid"
		}
		if reason == "" && d.Controller == "auto" && (strings.TrimSpace(d.ControllerID) == "" || d.ControllerID != d.OwnerAgentID || d.TargetAgentID != d.OwnerAgentID || d.TargetSessionID == nil || *d.TargetSessionID != d.OwnerAgentID || d.SessionTargetMode != SessionTargetFixed) {
			reason = "auto controller association is invalid"
		}
		if reason != "" {
			d.Enabled = false
			d.RecoveryRequired = true
			d.RecoveryReason = reason
			s.triggers[id] = d
			dirty = true
		}
	}
	if dirty {
		if err := s.saveLocked(); err != nil {
			s.triggers = old
			return err
		}
	}
	return nil
}

func (s *Store) GetAuthorized(p Principal, id string) (Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.triggers[id]
	if !ok || (p.Kind != "admin" && !p.canOwn(d)) {
		return Definition{}, errTriggerNotFound
	}
	return cloneDefinition(d), nil
}

func (p Principal) canOwn(d Definition) bool {
	if p.Kind == "admin" {
		return true
	}
	return p.Kind == "agent" && strings.TrimSpace(p.AgentID) != "" && strings.TrimSpace(d.OwnerAgentID) == strings.TrimSpace(p.AgentID)
}

// UpdateAuthorized performs ownership/controller/revision checks while holding
// the store lock. Agent principals can only modify their own agent-controlled
// triggers; managed controllers are reserved for the owning Goal.
func (s *Store) UpdateAuthorized(p Principal, id string, expected int64, patch UpdatePatch, now time.Time) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.triggers[id]
	if !ok {
		s.logAuthorization(p, Definition{TriggerID: id}, "update", "denied", "not_found")
		return Definition{}, errTriggerNotFound
	}
	deny := func(err error, reason string) (Definition, error) {
		s.logAuthorization(p, cur, "update", "denied", reason)
		return Definition{}, err
	}
	if !p.canOwn(cur) || cur.Controller != "user" {
		return deny(errTriggerNotFound, "not_owner_or_controller")
	}
	if expected > 0 && cur.Revision != expected {
		return deny(ErrRevisionConflict, "revision_conflict")
	}
	if p.Kind == "agent" && patch.TargetAgentID != nil && strings.TrimSpace(*patch.TargetAgentID) != strings.TrimSpace(cur.TargetAgentID) {
		return deny(errTriggerNotFound, "target_mismatch")
	}
	if p.Kind == "agent" && strings.TrimSpace(cur.TargetAgentID) != strings.TrimSpace(cur.OwnerAgentID) {
		return deny(errTriggerNotFound, "owner_target_mismatch")
	}
	old := cur
	if err := applyUpdatePatch(&cur, patch); err != nil {
		return deny(err, "invalid_patch")
	}
	cur.Revision++
	cur = cur.WithNextFire(now)
	s.triggers[id] = cur
	if err := s.saveLocked(); err != nil {
		s.triggers[id] = old
		return deny(err, "persist_failed")
	}
	s.logAuthorization(p, cur, "update", "allowed", "")
	return cloneDefinition(cur), nil
}

func applyUpdatePatch(current *Definition, patch UpdatePatch) error {
	if patch.Name != nil {
		current.Name = *patch.Name
	}
	if patch.TaskTemplate != nil {
		current.TaskTemplate = *patch.TaskTemplate
	}
	if patch.Condition != nil {
		if _, err := EnsureScheduleCondition(patch.Condition); err != nil {
			return err
		}
		current.Condition = cloneMap(patch.Condition)
	}
	if patch.TargetAgentID != nil {
		current.TargetAgentID = strings.TrimSpace(*patch.TargetAgentID)
	}
	if patch.TargetSessionID != nil {
		current.TargetSessionID = copyStringPtr(patch.TargetSessionID)
	}
	if patch.ClientID != nil {
		current.ClientID = copyStringPtr(patch.ClientID)
	}
	if patch.Enabled != nil {
		if *patch.Enabled && current.RecoveryRequired {
			return fmt.Errorf("trigger requires recovery before enabling")
		}
		current.Enabled = *patch.Enabled
	}
	if patch.SessionTargetMode != nil {
		if !ValidSessionTargetMode(*patch.SessionTargetMode) {
			return fmt.Errorf("invalid session_target_mode: %s", *patch.SessionTargetMode)
		}
		current.SessionTargetMode = *patch.SessionTargetMode
	}
	return nil
}

func (s *Store) DeleteAuthorized(p Principal, id string, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[id]
	if !ok || !p.canOwn(d) || d.Controller != "user" {
		s.logAuthorization(p, d, "delete", "denied", "not_owner_or_controller")
		return errTriggerNotFound
	}
	if p.Kind == "agent" && strings.TrimSpace(d.TargetAgentID) != strings.TrimSpace(d.OwnerAgentID) {
		s.logAuthorization(p, d, "delete", "denied", "owner_target_mismatch")
		return errTriggerNotFound
	}
	if expected > 0 && d.Revision != expected {
		s.logAuthorization(p, d, "delete", "denied", "revision_conflict")
		return ErrRevisionConflict
	}
	delete(s.triggers, id)
	if err := s.saveLocked(); err != nil {
		s.triggers[id] = d
		return err
	}
	s.logAuthorization(p, d, "delete", "allowed", "")
	return nil
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
		if err := s.saveLocked(); err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("persist recovery state: %w", err)
		}
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
		out = append(out, cloneDefinition(item))
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
	copy := cloneDefinition(item)
	return &copy, true
}

func samePendingOccurrence(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// BeginConditionCompletion atomically fences an approval completion. The
// pending delivery remains durable until the session acknowledges it.
func (s *Store) BeginConditionCompletion(req ConditionCompletion) (Definition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.triggers[strings.TrimSpace(req.TriggerID)]
	if !ok || cur.RecoveryRequired || !cur.Enabled || strings.TrimSpace(cur.PendingConditionReason) == "" || cur.PendingDeliveryID == nil || strings.TrimSpace(*cur.PendingDeliveryID) != strings.TrimSpace(req.DeliveryID) || cur.PendingSessionID == nil || strings.TrimSpace(*cur.PendingSessionID) != strings.TrimSpace(req.SessionID) || strings.TrimSpace(cur.TargetAgentID) != strings.TrimSpace(req.AgentID) || cur.Revision != req.Revision || !samePendingOccurrence(cur.PendingOccurrence, req.Occurrence) {
		return Definition{}, false, fmt.Errorf("condition completion identity conflict")
	}
	if cur.PendingConditionApproved {
		return cloneDefinition(cur), false, nil
	}
	old := cur
	result := cloneDefinition(cur)
	if !req.Matched {
		cur.PendingDeliveryID, cur.PendingSessionID, cur.PendingOccurrence = nil, nil, nil
		cur.PendingConditionReason, cur.PendingConditionContent, cur.PendingConditionPayload = "", "", nil
	} else {
		cur.PendingConditionApproved = true
	}
	s.triggers[cur.TriggerID] = cur
	if err := s.saveLocked(); err != nil {
		s.triggers[cur.TriggerID] = old
		return Definition{}, false, err
	}
	if !req.Matched && s.pending != nil {
		s.pending.ClearPendingDelivery(cur.TriggerID)
	}
	if !req.Matched {
		return result, true, nil
	}
	return cloneDefinition(cur), true, nil
}

func (s *Store) ResetConditionCompletion(triggerID, deliveryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.triggers[triggerID]
	if !ok || cur.PendingDeliveryID == nil || *cur.PendingDeliveryID != strings.TrimSpace(deliveryID) {
		return fmt.Errorf("condition completion identity conflict")
	}
	old := cur
	cur.PendingConditionApproved = false
	s.triggers[triggerID] = cur
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		return err
	}
	return nil
}

func (s *Store) SetPendingCondition(triggerID, deliveryID, reason, content string, payload map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.triggers[triggerID]
	if !ok || cur.PendingDeliveryID == nil || strings.TrimSpace(*cur.PendingDeliveryID) != strings.TrimSpace(deliveryID) {
		return fmt.Errorf("condition completion identity conflict")
	}
	old := cur
	cur.PendingConditionReason, cur.PendingConditionContent, cur.PendingConditionPayload = reason, content, cloneMap(payload)
	s.triggers[triggerID] = cur
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		return err
	}
	return nil
}

func (s *Store) CreateTrigger(def Definition) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	def = cloneDefinition(def)
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
	if current.Controller == "auto" {
		return Definition{}, fmt.Errorf("auto default trigger is system managed")
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
	if current.Revision < 1 {
		current.Revision = 1
	}
	current.Revision++
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

// UpsertManagedProjection atomically updates only the projection-owned fields,
// preserving delivery counters and fencing older intent generations.
func (s *Store) UpsertManagedProjection(def Definition) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.triggers[def.TriggerID]
	if ok {
		if cur.ManagedGoalID != def.ManagedGoalID || cur.OwnerAgentID != def.OwnerAgentID || cur.Controller != "goal" || cur.ControllerID != def.ControllerID {
			return Definition{}, errTriggerNotFound
		}
		if cur.ManagedGeneration > def.ManagedGeneration {
			return Definition{}, ErrRevisionConflict
		}
		if cur.ManagedGeneration == def.ManagedGeneration && cur.ManagedFingerprint != "" && cur.ManagedFingerprint != def.ManagedFingerprint {
			return Definition{}, ErrRevisionConflict
		}
		if cur.ManagedGeneration == def.ManagedGeneration && cur.ManagedFingerprint == def.ManagedFingerprint {
			return cloneDefinition(cur), nil
		}
		def.FireCount, def.LastFiredAt, def.PendingDeliveryID, def.PendingSessionID, def.RecoveryRequired, def.RecoveryReason = cur.FireCount, cur.LastFiredAt, cur.PendingDeliveryID, cur.PendingSessionID, cur.RecoveryRequired, cur.RecoveryReason
		if cur.ManagedGeneration < def.ManagedGeneration {
			if cur.PendingDeliveryID != nil && strings.TrimSpace(*cur.PendingDeliveryID) != "" {
				return Definition{}, fmt.Errorf("pending delivery requires recovery")
			}
			def.LastFiredAt = nil
			def.PendingDeliveryID = nil
			def.PendingSessionID = nil
			def.PendingOccurrence = nil
		}
	}
	if ok {
		def.Revision = cur.Revision + 1
	} else if def.Revision < 1 {
		def.Revision = 1
	}
	s.triggers[def.TriggerID] = cloneDefinition(def)
	if err := s.saveLocked(); err != nil {
		if ok {
			s.triggers[def.TriggerID] = cur
		} else {
			delete(s.triggers, def.TriggerID)
		}
		return Definition{}, err
	}
	return cloneDefinition(def), nil
}

// DisableManagedProjection disables a managed projection only when all
// identity and generation metadata still matches the supplied snapshot.
// The compare, mutation, and durable save happen under one store lock.
func (s *Store) DisableManagedProjection(expected Definition) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.triggers[expected.TriggerID]
	if !ok || cur.ManagedGoalID != expected.ManagedGoalID || cur.OwnerAgentID != expected.OwnerAgentID || cur.TargetAgentID != expected.TargetAgentID || !sameStringPtr(cur.TargetSessionID, expected.TargetSessionID) || cur.Controller != "goal" || cur.ControllerID != expected.ControllerID || cur.ManagedIntentID != expected.ManagedIntentID || cur.ManagedGeneration != expected.ManagedGeneration || cur.ManagedFingerprint != expected.ManagedFingerprint {
		return Definition{}, ErrRevisionConflict
	}
	if !cur.Enabled && cur.NextFireAt == nil {
		return cloneDefinition(cur), nil
	}
	old := cur
	cur.Enabled = false
	cur.NextFireAt = nil
	cur.Revision++
	s.triggers[cur.TriggerID] = cur
	if err := s.saveLocked(); err != nil {
		s.triggers[cur.TriggerID] = old
		return Definition{}, err
	}
	return cloneDefinition(cur), nil
}

func sameStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *Store) DeleteTrigger(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[id]
	if !ok || d.Controller == "auto" {
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

// ClaimDeliveryForOccurrenceRevision fences a scheduler snapshot against a
// definition edit made after the snapshot was read.
func (s *Store) ClaimDeliveryForOccurrenceRevision(triggerID string, revision int64, deliveryID, sessionID string, occurrence *float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[triggerID]
	if !ok {
		return errTriggerNotFound
	}
	if d.Revision != revision {
		return fmt.Errorf("revision conflict")
	}
	return s.claimDeliveryLocked(triggerID, deliveryID, sessionID, occurrence)
}

func (s *Store) ClaimAuthorized(p Principal, triggerID string, expected int64, deliveryID, sessionID string, occurrence *float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[triggerID]
	if !ok || !p.canOwn(d) || d.Controller != "user" {
		s.logAuthorization(p, d, "claim", "denied", "not_owner_or_controller")
		return errTriggerNotFound
	}
	if p.Kind == "agent" && strings.TrimSpace(d.TargetAgentID) != strings.TrimSpace(d.OwnerAgentID) {
		s.logAuthorization(p, d, "claim", "denied", "owner_target_mismatch")
		return errTriggerNotFound
	}
	if expected > 0 && d.Revision != expected {
		s.logAuthorization(p, d, "claim", "denied", "revision_conflict")
		return fmt.Errorf("revision conflict")
	}
	if err := s.claimDeliveryLocked(triggerID, deliveryID, sessionID, occurrence); err != nil {
		s.logAuthorization(p, d, "claim", "denied", "delivery_failed")
		return err
	}
	s.logAuthorization(p, d, "claim", "allowed", "")
	return nil
}

func (s *Store) claimDelivery(triggerID, deliveryID, sessionID string, occurrence *float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimDeliveryLocked(triggerID, deliveryID, sessionID, occurrence)
}

func (s *Store) claimDeliveryLocked(triggerID, deliveryID, sessionID string, occurrence *float64) error {
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
	d.PendingOccurrence = cloneFloatPtr(occurrence)
	s.triggers[triggerID] = d
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		return err
	}
	return nil
}

func (s *Store) clearPendingDeliveryIfMatch(triggerID, deliveryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[triggerID]
	if !ok || d.PendingDeliveryID == nil || *d.PendingDeliveryID != strings.TrimSpace(deliveryID) {
		return nil
	}
	old := d
	d.PendingDeliveryID = nil
	d.PendingSessionID = nil
	d.PendingOccurrence = nil
	d.PendingConditionReason, d.PendingConditionContent, d.PendingConditionPayload = "", "", nil
	d.PendingConditionApproved = false
	s.triggers[triggerID] = d
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		return err
	}
	if s.pending != nil {
		s.pending.ClearPendingDelivery(triggerID)
	}
	return nil
}

// ClearPendingDeliveryIfMatch preserves the delivery tracker interface. The
// scheduler uses the internal error-returning variant when cleanup is part of
// a condition decision and must report persistence failures.
func (s *Store) ClearPendingDeliveryIfMatch(triggerID, deliveryID string) {
	_ = s.clearPendingDeliveryIfMatch(triggerID, deliveryID)
}
func (s *Store) IsRecoveryRequired(triggerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.triggers[triggerID]
	return ok && d.RecoveryRequired
}

// MarkConditionRecovery preserves the claimed delivery while fencing future
// execution after a downstream completion/settlement failure.
func (s *Store) MarkConditionRecovery(triggerID, deliveryID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[triggerID]
	if !ok || d.PendingDeliveryID == nil || *d.PendingDeliveryID != strings.TrimSpace(deliveryID) {
		return fmt.Errorf("condition delivery is stale")
	}
	old := s.triggers[triggerID]
	d.RecoveryRequired = true
	d.Enabled = false
	d.RecoveryReason = strings.TrimSpace(reason)
	s.triggers[triggerID] = d
	if err := s.saveLocked(); err != nil {
		s.triggers[triggerID] = old
		return err
	}
	return nil
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
	if err := s.recoverPendingDeliveryLocked(triggerID, deliveryID); err != nil {
		return err
	}
	return nil
}

func (s *Store) recoverPendingDeliveryLocked(triggerID, deliveryID string) error {
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
	d.PendingOccurrence = nil
	d.PendingConditionReason = ""
	d.PendingConditionContent = ""
	d.PendingConditionPayload = nil
	d.PendingConditionApproved = false
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

func (s *Store) RecoverAuthorized(p Principal, triggerID string, expected int64, deliveryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.triggers[triggerID]
	if !ok || !p.canOwn(d) || (p.Kind == "agent" && d.Controller != "user") {
		s.logAuthorization(p, d, "recover", "denied", "not_owner_or_controller")
		return errTriggerNotFound
	}
	if expected > 0 && d.Revision != expected {
		s.logAuthorization(p, d, "recover", "denied", "revision_conflict")
		return fmt.Errorf("revision conflict")
	}
	if d.Controller != "user" && d.Controller != "auto" {
		s.logAuthorization(p, d, "recover", "denied", "retired_controller")
		return fmt.Errorf("trigger controller is retired or invalid")
	}
	if d.ManagedGoalID != "" {
		s.logAuthorization(p, d, "recover", "denied", "managed_controller")
		return fmt.Errorf("managed goal trigger is controlled by goal")
	}
	if err := s.recoverPendingDeliveryLocked(triggerID, deliveryID); err != nil {
		s.logAuthorization(p, d, "recover", "denied", "recovery_failed")
		return err
	}
	s.logAuthorization(p, d, "recover", "allowed", "")
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
			d.PendingOccurrence = nil
			d.PendingConditionReason, d.PendingConditionContent, d.PendingConditionPayload = "", "", nil
			d.PendingConditionApproved = false
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
	record = cloneFireRecord(record)
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
			out = append(out, cloneFireRecord(record))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].FiredAt > out[j].FiredAt
	})
	return out
}

func (s *Store) HistoryAuthorized(p Principal, triggerID string) ([]FireRecord, error) {
	s.mu.RLock()
	d, ok := s.triggers[triggerID]
	if !ok || !p.canOwn(d) {
		s.mu.RUnlock()
		return nil, errTriggerNotFound
	}
	out := make([]FireRecord, 0)
	for _, record := range s.history {
		if record.TriggerID == triggerID {
			out = append(out, cloneFireRecord(record))
		}
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].FiredAt > out[j].FiredAt })
	s.logAuthorization(p, d, "history", "allowed", "")
	return out, nil
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
		SchemaVersion int          `json:"schema_version"`
		Triggers      []Definition `json:"triggers"`
		History       []FireRecord `json:"history"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("parse triggers store: %w", err)
	}
	if payload.SchemaVersion > 2 {
		return fmt.Errorf("unsupported triggers schema_version %d", payload.SchemaVersion)
	}
	migrated := payload.SchemaVersion < 2
	if migrated && s.path != "" {
		bak := s.path + ".v1.bak"
		if _, e := os.Stat(bak); errors.Is(e, os.ErrNotExist) {
			if e := os.WriteFile(bak, raw, 0o600); e != nil {
				return fmt.Errorf("backup triggers store: %w", e)
			}
		} else if e != nil {
			return fmt.Errorf("check triggers backup: %w", e)
		}
	}
	s.triggers = make(map[string]Definition, len(payload.Triggers))
	for _, item := range payload.Triggers {
		if migrated && item.OwnerAgentID == "" {
			item.OwnerAgentID = item.TargetAgentID
		}
		if migrated && item.ManagedGoalID != "" {
			item.Controller = "goal"
			item.ControllerID = item.ManagedGoalID
		} else if migrated && item.Controller == "" {
			item.Controller = "user"
		}
		if migrated && item.ControllerID == "" {
			item.ControllerID = item.OwnerAgentID
		}
		if item.Revision < 1 {
			item.Revision = 1
		}
		if item.CreatedBy == "" {
			item.CreatedBy = item.OwnerAgentID
		}
		if item.Enabled && item.NextFireAt == nil {
			return fmt.Errorf("trigger %q is enabled but next_fire_at is missing", item.TriggerID)
		}
		s.triggers[item.TriggerID] = item
	}
	s.history = payload.History
	if s.history == nil {
		s.history = []FireRecord{}
	}
	if migrated {
		if err := s.saveLocked(); err != nil {
			return fmt.Errorf("persist migrated triggers store: %w", err)
		}
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
		"schema_version": 2,
		"history":        s.history,
		"triggers":       triggers,
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
