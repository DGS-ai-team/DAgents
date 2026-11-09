package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

// triggerActivationProvider validates that a trigger delivery is an authorized
// Auto activation. The turn budget itself always comes from the Agent's LLM
// defaults and is not overridden here.
func (s *Server) triggerActivationProvider(ctx context.Context, agentID, triggerID, deliveryID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	agentID, triggerID, deliveryID = strings.TrimSpace(agentID), strings.TrimSpace(triggerID), strings.TrimSpace(deliveryID)
	if agentID == "" || triggerID == "" {
		return false, nil
	}
	isDefault := triggerID == triggers.AutoDefaultTriggerID(agentID)
	if !isDefault && strings.HasPrefix(triggerID, "auto-default:") {
		return false, fmt.Errorf("auto trigger identity mismatch")
	}
	if s == nil || s.triggerStore == nil {
		if isDefault {
			return false, fmt.Errorf("trusted auto trigger unavailable")
		}
		return false, nil
	}
	d, ok := s.triggerStore.GetTrigger(triggerID)
	if !ok {
		if isDefault {
			return false, fmt.Errorf("untrusted auto trigger delivery")
		}
		return false, nil
	}
	if deliveryID == "" || s.autonomyStore == nil {
		return false, fmt.Errorf("trusted auto trigger unavailable")
	}
	if s.agents == nil {
		return false, fmt.Errorf("agent registry unavailable")
	}
	record, err := s.agents.Get(ctx, agentID)
	if err != nil || record == nil || record.Archived {
		return false, fmt.Errorf("agent is unavailable for auto trigger")
	}
	snapshot, err := agentruntime.ParseSnapshot(record.ConfigSnapshot)
	if err != nil || !strings.EqualFold(strings.TrimSpace(snapshot.AgentType), "auto") {
		if !isDefault {
			return false, nil
		}
		return false, fmt.Errorf("agent is not an auto runtime")
	}
	if !isDefault {
		// Only user-owned triggers targeting the Auto Agent's canonical main
		// session receive trusted activation capabilities.
		if d.Controller != "user" || d.TargetSessionID == nil {
			return false, nil
		}
		if d.OwnerAgentID != agentID || d.TargetAgentID != agentID || *d.TargetSessionID != agentID || d.SessionTargetMode != triggers.SessionTargetFixed {
			return false, fmt.Errorf("user trigger is not owned by auto main session")
		}
	}
	if !d.Enabled || d.RecoveryRequired || d.PendingDeliveryID == nil || *d.PendingDeliveryID != deliveryID || !s.triggerStore.IsPendingDelivery(triggerID, deliveryID) {
		return false, fmt.Errorf("untrusted auto trigger delivery")
	}
	if isDefault && (d.Controller != "auto" || d.ControllerID != agentID || d.OwnerAgentID != agentID || d.TargetAgentID != agentID || d.TargetSessionID == nil || *d.TargetSessionID != agentID || d.SessionTargetMode != triggers.SessionTargetFixed) {
		return false, fmt.Errorf("untrusted auto trigger delivery")
	}
	p, ok := s.autonomyStore.GetProfile(agentID)
	if !isDefault {
		if !ok {
			return false, fmt.Errorf("auto trigger configuration is unavailable")
		}
		return true, nil
	}
	interval, validInterval := triggers.ConfiguredIntervalSeconds(d.Condition)
	if !ok || p.WakeIntervalSeconds <= 0 || !validInterval || interval != p.WakeIntervalSeconds {
		return false, fmt.Errorf("auto trigger configuration is stale")
	}
	return true, nil
}
