package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// occurrenceMaintenanceUsage scopes every runner receipt operation to the
// occurrence currently claimed by the scheduler. Embedding preserves the
// Goals store's evidence and reconciliation methods, while these overrides
// prevent a runner from settling a manual or different-day receipt.
type occurrenceMaintenanceUsage struct {
	*goals.Store
	agentID, localDate string
	revision           int64
}

func (u occurrenceMaintenanceUsage) inScope(receiptID string) bool {
	r, ok := u.Store.GetMaintenanceReceipt(receiptID)
	return ok && r.AgentID == u.agentID && r.OccurrenceLocalDate == u.localDate && r.OccurrenceScheduleRevision == u.revision
}

func (u occurrenceMaintenanceUsage) BeginMaintenance(agentID, receiptID, fingerprint string, estimated int64, now time.Time) (goals.MaintenanceReservation, error) {
	if agentID != u.agentID {
		return goals.MaintenanceReservation{}, fmt.Errorf("maintenance agent is outside occurrence scope")
	}
	if u.localDate == "" && u.revision == 0 {
		return u.Store.BeginMaintenance(agentID, receiptID, fingerprint, estimated, now)
	}
	return u.Store.BeginMaintenanceForOccurrence(u.agentID, u.localDate, u.revision, receiptID, fingerprint, estimated, now)
}

func (u occurrenceMaintenanceUsage) SaveMaintenanceResult(agentID, receiptID string, candidates json.RawMessage, nextCursor, used int64, unknown bool) error {
	if agentID != u.agentID || !u.inScope(receiptID) {
		return fmt.Errorf("maintenance receipt is outside occurrence scope")
	}
	return u.Store.SaveMaintenanceResult(agentID, receiptID, candidates, nextCursor, used, unknown)
}

func (u occurrenceMaintenanceUsage) SaveMaintenanceResultWithEvidence(agentID, receiptID string, candidates, evidence json.RawMessage, nextCursor, used int64, unknown bool) error {
	if agentID != u.agentID || !u.inScope(receiptID) {
		return fmt.Errorf("maintenance receipt is outside occurrence scope")
	}
	return u.Store.SaveMaintenanceResultWithEvidence(agentID, receiptID, candidates, evidence, nextCursor, used, unknown)
}

func (u occurrenceMaintenanceUsage) SettleMaintenance(agentID, receiptID string, used int64, unknown bool, now time.Time) (goals.AgentUsage, error) {
	if agentID != u.agentID || !u.inScope(receiptID) {
		return goals.AgentUsage{}, fmt.Errorf("maintenance receipt is outside occurrence scope")
	}
	return u.Store.SettleMaintenance(agentID, receiptID, used, unknown, now)
}

func (u occurrenceMaintenanceUsage) GetMaintenanceReceipt(receiptID string) (goals.MaintenanceReceipt, bool) {
	if !u.inScope(receiptID) {
		return goals.MaintenanceReceipt{}, false
	}
	return u.Store.GetMaintenanceReceipt(receiptID)
}

func (u occurrenceMaintenanceUsage) ListMaintenanceReceipts(agentID string) []goals.MaintenanceReceipt {
	if agentID != u.agentID {
		return nil
	}
	if u.localDate == "" && u.revision == 0 {
		result := make([]goals.MaintenanceReceipt, 0)
		for _, receipt := range u.Store.ListMaintenanceReceipts(agentID) {
			if receipt.OccurrenceLocalDate == "" && receipt.OccurrenceScheduleRevision == 0 {
				result = append(result, receipt)
			}
		}
		return result
	}
	entries := u.Store.ListMaintenanceOccurrenceReceipts(u.agentID, u.localDate, u.revision)
	result := make([]goals.MaintenanceReceipt, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Receipt)
	}
	return result
}

func containsMaintenanceFS(groups []string) bool {
	for _, g := range groups {
		if g == "fs" || g == "filesystem" {
			return true
		}
	}
	return false
}

// maintenanceScheduler is deliberately a bounded, best-effort poller. Durable
// occurrence state remains the source of truth, so a restart cannot replay a
// pending LLM operation.
type maintenanceScheduler struct {
	server    *Server
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
	startOnce sync.Once
	started   bool
	cancel    context.CancelFunc
	work      sync.WaitGroup
	stopDone  chan struct{}
	activeMu  sync.Mutex
	active    map[string]context.CancelFunc
	lifecycle context.Context
	admitMu   sync.Mutex
	stopped   bool
}

func maintenanceProfileChanged(a, b goals.AutoProfile) bool {
	return a.Enabled != b.Enabled || a.MaintenanceEnabled != b.MaintenanceEnabled || a.MaintenanceSchedule != b.MaintenanceSchedule || a.Timezone != b.Timezone || a.MaintenanceRevision != b.MaintenanceRevision
}

