package triggers

import (
	"fmt"
	"strings"
	"time"
)

const autoDefaultTaskTemplate = "请结合此前对话、当前待办和可访问资源的变化，判断现在是否有值得推进的工作。有则在职责和现有授权范围内执行，并更新待办；没有则结束本轮。不得将未经检查的资源视为没有变化。"
const autoDefaultDisabledInterval int64 = 1800
const autoDefaultName = "默认唤醒"

// AutoDefaultTriggerID returns the stable, process-independent trigger ID for
// an Agent's system-owned Auto wake-up.
func AutoDefaultTriggerID(agentID string) string {
	return "auto-default:" + strings.TrimSpace(agentID)
}

// EnsureAutoDefault creates or reconciles the one system-owned Auto trigger
// for agentID. A zero interval disables it without deleting the durable
// definition, and clears any in-process delivery identity so an already
// queued envelope cannot execute after the setting is turned off.
func (s *Store) EnsureAutoDefault(agentID string, intervalSeconds int64, now time.Time) (Definition, error) {
	if s == nil {
		return Definition{}, fmt.Errorf("trigger store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return Definition{}, fmt.Errorf("agent_id is required")
	}
	if intervalSeconds < 0 || intervalSeconds > 31536000 {
		return Definition{}, fmt.Errorf("invalid auto interval")
	}
	if now.IsZero() {
		return Definition{}, fmt.Errorf("now is required")
	}
	id := AutoDefaultTriggerID(agentID)
	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.triggers[id]
	if exists && (current.Controller != "auto" || current.ControllerID != agentID || current.OwnerAgentID != agentID || current.TargetAgentID != agentID) {
		return Definition{}, fmt.Errorf("auto default trigger identity collision: %s", id)
	}
	if exists && current.RecoveryRequired && intervalSeconds > 0 {
		return Definition{}, fmt.Errorf("auto default trigger requires recovery: %s", current.RecoveryReason)
	}
	old := current
	if !exists {
		current = Definition{
			TriggerID:         id,
			Name:              autoDefaultName,
			TargetAgentID:     agentID,
			TargetSessionID:   stringPtr(agentID),
			SessionTargetMode: SessionTargetFixed,
			TaskTemplate:      autoDefaultTaskTemplate,
			OwnerAgentID:      agentID,
			Controller:        "auto",
			ControllerID:      agentID,
			CreatedBy:         "system",
			Revision:          1,
			CreatedAt:         timeToUnixFloat(now),
		}
	}
	if exists && sameAutoDefaultConfig(current, agentID, intervalSeconds) {
		return cloneDefinition(current), nil
	}
	current.Name = autoDefaultName
	current.TargetAgentID = agentID
	current.TargetSessionID = stringPtr(agentID)
	current.SessionTargetMode = SessionTargetFixed
	current.TaskTemplate = autoDefaultTaskTemplate
	current.OwnerAgentID = agentID
	current.Controller = "auto"
	current.ControllerID = agentID
	current.CreatedBy = "system"
	if intervalSeconds == 0 {
		if intFromAny(current.Condition["interval_seconds"]) <= 0 {
			current.Condition = map[string]any{"interval_seconds": autoDefaultDisabledInterval}
		}
	} else {
		current.Condition = map[string]any{"interval_seconds": intervalSeconds}
	}
	current.RecoveryRequired = false
	current.RecoveryReason = ""
	if intervalSeconds == 0 {
		current.Enabled = false
		current.NextFireAt = nil
		current.PendingDeliveryID = nil
		current.PendingSessionID = nil
		current.UpdatedAt = timeToUnixFloat(now)
	} else {
		current.Enabled = true
		current = current.WithNextFire(now)
	}
	if exists {
		current.Revision++
	}
	s.triggers[id] = current
	if err := s.saveLocked(); err != nil {
		if exists {
			s.triggers[id] = old
		} else {
			delete(s.triggers, id)
		}
		return Definition{}, err
	}
	if intervalSeconds == 0 {
		s.pending.ClearPendingDelivery(id)
	}
	return cloneDefinition(current), nil
}

func sameAutoDefaultConfig(d Definition, agentID string, intervalSeconds int64) bool {
	if d.Controller != "auto" || d.ControllerID != agentID || d.OwnerAgentID != agentID || d.TargetAgentID != agentID || d.TargetSessionID == nil || *d.TargetSessionID != agentID || d.SessionTargetMode != SessionTargetFixed || d.TaskTemplate != autoDefaultTaskTemplate || d.Name != autoDefaultName {
		return false
	}
	if intervalSeconds == 0 {
		return !d.Enabled && d.NextFireAt == nil && intFromAny(d.Condition["interval_seconds"]) > 0 && d.PendingDeliveryID == nil && d.PendingSessionID == nil
	}
	return d.Enabled && !d.RecoveryRequired && d.NextFireAt != nil && len(d.Condition) == 1 && intFromAny(d.Condition["interval_seconds"]) == int(intervalSeconds)
}

func stringPtr(value string) *string { return &value }
