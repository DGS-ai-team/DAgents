package session

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestHydrateChildSessionUsesRuntimeTranscriptWhenManagerHasStore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "child-hydrate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	rt := newLifecycleTestRuntime()
	rt.session.ID = "child-session"
	rt.store = st
	rt.queue = queue.NewMessageQueue()
	rt.childMeta = &childRuntimeMeta{parentSessionID: "parent-session"}
	rt.messages = []llm.Message{
		{Role: "user", Content: "child task"},
		{Role: "assistant", Content: "child result"},
	}
	manager.sessions[rt.session.ID] = rt

	view, err := manager.GetHydrateView(rt.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Transcript) != 2 || view.Transcript[0]["kind"] != "user" || view.Transcript[0]["text"] != "child task" || view.Transcript[1]["kind"] != "assistant" || view.Transcript[1]["text"] != "child result" {
		t.Fatalf("child transcript = %#v, want its in-memory user / assistant exchange", view.Transcript)
	}
}

func TestActiveHydrateKeepsConversationRevisionPairedWithDurableWindow(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "hydrate-window-revision.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const sessionID = "session-window-revision"
	if err := st.SaveConversationSnapshot(ctx, store.Record{
		AgentID: sessionID,
		NodeID:  "agent-1",
		Messages: []llm.Message{{
			Role: "user", Content: "latest committed input", RecordID: sessionID + "/input/1",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	committed, err := st.PersistRuntimeDelta(ctx, store.Record{AgentID: sessionID, NodeID: "agent-1"}, &store.ConversationDelta{
		ConversationID:   sessionID,
		NodeID:           "agent-1",
		ExpectedRevision: 0,
		Upserts: []llm.Message{{
			Role: "assistant", Content: "latest committed reply", RecordID: "step-window-revision/assistant",
			DisplayIdentity: llm.AssistantDisplayIdentityForStep("step-window-revision"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	window, err := st.LoadConversationMainWindow(ctx, sessionID, 5, 180)
	if err != nil {
		t.Fatal(err)
	}
	if window.ConversationRevision != committed.ConversationRevision || window.ConversationRevision == 0 {
		t.Fatalf("fixture window revision=%d, commit revision=%d", window.ConversationRevision, committed.ConversationRevision)
	}

	// Store commit and runtime-memory acknowledgement are separate instructions.
	// Simulate a read arriving in that small gap: SQLite has the new window while
	// the active runtime still advertises its previous revision.
	rt := newLifecycleTestRuntime()
	rt.session.ID = sessionID
	rt.conversationRevision = window.ConversationRevision - 1
	rt.queue = queue.NewMessageQueue()
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	manager.sessions[sessionID] = rt

	view, err := manager.GetHydrateView(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ConversationRevision != window.ConversationRevision {
		t.Fatalf("hydrate revision=%d, want durable window revision=%d", view.ConversationRevision, window.ConversationRevision)
	}
	if len(view.Transcript) != 2 || view.Transcript[0]["text"] != "latest committed input" || view.Transcript[1]["text"] != "latest committed reply" {
		t.Fatalf("hydrate transcript is not the durable window described by its revision: %#v", view.Transcript)
	}
}

func TestAssistantItemIdentitySurvivesStoreRestartAndHydrate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "identity.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	message := llm.Message{
		Role:             "assistant",
		Content:          "final answer",
		ReasoningContent: "final reasoning",
		DisplayIdentity:  llm.AssistantDisplayIdentityForStep("step-durable"),
	}
	if err := st.SaveConversationSnapshot(context.Background(), store.Record{AgentID: "session-durable", NodeID: "agent-1", Messages: []llm.Message{message}}); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	manager := NewManager("agent-1", nil, nil, nil, nil, reopened, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	view, err := manager.GetHydrateView("session-durable")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Transcript) != 2 {
		t.Fatalf("transcript = %#v, want reasoning and assistant entries", view.Transcript)
	}
	wantIDs := []string{"step-durable/assistant/reasoning", "step-durable/assistant/content"}
	for i, want := range wantIDs {
		entry := view.Transcript[i]
		if entry["item_id"] != want || entry["id"] != want || entry["item_version"] != 1 {
			t.Fatalf("entry %d identity = %#v, want %q version 1", i, entry, want)
		}
	}
}

func TestHydrateProjectsPersistedItemRowRevisionsForVisibleKinds(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hydrate-item-versions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	assistant := llm.Message{
		Role:             "assistant",
		Content:          "answer v1",
		ReasoningContent: "reason v1",
		RecordID:         "step-version/assistant",
		DisplayIdentity:  llm.AssistantDisplayIdentityForStep("step-version"),
		ToolCalls: []llm.ToolCall{{
			ID: "call-version", Type: "function",
			Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`},
		}},
	}
	toolResult := llm.ToolResultMessage("call-version", "read_file", "file contents")
	toolResult.RecordID = llm.ToolResultRecordID("call-version")
	messages := []llm.Message{
		{Role: "user", Content: "question", RecordID: "session-version/input/1"},
		assistant,
		toolResult,
	}
	if err := st.SaveConversationSnapshot(ctx, store.Record{AgentID: "session-version", NodeID: "agent-1", Messages: messages, RuntimeState: store.RuntimeState{HistoryRevision: 1}}); err != nil {
		t.Fatal(err)
	}
	assistant.Content = "answer v2"
	assistant.ReasoningContent = "reason v2"
	if _, err := st.PersistRuntimeDelta(ctx, store.Record{AgentID: "session-version", NodeID: "agent-1", RuntimeState: store.RuntimeState{HistoryRevision: 2}}, &store.ConversationDelta{
		ConversationID: "session-version", NodeID: "agent-1", ExpectedRevision: 1, HistoryRevision: 2, Upserts: []llm.Message{assistant},
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	view, err := manager.GetHydrateView("session-version")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		kind    string
		version int
	}{
		"session-version/input/1":              {kind: "user", version: 1},
		"step-version/assistant/reasoning":     {kind: "reasoning", version: 2},
		"step-version/assistant/content":       {kind: "assistant", version: 2},
		"tool-call/call-version":               {kind: "tool_call", version: 2},
		llm.ToolResultRecordID("call-version"): {kind: "tool_result", version: 1},
	}
	for _, entry := range view.Transcript {
		id, _ := entry["item_id"].(string)
		expect, ok := want[id]
		if !ok {
			continue
		}
		if entry["kind"] != expect.kind || entry["item_version"] != expect.version {
			t.Fatalf("item %q projection=%#v want kind=%s version=%d", id, entry, expect.kind, expect.version)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("hydrate omitted visible items: %v; transcript=%#v", want, view.Transcript)
	}
}

func TestHydrateOmitsDurableVersionForChangedLiveOverlay(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hydrate-live-overlay-version.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	identity := llm.AssistantDisplayIdentityForStep("step-live")
	durable := llm.Message{Role: "assistant", Content: "old durable text", RecordID: "step-live/assistant", DisplayIdentity: identity}
	if err := st.SaveConversationSnapshot(context.Background(), store.Record{AgentID: "session-live-version", NodeID: "agent-1", Messages: []llm.Message{durable}}); err != nil {
		t.Fatal(err)
	}
	live := durable
	live.Content = "new uncommitted text"
	rt := newLifecycleTestRuntime()
	rt.session.ID = "session-live-version"
	rt.store = st
	rt.queue = queue.NewMessageQueue()
	rt.messages = []llm.Message{live}
	rt.recordDeltaUpserts = map[string]pendingRecordMutation{
		live.RecordID: {Message: live, Sequence: 1},
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	manager.sessions[rt.session.ID] = rt
	view, err := manager.GetHydrateView(rt.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range view.Transcript {
		if entry["kind"] == "assistant" {
			if entry["text"] != "new uncommitted text" {
				t.Fatalf("live overlay missing: %#v", entry)
			}
			if _, ok := entry["item_version"]; ok {
				t.Fatalf("uncommitted live payload claimed old durable version: %#v", entry)
			}
			return
		}
	}
	t.Fatalf("assistant item missing: %#v", view.Transcript)
}

func TestHydrateUsesDurableTranscriptWhenModelContextHasChangedCleanCopy(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hydrate-clean-context-copy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	durable := llm.Message{Role: "assistant", Content: "original displayed answer", RecordID: "step-clean/assistant", DisplayIdentity: llm.AssistantDisplayIdentityForStep("step-clean")}
	if err := st.SaveConversationSnapshot(context.Background(), store.Record{AgentID: "session-clean-context", NodeID: "agent-1", Messages: []llm.Message{durable}}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	rt := newLifecycleTestRuntime()
	rt.session.ID = "session-clean-context"
	rt.store = st
	rt.queue = queue.NewMessageQueue()
	compressedContextCopy := durable
	compressedContextCopy.Content = "shortened model context summary"
	rt.messages = []llm.Message{compressedContextCopy}
	manager.sessions[rt.session.ID] = rt

	view, err := manager.GetHydrateView(rt.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range view.Transcript {
		if entry["kind"] != "assistant" {
			continue
		}
		if entry["text"] != "original displayed answer" || entry["item_version"] != 1 {
			t.Fatalf("clean model-context copy replaced durable display history: %#v", entry)
		}
		return
	}
	t.Fatalf("durable assistant record missing from hydrate: %#v", view.Transcript)
}

func TestDecorateHydrateAssignsProvisionalOrderOnlyToDirtyActiveRoundItems(t *testing.T) {
	identity := llm.AssistantDisplayIdentityForStep("step-live-order")
	message := llm.Message{
		Role:             "assistant",
		Content:          "streaming answer",
		ReasoningContent: "streaming reasoning",
		RecordID:         "step-live-order/assistant",
		DisplayIdentity:  identity,
		ToolCalls: []llm.ToolCall{{
			ID: "call-live-order", Type: "function",
			Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`},
		}},
	}
	records := []store.ConversationRecord{{
		ItemID: "active/input/1", Ordinal: 100, RoundID: "round-active", RoundOrdinal: 8,
		RoundStatus: "active", RevisionID: "revision-active",
	}}
	entries := []TranscriptEntry{
		{"item_id": identity.ReasoningItemID, "kind": "reasoning"},
		{"item_id": identity.ContentItemID, "kind": "assistant"},
		{"item_id": "tool-call/call-live-order", "kind": "tool_call"},
		{"item_id": "old-dirty/assistant/content", "kind": "assistant"},
	}
	dirty := map[string]store.RecordOwnership{
		message.RecordID:      {RoundID: "round-active", RevisionID: "revision-active"},
		"old-dirty/assistant": {RoundID: "round-old", RevisionID: "revision-old"},
	}

	decorateTranscriptWithConversationOrder(entries, records, []llm.Message{message, {
		Role: "assistant", RecordID: "old-dirty/assistant",
		DisplayIdentity: llm.AssistantDisplayIdentityForStep("old-dirty"),
	}}, dirty)

	for index, entry := range entries[:3] {
		if got := entry["conversation_ordinal"]; got != int64(101) {
			t.Fatalf("entry %d provisional ordinal=%#v, want 101", index, got)
		}
		if got := entry["round_id"]; got != "round-active" {
			t.Fatalf("entry %d round_id=%#v", index, got)
		}
		if got := entry["round_revision_id"]; got != "revision-active" {
			t.Fatalf("entry %d revision_id=%#v", index, got)
		}
	}
	if entries[0]["conversation_entry_index"] != 0 || entries[1]["conversation_entry_index"] != 1 || entries[2]["conversation_entry_index"] != 2 {
		t.Fatalf("provisional children are not stably indexed: %#v", entries[:3])
	}
	if _, ok := entries[3]["conversation_ordinal"]; ok {
		t.Fatalf("dirty prior-round item received an active-tail ordinal: %#v", entries[3])
	}
}

func TestAssistantDraftSSEIdentityMatchesFinalHydrateItemRevision(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "assistant-stream-hydrate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hub := stream.NewHub(64, logx.Discard())
	registry, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager("agent-1", hub, &llm.MockClient{}, registry, policy.NewDefaultEngine(), st, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer manager.Stop()
	sessionRecord, _, err := manager.Create("")
	if err != nil {
		t.Fatal(err)
	}
	events := hub.SubscribeAgent(0, sessionRecord.ID)
	defer hub.Unsubscribe(events)
	if _, err := manager.EnqueueMessage(context.Background(), sessionRecord.ID, "message", "identity handoff", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		stored, err := st.ListTurnEvents(context.Background(), sessionRecord.ID, 0, 200)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range stored {
			if event.EventType == turn.EventTurnCompleted {
				completed = true
				break
			}
		}
		if completed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !completed {
		t.Fatal("assistant turn did not complete")
	}
	var streamItemID string
	for len(events) > 0 {
		event := <-events
		if event.Type == "conversation_delta" && event.Data["part"] == "content" && event.Data["display_type"] == "delta" {
			if id, ok := event.Data["item_id"].(string); ok && id != "" {
				streamItemID = id
				break
			}
		}
	}
	if streamItemID == "" {
		t.Fatal("assistant draft SSE did not carry item identity")
	}
	view, err := manager.GetHydrateView(sessionRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range view.Transcript {
		if entry["kind"] == "assistant" && entry["item_id"] == streamItemID {
			if entry["id"] != streamItemID || entry["item_version"] != 1 {
				t.Fatalf("final hydrate item=%#v, want SSE identity %q at initial row revision 1", entry, streamItemID)
			}
			return
		}
	}
	t.Fatalf("final hydrate omitted SSE draft item %q: %#v", streamItemID, view.Transcript)
}

func TestFailedResultCommitNeverClaimsTranscriptSynchronizedWhenRecordReadSucceeds(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hydrate-failed-status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SaveConversationSnapshot(ctx, store.Record{AgentID: "session-failed", NodeID: "agent-1", Messages: []llm.Message{{Role: "assistant", Content: "previous durable answer", RecordID: "old/answer"}}}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	rt := newLifecycleTestRuntime()
	rt.session.ID = "session-failed"
	rt.store = st
	rt.queue = queue.NewMessageQueue()
	rt.messages = []llm.Message{{Role: "assistant", Content: "new uncommitted answer", RecordID: "new/answer"}}
	rt.resultCommitStatus = "failed"
	rt.executionStopped = true
	manager.sessions[rt.session.ID] = rt
	view, err := manager.GetHydrateView(rt.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.TranscriptSynchronized {
		t.Fatal("hydrate claimed synchronized after failed commit despite successful old-record read")
	}
	if view.TurnState.Phase != "recovery_required" || !view.TurnState.RecoveryRequired || view.HasActiveTurn {
		t.Fatalf("failed commit view=%+v", view)
	}
}

func TestHydrateDoesNotClaimDirtyTranscriptSynchronized(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hydrate-dirty-status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SaveConversationSnapshot(ctx, store.Record{AgentID: "session-dirty", NodeID: "agent-1", Messages: []llm.Message{{Role: "assistant", Content: "previous durable answer", RecordID: "old/answer"}}}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	rt := newLifecycleTestRuntime()
	rt.session.ID = "session-dirty"
	rt.store = st
	rt.queue = queue.NewMessageQueue()
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "assistant", Content: "previous durable answer", RecordID: "old/answer"},
		{Role: "assistant", Content: "new uncommitted answer", RecordID: "new/answer"},
	}
	rt.noteRecordUpsertLocked(rt.messages[1])
	rt.mu.Unlock()
	manager.sessions[rt.session.ID] = rt

	view, err := manager.GetHydrateView(rt.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAssistantText(view.Transcript, "new uncommitted answer") {
		t.Fatalf("hydrate omitted live dirty record: %#v", view.Transcript)
	}
	if view.TranscriptSynchronized {
		t.Fatal("hydrate claimed synchronized while a live transcript delta was uncommitted")
	}

	if err := rt.persist(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = manager.GetHydrateView(rt.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.TranscriptSynchronized {
		t.Fatalf("hydrate remained unsynchronized after the dirty record committed: %+v", view)
	}
}

func containsAssistantText(entries []TranscriptEntry, want string) bool {
	for _, entry := range entries {
		if entry["kind"] == "assistant" && strings.Contains(fmt.Sprint(entry["text"]), want) {
			return true
		}
	}
	return false
}

func TestRuntimeDeltaRemainsDirtyAfterFailedTransactionAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-delta-retry.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rt := &runtime{session: Session{ID: "session-delta", AgentID: "agent-1"}, store: st, recordDeltaUpserts: make(map[string]pendingRecordMutation), recordDeltaDeletes: make(map[string]uint64)}
	message := llm.Message{Role: "assistant", Content: "answer", RecordID: "step/assistant/content"}
	rt.mu.Lock()
	rt.noteRecordUpsertLocked(message)
	rt.mu.Unlock()
	triggerDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := triggerDB.Exec(`CREATE TRIGGER fail_runtime_delta BEFORE INSERT ON conversation_records BEGIN SELECT RAISE(ABORT,'injected record failure'); END`); err != nil {
		t.Fatal(err)
	}
	_ = triggerDB.Close()
	if err := rt.persist(context.Background()); err == nil {
		t.Fatal("expected injected delta failure")
	}
	rt.deltaMu.Lock()
	pending := rt.recordDeltaUpserts[message.RecordID]
	rt.deltaMu.Unlock()
	if pending.Message.Content != "answer" {
		t.Fatal("failed transaction discarded dirty item")
	}
	triggerDB, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := triggerDB.Exec(`DROP TRIGGER fail_runtime_delta`); err != nil {
		t.Fatal(err)
	}
	_ = triggerDB.Close()
	if err := rt.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	rt.deltaMu.Lock()
	remaining := len(rt.recordDeltaUpserts)
	rt.deltaMu.Unlock()
	if remaining != 0 {
		t.Fatalf("successful retry left %d dirty items", remaining)
	}
	records, err := st.LoadConversationRecords(context.Background(), rt.session.ID)
	if err != nil || len(records) != 1 || records[0].Content != "answer" {
		t.Fatalf("records=%+v err=%v", records, err)
	}
}