func newMaintenanceScheduler(server *Server) *maintenanceScheduler {
	return &maintenanceScheduler{server: server, stop: make(chan struct{}), done: make(chan struct{}), stopDone: make(chan struct{}), active: make(map[string]context.CancelFunc)}
}

func (m *maintenanceScheduler) Start() {
	m.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		m.admitMu.Lock()
		m.started = true
		m.lifecycle = ctx
		m.cancel = cancel
		m.admitMu.Unlock()
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			defer close(m.done)
			for {
				select {
				case now := <-ticker.C:
					m.RunOnce(ctx, now.UTC())
				case <-m.stop:
					return
				}
			}
		}()
	})
}

func (m *maintenanceScheduler) Stop() {
	if m == nil {
		return
	}
	m.admitMu.Lock()
	if !m.started {
		m.admitMu.Unlock()
		return
	}
	if !m.stopped {
		m.stopped = true
	}
	m.admitMu.Unlock()
	m.once.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		close(m.stop)
		go func() { <-m.done; m.work.Wait(); close(m.stopDone) }()
	})
	<-m.stopDone
}

// RunOnceForTest exposes the same bounded tick used by production tests.
func (m *maintenanceScheduler) RunOnceForTest(ctx context.Context, now time.Time) {
	m.RunOnce(ctx, now)
}

func (m *maintenanceScheduler) Cancel(agentID string) {
	m.activeMu.Lock()
	cancel := m.active[agentID]
	m.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *maintenanceScheduler) RunOnce(ctx context.Context, now time.Time) {
	if m == nil {
		return
	}
	m.admitMu.Lock()
	if m.stopped {
		m.admitMu.Unlock()
		return
	}
	if m.lifecycle != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		stop := context.AfterFunc(m.lifecycle, cancel)
		defer func() { stop(); cancel() }()
	}
	m.work.Add(1)
	m.admitMu.Unlock()
	defer m.work.Done()
	if m == nil || m.server == nil || m.server.goalStore == nil || m.server.agents == nil {
		return
	}
	records, err := m.server.agents.List(ctx)
	if err != nil {
		return
	}
	for _, rec := range records {
		if ctx.Err() != nil {
			return
		}
		profile, ok := m.server.goalStore.GetProfile(rec.AgentID)
		if !ok || !profile.Enabled || !profile.MaintenanceEnabled || rec.Archived {
			continue
		}
		_ = m.runAgent(ctx, rec.AgentID, now)
	}
}

