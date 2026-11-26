package session

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	MaxInitialToolSummariesPerGroup = 50
	MaxInitialTranscriptBytes       = 512 * 1024
	maxInlineTranscriptBodyBytes    = 16 * 1024
	transcriptBodyPreviewBytes      = 2048
)

// BoundTranscriptDetails keeps the first-render projection small without
// changing durable records. Tool results/arguments are fetched by source
// assistant record and provider call ID;
// oversized user/assistant text is represented by an explicit detail link.
func BoundTranscriptDetails(entries []TranscriptEntry) []TranscriptEntry {
	if len(entries) == 0 {
		return []TranscriptEntry{}
	}

	type toolSource struct {
		groupID     int
		sourceID    string
		visible     int
		total       int
		lastVisible string
		firstIndex  int
		meta        TranscriptEntry
	}
	sources := map[string]*toolSource{}
	hiddenCalls := map[string]struct{}{}
	hiddenCallIDs := map[string]struct{}{}
	groupEnds := map[int]int{}
	groupVisible := map[int]int{}
	groupID := -1
	inToolGroup := false
	for index, entry := range entries {
		if entry == nil {
			continue
		}
		kind := transcriptString(entry["kind"])
		if kind != "tool_call" && kind != "tool_result" {
			if kind != "reasoning" {
				inToolGroup = false
			}
			continue
		}
		if !inToolGroup {
			groupID++
			inToolGroup = true
		}
		groupEnds[groupID] = index
		if kind != "tool_call" {
			continue
		}
		sourceID := transcriptString(entry["detail_item_id"])
		callID := transcriptCallID(entry)
		if sourceID == "" || callID == "" || !hasTranscriptDetailEndpoint(entry) {
			continue
		}
		sourceKey := fmt.Sprintf("%d\x00%s", groupID, sourceID)
		source := sources[sourceKey]
		if source == nil {
			source = &toolSource{groupID: groupID, sourceID: sourceID, firstIndex: index, meta: entry}
			sources[sourceKey] = source
		}
		source.total++
		if groupVisible[groupID] < MaxInitialToolSummariesPerGroup {
			source.visible++
			groupVisible[groupID]++
			source.lastVisible = callID
		} else {
			hiddenCallIDs[callID] = struct{}{}
			if itemID := transcriptCallItemID(entry); itemID != "" {
				hiddenCalls[itemID] = struct{}{}
			}
		}
	}

	markerAt := make(map[int][]TranscriptEntry)
	sourceList := make([]*toolSource, 0, len(sources))
	for _, source := range sources {
		sourceList = append(sourceList, source)
	}
	sort.Slice(sourceList, func(i, j int) bool {
		if sourceList[i].groupID != sourceList[j].groupID {
			return sourceList[i].groupID < sourceList[j].groupID
		}
		return sourceList[i].firstIndex < sourceList[j].firstIndex
	})
	for _, source := range sourceList {
		if source.total <= source.visible {
			continue
		}
		base := source.meta
		id := "tool-more/" + source.sourceID + "/" + source.lastVisible
		if source.lastVisible == "" {
			id = "tool-more/" + source.sourceID + "/start"
		}
		marker := TranscriptEntry{
			"kind": "tool_group_more", "id": id, "item_id": id,
			"detail_item_id": source.sourceID, "tool_call_item_id": source.sourceID,
			"after_tool_call_id": source.lastVisible,
			"remaining_count":    source.total - source.visible,
		}
		for _, key := range []string{"round_id", "round_ordinal", "round_revision_id", "conversation_ordinal"} {
			if value, ok := base[key]; ok {
				marker[key] = value
			}
		}
		markerAt[groupEnds[source.groupID]] = append(markerAt[groupEnds[source.groupID]], marker)
	}

	out := make([]TranscriptEntry, 0, len(entries)+len(markerAt))
	for index, entry := range entries {
		if entry == nil {
			continue
		}
		include := true
		if entry["kind"] == "tool_call" {
			itemID := transcriptCallItemID(entry)
			_, hidden := hiddenCalls[itemID]
			if itemID == "" {
				_, hidden = hiddenCallIDs[transcriptCallID(entry)]
			}
			if hidden {
				include = false
			} else if hasTranscriptDetailEndpoint(entry) {
				compactToolCallEntry(entry)
			}
		} else if entry["kind"] == "tool_result" {
			if itemID := transcriptCallItemID(entry); itemID != "" {
				_, hidden := hiddenCalls[itemID]
				if hidden {
					include = false
				} else if hasTranscriptDetailEndpoint(entry) {
					compactToolResultEntry(entry)
				}
			} else if _, hidden := hiddenCallIDs[transcriptCallID(entry)]; hidden {
				include = false
			} else if hasTranscriptDetailEndpoint(entry) {
				compactToolResultEntry(entry)
			}
		} else if entry["kind"] == "assistant" || entry["kind"] == "user" {
			if hasTranscriptDetailEndpoint(entry) {
				compactTranscriptBody(entry)
			}
		}
		if include {
			out = append(out, entry)
		}
		if markers := markerAt[index]; len(markers) > 0 {
			out = append(out, markers...)
		}
	}
	if len(out) == 0 {
		return []TranscriptEntry{}
	}
	return enforceInitialTranscriptByteBudget(out)
}

func compactToolCallEntry(entry TranscriptEntry) {
	entry["details_available"] = true
	data, _ := entry["data"].(map[string]any)
	if data == nil {
		return
	}
	args, _ := data["arguments"].(map[string]any)
	purpose := transcriptString(args["call_purpose"])
	if len(args) > 0 {
		if purpose != "" {
			purpose = boundedPurpose(purpose)
			data["arguments"] = map[string]any{"call_purpose": purpose}
		} else {
			delete(data, "arguments")
		}
	}
	compactToolSummary(entry, data)
}

