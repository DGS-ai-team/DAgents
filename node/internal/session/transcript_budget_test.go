package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBoundTranscriptDetailsPagesLargeToolRecordWithoutBreakingPairs(t *testing.T) {
	entries := make([]TranscriptEntry, 0, 2000)
	for index := 1; index <= 1000; index++ {
		callID := fmt.Sprintf("call-%02d", index)
		callIDEntry := TranscriptEntry{"kind": "tool_call", "id": "tool-call/" + callID, "item_id": "tool-call/" + callID, "detail_item_id": "assistant-tools", "round_id": "round-1", "round_revision_id": "revision-1", "data": map[string]any{"tool_call_id": callID, "tool_name": "read_file", "arguments": map[string]any{"call_purpose": "inspect file", "path": fmt.Sprintf("/private/%d", index)}}}
		callIDEntry["summary"] = "read_file(inspect file)"
		entries = append(entries, callIDEntry)
	}
	for index := 1; index <= 1000; index++ {
		callID := fmt.Sprintf("call-%02d", index)
		entries = append(entries, TranscriptEntry{"kind": "tool_result", "id": "result-" + callID, "item_id": "result-" + callID, "detail_item_id": "result-" + callID, "round_id": "round-1", "round_revision_id": "revision-1", "data": map[string]any{"tool_call_id": callID, "tool_name": "read_file", "content": "very large result"}})
	}

	bounded := BoundTranscriptDetails(entries)
	var calls, results, markers int
	for _, entry := range bounded {
		switch entry["kind"] {
		case "tool_call":
			calls++
			data := entry["data"].(map[string]any)
			if _, exists := data["arguments"].(map[string]any)["path"]; exists {
				t.Fatalf("large arguments leaked into summary: %+v", data)
			}
		case "tool_result":
			results++
			data := entry["data"].(map[string]any)
			if _, exists := data["content"]; exists {
				t.Fatalf("result body leaked into summary: %+v", data)
			}
		case "tool_group_more":
			markers++
			if entry["remaining_count"] != 950 || entry["after_tool_call_id"] != "call-50" {
				t.Fatalf("bad pagination marker: %+v", entry)
			}
		}
	}
	if calls != 50 || results != 50 || markers != 1 {
		t.Fatalf("got %d calls, %d results, %d markers", calls, results, markers)
	}
	encoded, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxInitialTranscriptBytes {
		t.Fatalf("bounded transcript is %d bytes, limit %d", len(encoded), MaxInitialTranscriptBytes)
	}
}

func TestBoundTranscriptDetailsCapsLargeMessagesWithExplicitReference(t *testing.T) {
	long := make([]byte, maxInlineTranscriptBodyBytes+2048)
	for index := range long {
		long[index] = 'x'
	}
	entry := TranscriptEntry{"kind": "assistant", "id": "assistant-content", "item_id": "assistant-content", "detail_item_id": "assistant-record", "round_id": "round-2", "round_revision_id": "revision-2", "text": string(long)}
	bounded := BoundTranscriptDetails([]TranscriptEntry{entry})
	if len(bounded) != 1 || bounded[0]["details_available"] != true || bounded[0]["detail_item_id"] != "assistant-record" {
		t.Fatalf("large body did not retain explicit detail reference: %+v", bounded)
	}
	if got := len([]byte(bounded[0]["text"].(string))); got >= len(long) {
		t.Fatalf("body was not bounded: preview=%d original=%d", got, len(long))
	}
}

func TestBoundTranscriptDetailsCapsWholeVisualGroupAcrossSourceRecords(t *testing.T) {
	entries := make([]TranscriptEntry, 0, 120)
	for sourceIndex, start := range []int{1, 31} {
		sourceID := fmt.Sprintf("assistant-tools-%d", sourceIndex+1)
		for index := start; index < start+30; index++ {
			callID := fmt.Sprintf("call-%02d", index)
			entries = append(entries, TranscriptEntry{"kind": "tool_call", "id": "tool-call/" + callID, "item_id": "tool-call/" + callID, "detail_item_id": sourceID, "round_id": "round-group", "round_revision_id": "revision-group", "data": map[string]any{"tool_call_id": callID, "tool_name": "read_file"}})
		}
	}
	for index := 1; index <= 60; index++ {
		callID := fmt.Sprintf("call-%02d", index)
		entries = append(entries, TranscriptEntry{"kind": "tool_result", "id": "result-" + callID, "item_id": "result-" + callID, "detail_item_id": "result-" + callID, "round_id": "round-group", "round_revision_id": "revision-group", "data": map[string]any{"tool_call_id": callID, "content": "done"}})
	}
	bounded := BoundTranscriptDetails(entries)
	var calls, results, markers int
	for _, entry := range bounded {
		switch entry["kind"] {
		case "tool_call":
			calls++
		case "tool_result":
			results++
		case "tool_group_more":
			markers++
			if entry["detail_item_id"] != "assistant-tools-2" || entry["remaining_count"] != 10 {
				t.Fatalf("bad source-specific overflow: %+v", entry)
			}
		}
	}
	if calls != 50 || results != 50 || markers != 1 {
		t.Fatalf("whole group cap produced %d calls, %d results, %d markers", calls, results, markers)
	}
}

func TestBoundTranscriptDetailsEnforcesCombinedByteBudgetForMediumBodies(t *testing.T) {
	entries := make([]TranscriptEntry, 100)
	body := strings.Repeat("body ", 1800)
	for index := range entries {
		entries[index] = TranscriptEntry{"kind": "assistant", "id": fmt.Sprintf("assistant-%d", index), "item_id": fmt.Sprintf("assistant-%d", index), "detail_item_id": fmt.Sprintf("record-%d", index), "round_id": "round-wide", "round_revision_id": "revision-wide", "text": body}
	}
	bounded := BoundTranscriptDetails(entries)
	encoded, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxInitialTranscriptBytes {
		t.Fatalf("combined transcript is %d bytes, limit %d", len(encoded), MaxInitialTranscriptBytes)
	}
	var shortened, intact int
	originalBytes := len([]byte(body))
	for _, entry := range bounded {
		if len([]byte(entry["text"].(string))) < originalBytes {
			shortened++
			if entry["details_available"] != true || entry["text_bytes"] != originalBytes {
				t.Fatalf("shortened body lost its detail affordance or original length: %+v", entry)
			}
		} else {
			intact++
		}
	}
	if shortened == 0 || intact == 0 {
		t.Fatalf("expected budget enforcement to preserve some complete bodies and expose details for shortened older bodies; shortened=%d intact=%d", shortened, intact)
	}
}
