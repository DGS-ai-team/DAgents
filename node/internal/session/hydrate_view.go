package session

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/media"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// HydrateView 为 GET /v1/agents/{id}/hydrate 的聚合视图。
type HydrateView struct {
	SessionID              string
	StreamEpoch            string
	CoveredAgentSeq        int
	CoveredStreamSeq       int
	AssistantDrafts        []stream.AssistantDraftSnapshot
	ConversationEpoch      uint64
	ConversationRevision   uint64
	Transcript             []TranscriptEntry
	HasEarlierHistory      bool
	WindowStartRound       int64
	WindowEndRound         int64
	PendingInputs          []PendingInputView
	TranscriptSynchronized bool
	PendingHITL            map[string]any
	TurnState              TurnStateView
	HasActiveTurn          bool
	QueuePending           int
	QueuePaused            bool
	QueueControlRevision   uint64
	HistoryRevision        uint64
	NotifySeq              int
	AckSeq                 int
	HasUnread              bool
	ChildAgents            []ChildAgentView
}

// ConversationWatermark is a content-free hint that lets Desktop discover
// missed updates without hydrating every conversation. It is a target to
// compare against local state, never proof that any SSE event was applied.
type ConversationWatermark struct {
	AgentID              string `json:"agent_id"`
	ConversationID       string `json:"conversation_id"`
	ConversationRevision uint64 `json:"conversation_revision"`
	LatestAgentSeq       int    `json:"latest_agent_seq"`
	ResultCommitStatus   string `json:"result_commit_status"`
}

