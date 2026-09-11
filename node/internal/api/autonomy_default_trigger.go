package api

import (
	"fmt"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

// reconcileAutoDefaults rebuilds only the deterministic system trigger
// projection. It is called before the scheduler starts during startup.
func (s *Server) reconcileAutoDefaults(validAuto map[string]bool) error {
	if s == nil || s.autonomyStore == nil || s.triggerStore == nil {
		return fmt.Errorf("auto trigger stores unavailable")
	}
	s.autoConfigMu.Lock()
	defer s.autoConfigMu.Unlock()
	for agentID := range validAuto {
		// A pending delivery is a durable recovery fence. Startup must keep the
		// trigger frozen and preserve its delivery identity; reconciling it (in
		// particular with interval 0) would discard evidence or re-arm it.
		if s.triggerStore.IsRecoveryRequired(triggers.AutoDefaultTriggerID(agentID)) {
			continue
		}
		profile, exists := s.autonomyStore.GetProfile(agentID)
		if !exists {
			profile.WakeIntervalSeconds = 0
		}
		if _, err := s.triggerStore.EnsureAutoDefault(agentID, profile.WakeIntervalSeconds, time.Now().UTC()); err != nil {
			return fmt.Errorf("restore auto default trigger for %s: %w", agentID, err)
		}
	}
	return nil
}