func compactToolResultEntry(entry TranscriptEntry) {
	data, _ := entry["data"].(map[string]any)
	if data == nil {
		return
	}
	if content, ok := data["content"].(string); ok && content != "" {
		data["content_bytes"] = len([]byte(content))
		delete(data, "content")
		entry["details_available"] = true
	}
	compactToolSummary(entry, data)
}

func compactToolSummary(entry TranscriptEntry, data map[string]any) {
	for _, key := range []string{"summary"} {
		if value, ok := entry[key].(string); ok && len([]byte(value)) > 384 {
			entry[key] = utf8BytePrefix(value, 384) + "…"
			entry["details_available"] = true
		}
		if value, ok := data[key].(string); ok && len([]byte(value)) > 384 {
			data[key] = utf8BytePrefix(value, 384) + "…"
			entry["details_available"] = true
		}
	}
}

func boundedPurpose(value string) string {
	if len([]byte(value)) <= 256 {
		return value
	}
	return utf8BytePrefix(value, 256) + "…"
}

func compactTranscriptBody(entry TranscriptEntry) {
	text, ok := entry["text"].(string)
	if !ok || len([]byte(text)) <= maxInlineTranscriptBodyBytes {
		return
	}
	entry["text_bytes"] = len([]byte(text))
	entry["text"] = utf8BytePrefix(text, transcriptBodyPreviewBytes) + " …"
	entry["details_available"] = true
}

func enforceInitialTranscriptByteBudget(entries []TranscriptEntry) []TranscriptEntry {
	marshalSize := func() int {
		payload, err := json.Marshal(entries)
		if err != nil {
			return MaxInitialTranscriptBytes + 1
		}
		return len(payload)
	}
	if marshalSize() <= MaxInitialTranscriptBytes {
		return entries
	}

	// Keep all ordinary content readable when it fits. If the combined recent
	// window still exceeds the budget, convert the largest referenced body into
	// an explicit preview/detail entry, then shrink previews until it fits.
	for marshalSize() > MaxInitialTranscriptBytes {
		largestIndex, largestSize := -1, 0
		for index, entry := range entries {
			if entry == nil || !hasTranscriptDetailEndpoint(entry) {
				continue
			}
			text, ok := entry["text"].(string)
			if ok && len(text) > largestSize && len(text) > 96 {
				largestIndex, largestSize = index, len(text)
			}
		}
		if largestIndex >= 0 {
			entry := entries[largestIndex]
			text := entry["text"].(string)
			target := len([]byte(text)) / 2
			if target < 1 {
				target = 0
			}
			if _, exists := entry["text_bytes"]; !exists {
				entry["text_bytes"] = len([]byte(text))
			}
			entry["details_available"] = true
			preview := utf8BytePrefix(text, target)
			if preview == text {
				preview = ""
			}
			entry["text"] = preview
			continue
		}
		if compactOversizedImagePayloads(entries) || compactLargestOptionalTranscriptField(entries) {
			continue
		}
		// The projection contains only required identity fields now. Stop rather
		// than risking a persistence or hydration hang on an irreducible payload.
		break
	}
	return entries
}

func compactOversizedImagePayloads(entries []TranscriptEntry) bool {
	changed := false
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		images, ok := entry["images"].([]string)
		if !ok || len(images) == 0 {
			continue
		}
		kept := make([]string, 0, len(images))
		for _, image := range images {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(image)), "data:") || len([]byte(image)) > 4096 {
				changed = true
				entry["images_omitted"] = true
				entry["details_available"] = true
				continue
			}
			kept = append(kept, image)
		}
		if changed {
			entry["images"] = kept
		}
	}
	return changed
}

func compactLargestOptionalTranscriptField(entries []TranscriptEntry) bool {
	protected := map[string]struct{}{
		"kind": {}, "id": {}, "item_id": {}, "detail_item_id": {}, "round_id": {},
		"round_revision_id": {}, "conversation_ordinal": {}, "item_version": {}, "text": {},
	}
	largestEntry, largestKey, largestSize := -1, "", 0
	for entryIndex, entry := range entries {
		if entry == nil || !hasTranscriptDetailEndpoint(entry) {
			continue
		}
		for key, value := range entry {
			if _, keep := protected[key]; keep || key == "details_available" {
				continue
			}
			encoded, err := json.Marshal(value)
			if err == nil && len(encoded) > largestSize {
				largestEntry, largestKey, largestSize = entryIndex, key, len(encoded)
			}
		}
	}
	if largestEntry < 0 || largestSize == 0 {
		return false
	}
	entry := entries[largestEntry]
	delete(entry, largestKey)
	entry["details_available"] = true
	if largestKey == "images" {
		entry["images_omitted"] = true
	}
	return true
}

func hasTranscriptDetailEndpoint(entry TranscriptEntry) bool {
	return transcriptString(entry["detail_item_id"]) != "" &&
		transcriptString(entry["round_id"]) != "" &&
		transcriptString(entry["round_revision_id"]) != ""
}

func utf8BytePrefix(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && value[end]&0xc0 == 0x80 {
		end--
	}
	return strings.ToValidUTF8(value[:end], "�")
}

func transcriptString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func transcriptCallID(entry TranscriptEntry) string {
	data, _ := entry["data"].(map[string]any)
	return transcriptString(data["tool_call_id"])
}

func transcriptCallItemID(entry TranscriptEntry) string {
	data, _ := entry["data"].(map[string]any)
	if transcriptString(entry["kind"]) == "tool_result" {
		return transcriptString(data["tool_call_item_id"])
	}
	if itemID := transcriptString(entry["item_id"]); itemID != "" {
		return itemID
	}
	return transcriptString(entry["id"])
}
