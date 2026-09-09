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
	// Ordinary user/Goal triggers retain their existing behavior.
	// Only the exact system-owned ID is eligible for this Auto-only cap.
	if agentID == "" || triggerID == "" {
		return 0, false, nil
	}
	if triggerID != triggers.AutoDefaultTriggerID(agentID) {
		if strings.HasPrefix(triggerID, "auto-default:") {
			return 0, false, fmt.Errorf("Auto trigger identity mismatch")
		}
		return 0, false, nil
	}
	if deliveryID == "" || s == nil || s.autonomyStore == nil || s.triggerStore == nil {
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
		return 0, false, fmt.Errorf("Agent is not an Auto runtime")
	}
	d, ok := s.triggerStore.GetTrigger(triggerID)
	if !ok || d.Controller != "auto" || d.ControllerID != agentID || d.OwnerAgentID != agentID || d.TargetAgentID != agentID || d.TargetSessionID == nil || *d.TargetSessionID != agentID || d.SessionTargetMode != triggers.SessionTargetFixed || !d.Enabled || d.RecoveryRequired || d.PendingDeliveryID == nil || *d.PendingDeliveryID != deliveryID || !s.triggerStore.IsPendingDelivery(triggerID, deliveryID) {
		return 0, false, fmt.Errorf("untrusted Auto trigger delivery")
	}
	p, ok := s.autonomyStore.GetProfile(agentID)
	interval, validInterval := triggers.ConfiguredIntervalSeconds(d.Condition)
	if !ok || p.WakeIntervalSeconds <= 0 || p.MaxToolRounds <= 0 || !validInterval || interval != p.WakeIntervalSeconds {
		return 0, false, fmt.Errorf("Auto trigger configuration is stale")
	}
	return p.MaxToolRounds, true, nil
}
