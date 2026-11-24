package session

import (
	"encoding/json"
	"sort"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// publishConversationPatch projects only the records committed by the latest
// SQLite transaction. It never rebuilds the transcript from runtime.messages.
func (r *runtime) publishConversationPatch(result store.ConversationCommitResult, historyRevision uint64, status string, terminal *turn.CoordinatorSnapshot, roundID, revisionID string, inputBoxState json.RawMessage, inputQueueRows []store.InputQueueRow) {
	if r == nil || r.publisher == nil || r.session.ID == "" {
		return
	}
	if hub, ok := r.publisher.(*stream.Hub); ok && hub == nil {
		return
	}
	entries, removedIDs := committedTranscriptPatchItems(result)
	if len(entries) == 0 && len(removedIDs) == 0 && result.ConversationRevision == 0 && result.CheckpointRevision == 0 && terminal == nil {
		return
	}
	data := map[string]any{
		"conversation_id":       r.session.ID,
		"conversation_revision": result.ConversationRevision,
		"context_revision":      result.CheckpointRevision,
		"history_revision":      historyRevision,
		"upsert_items":          entries,
		"remove_item_ids":       removedIDs,
	}
	if status != "" {
		data["result_commit_status"] = status
	}
	if roundID != "" {
		data["round_id"] = roundID
	}
	if revisionID != "" {
		data["revision_id"] = revisionID
	}
	if len(inputBoxState) > 0 {
		queueState := map[string]any{}
		if inputQueueRows != nil {
			if inputRecords, err := pendingUserInputsFromQueueRows(inputQueueRows); err == nil {
				data["input_updates"] = pendingInputViews(r.session.ID, inputRecords)
				data["input_updates_complete"] = true
			}
			queueState["pending_count"] = countQueuedInputRows(inputQueueRows)
		}
		if paused, controlRevision, err := QueueControlFromSnapshot(inputBoxState); err == nil {
			queueState["paused"] = paused
			queueState["control_revision"] = controlRevision
		}
		if len(queueState) > 0 {
			data["queue_state"] = queueState
		}
	}
	if terminal != nil {
		lifecycleSeq := r.lifecycleEventSequence()
		turnState := turnStateEventData(*terminal, lifecycleSeq, historyRevision, "")
		turnState["result_commit_status"] = status
		data["turn_state"] = turnState
		data["pending_hitl"] = turn.BuildHITLRequiredSnapshot(pendingFromLifecycleSnapshot(*terminal))
	}
	r.publisher.Publish(r.session.ID, "conversation_patch", data)
}

func committedTranscriptPatchItems(result store.ConversationCommitResult) ([]TranscriptEntry, []string) {
	if len(result.ChangedItems) == 0 && len(result.RemovedPayloads) == 0 && len(result.RemovedItemIDs) == 0 {
		return []TranscriptEntry{}, []string{}
	}
	versions := make(map[string]uint64, len(result.ChangedItems)*2)
	orderByDisplayID := make(map[string]store.ConversationItemRevision, len(result.ChangedItems)*3)
	for index, item := range result.ChangedItems {
		versions[item.ItemID] = item.Revision
		if index >= len(result.ChangedPayloads) {
			continue
		}
		message := result.ChangedPayloads[index]
		for _, displayID := range transcriptItemIDsForMessage(message) {
			versions[displayID] = item.Revision
			orderByDisplayID[displayID] = item
		}
		if boundaryID := conversationBoundaryID(message); boundaryID != "" {
			orderByDisplayID[boundaryID] = item
		}
	}
	entries := MessagesToTranscriptEntriesWithItemVersions(result.ChangedPayloads, versions)
	entryIndexBySource := make(map[string]int)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		itemID, _ := entry["item_id"].(string)
		item, found := orderByDisplayID[itemID]
		if !found {
			boundaryID, _ := entry["boundary_id"].(string)
			item, found = orderByDisplayID[boundaryID]
			if found {
				itemID = item.ItemID
				entry["item_id"], entry["id"] = itemID, itemID
			}
		}
		if !found {
			continue
		}
		sourceID := item.ItemID
		entry["item_version"] = item.Revision
		entry["conversation_ordinal"] = item.Ordinal
		entry["conversation_entry_index"] = entryIndexBySource[sourceID]
		entryIndexBySource[sourceID]++
		entry["detail_item_id"] = sourceID
		if item.RoundID != "" {
			entry["round_id"] = item.RoundID
		}
		if item.RevisionID != "" {
			entry["round_revision_id"] = item.RevisionID
		}
		if item.RoundOrdinal > 0 {
			entry["round_ordinal"] = item.RoundOrdinal
		}
	}
	entries = BoundTranscriptDetails(entries)
	sort.SliceStable(entries, func(i, j int) bool {
		leftOrdinal, leftOK := entries[i]["conversation_ordinal"].(int64)
		rightOrdinal, rightOK := entries[j]["conversation_ordinal"].(int64)
		if leftOK && rightOK && leftOrdinal != rightOrdinal {
			return leftOrdinal < rightOrdinal
		}
		leftIndex, _ := entries[i]["conversation_entry_index"].(int)
		rightIndex, _ := entries[j]["conversation_entry_index"].(int)
		if leftOK && rightOK && leftOrdinal == rightOrdinal && leftIndex != rightIndex {
			return leftIndex < rightIndex
		}
		return false
	})
	removed := make([]string, 0, len(result.RemovedPayloads)*3+len(result.RemovedItemIDs))
	seen := make(map[string]struct{}, cap(removed))
	appendRemoved := func(id string) {
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		removed = append(removed, id)
	}
	for _, message := range result.RemovedPayloads {
		for _, id := range transcriptItemIDsForMessage(message) {
			appendRemoved(id)
		}
	}
	for _, id := range result.RemovedItemIDs {
		appendRemoved(id)
	}
	return entries, removed
}
