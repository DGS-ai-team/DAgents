package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestConversationSchemaV2MigrationAddsAndBackfillsToolCallIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-call-v2.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result := llm.Message{Role: "tool", ToolCallID: "call-backfill", Content: "done", RecordID: "legacy-result"}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_conversation_records_tool_call`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE conversation_records DROP COLUMN tool_call_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE conversation_schema_meta SET version=2 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind,created_at,updated_at) VALUES('agent-v2','legacy-result',1,'round-v2','revision-v2','tool',?,1,'',?,?)`, string(raw), now, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var got string
	if err := st.db.QueryRow(`SELECT tool_call_id FROM conversation_records WHERE conversation_id='agent-v2' AND item_id='legacy-result'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "call-backfill" {
		t.Fatalf("backfilled tool_call_id=%q", got)
	}
}

func TestConversationToolStepsPageKeepsCallResultPairsAndStableCursor(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "tool-steps.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	seedConversationHistory(t, st, "agent-tools", 1)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	calls := make([]llm.ToolCall, 125)
	for index := range calls {
		calls[index] = llm.ToolCall{ID: fmt.Sprintf("call-%03d", index+1), Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: fmt.Sprintf(`{"call_purpose":"read %d","path":"/tmp/%d"}`, index+1, index+1)}}
	}
	callMessage := llm.Message{Role: "assistant", ToolCalls: calls, RecordID: "round-1/tool-calls"}
	callRaw, err := json.Marshal(callMessage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'assistant',?,'',1,'',?,?)`, "agent-tools", callMessage.RecordID, 20, "round-1", "revision-1", string(callRaw), now, now); err != nil {
		t.Fatal(err)
	}
	for index, call := range calls {
		result := llm.Message{Role: "tool", Name: "read_file", ToolCallID: call.ID, ToolCallSourceID: callMessage.RecordID, Content: fmt.Sprintf("full result for %s", call.ID), ToolResultMetadata: &llm.ToolResultMetadata{Status: "succeeded"}, RecordID: fmt.Sprintf("round-1/result-%03d", index+1)}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'tool',?,?,1,'',?,?)`, "agent-tools", result.RecordID, 21+int64(index), "round-1", "revision-1", string(raw), call.ID, now, now); err != nil {
			t.Fatal(err)
		}
	}

	first, err := st.LoadConversationToolSteps(ctx, "agent-tools", "round-1", "revision-1", callMessage.RecordID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalCount != len(calls) || len(first.Steps) != 50 || !first.HasMore || first.NextAfterToolCallID != "call-050" {
		t.Fatalf("unexpected first tool page: count=%d len=%d has_more=%v cursor=%q", first.TotalCount, len(first.Steps), first.HasMore, first.NextAfterToolCallID)
	}
	if first.Steps[0].Call.ID != "call-001" || first.Steps[0].Result == nil || first.Steps[0].Result.Content != "full result for call-001" {
		t.Fatalf("tool call/result association was lost: %+v", first.Steps[0])
	}
	second, err := st.LoadConversationToolSteps(ctx, "agent-tools", "round-1", "revision-1", callMessage.RecordID, first.NextAfterToolCallID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Steps) != 50 || second.Steps[0].ToolCallID != "call-051" || second.NextAfterToolCallID != "call-100" {
		t.Fatalf("unexpected second tool page: len=%d first=%q cursor=%q", len(second.Steps), second.Steps[0].ToolCallID, second.NextAfterToolCallID)
	}
	last, err := st.LoadConversationToolSteps(ctx, "agent-tools", "round-1", "revision-1", callMessage.RecordID, second.NextAfterToolCallID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Steps) != 25 || last.Steps[0].ToolCallID != "call-101" || last.HasMore {
		t.Fatalf("unexpected last tool page: len=%d first=%q has_more=%v", len(last.Steps), last.Steps[0].ToolCallID, last.HasMore)
	}
	if _, err := st.LoadConversationToolSteps(ctx, "agent-tools", "round-1", "revision-1", callMessage.RecordID, "missing", 50); err != ErrConversationHistoryCursor {
		t.Fatalf("stale cursor error=%v", err)
	}
}

func TestConversationToolStepDetailLoadsOnlyRequestedPair(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "tool-step-detail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedConversationHistory(t, st, "agent-tool-detail", 1)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	call := llm.ToolCall{ID: "call-detail", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"/private/notes.txt"}`}}
	callMessage := llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}, RecordID: "round-1/calls"}
	callRaw, _ := json.Marshal(callMessage)
	if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'assistant',?,'',1,'',?,?)`, "agent-tool-detail", callMessage.RecordID, 20, "round-1", "revision-1", string(callRaw), now, now); err != nil {
		t.Fatal(err)
	}
	result := llm.Message{Role: "tool", Name: "read_file", ToolCallID: call.ID, ToolCallSourceID: callMessage.RecordID, Content: "full detail body", RecordID: "round-1/result"}
	raw, _ := json.Marshal(result)
	if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'tool',?,?,1,'',?,?)`, "agent-tool-detail", result.RecordID, 21, "round-1", "revision-1", string(raw), call.ID, now, now); err != nil {
		t.Fatal(err)
	}
	page, err := st.LoadConversationToolStep(context.Background(), "agent-tool-detail", "round-1", "revision-1", callMessage.RecordID, call.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Steps) != 1 || page.Steps[0].Result == nil || page.Steps[0].Result.Content != "full detail body" || page.Steps[0].Call.Function.Arguments != call.Function.Arguments {
		t.Fatalf("unexpected on-demand detail: %+v", page)
	}
	content, version, selected, err := st.LoadConversationItemContent(context.Background(), "agent-tool-detail", "round-1", "revision-1", "round-1/item-2", "content")
	if err != nil {
		t.Fatal(err)
	}
	if content != "response 1" || version != 1 || !selected {
		t.Fatalf("unexpected item detail: text=%q version=%d selected=%v", content, version, selected)
	}
}

