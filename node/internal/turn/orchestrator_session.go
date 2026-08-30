package turn

import "strings"

// ForgetSession releases per-session orchestration state after a runtime is
// stopped or replaced. The runtime owns the lifecycle; this method prevents
// diagnostic counters, pending invalidations and snapshots from retaining a
// session indefinitely.
func (o *Orchestrator) ForgetSession(sessionID string) {
	if o == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if o.modelSnapshots != nil {
		o.modelSnapshots.clear(sessionID)
	}
	o.contextMutationMu.Lock()
	delete(o.contextMutations, sessionID)
	o.contextMutationMu.Unlock()
	o.summaryMu.Lock()
	delete(o.summaryNext, sessionID)
	o.summaryMu.Unlock()
	o.turnUsageMu.Lock()
	delete(o.turnUsage, sessionID)
	delete(o.turnUsageLast, sessionID)
	o.turnUsageMu.Unlock()
	if o.ctxMetrics != nil {
		o.ctxMetrics.delete(sessionID)
	}
}