func (m *maintenanceScheduler) runAgent(ctx context.Context, agentID string, now time.Time) error {
	s := m.server
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx = runCtx
	if s.sessions != nil {
		_, active, _, _ := s.sessions.RuntimeInfo(agentID)
		if active {
			return nil
		}
		for _, g := range s.goalStore.List() {
			if g.AgentID != agentID {
				continue
			}
			for _, run := range s.goalStore.Runs(g.ID) {
				if run.FinishedAt == nil {
					return nil
				}
			}
		}
	}
	leaseCtx := ctx
	release := func() {}
	if s.sessions != nil {
		var acquired bool
		var err error
		leaseCtx, release, acquired, err = s.sessions.TryAcquireMaintenanceContext(ctx, agentID)
		if err != nil || !acquired {
			return err
		}
	}
	var handbookCleanups []func()
	var handbookErr error
	defer func() {
		// Release the execution gate before synchronously stopping the temporary
		// handbook runtime; this ordering avoids a consumer/gate deadlock.
		release()
		for i := len(handbookCleanups) - 1; i >= 0; i-- {
			if handbookCleanups[i] != nil {
				handbookCleanups[i]()
			}
		}
	}()
	m.activeMu.Lock()
	m.active[agentID] = cancel
	m.activeMu.Unlock()
	defer func() { m.activeMu.Lock(); delete(m.active, agentID); m.activeMu.Unlock() }()
	profile, ok := s.goalStore.GetProfile(agentID)
	if !ok || !profile.Enabled || !profile.MaintenanceEnabled {
		return nil
	}
	rec, err := s.agents.Get(ctx, agentID)
	if err != nil || rec == nil || rec.Archived {
		return err
	}
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil || snap.AgentType != "auto" {
		return nil
	}
	claim, err := s.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil || (!claim.Claimed && claim.Occurrence.Status != goals.MaintenanceOccurrencePending) {
		return err
	}
	current, ok := s.goalStore.GetProfile(agentID)
	if !ok || !current.Enabled || !current.MaintenanceEnabled || current.MaintenanceRevision != claim.Occurrence.ScheduleRevision {
		_, _ = s.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "maintenance configuration changed", now)
		return nil
	}
	// Resume durable handbook children only after this tick has a claimed
	// occurrence. Their reservation may consume the allowance, and extracting
	// first would leave a prepared child permanently stranded.
	if containsMaintenanceFS(agentruntime.EnabledToolGroups(snap)) {
		for _, parent := range selectOccurrenceHandbookParents(s.goalStore, agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, 8) {
			child, exists := s.goalStore.GetMaintenanceReceipt(parent.Receipt.HandbookReceiptID)
			if !exists || child.PhaseState == goals.MaintenancePhaseComplete {
				continue
			}
			var cleanup func()
			if _, cleanup, handbookErr = s.runHandbookMaintenance(leaseCtx, leaseCtx, *rec, "Resume the durable handbook update from the persisted evidence. Read existing entries first; preserve structure; use only the supplied evidence as data.", parent.ReceiptID, parent.Receipt, turn.TurnBudget{MaxSteps: 8, MaxTotalTokens: 30000, MaxWallTime: 30 * time.Second}); handbookErr != nil {
				handbookCleanups = append(handbookCleanups, cleanup)
				_, _ = s.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", handbookErr.Error(), time.Now().UTC())
				return handbookErr
			}
			handbookCleanups = append(handbookCleanups, cleanup)
		}
	}
	ms, err := s.openAgentMemoryService(agentID, rec)
	if err != nil {
		_, _ = s.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceFailed, "", err.Error(), now)
		return err
	}
	defer ms.Close()
	extractor := s.maintenanceExtractor
	if extractor == nil {
		client, _, resolveErr := s.llmClientForAgent(leaseCtx, rec, agentID)
		if resolveErr != nil {
			_, _ = s.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", resolveErr.Error(), now)
			return resolveErr
		}
		extractor = memory.NewLLMCandidateExtractor(client, 8, 24000)
	}
	cursor, err := ms.GetMaintenanceCursor(leaseCtx)
	if err != nil {
		_, _ = s.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceFailed, "", err.Error(), now)
		return err
	}
	occurrenceUsage := occurrenceMaintenanceUsage{Store: s.goalStore, agentID: agentID, localDate: claim.Occurrence.LocalDate, revision: claim.Occurrence.ScheduleRevision}
	runner := &memory.MaintenanceRunner{Source: maintenanceSource{store: s.store, goals: s.goalStore}, Extractor: extractor, Memory: ms, Usage: occurrenceUsage}
	var runErr error
	deadline := time.Now().Add(30 * time.Second)
	attempts := 0
	for ; attempts < 8 && time.Now().Before(deadline); attempts++ {
		nextSequence, _, err := runner.RunOnceWithEvidence(leaseCtx, agentID, cursor)
		runErr = err
		if runErr != nil || nextSequence == cursor.Sequence {
			break
		}
		cursor.Sequence = nextSequence
	}
	// A bounded batch that advanced the cursor but hit the limit remains
	// pending. The next in-process tick resumes it; OpenStore converts pending
	// work to recovery_required before any post-restart LLM call.
	if runErr == nil && attempts >= 8 {
		return nil
	}
	if runErr == nil {
		// Handbook writes are an explicit fs capability in the Agent snapshot.
		// Legacy memory-only profiles keep their existing scheduler behavior.
		if snap, parseErr := agentruntime.ParseSnapshot(rec.ConfigSnapshot); parseErr == nil && containsMaintenanceFS(agentruntime.EnabledToolGroups(snap)) {
			parents := selectOccurrenceHandbookParents(s.goalStore, agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, 8)
			for _, parent := range parents {
				var handbookCleanup func()
				if _, handbookCleanup, handbookErr = s.runHandbookMaintenance(leaseCtx, leaseCtx, *rec, "Review recent durable experience and update the handbook only when evidence supports it. Read existing entries first; preserve structure; use read_file digests for CAS edits.", parent.ReceiptID, parent.Receipt, turn.TurnBudget{MaxSteps: 8, MaxTotalTokens: 30000, MaxWallTime: 30 * time.Second}); handbookErr != nil {
					handbookCleanups = append(handbookCleanups, handbookCleanup)
					break
				}
				handbookCleanups = append(handbookCleanups, handbookCleanup)
			}
			if handbookErr != nil {
				runErr = handbookErr
			}
		}
	}
	if runErr == nil && containsMaintenanceFS(agentruntime.EnabledToolGroups(snap)) && hasPendingOccurrenceHandbookParents(s.goalStore, agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision) {
		// Keep the occurrence pending so the next bounded tick resumes the
		// remaining durable parents instead of reporting a false completion.
		return nil
	}
	status := goals.MaintenanceOccurrenceCompleted
	reason := ""
	if runErr != nil {
		status, reason = goals.MaintenanceOccurrenceRecoveryRequired, runErr.Error()
	}
	_, finishErr := s.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, status, "", reason, time.Now().UTC())
	if runErr != nil {
		return runErr
	}
	return finishErr
}
