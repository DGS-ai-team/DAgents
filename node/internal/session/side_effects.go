package session

import (
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type readySideEffect struct {
	seq   uint64
	built turn.SideEffectMessages
	async queue.AsyncToolResultPayload
}

type sideEffectStore struct {
	mu              sync.Mutex
	seq             uint64
	queue           []readySideEffect
	continuePending bool
	// asyncJobs is a session-scoped idempotency set. An async callback can be
	// delivered more than once before the first one is applied to history.
	asyncJobs map[string]struct{}
}

func newSideEffectStore() *sideEffectStore {
	return &sideEffectStore{asyncJobs: make(map[string]struct{})}
}

func (s *sideEffectStore) HasReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue) > 0
}

func (s *sideEffectStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

func (s *sideEffectStore) markContinuePending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.continuePending {
		return false
	}
	s.continuePending = true
	return true
}

func (s *sideEffectStore) clearContinuePending() {
	s.mu.Lock()
	s.continuePending = false
	s.mu.Unlock()
}

type sideEffectProduceInput struct {
	Async *queue.AsyncToolResultPayload
}

func (s *sideEffectStore) Produce(
	orch *turn.Orchestrator,
	sessionID string,
	messages []llm.Message,
	in sideEffectProduceInput,
) {
	if in.Async == nil {
		return
	}
	async := *in.Async
	jobID := strings.TrimSpace(async.JobID)
	if jobID != "" {
		// History-level idempotence handles callbacks that arrive after the
		// first callback was applied. This check handles duplicates that race
		// before either callback reaches history.
		if turn.SideEffectAlreadyApplied(messages, async) {
			return
		}
		s.mu.Lock()
		if s.asyncJobs == nil {
			s.asyncJobs = make(map[string]struct{})
		}
		if _, exists := s.asyncJobs[jobID]; exists {
			s.mu.Unlock()
			return
		}
		s.asyncJobs[jobID] = struct{}{}
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()

	built := orch.BuildAsyncSideEffectMessages(sessionID, messages, async)
	orch.PublishSideEffectCallback(sessionID, built, seq)

	entry := readySideEffect{
		seq:   seq,
		built: built,
		async: async,
	}
	s.mu.Lock()
	s.queue = append(s.queue, entry)
	s.mu.Unlock()
}

type sideEffectApplyResult struct {
	AppliedCount int
	AppliedSeqs  []uint64
	Continue     bool
}

func (s *sideEffectStore) ApplyReady(
	sessionID string,
	orch *turn.Orchestrator,
	history *[]llm.Message,
	factSinks ...func(readySideEffect),
) sideEffectApplyResult {
	var result sideEffectApplyResult
	for {
		applied, seqs, cont := s.applyOneBatch(sessionID, orch, history, factSinks...)
		if applied == 0 {
			break
		}
		result.AppliedCount += applied
		result.AppliedSeqs = append(result.AppliedSeqs, seqs...)
		if cont {
			result.Continue = true
		}
	}
	if len(result.AppliedSeqs) > 0 {
		orch.PublishSideEffectApplied(sessionID, result.AppliedSeqs)
	}
	return result
}

func (s *sideEffectStore) applyOneBatch(
	sessionID string,
	orch *turn.Orchestrator,
	history *[]llm.Message,
	factSinks ...func(readySideEffect),
) (applied int, appliedSeqs []uint64, shouldContinue bool) {
	batch := s.collectBatch(*history)
	if len(batch) == 0 {
		return 0, nil, false
	}

	// 幂等 skip
	var pending []readySideEffect
	for _, e := range batch {
		if turn.SideEffectAlreadyApplied(*history, e.async) {
			s.removeEntry(e)
			applied++
			appliedSeqs = append(appliedSeqs, e.seq)
			continue
		}
		pending = append(pending, e)
	}
	if len(pending) == 0 {
		return applied, appliedSeqs, false
	}

	head := pending[0]
	site := turn.ResolveSideEffectInsertSite(*history, head.built)
	if !site.Ready {
		return applied, appliedSeqs, false
	}

	var plan turn.SideEffectApplyPlan
	if len(pending) >= 2 {
		entries := make([]turn.SideEffectBatchEntry, len(pending))
		for i, e := range pending {
			entries[i] = sideEffectEntryToBatch(e)
		}
		plan = turn.BuildMergedCallbackBatch(entries, *history)
		site.InsertAt = len(*history)
	} else {
		plan = turn.PlanSingleSideEffectApply(*history, pending[0].built)
	}
	if len(plan.Messages) == 0 {
		return applied, appliedSeqs, false
	}

	for _, sink := range factSinks {
		if sink == nil {
			continue
		}
		for _, e := range pending {
			sink(e)
		}
	}
	orch.ApplySideEffectPlan(sessionID, history, site, plan)
	for _, e := range pending {
		s.removeEntry(e)
		appliedSeqs = append(appliedSeqs, e.seq)
	}
	return applied + len(pending), appliedSeqs, plan.Continue
}

func (s *sideEffectStore) collectBatch(history []llm.Message) []readySideEffect {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return nil
	}
	head := s.queue[0]
	if turn.SideEffectAlreadyApplied(history, head.async) {
		return []readySideEffect{head}
	}
	site := turn.ResolveSideEffectInsertSite(history, head.built)
	if !site.Ready {
		return nil
	}
	targetAt := site.InsertAt
	sim := append([]llm.Message(nil), history...)
	var batch []readySideEffect
	for i := 0; i < len(s.queue); i++ {
		e := s.queue[i]
		if turn.SideEffectAlreadyApplied(sim, e.async) {
			batch = append(batch, e)
			continue
		}
		es := turn.ResolveSideEffectInsertSite(sim, e.built)
		if !es.Ready {
			break
		}
		if len(batch) == 0 {
			if es.InsertAt != targetAt {
				break
			}
		} else if es.InsertAt != targetAt {
			break
		}
		batch = append(batch, e)
		plan := turn.PlanSingleSideEffectApply(sim, e.built)
		sim = append(sim, plan.Messages...)
	}
	return batch
}

