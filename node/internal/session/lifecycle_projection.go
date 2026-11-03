package session

import (
	"context"
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

const lifecycleProjectionCacheCapacity = 32

type lifecycleProjectionKey struct {
	sessionID  string
	latestID   int64
	eventCount int64
}

type lifecycleProjectionCacheEntry struct {
	snapshot  turn.CoordinatorSnapshot
	hasEvents bool
	sequence  uint64
}

type lifecycleProjectionData struct {
	snapshot turn.CoordinatorSnapshot
	sequence uint64
	events   []turn.TurnEventEnvelope
}

// loadLifecycleProjectionData loads and replays a session's lifecycle once.
// Callers that construct a runtime can pass the returned events through so
// runtime restoration does not issue a second identical database scan.
func (m *Manager) loadLifecycleProjectionData(ctx context.Context, sessionID, agentID string) (lifecycleProjectionData, error) {
	if m == nil || m.store == nil {
		return lifecycleProjectionData{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var events []turn.TurnEventEnvelope
	var afterSeq uint64
	for {
		page, err := m.store.ListTurnEvents(ctx, sessionID, afterSeq, 1000)
		if err != nil {
			return lifecycleProjectionData{}, err
		}
		events = append(events, page...)
		if len(page) < 1000 {
			break
		}
		afterSeq = page[len(page)-1].SessionSeq
	}
	if len(events) == 0 {
		return lifecycleProjectionData{}, nil
	}
	coordinator := turn.NewTurnCoordinator(sessionID, agentID)
	if err := coordinator.Restore(events); err != nil {
		return lifecycleProjectionData{events: events}, err
	}
	return lifecycleProjectionData{
		snapshot: coordinator.Snapshot(),
		sequence: events[len(events)-1].SessionSeq,
		events:   events,
	}, nil
}

// loadLifecycleProjection restores the durable Turn/Step projection for a
// cold session. Runtime instances use the same restore path during startup;
// read-only APIs consult the same projection so every lifecycle view has one
// authority.
func (m *Manager) loadLifecycleProjection(ctx context.Context, sessionID, agentID string) (turn.CoordinatorSnapshot, bool, uint64, error) {
	if m == nil || m.store == nil {
		return turn.CoordinatorSnapshot{}, false, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; attempt < 2; attempt++ {
		latestID, eventCount, err := m.store.TurnEventLogVersion(ctx, sessionID)
		if err != nil {
			return turn.CoordinatorSnapshot{}, false, 0, err
		}
		key := lifecycleProjectionKey{sessionID: sessionID, latestID: latestID, eventCount: eventCount}
		if entry, ok := m.cachedLifecycleProjection(key); ok {
			return cloneCoordinatorSnapshot(entry.snapshot), entry.hasEvents, entry.sequence, nil
		}

		data, err := m.loadLifecycleProjectionData(ctx, sessionID, agentID)
		if err != nil {
			return data.snapshot, len(data.events) > 0, data.sequence, err
		}
		loadedID := int64(0)
		if len(data.events) > 0 {
			loadedID = data.events[len(data.events)-1].ID
		}
		currentID, currentCount, err := m.store.TurnEventLogVersion(ctx, sessionID)
		if err != nil {
			return turn.CoordinatorSnapshot{}, false, 0, err
		}
		if currentID != loadedID || currentCount != eventCount {
			if attempt == 0 {
				continue
			}
			// A continuously changing journal remains authoritative. Return the
			// just-loaded view without caching it under a version we did not read.
			return data.snapshot, len(data.events) > 0, data.sequence, nil
		}
		entry := lifecycleProjectionCacheEntry{
			snapshot:  cloneCoordinatorSnapshot(data.snapshot),
			hasEvents: len(data.events) > 0,
			sequence:  data.sequence,
		}
		m.cacheLifecycleProjection(key, entry)
		return cloneCoordinatorSnapshot(entry.snapshot), entry.hasEvents, entry.sequence, nil
	}
	return turn.CoordinatorSnapshot{}, false, 0, nil
}

func (m *Manager) cachedLifecycleProjection(key lifecycleProjectionKey) (lifecycleProjectionCacheEntry, bool) {
	m.lifecycleProjectionMu.Lock()
	defer m.lifecycleProjectionMu.Unlock()
	entry, ok := m.lifecycleProjectionCache[key]
	return entry, ok
}

func (m *Manager) cacheLifecycleProjection(key lifecycleProjectionKey, entry lifecycleProjectionCacheEntry) {
	m.lifecycleProjectionMu.Lock()
	defer m.lifecycleProjectionMu.Unlock()
	if m.lifecycleProjectionCache == nil {
		m.lifecycleProjectionCache = make(map[lifecycleProjectionKey]lifecycleProjectionCacheEntry)
	}
	if _, exists := m.lifecycleProjectionCache[key]; exists {
		return
	}
	for len(m.lifecycleProjectionOrder) >= lifecycleProjectionCacheCapacity {
		oldest := m.lifecycleProjectionOrder[0]
		m.lifecycleProjectionOrder = m.lifecycleProjectionOrder[1:]
		delete(m.lifecycleProjectionCache, oldest)
	}
	m.lifecycleProjectionOrder = append(m.lifecycleProjectionOrder, key)
	m.lifecycleProjectionCache[key] = entry
}

func cloneCoordinatorSnapshot(snapshot turn.CoordinatorSnapshot) turn.CoordinatorSnapshot {
	clone := snapshot
	if snapshot.TurnFinishedAt != nil {
		finishedAt := *snapshot.TurnFinishedAt
		clone.TurnFinishedAt = &finishedAt
	}
	clone.InteractionPayload = append(json.RawMessage(nil), snapshot.InteractionPayload...)
	clone.ToolExecutions = append([]turn.ToolExecutionView(nil), snapshot.ToolExecutions...)
	clone.ActiveToolCalls = make([]turn.ToolCallProjection, len(snapshot.ActiveToolCalls))
	for index, call := range snapshot.ActiveToolCalls {
		clone.ActiveToolCalls[index] = call
		clone.ActiveToolCalls[index].Arguments = append(json.RawMessage(nil), call.Arguments...)
	}
	if snapshot.ContextSnapshot != nil {
		clone.ContextSnapshot = snapshot.ContextSnapshot.Clone()
	}
	return clone
}

func turnStateFromCoordinatorSnapshot(snapshot turn.CoordinatorSnapshot) turn.State {
	if !snapshot.HasActiveTurn || snapshot.StepStatus.Terminal() {
		return turn.StateIdle
	}
	if snapshot.StepStatus == turn.StepStatusExecutingTools || snapshot.StepStatus == turn.StepStatusWaitingInteraction {
		return turn.StateAwaitingTool
	}
	return turn.StateModelStreaming
}