// ListConversationWatermarks reads the stored revision metadata and overlays
// loaded runtimes without starting sessions or loading message payloads.
func (m *Manager) ListConversationWatermarks(ctx context.Context) ([]ConversationWatermark, error) {
	byID := make(map[string]ConversationWatermark)
	if m == nil {
		return []ConversationWatermark{}, nil
	}
	if m.store != nil {
		stored, err := m.store.ListConversationWatermarks(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range stored {
			id := strings.TrimSpace(row.ConversationID)
			if id == "" {
				continue
			}
			byID[id] = ConversationWatermark{
				AgentID: id, ConversationID: id,
				ConversationRevision: row.ConversationRevision,
				ResultCommitStatus:   "unknown",
			}
		}
	}

	for _, session := range m.ListActive() {
		if session == nil || strings.TrimSpace(session.ID) == "" {
			continue
		}
		rt := m.getRuntime(session.ID)
		if rt == nil {
			continue
		}
		// Match hydrate's lock order: lifecycle transition, then runtime state.
		rt.lifecycleMu.Lock()
		rt.mu.Lock()
		conversationRevision := rt.conversationRevision
		resultCommitStatus := strings.TrimSpace(rt.resultCommitStatus)
		activeTurn := false
		if rt.turnCoordinator != nil {
			activeTurn = rt.turnCoordinator.Snapshot().HasActiveTurn
		}
		rt.mu.Unlock()
		rt.lifecycleMu.Unlock()
		if resultCommitStatus == "" {
			if activeTurn {
				resultCommitStatus = "pending"
			} else {
				resultCommitStatus = "unknown"
			}
		}
		id := strings.TrimSpace(session.ID)
		byID[id] = ConversationWatermark{
			AgentID: id, ConversationID: id,
			ConversationRevision: conversationRevision,
			ResultCommitStatus:   resultCommitStatus,
		}
	}

	out := make([]ConversationWatermark, 0, len(byID))
	for id, item := range byID {
		if m.hub != nil {
			item.LatestAgentSeq = m.hub.CurrentAgentSeq(id)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out, nil
}

// PendingInputView is the bounded user-facing projection of queued input. It
// intentionally omits image bytes; the sending Desktop keeps optimistic media
// previews, while a reopened Desktop can still show the attachment count.
type PendingInputView struct {
	InputID         string              `json:"input_id"`
	ClientRequestID string              `json:"client_request_id,omitempty"`
	InputSeq        uint64              `json:"input_seq"`
	InputRevision   uint64              `json:"input_revision"`
	Status          string              `json:"status"`
	Content         string              `json:"content,omitempty"`
	ImageCount      int                 `json:"image_count,omitempty"`
	FileReferences  []llm.FileReference `json:"file_refs,omitempty"`
}

func pendingInputViews(conversationID string, records []InputRecord) []PendingInputView {
	out := make([]PendingInputView, 0, len(records))
	for _, record := range records {
		if record.Kind != InputKindUser {
			continue
		}
		revision := record.Revision
		if revision == 0 {
			revision = 1
		}
		status := "queued"
		if record.Completed {
			continue
		}
		if record.Claimed {
			status = "claimed"
		}
		imageCount := 0
		for _, part := range record.Env.ContentParts {
			if part.Type == "image_url" && part.ImageURL != nil {
				imageCount++
			}
		}
		refs := append([]llm.FileReference(nil), record.Env.FileReferences...)
		out = append(out, PendingInputView{
			InputID: llm.InputRecordID(conversationID, record.Seq), ClientRequestID: record.ClientRequestID, InputSeq: record.Seq,
			InputRevision: revision, Status: status, Content: record.Env.Content,
			ImageCount: imageCount, FileReferences: refs,
		})
	}
	return out
}

// GetHydrateView 返回 session hydrate 快照（活跃 runtime 或 DB 持久化）。
func (m *Manager) GetHydrateView(sessionID string) (*HydrateView, error) {
	return m.GetHydrateViewContext(context.Background(), sessionID)
}

// GetHydrateViewContext returns a hydrate snapshot while honoring cancellation
// from the HTTP request that is waiting for the durable transcript window.
func (m *Manager) GetHydrateViewContext(ctx context.Context, sessionID string) (*HydrateView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Capture the stream cursor before reading the projection. Events published
	// after this point are replayed by the client; open assistant deltas at the
	// cursor are included in the same Hub capture as their covered watermark.
	syncSnapshot := stream.AgentSyncSnapshot{AssistantDrafts: []stream.AssistantDraftSnapshot{}}
	if m.hub != nil {
		syncSnapshot = m.hub.CaptureAgentSyncSnapshot(sessionID)
	}
	rt := m.getRuntime(sessionID)
	if rt != nil {
		durableTranscript := m.store != nil && !rt.isChildSession()
		// Lifecycle transitions publish turn_state while holding lifecycleMu.
		// Take the same lock before copying messages so a hydrate snapshot cannot
		// combine a pre-commit history with a newer terminal projection.
		rt.lifecycleMu.Lock()
		rt.mu.Lock()
		var messages []llm.Message
		if !durableTranscript || rt.resultCommitStatus == "failed" {
			// Storeless and child-session runtimes still use their in-memory
			// transcript. Durable sessions are projected from the bounded store
			// window below and must not clone the model working set here.
			// A failed commit is the exceptional fallback that may need the full
			// in-memory transcript if the durable read also fails.
			messages = append([]llm.Message(nil), rt.messages...)
		}
		historyRevision := rt.historyRevision
		conversationEpoch := rt.sessionEpoch
		conversationRevision := rt.conversationRevision
		historyCommitDirty := rt.historyCommitDirty
		projectionBoundary := rt.toolCallProjectionHiddenThroughGeneration
		queuePending := rt.queue.Len()
		if rt.inputBox != nil {
			queuePending += rt.inputBox.Len()
		}
		pendingInputs := []InputRecord(nil)
		queuePaused, queueControlRevision := false, uint64(1)
		if rt.inputBox != nil {
			pendingInputs = rt.inputBox.PendingUserInputs()
			queuePaused, queueControlRevision = rt.inputBox.QueueControlState()
		}
		notifySeq := rt.notifySeq
		ackSeq := rt.ackSeq
		pending := rt.pendingSnapshot()
		resultCommitStatus := rt.resultCommitStatus
		lifecycle := rt.turnCoordinator.Snapshot()
		hasActiveTurn := lifecycle.HasActiveTurn
		lifecycle = hideClearedToolCallProjection(lifecycle, projectionBoundary)
		lifecycleSeq := rt.lifecycleEventSequence()
		rt.deltaMu.Lock()
		dirtyItemIDs := make(map[string]struct{}, len(rt.recordDeltaUpserts))
		dirtyRecordIDs := make(map[string]store.RecordOwnership, len(rt.recordDeltaUpserts))
		type sequencedDirtyMessage struct {
			sequence uint64
			message  llm.Message
		}
		dirtyMessagesBySequence := make([]sequencedDirtyMessage, 0, len(rt.recordDeltaUpserts))
		for recordID, pending := range rt.recordDeltaUpserts {
			if strings.TrimSpace(recordID) != "" {
				dirtyRecordIDs[recordID] = pending.Owner
				dirtyMessagesBySequence = append(dirtyMessagesBySequence, sequencedDirtyMessage{
					sequence: pending.Sequence,
					message:  llm.CloneMessage(pending.Message),
				})
			}
			for _, id := range transcriptItemIDsForMessage(pending.Message) {
				dirtyItemIDs[id] = struct{}{}
			}
		}
		hasDirtyTranscriptMutations := historyCommitDirty || len(rt.recordDeltaUpserts) > 0 || len(rt.recordDeltaDeletes) > 0
		rt.deltaMu.Unlock()
		rt.mu.Unlock()
		rt.lifecycleMu.Unlock()
		sort.Slice(dirtyMessagesBySequence, func(i, j int) bool {
			if dirtyMessagesBySequence[i].sequence != dirtyMessagesBySequence[j].sequence {
				return dirtyMessagesBySequence[i].sequence < dirtyMessagesBySequence[j].sequence
			}
			return llm.StoredRecordID(dirtyMessagesBySequence[i].message) < llm.StoredRecordID(dirtyMessagesBySequence[j].message)
		})
		dirtyMessages := make([]llm.Message, len(dirtyMessagesBySequence))
		for i, pending := range dirtyMessagesBySequence {
			dirtyMessages[i] = pending.message
		}
		viewSynchronized := false
		windowLoaded := false
		itemVersions := map[string]uint64(nil)
		untrustedItemIDs := dirtyItemIDs
		var mainWindow store.ConversationMainWindow
		if durableTranscript {
			var windowErr error
			mainWindow, windowErr = m.store.LoadConversationMainWindow(ctx, sessionID, 5, 180)
			if windowErr != nil {
				// The live runtime's recovery marker is the authoritative signal
				// when result persistence itself is broken. Keep hydrate available
				// so clients can leave the generating state; the live transcript
				// here is explicitly an in-memory recovery projection, not proof
				// that the durable records were committed.
				if resultCommitStatus != "failed" {
					return nil, windowErr
				}
				// Persistence failure is the only path that needs the in-memory
				// recovery transcript. Keep this exceptional copy out of normal
				// hydrate requests.
				addItemIDs(untrustedItemIDs, transcriptItemIDsForMessages(messages))
			} else {
				windowLoaded = true
				persistedMessages := conversationRecordMessages(mainWindow.Records)
				messages = mergeHydrateRecordHistory(persistedMessages, dirtyMessages, dirtyRecordIDs)
				itemVersions = transcriptItemVersions(mainWindow.Records)
				// Hydrate may merge explicit live deltas for responsiveness, but
				// that in-memory projection is not proof the durable transcript has
				// caught up. Keep the synchronization watermark behind until every
				// visible record mutation has committed.
				viewSynchronized = resultCommitStatus != "failed" && !hasDirtyTranscriptMutations
			}
		} else {
			addItemIDs(untrustedItemIDs, transcriptItemIDsForMessages(messages))
		}
		view := m.buildHydrateView(sessionID, messages, pending, lifecycle, lifecycleSeq, historyRevision, queuePending, hasActiveTurn, notifySeq, ackSeq, itemVersions, untrustedItemIDs)
		if durableTranscript && windowLoaded {
			view.ConversationRevision = mainWindow.ConversationRevision
			decorateTranscriptWithConversationOrder(view.Transcript, mainWindow.Records, messages, dirtyRecordIDs)
			view.HasEarlierHistory = mainWindow.HasEarlierHistory
			view.WindowStartRound = mainWindow.WindowStartOrdinal
			view.WindowEndRound = mainWindow.WindowEndOrdinal
		}
		view.Transcript = mergeActiveToolCallsAtAnchor(view.Transcript, lifecycle.ActiveToolCalls)
		if durableTranscript && windowLoaded {
			view.Transcript = BoundTranscriptDetails(view.Transcript)
		}
		view.ConversationEpoch = conversationEpoch
		// The durable main-window query returns its content and revision from
		// the same SQLite read transaction. Keep that revision paired with the
		// transcript it describes; runtime memory is updated just after a commit
		// and can briefly lag while the DB already exposes the new window.
		if !durableTranscript || !windowLoaded {
			view.ConversationRevision = conversationRevision
		}
		view.QueuePaused = queuePaused
		view.QueueControlRevision = queueControlRevision
		view.PendingInputs = pendingInputViews(sessionID, pendingInputs)
		view.TranscriptSynchronized = viewSynchronized
		view.StreamEpoch = syncSnapshot.StreamEpoch
		view.CoveredAgentSeq = syncSnapshot.CoveredAgentSeq
		view.CoveredStreamSeq = syncSnapshot.CoveredStreamSeq
		view.AssistantDrafts = syncSnapshot.AssistantDrafts
		view.TurnState.ResultCommitStatus = resultCommitStatus
		if resultCommitStatus == "failed" {
			view.HasActiveTurn = false
			view.TurnState.Phase = "recovery_required"
			view.TurnState.ExecutionStopped = true
			view.TurnState.RecoveryRequired = true
		}
		view.ChildAgents, _ = m.ListChildAgents(sessionID)
		return view, nil
	}
	if m.store == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	rec, err := m.store.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	mainWindow, err := m.store.LoadConversationMainWindow(ctx, sessionID, 5, 180)
	if err != nil {
		return nil, err
	}
	messages := conversationRecordMessages(mainWindow.Records)
	pending := (*turn.PendingHITL)(nil)
	hasActiveTurn := false
	lifecycle, projected, lifecycleSeq, projectionErr := m.loadLifecycleProjection(ctx, sessionID, rec.NodeID)
	if projectionErr != nil {
		m.logger.Warn("load persisted turn lifecycle projection failed", "session_id", sessionID, "error", projectionErr)
	} else if projected {
		pending = pendingFromLifecycleSnapshot(lifecycle)
		hasActiveTurn = lifecycle.HasActiveTurn
	}
	queuePending := inputBoxPendingCount(rec.RuntimeState.InputBoxState)
	pendingInputs := pendingUserInputsFromSnapshot(rec.RuntimeState.InputBoxState)
	if headerIsRowBacked(rec.RuntimeState.InputBoxState) {
		queuePending, err = m.store.CountQueuedInputs(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("count persisted queued inputs: %w", err)
		}
		queueRows, loadErr := m.store.LoadInputQueueByKind(ctx, sessionID, string(InputKindUser))
		if loadErr != nil {
			return nil, fmt.Errorf("load persisted user input queue: %w", loadErr)
		}
		pendingInputs, err = pendingUserInputsFromQueueRows(queueRows)
		if err != nil {
			return nil, err
		}
	}
	lifecycle = hideClearedToolCallProjection(lifecycle, rec.RuntimeState.ToolCallProjectionHiddenThroughGeneration)
	view := m.buildHydrateView(sessionID, messages, pending, lifecycle, lifecycleSeq, rec.RuntimeState.HistoryRevision, queuePending, hasActiveTurn, rec.RuntimeState.NotifySeq, rec.RuntimeState.AckSeq, transcriptItemVersions(mainWindow.Records), nil)
	decorateTranscriptWithConversationOrder(view.Transcript, mainWindow.Records, nil, nil)
	view.Transcript = mergeActiveToolCallsAtAnchor(view.Transcript, lifecycle.ActiveToolCalls)
	view.Transcript = BoundTranscriptDetails(view.Transcript)
	view.HasEarlierHistory = mainWindow.HasEarlierHistory
	view.WindowStartRound = mainWindow.WindowStartOrdinal
	view.WindowEndRound = mainWindow.WindowEndOrdinal
	view.ConversationEpoch, err = ConversationEpochFromSnapshot(rec.RuntimeState.InputBoxState)
	if err != nil {
		return nil, err
	}
	view.ConversationRevision = mainWindow.ConversationRevision
	view.QueuePaused, view.QueueControlRevision, err = QueueControlFromSnapshot(rec.RuntimeState.InputBoxState)
	if err != nil {
		return nil, err
	}
	view.PendingInputs = pendingInputViews(sessionID, pendingInputs)
	view.TranscriptSynchronized = true
	view.StreamEpoch = syncSnapshot.StreamEpoch
	view.CoveredAgentSeq = syncSnapshot.CoveredAgentSeq
	view.CoveredStreamSeq = syncSnapshot.CoveredStreamSeq
	view.AssistantDrafts = syncSnapshot.AssistantDrafts
	view.ChildAgents, _ = m.ListChildAgents(sessionID)
	return view, nil
}

func mergeHydrateRecordHistory(persisted, live []llm.Message, dirtyRecordIDs map[string]store.RecordOwnership) []llm.Message {
	out := append([]llm.Message(nil), persisted...)
	indices := make(map[string]int, len(out))
	for i, msg := range out {
		if id := strings.TrimSpace(msg.RecordID); id != "" {
			indices[id] = i
		}
	}
	for _, msg := range live {
		if msg.Name == llm.UserNameCompression || llm.IsMessageSource(msg, llm.MessageSourceCompression, "", "") {
			continue
		}
		id := strings.TrimSpace(msg.RecordID)
		if id == "" {
			continue
		}
		if _, dirty := dirtyRecordIDs[id]; !dirty {
			continue
		}
		if i, ok := indices[id]; ok {
			out[i] = msg
			continue
		}
		indices[id] = len(out)
		out = append(out, msg)
	}
	return out
}

func decorateTranscriptWithConversationOrder(entries []TranscriptEntry, records []store.ConversationRecord, liveMessages []llm.Message, dirtyRecordIDs map[string]store.RecordOwnership) {
	type itemOrder struct {
		ordinal      int64
		roundID      string
		roundOrdinal int64
		revisionID   string
		recordID     string
	}
	byItemID := make(map[string]itemOrder, len(records)*3+len(dirtyRecordIDs)*3)
	storedRecordIDs := make(map[string]struct{}, len(records))
	var lastOrdinal int64
	var activeRound itemOrder
	for _, record := range records {
		order := itemOrder{ordinal: record.Ordinal, roundID: record.RoundID, roundOrdinal: record.RoundOrdinal, revisionID: record.RevisionID, recordID: record.ItemID}
		if record.Ordinal > lastOrdinal {
			lastOrdinal = record.Ordinal
		}
		if record.ItemID != "" {
			storedRecordIDs[record.ItemID] = struct{}{}
		}
		if record.RoundStatus == "active" && record.RoundOrdinal >= activeRound.roundOrdinal {
			activeRound = itemOrder{roundID: record.RoundID, roundOrdinal: record.RoundOrdinal, revisionID: record.RevisionID, recordID: record.ItemID}
		}
		if record.ItemID != "" {
			byItemID[record.ItemID] = order
		}
		if identity := record.Payload.DisplayIdentity; identity != nil {
			if identity.ContentItemID != "" {
				byItemID[identity.ContentItemID] = order
			}
			if identity.ReasoningItemID != "" {
				byItemID[identity.ReasoningItemID] = order
			}
		}
		for _, call := range record.Payload.ToolCalls {
			if call.ID != "" {
				byItemID[ToolCallTranscriptItemID(record.Payload.RecordID, call.ID)] = order
			}
		}
	}
	// The runtime may already have accepted a live item whose durable delta has
	// not committed yet. Give only those explicit dirty records a provisional
	// ordinal so they remain visible at the end of the selected active round.
	// Older model-context messages are deliberately excluded from this path.
	for _, message := range liveMessages {
		recordID := strings.TrimSpace(message.RecordID)
		if recordID == "" {
			continue
		}
		if _, exists := storedRecordIDs[recordID]; exists {
			continue
		}
		owner, dirty := dirtyRecordIDs[recordID]
		if !dirty || activeRound.roundID == "" || owner.RoundID != activeRound.roundID || owner.RevisionID != activeRound.revisionID {
			continue
		}
		lastOrdinal++
		order := activeRound
		order.ordinal = lastOrdinal
		order.recordID = recordID
		byItemID[recordID] = order
		if message.DisplayIdentity != nil {
			if id := strings.TrimSpace(message.DisplayIdentity.ContentItemID); id != "" {
				byItemID[id] = order
			}
			if id := strings.TrimSpace(message.DisplayIdentity.ReasoningItemID); id != "" {
				byItemID[id] = order
			}
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "user") {
			if id := userTranscriptItemID(message); id != "" {
				byItemID[id] = order
			}
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "assistant") {
			if id := assistantTranscriptItemID(message, "content"); id != "" {
				byItemID[id] = order
			}
			if id := assistantTranscriptItemID(message, "reasoning"); id != "" {
				byItemID[id] = order
			}
			for _, call := range message.ToolCalls {
				if id := strings.TrimSpace(call.ID); id != "" {
					byItemID[ToolCallTranscriptItemID(message.RecordID, id)] = order
				}
			}
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
			if id := toolResultTranscriptItemID(message); id != "" {
				byItemID[id] = order
			}
		}
	}
	entryIndexes := make(map[int64]int)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		itemID, _ := entry["item_id"].(string)
		order, ok := byItemID[itemID]
		if !ok {
			continue
		}
		entry["conversation_ordinal"] = order.ordinal
		entry["conversation_entry_index"] = entryIndexes[order.ordinal]
		entryIndexes[order.ordinal]++
		if order.recordID != "" {
			entry["detail_item_id"] = order.recordID
		}
		if order.roundID != "" {
			entry["round_id"] = order.roundID
			entry["round_ordinal"] = order.roundOrdinal
			entry["round_revision_id"] = order.revisionID
		}
	}
}

func hideClearedToolCallProjection(snapshot turn.CoordinatorSnapshot, throughGeneration uint64) turn.CoordinatorSnapshot {
	if snapshot.Generation <= throughGeneration {
		snapshot.ActiveToolCalls = nil
	}
	return snapshot
}

func (m *Manager) buildHydrateView(
	sessionID string,
	messages []llm.Message,
	pending *turn.PendingHITL,
	lifecycle turn.CoordinatorSnapshot,
	lifecycleSeq uint64,
	historyRevision uint64,
	queuePending int,
	hasActiveTurn bool,
	notifySeq int,
	ackSeq int,
	itemVersions map[string]uint64,
	untrustedItemIDs map[string]struct{},
) *HydrateView {
	transcript := MessagesToTranscriptEntriesWithMedia(messages, m.mediaRegistry(sessionID))
	if transcript == nil {
		transcript = []TranscriptEntry{}
	}
	applyTranscriptItemVersions(transcript, itemVersions)
	clearUntrustedTranscriptItemVersions(transcript, untrustedItemIDs)
	if reg := m.mediaRegistry(sessionID); reg != nil {
		callIndex := buildToolCallIndex(messages)
		mediaByCall := media.RehydrateFromMessages(reg, messages, callIndex)
		EnrichTranscriptMedia(transcript, mediaByCall)
	}
	turnState := buildTurnStateView(lifecycle, lifecycleSeq)
	turnState.HistoryRevision = historyRevision
	return &HydrateView{
		SessionID:       sessionID,
		Transcript:      transcript,
		PendingHITL:     turn.BuildHITLRequiredSnapshot(pending),
		TurnState:       turnState,
		HasActiveTurn:   hasActiveTurn,
		QueuePending:    queuePending,
		HistoryRevision: historyRevision,
		NotifySeq:       notifySeq,
		AckSeq:          ackSeq,
		HasUnread:       notifySeq > ackSeq,
		ChildAgents:     []ChildAgentView{},
	}
}

func conversationRecordMessages(records []store.ConversationRecord) []llm.Message {
	messages := make([]llm.Message, 0, len(records))
	for _, record := range records {
		messages = append(messages, record.Payload)
	}
	return messages
}

func transcriptItemVersions(records []store.ConversationRecord) map[string]uint64 {
	versions := make(map[string]uint64, len(records))
	for _, record := range records {
		if record.Revision == 0 {
			continue
		}
		message := record.Payload
		versions[record.ItemID] = record.Revision
		if message.DisplayIdentity != nil {
			if id := strings.TrimSpace(message.DisplayIdentity.ContentItemID); id != "" {
				versions[id] = record.Revision
			}
			if id := strings.TrimSpace(message.DisplayIdentity.ReasoningItemID); id != "" {
				versions[id] = record.Revision
			}
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "user") {
			if id := userTranscriptItemID(message); id != "" {
				versions[id] = record.Revision
			}
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "assistant") {
			if id := assistantTranscriptItemID(message, "content"); id != "" {
				versions[id] = record.Revision
			}
			if id := assistantTranscriptItemID(message, "reasoning"); id != "" {
				versions[id] = record.Revision
			}
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
			if id := toolResultTranscriptItemID(message); id != "" {
				versions[id] = record.Revision
			}
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "assistant") {
			for _, call := range message.ToolCalls {
				if id := strings.TrimSpace(call.ID); id != "" {
					versions[ToolCallTranscriptItemID(message.RecordID, id)] = record.Revision
				}
			}
		}
	}
	return versions
}

func transcriptItemIDsForMessage(message llm.Message) []string {
	ids := make([]string, 0, 3+len(message.ToolCalls))
	if id := strings.TrimSpace(message.RecordID); id != "" {
		ids = append(ids, id)
	}
	if message.DisplayIdentity != nil {
		if id := strings.TrimSpace(message.DisplayIdentity.ContentItemID); id != "" {
			ids = append(ids, id)
		}
		if id := strings.TrimSpace(message.DisplayIdentity.ReasoningItemID); id != "" {
			ids = append(ids, id)
		}
	}
	if strings.EqualFold(strings.TrimSpace(message.Role), "user") {
		if id := userTranscriptItemID(message); id != "" {
			ids = append(ids, id)
		}
	}
	if strings.EqualFold(strings.TrimSpace(message.Role), "assistant") {
		if id := assistantTranscriptItemID(message, "content"); id != "" {
			ids = append(ids, id)
		}
		if id := assistantTranscriptItemID(message, "reasoning"); id != "" {
			ids = append(ids, id)
		}
		for _, call := range message.ToolCalls {
			if id := strings.TrimSpace(call.ID); id != "" {
				ids = append(ids, ToolCallTranscriptItemID(message.RecordID, id))
			}
		}
	}
	if strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
		if id := toolResultTranscriptItemID(message); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func transcriptItemIDsForMessages(messages []llm.Message) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, message := range messages {
		for _, id := range transcriptItemIDsForMessage(message) {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func addItemIDs(target map[string]struct{}, source map[string]struct{}) {
	for id := range source {
		target[id] = struct{}{}
	}
}

func applyTranscriptItemVersions(entries []TranscriptEntry, versions map[string]uint64) {
	if len(versions) == 0 {
		return
	}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		itemID, _ := entry["item_id"].(string)
		if version, ok := versions[itemID]; ok {
			entry["item_version"] = int(version)
		}
	}
}

func clearUntrustedTranscriptItemVersions(entries []TranscriptEntry, untrusted map[string]struct{}) {
	if len(untrusted) == 0 {
		return
	}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if itemID, _ := entry["item_id"].(string); itemID != "" {
			if _, ok := untrusted[itemID]; ok {
				delete(entry, "item_version")
			}
		}
	}
}

// mergeActiveToolCallsAtAnchor projects lifecycle calls that have not reached
// the transcript yet. It places them next to their assistant item when present,
// or immediately before their first result; the array tail is only a fallback
// when neither durable anchor is available.
func mergeActiveToolCallsAtAnchor(transcript []TranscriptEntry, calls []turn.ToolCallProjection) []TranscriptEntry {
	if len(calls) == 0 {
		return transcript
	}
	seen := make(map[string]bool, len(transcript))
	for _, entry := range transcript {
		if entry == nil {
			continue
		}
		kind, _ := entry["kind"].(string)
		if kind != "tool_call" {
			continue
		}
		itemID, _ := entry["item_id"].(string)
		data, _ := entry["data"].(map[string]any)
		callID, _ := data["tool_call_id"].(string)
		if strings.TrimSpace(callID) == "" {
			callID, _ = entry["blockId"].(string)
		}
		if strings.TrimSpace(itemID) == "" {
			sourceID, _ := entry["detail_item_id"].(string)
			if strings.TrimSpace(sourceID) == "" {
				sourceID, _ = entry["parent_item_id"].(string)
			}
			itemID = ToolCallTranscriptItemID(sourceID, callID)
		}
		if strings.TrimSpace(itemID) != "" {
			seen[strings.TrimSpace(itemID)] = true
		}
	}
	callIndexes := make(map[string]int, len(calls)*2)
	for i, call := range calls {
		id := strings.TrimSpace(call.ID)
		if id == "" {
			continue
		}
		index := call.BatchIndex
		if index < 0 {
			index = i
		}
		callIndexes[ToolCallTranscriptItemID(activeToolCallAssistantRecordID(call), id)] = index
		if _, exists := callIndexes[id]; !exists {
			callIndexes[id] = index
		}
	}
	parentID := ""
	for _, call := range calls {
		if parentID = activeToolCallAssistantRecordID(call); parentID != "" {
			break
		}
	}
	parentPrefix := ""
	if parentID != "" {
		parentPrefix = strings.TrimSuffix(parentID, "/assistant") + "/assistant/"
	}
	orderedCalls := append([]turn.ToolCallProjection(nil), calls...)
	sort.SliceStable(orderedCalls, func(i, j int) bool {
		return orderedCalls[i].BatchIndex < orderedCalls[j].BatchIndex
	})
	inserted := false
	for _, call := range orderedCalls {
		id := strings.TrimSpace(call.ID)
		itemID := ToolCallTranscriptItemID(activeToolCallAssistantRecordID(call), id)
		if id == "" || seen[itemID] || tools.IsAskUserInformation(call.ToolName) {
			continue
		}
		entry := toolCallEntryFromCall(llm.ToolCall{
			ID:       id,
			Type:     "function",
			Function: llm.ToolCallFunction{Name: call.ToolName, Arguments: string(call.Arguments)},
		}, nil)
		entry["item_id"] = itemID
		entry["id"] = itemID
		if parentID != "" {
			entry["parent_item_id"] = parentID
			entry["detail_item_id"] = parentID
			inheritActiveToolCallOrder(entry, transcript, parentID, parentPrefix)
		}
		if stepID := strings.TrimSpace(call.StepID); stepID != "" {
			entry["step_id"] = stepID
		}
		entry["tool_batch_index"] = call.BatchIndex
		insertAt := activeToolCallInsertionIndex(transcript, callIndexes, parentID, parentPrefix, call.BatchIndex)
		transcript = append(transcript, nil)
		copy(transcript[insertAt+1:], transcript[insertAt:])
		transcript[insertAt] = entry
		seen[itemID] = true
		inserted = true
	}
	if inserted {
		reindexTranscriptEntries(transcript)
	}
	return transcript
}

func activeToolCallAssistantRecordID(call turn.ToolCallProjection) string {
	if assistantID := strings.TrimSpace(call.AssistantMessageID); assistantID != "" {
		return assistantID
	}
	if stepID := strings.TrimSpace(call.StepID); stepID != "" {
		return strings.TrimSuffix(stepID, "/assistant") + "/assistant"
	}
	return ""
}

func activeToolCallInsertionIndex(entries []TranscriptEntry, callIndexes map[string]int, parentID, parentPrefix string, batchIndex int) int {
	parentEnd := -1
	for i, entry := range entries {
		if entry == nil {
			continue
		}
		detailID, _ := entry["detail_item_id"].(string)
		itemID, _ := entry["item_id"].(string)
		entryParent, _ := entry["parent_item_id"].(string)
		if parentID != "" && (strings.TrimSpace(detailID) == parentID || strings.TrimSpace(entryParent) == parentID || strings.HasPrefix(strings.TrimSpace(itemID), parentPrefix)) {
			parentEnd = i + 1
		}
	}

	lastEarlierCall := -1
	firstLaterCall := -1
	for i, entry := range entries {
		if entry == nil || entry["kind"] != "tool_call" {
			continue
		}
		data, _ := entry["data"].(map[string]any)
		itemID, _ := entry["item_id"].(string)
		id, _ := data["tool_call_id"].(string)
		if strings.TrimSpace(itemID) == "" {
			sourceID, _ := entry["detail_item_id"].(string)
			if strings.TrimSpace(sourceID) == "" {
				sourceID, _ = entry["parent_item_id"].(string)
			}
			itemID = ToolCallTranscriptItemID(sourceID, id)
		}
		existingIndex, ok := callIndexes[strings.TrimSpace(itemID)]
		if !ok {
			existingIndex, ok = callIndexes[strings.TrimSpace(id)]
		}
		if !ok {
			continue
		}
		if existingIndex < batchIndex && i > lastEarlierCall {
			lastEarlierCall = i
		}
		if existingIndex > batchIndex && (firstLaterCall < 0 || i < firstLaterCall) {
			firstLaterCall = i
		}
	}
	insertAt := len(entries)
	switch {
	case firstLaterCall >= 0:
		insertAt = firstLaterCall
	case lastEarlierCall >= 0:
		insertAt = lastEarlierCall + 1
	case parentEnd >= 0:
		insertAt = parentEnd
	}
	// A lifecycle call may race ahead of its transcript record. Never place the
	// missing call after a result from the same batch: that would invert the
	// provider's assistant-call/result order in the hydrated conversation.
	for i, entry := range entries {
		if entry == nil || entry["kind"] != "tool_result" {
			continue
		}
		data, _ := entry["data"].(map[string]any)
		id, _ := data["tool_call_id"].(string)
		itemID, _ := data["tool_call_item_id"].(string)
		_, ok := callIndexes[strings.TrimSpace(itemID)]
		if !ok {
			_, ok = callIndexes[strings.TrimSpace(id)]
		}
		if ok {
			if i < insertAt {
				insertAt = i
			}
			break
		}
	}
	return insertAt
}

func inheritActiveToolCallOrder(entry TranscriptEntry, entries []TranscriptEntry, parentID, parentPrefix string) {
	for _, candidate := range entries {
		if candidate == nil {
			continue
		}
		detailID, _ := candidate["detail_item_id"].(string)
		itemID, _ := candidate["item_id"].(string)
		if strings.TrimSpace(detailID) != parentID && !strings.HasPrefix(strings.TrimSpace(itemID), parentPrefix) {
			continue
		}
		for _, key := range []string{"conversation_ordinal", "round_id", "round_ordinal", "round_revision_id"} {
			if value, ok := candidate[key]; ok {
				entry[key] = value
			}
		}
		return
	}
}

func reindexTranscriptEntries(entries []TranscriptEntry) {
	indexes := make(map[int64]int)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		var ordinal int64
		switch value := entry["conversation_ordinal"].(type) {
		case int:
			ordinal = int64(value)
		case int64:
			ordinal = value
		case float64:
			ordinal = int64(value)
		default:
			continue
		}
		entry["conversation_entry_index"] = indexes[ordinal]
		indexes[ordinal]++
	}
}