func sideEffectEntryToBatch(e readySideEffect) turn.SideEffectBatchEntry {
	return turn.SideEffectBatchEntry{
		Built: e.built,
		Async: e.async,
	}
}

func (s *sideEffectStore) removeEntry(e readySideEffect) {
	s.mu.Lock()
	out := s.queue[:0]
	for _, item := range s.queue {
		if item.seq == e.seq {
			continue
		}
		out = append(out, item)
	}
	s.queue = out
	s.mu.Unlock()
}

func (s *sideEffectStore) ClearSession(sessionID string, orch *turn.Orchestrator) {
	s.mu.Lock()
	items := append([]readySideEffect(nil), s.queue...)
	s.queue = nil
	s.continuePending = false
	s.asyncJobs = make(map[string]struct{})
	s.mu.Unlock()
	seqs := make([]uint64, 0, len(items))
	for _, e := range items {
		seqs = append(seqs, e.seq)
	}
	if orch != nil && len(seqs) > 0 {
		orch.PublishSideEffectsCleared(sessionID, len(seqs), seqs)
	}
}

func (s *sideEffectStore) ReconcileAfterStep(
	sessionID string,
	orch *turn.Orchestrator,
	history *[]llm.Message,
	pending *turn.PendingHITL,
	outcome turn.StepOutcome,
	scheduleContinue func(),
	factSinks ...func(readySideEffect),
) turn.StepOutcome {
	if outcome.ScheduleToolResult || pending != nil {
		return outcome
	}
	if !turn.TaskComplete(*history, pending) {
		return outcome
	}
	apply := s.ApplyReady(sessionID, orch, history, factSinks...)
	if apply.Continue {
		scheduleContinue()
	}
	return outcome
}
