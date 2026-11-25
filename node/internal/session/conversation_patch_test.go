package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

type conversationPatchRecorder struct{ events []stream.Event }

func (p *conversationPatchRecorder) Publish(agentID, eventType string, data map[string]any) stream.Event {
	event := stream.Event{AgentID: agentID, Type: eventType, Data: data}
	p.events = append(p.events, event)
	return event
}

func (p *conversationPatchRecorder) PublishEphemeral(agentID, eventType string, data map[string]any) stream.Event {
	return p.Publish(agentID, eventType, data)
}

func TestCommittedTranscriptPatchPreservesRecordOrderIdentityAndBoundsBodies(t *testing.T) {
	assistant := llm.Message{
		Role: "assistant", Content: strings.Repeat("answer ", 5000),
		RecordID:        "record-assistant",
		DisplayIdentity: &llm.MessageDisplayIdentity{ContentItemID: "assistant-content", ReasoningItemID: "assistant-reasoning"},
		ToolCalls:       []llm.ToolCall{{ID: "call-a", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"call_purpose":"inspect"}`}}},
	}
	result := store.ConversationCommitResult{
		ChangedItems:    []store.ConversationItemRevision{{ItemID: assistant.RecordID, Ordinal: 2, Revision: 4, RoundID: "round-2", RevisionID: "revision-3", RoundOrdinal: 2}},
		ChangedPayloads: []llm.Message{assistant},
		RemovedItemIDs:  []string{"record-old"},
		RemovedPayloads: []llm.Message{{Role: "assistant", RecordID: "record-old", DisplayIdentity: &llm.MessageDisplayIdentity{ContentItemID: "old-content"}}},
	}
	entries, removed := committedTranscriptPatchItems(result)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want assistant content and tool call", entries)
	}
	if entries[0]["kind"] != "assistant" || entries[0]["item_id"] != "assistant-content" || entries[0]["item_version"] != uint64(4) {
		t.Fatalf("assistant identity/version = %#v", entries[0])
	}
	if entries[0]["conversation_ordinal"] != int64(2) || entries[0]["round_id"] != "round-2" || entries[0]["round_revision_id"] != "revision-3" {
		t.Fatalf("assistant order/round = %#v", entries[0])
	}
	if text, _ := entries[0]["text"].(string); len(text) > transcriptBodyPreviewBytes+8 || entries[0]["details_available"] != true {
		t.Fatalf("assistant body was not bounded: text bytes=%d entry=%#v", len(text), entries[0])
	}
	if entries[1]["kind"] != "tool_call" || entries[1]["item_id"] != "tool-call/call-a" || entries[1]["detail_item_id"] != assistant.RecordID {
		t.Fatalf("tool call projection = %#v", entries[1])
	}
	if len(removed) != 2 || removed[0] != "record-old" || removed[1] != "old-content" {
		t.Fatalf("removed display IDs = %#v", removed)
	}
}

func TestCommittedTranscriptPatchSkipsHiddenRecords(t *testing.T) {
	result := store.ConversationCommitResult{
		ChangedItems:    []store.ConversationItemRevision{{ItemID: "internal", Ordinal: 1, Revision: 1}},
		ChangedPayloads: []llm.Message{{Role: "user", RecordID: "internal", Name: llm.UserNameCompression, Content: "hidden", Source: &llm.MessageSource{Kind: llm.MessageSourceCompression, Form: llm.MessageFormSummary}}},
	}
	entries, removed := committedTranscriptPatchItems(result)
	if len(entries) != 0 || len(removed) != 0 {
		t.Fatalf("hidden record patch = entries %#v, removed %#v", entries, removed)
	}
}

func TestPublishConversationPatchUsesCommittedSafeTranscriptProjection(t *testing.T) {
	publisher := &conversationPatchRecorder{}
	message := llm.Message{Role: "assistant", Content: "answer", RecordID: "record-1", DisplayIdentity: &llm.MessageDisplayIdentity{ContentItemID: "content-1"}}
	runtime := &runtime{session: Session{ID: "agent-1"}, publisher: publisher}
	runtime.publishConversationPatch(store.ConversationCommitResult{
		ConversationRevision: 8,
		CheckpointRevision:   3,
		ChangedItems:         []store.ConversationItemRevision{{ItemID: "record-1", Ordinal: 5, Revision: 2, RoundID: "round-2", RevisionID: "revision-1", RoundOrdinal: 2}},
		ChangedPayloads:      []llm.Message{message},
	}, 12, "committed", nil, "", "", nil, nil)
	if len(publisher.events) != 1 || publisher.events[0].Type != "conversation_patch" {
		t.Fatalf("published events=%+v", publisher.events)
	}
	data := publisher.events[0].Data
	if data["conversation_revision"] != uint64(8) || data["context_revision"] != uint64(3) || data["result_commit_status"] != "committed" {
		t.Fatalf("patch revisions/status=%+v", data)
	}
	items, ok := data["upsert_items"].([]TranscriptEntry)
	if !ok || len(items) != 1 || items[0]["item_id"] != "content-1" || items[0]["detail_item_id"] != "record-1" {
		t.Fatalf("patch upsert projection=%#v", data["upsert_items"])
	}
}

func TestPublishConversationPatchCarriesCommittedQueueProjection(t *testing.T) {
	box := NewInputBox()
	if _, err := box.Append(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "queued message"}); err != nil {
		t.Fatal(err)
	}
	state, rows, _, _ := box.PersistenceState()
	publisher := &conversationPatchRecorder{}
	runtime := &runtime{session: Session{ID: "agent-1"}, publisher: publisher}
	runtime.publishConversationPatch(store.ConversationCommitResult{ConversationRevision: 1, CheckpointRevision: 1}, 1, "committed", nil, "", "", state, rows)
	if len(publisher.events) != 1 {
		t.Fatalf("published events=%+v", publisher.events)
	}
	data := publisher.events[0].Data
	if data["input_updates_complete"] != true {
		t.Fatalf("patch does not declare its queue projection complete: %+v", data)
	}
	updates, ok := data["input_updates"].([]PendingInputView)
	if !ok || len(updates) != 1 || updates[0].Content != "queued message" || updates[0].InputSeq != 1 {
		t.Fatalf("input updates=%#v", data["input_updates"])
	}
	queueState, ok := data["queue_state"].(map[string]any)
	if !ok || queueState["pending_count"] != 1 || queueState["paused"] != false || queueState["control_revision"] != uint64(1) {
		t.Fatalf("queue state=%#v", data["queue_state"])
	}
	if _, err := json.Marshal(data); err != nil {
		t.Fatalf("patch data is not JSON serializable: %v", err)
	}
}

func TestQueueOnlyChangePublishesWithoutTranscriptRevisionChange(t *testing.T) {
	box := NewInputBox()
	if _, err := box.Append(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "queued message"}); err != nil {
		t.Fatal(err)
	}
	publisher := &conversationPatchRecorder{}
	runtime := &runtime{
		session: Session{ID: "agent-1"}, publisher: publisher, inputBox: box,
		conversationRevision: 7, historyRevision: 9, contextCheckpointRevision: 4,
	}
	runtime.publishInputQueueSnapshot()
	if len(publisher.events) != 1 {
		t.Fatalf("published events=%+v, want one queue patch", publisher.events)
	}
	data := publisher.events[0].Data
	if data["conversation_revision"] != uint64(7) || data["history_revision"] != uint64(9) || data["context_revision"] != uint64(4) {
		t.Fatalf("queue-only patch advanced or lost transcript watermarks: %+v", data)
	}
	updates, ok := data["input_updates"].([]PendingInputView)
	if !ok || len(updates) != 1 || updates[0].Content != "queued message" {
		t.Fatalf("queue-only patch missing pending input: %#v", data["input_updates"])
	}
}
