package api

import (
	"context"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
)

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
	defer release()
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
	runner := &memory.MaintenanceRunner{Source: maintenanceSource{store: s.store}, Extractor: extractor, Memory: ms, Usage: s.goalStore}
	var runErr error
	deadline := time.Now().Add(30 * time.Second)
	attempts := 0
	for ; attempts < 8 && time.Now().Before(deadline); attempts++ {
		nextSequence, err := runner.RunOnce(leaseCtx, agentID, cursor)
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