func TestConversationToolStepDetailScopesRepeatedCallIDsToSourceAssistant(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "tool-step-source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedConversationHistory(t, st, "agent-tool-source", 1)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const callID = "provider-reused"
	for index, itemID := range []string{"round-1/assistant-first", "round-1/assistant-second"} {
		call := llm.ToolCall{ID: callID, Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"file-%d.txt"}`, index+1)}}
		assistant := llm.Message{Role: "assistant", RecordID: itemID, ToolCalls: []llm.ToolCall{call}}
		assistantRaw, err := json.Marshal(assistant)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'assistant',?,'',1,'',?,?)`, "agent-tool-source", itemID, 20+int64(index*2), "round-1", "revision-1", string(assistantRaw), now, now); err != nil {
			t.Fatal(err)
		}
		result := llm.Message{Role: "tool", Name: "read_file", ToolCallID: callID, ToolCallSourceID: itemID, Content: fmt.Sprintf("result-%d", index+1), RecordID: fmt.Sprintf("round-1/result-%d", index+1)}
		resultRaw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'tool',?,?,1,'',?,?)`, "agent-tool-source", result.RecordID, 21+int64(index*2), "round-1", "revision-1", string(resultRaw), callID, now, now); err != nil {
			t.Fatal(err)
		}
	}
	for index, itemID := range []string{"round-1/assistant-first", "round-1/assistant-second"} {
		page, err := st.LoadConversationToolStep(context.Background(), "agent-tool-source", "round-1", "revision-1", itemID, callID)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Steps) != 1 || page.Steps[0].Result == nil || page.Steps[0].Result.Content != fmt.Sprintf("result-%d", index+1) {
			t.Fatalf("detail for %s returned wrong repeated call result: %+v", itemID, page.Steps)
		}
	}
}
