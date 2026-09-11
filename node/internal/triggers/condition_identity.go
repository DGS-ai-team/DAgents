package triggers

import "fmt"

// ValidateConditionIdentity checks the durable trigger claim immediately
// before a condition side effect is opened.
func ValidateConditionIdentity(def Definition, agentID, sessionID, deliveryID string, revision int64, occurrence *float64) error {
	if !def.Enabled || def.RecoveryRequired {
		return fmt.Errorf("condition trigger is disabled or recovering")
	}
	if def.TargetAgentID != agentID || def.OwnerAgentID != agentID {
		return fmt.Errorf("condition trigger owner changed")
	}
	if def.Revision != revision {
		return fmt.Errorf("condition trigger identity changed")
	}
	if def.PendingDeliveryID == nil || *def.PendingDeliveryID != deliveryID {
		return fmt.Errorf("condition delivery is stale")
	}
	if def.PendingSessionID == nil || *def.PendingSessionID != sessionID {
		return fmt.Errorf("condition session is stale")
	}
	if (def.PendingOccurrence == nil) != (occurrence == nil) || (def.PendingOccurrence != nil && *def.PendingOccurrence != *occurrence) {
		return fmt.Errorf("condition occurrence is stale")
	}
	return nil
}
