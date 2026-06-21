package session

import (
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type readySideEffect struct {
	seq            uint64
	kind           turn.SideEffectKind
	ssePublished   bool
	built          turn.SideEffectMessages
	async          queue.AsyncToolResultPayload
	messageContent string
	userName       string
	triggerID      string
}

type sideEffectStore struct {
	mu              sync.Mutex
	seq             uint64
	queue           []readySideEffect
	continuePending bool
}

func newSideEffectStore() *sideEffectStore {
	return &sideEffectStore{}
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
	Kind           turn.SideEffectKind
	Async          *queue.AsyncToolResultPayload
	MessageContent string
	UserName       string
	TriggerID      string
}

func (s *sideEffectStore) Produce(
	orch *turn.Orchestrator,
	sessionID string,
	messages []llm.Message,
	in sideEffectProduceInput,
) {
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()

	var async queue.AsyncToolResultPayload
	if in.Async != nil {
		async = *in.Async
	}
	built := orch.BuildSideEffectMessages(in.Kind, sessionID, async, in.MessageContent, in.UserName)

	switch in.Kind {
	case turn.SideEffectAsync:
		orch.PublishSideEffectCallback(sessionID, built, seq)
	case turn.SideEffectExternalMessage:
		if turn.IsBridgeTail(messages) {
			orch.PublishExternalSideEffectDeferred(sessionID, in.MessageContent, in.UserName, in.TriggerID, seq)
			orch.PublishSideEffectCallback(sessionID, built, seq)
		} else {
			orch.PublishSideEffectCallback(sessionID, built, seq)
		}
	}

	entry := readySideEffect{
		seq:            seq,
		kind:           in.Kind,
		ssePublished:   true,
		built:          built,
		async:          async,
		messageContent: in.MessageContent,
		userName:       in.UserName,
		triggerID:      in.TriggerID,
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
	delivery triggers.DeliveryTracker,
) sideEffectApplyResult {
	var result sideEffectApplyResult
	for {
		applied, seqs, cont := s.applyOneBatch(sessionID, orch, history, delivery)
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
	delivery triggers.DeliveryTracker,
) (applied int, appliedSeqs []uint64, shouldContinue bool) {
	batch := s.collectBatch(*history)
	if len(batch) == 0 {
		return 0, nil, false
	}

	// 幂等 skip
	var pending []readySideEffect
	for _, e := range batch {
		if turn.SideEffectAlreadyApplied(*history, e.kind, e.async, e.messageContent, e.userName) {
			s.removeEntry(e, delivery)
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

	orch.ApplySideEffectPlan(sessionID, history, site, plan)
	for _, e := range pending {
		s.removeEntry(e, delivery)
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
	if turn.SideEffectAlreadyApplied(history, head.kind, head.async, head.messageContent, head.userName) {
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
		if turn.SideEffectAlreadyApplied(sim, e.kind, e.async, e.messageContent, e.userName) {
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
		for _, m := range plan.Messages {
			sim = append(sim, m)
		}
	}
	return batch
}

func sideEffectEntryToBatch(e readySideEffect) turn.SideEffectBatchEntry {
	return turn.SideEffectBatchEntry{
		Kind:           e.kind,
		Built:          e.built,
		Async:          e.async,
		MessageContent: e.messageContent,
		UserName:       e.userName,
		TriggerID:      e.triggerID,
	}
}

func (s *sideEffectStore) removeEntry(e readySideEffect, delivery triggers.DeliveryTracker) {
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
	clearTriggerDelivery(delivery, e.triggerID)
}

func clearTriggerDelivery(delivery triggers.DeliveryTracker, triggerID string) {
	if delivery == nil || triggerID == "" {
		return
	}
	delivery.ClearPendingDelivery(triggerID)
}

func (s *sideEffectStore) ClearSession(sessionID string, orch *turn.Orchestrator, delivery triggers.DeliveryTracker) {
	s.mu.Lock()
	items := append([]readySideEffect(nil), s.queue...)
	s.queue = nil
	s.continuePending = false
	s.mu.Unlock()
	seqs := make([]uint64, 0, len(items))
	for _, e := range items {
		seqs = append(seqs, e.seq)
		clearTriggerDelivery(delivery, e.triggerID)
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
	delivery triggers.DeliveryTracker,
	scheduleContinue func(),
) turn.StepOutcome {
	if outcome.ScheduleToolResult || pending != nil {
		return outcome
	}
	if !turn.TaskComplete(*history, pending) {
		return outcome
	}
	apply := s.ApplyReady(sessionID, orch, history, delivery)
	if apply.Continue && pending == nil {
		scheduleContinue()
	}
	return outcome
}
