package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func (s *Server) triggerToolRoundProvider(ctx context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	agentID, triggerID, deliveryID = strings.TrimSpace(agentID), strings.TrimSpace(triggerID), strings.TrimSpace(deliveryID)
	if agentID == "" || triggerID == "" {
		return 0, false, nil
	}
	isDefault := triggerID == triggers.AutoDefaultTriggerID(agentID)
	if !isDefault {
		if strings.HasPrefix(triggerID, "auto-default:") {
			return 0, false, fmt.Errorf("Auto trigger identity mismatch")
		}
	}
	if s == nil || s.triggerStore == nil {
		if isDefault {
			return 0, false, fmt.Errorf("trusted Auto trigger unavailable")
		}
		return 0, false, nil
	}
	d, ok := s.triggerStore.GetTrigger(triggerID)
	if !ok {
		if isDefault {
			return 0, false, fmt.Errorf("untrusted Auto trigger delivery")
		}
		return 0, false, nil
	}
	if deliveryID == "" || s.autonomyStore == nil {
		return 0, false, fmt.Errorf("trusted Auto trigger unavailable")
	}
	if s.agents == nil {
		return 0, false, fmt.Errorf("Agent registry unavailable")
	}
	record, err := s.agents.Get(ctx, agentID)
	if err != nil || record == nil || record.Archived {
		return 0, false, fmt.Errorf("Agent is unavailable for Auto trigger")
	}
	snapshot, err := agentruntime.ParseSnapshot(record.ConfigSnapshot)
	if err != nil || !strings.EqualFold(strings.TrimSpace(snapshot.AgentType), "auto") {
		if !isDefault {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("Agent is not an Auto runtime")
	}
	if !isDefault {
		// User-owned triggers targeting an Auto Agent's canonical main session
		// are still Auto activations and use the Agent-owned round cap. Other
		// other user-owned triggers retain their existing behavior.
		if d.Controller != "user" || d.TargetSessionID == nil {
			return 0, false, nil
		}
		if d.OwnerAgentID != agentID || d.TargetAgentID != agentID || *d.TargetSessionID != agentID || d.SessionTargetMode != triggers.SessionTargetFixed {
			return 0, false, fmt.Errorf("user trigger is not owned by Auto main session")
		}
	}
	if !d.Enabled || d.RecoveryRequired || d.PendingDeliveryID == nil || *d.PendingDeliveryID != deliveryID || !s.triggerStore.IsPendingDelivery(triggerID, deliveryID) {
		return 0, false, fmt.Errorf("untrusted Auto trigger delivery")
	}
	if isDefault && (d.Controller != "auto" || d.ControllerID != agentID || d.OwnerAgentID != agentID || d.TargetAgentID != agentID || d.TargetSessionID == nil || *d.TargetSessionID != agentID || d.SessionTargetMode != triggers.SessionTargetFixed) {
		return 0, false, fmt.Errorf("untrusted Auto trigger delivery")
	}
	p, ok := s.autonomyStore.GetProfile(agentID)
	if !isDefault {
		if !ok || p.MaxToolRounds <= 0 {
			return 0, false, fmt.Errorf("Auto trigger configuration is unavailable")
		}
		return p.MaxToolRounds, true, nil
	}
	interval, validInterval := triggers.ConfiguredIntervalSeconds(d.Condition)
	if !ok || p.WakeIntervalSeconds <= 0 || p.MaxToolRounds <= 0 || !validInterval || interval != p.WakeIntervalSeconds {
		return 0, false, fmt.Errorf("Auto trigger configuration is stale")
	}
	return p.MaxToolRounds, true, nil
}
