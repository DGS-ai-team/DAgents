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

func TestBoundTranscriptDetailsPreservesLegacyBodyWithoutRoundDetailEndpoint(t *testing.T) {
	text := strings.Repeat("legacy body ", 2_000)
	entry := TranscriptEntry{
		"kind": "user", "id": "legacy-user", "item_id": "legacy-user", "detail_item_id": "legacy-user-record",
		"text": text,
	}

	bounded := BoundTranscriptDetails([]TranscriptEntry{entry})

	if len(bounded) != 1 || bounded[0]["text"] != text {
		t.Fatalf("legacy body was truncated without a usable detail endpoint: entry=%#v", bounded)
	}
	if bounded[0]["details_available"] == true {
		t.Fatalf("legacy entry advertised an unavailable detail endpoint: %#v", bounded[0])
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

func TestBoundTranscriptDetailsScopesHiddenCallsToSourceAssistant(t *testing.T) {
	entries := make([]TranscriptEntry, 0, 102)
	callItemIDs := make([]string, 51)
	for index := 0; index < 51; index++ {
		callID := fmt.Sprintf("call-%02d", index)
		if index == 0 || index == 50 {
			callID = "reused-call-id"
		}
		sourceID := fmt.Sprintf("assistant-step-%02d", index)
		itemID := ToolCallTranscriptItemID(sourceID, callID)
		callItemIDs[index] = itemID
		entries = append(entries, TranscriptEntry{
			"kind": "tool_call", "id": itemID, "item_id": itemID, "detail_item_id": sourceID,
			"round_id": "round-1", "round_revision_id": "revision-1",
			"data": map[string]any{"tool_call_id": callID, "tool_name": "read_file", "arguments": map[string]any{"path": "/tmp/probe"}},
		})
		entries = append(entries, TranscriptEntry{
			"kind": "tool_result", "id": "result-" + fmt.Sprintf("%02d", index), "item_id": "result-" + fmt.Sprintf("%02d", index),
			"detail_item_id": "result-record-" + fmt.Sprintf("%02d", index),
			"round_id": "round-1", "round_revision_id": "revision-1",
			"data":           map[string]any{"tool_call_id": callID, "tool_call_item_id": itemID, "tool_name": "read_file", "content": "done"},
		})
	}

	bounded := BoundTranscriptDetails(entries)
	var firstCallVisible, firstResultVisible, hiddenCallVisible, hiddenResultVisible bool
	for _, entry := range bounded {
		switch entry["kind"] {
		case "tool_call":
			id := transcriptCallItemID(entry)
			firstCallVisible = firstCallVisible || id == callItemIDs[0]
			hiddenCallVisible = hiddenCallVisible || id == callItemIDs[50]
		case "tool_result":
			id := transcriptCallItemID(entry)
			firstResultVisible = firstResultVisible || id == callItemIDs[0]
			hiddenResultVisible = hiddenResultVisible || id == callItemIDs[50]
		}
	}
	if !firstCallVisible || !firstResultVisible {
		t.Fatalf("visible call was hidden with a later reused provider ID: call=%v result=%v", firstCallVisible, firstResultVisible)
	}
	if hiddenCallVisible || hiddenResultVisible {
		t.Fatalf("hidden call/result leaked into initial summary: call=%v result=%v", hiddenCallVisible, hiddenResultVisible)
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

func TestBoundTranscriptDetailsTerminatesAndDropsOversizedInlineImagePayload(t *testing.T) {
	image := "data:image/png;base64," + strings.Repeat("A", MaxInitialTranscriptBytes+64*1024)
	entry := TranscriptEntry{
		"kind": "user", "id": "user-1", "item_id": "user-1", "detail_item_id": "user-record-1",
		"round_id": "round-1", "round_revision_id": "revision-1", "text": strings.Repeat("x", 200),
		"images": []string{image},
	}
	bounded := BoundTranscriptDetails([]TranscriptEntry{entry})
	encoded, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxInitialTranscriptBytes {
		t.Fatalf("projection with an inline image is %d bytes, limit %d", len(encoded), MaxInitialTranscriptBytes)
	}
	if strings.Contains(string(encoded), "data:image/png;base64") {
		t.Fatal("oversized inline image payload leaked into the summary projection")
	}
	if bounded[0]["details_available"] != true || bounded[0]["images_omitted"] != true {
		t.Fatalf("omitted image lost its detail affordance: %#v", bounded[0])
	}
}
