package turn

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// memoryToolEntryView is deliberately smaller than memory.Entry. A model
// lookup must never copy Value/Qualifiers and an unbounded Content field into
// durable tool-result history by accident.
type memoryToolEntryView map[string]any

func buildMemorySearchOutput(query, scope string, results []memory.SearchResult) (string, error) {
	queryBound := memory.BoundText(query, memory.DefaultMemoryQueryTokenBudget)
	views := make([]memoryToolEntryView, 0, len(results))
	for _, result := range results {
		views = append(views, memorySearchEntryView(result))
	}

	originalCount := len(views)
	for len(views) > 0 {
		payload := map[string]any{
			"status":            "succeeded",
			"query":             queryBound.Text,
			"query_truncated":   queryBound.Truncated,
			"scope":             scope,
			"result_count":      originalCount,
			"results_truncated": len(views) < originalCount,
			"results":           views,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		if tokens.EstimateInt(string(raw)) <= memory.DefaultMemorySearchTokenBudget {
			return string(raw), nil
		}
		if len(views) == 1 && shrinkMemoryViewContent(payload, views[0], memory.DefaultMemorySearchTokenBudget) {
			payload["results_truncated"] = false
			raw, err = json.Marshal(payload)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		}
		views = views[:len(views)-1]
	}

	// Keep the response valid and useful even when metadata alone is close to
	// the budget. This branch is practically unreachable with the bounded
	// query and view schema, but is safer than returning an oversized payload.
	payload := map[string]any{
		"status":            "succeeded",
		"query":             queryBound.Text,
		"query_truncated":   queryBound.Truncated,
		"result_count":      originalCount,
		"results_truncated": originalCount > 0,
		"results":           []memoryToolEntryView{},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func memorySearchEntryView(result memory.SearchResult) memoryToolEntryView {
	entry := result.Entry
	content := memory.BoundText(entry.Content, memory.DefaultMemorySearchEntryBudget)
	view := memoryToolEntryView{
		"id":                      boundOptionalMemoryText(entry.ID, 80),
		"scope":                   boundOptionalMemoryText(string(entry.Scope), 20),
		"tier":                    boundOptionalMemoryText(string(entry.Tier), 20),
		"kind":                    boundOptionalMemoryText(string(entry.Kind), 32),
		"semantic_key":            boundOptionalMemoryText(entry.SemanticKey, 80),
		"subject":                 boundOptionalMemoryText(entry.Subject, 80),
		"predicate":               boundOptionalMemoryText(entry.Predicate, 80),
		"cardinality":             boundOptionalMemoryText(entry.Cardinality, 20),
		"content":                 content.Text,
		"content_tokens":          content.OriginalTokens,
		"returned_content_tokens": content.ReturnedTokens,
		"content_truncated":       content.Truncated,
		"importance":              entry.Importance,
		"confidence":              entry.Confidence,
		"updated_at":              entry.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if result.Score != 0 {
		view["score"] = result.Score
	}
	return view
}

func buildMemoryGetOutput(entry memory.Entry, offset, maxTokens int) (string, error) {
	if offset < 0 {
		offset = 0
	}
	if maxTokens <= 0 || maxTokens > memory.DefaultMemoryGetContentBudget {
		maxTokens = memory.DefaultMemoryGetContentBudget
	}
	for contentBudget := maxTokens; contentBudget >= 16; {
		view := memoryGetEntryView(entry, offset, contentBudget)
		payload := map[string]any{"status": "succeeded", "entry": view}
		raw, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		if tokens.EstimateInt(string(raw)) <= memory.DefaultMemoryGetTokenBudget {
			return string(raw), nil
		}
		contentBudget = contentBudget * 3 / 4
	}

	// The entry metadata itself is still useful if a pathological Value or
	// Qualifiers object leaves no room for content.
	view := memoryGetEntryView(entry, offset, 0)
	view["content"] = ""
	view["content_truncated"] = len([]rune(entry.Content)) > offset
	view["returned_content_tokens"] = 0
	view["next_offset"] = offset
	view["has_more"] = len([]rune(entry.Content)) > offset
	raw, err := json.Marshal(map[string]any{"status": "succeeded", "entry": view})
	if err != nil {
		return "", err
	}
	if tokens.EstimateInt(string(raw)) > memory.DefaultMemoryGetTokenBudget {
		// All user/model-controlled metadata is bounded above, but retain a
		// final minimal response as a hard fence for future schema additions.
		raw, err = json.Marshal(map[string]any{
			"status": "succeeded",
			"entry": map[string]any{
				"id":     boundOptionalMemoryText(entry.ID, 80),
				"status": "succeeded",
			},
		})
		if err != nil {
			return "", err
		}
	}
	return string(raw), nil
}

func memoryGetEntryView(entry memory.Entry, offset, contentBudget int) memoryToolEntryView {
	runes := []rune(entry.Content)
	if offset > len(runes) {
		offset = len(runes)
	}
	page := tokens.TakePrefixForTokenBudget(string(runes[offset:]), float64(contentBudget))
	nextOffset := offset + len([]rune(page))
	hasMore := nextOffset < len(runes)
	view := memoryToolEntryView{
		"id":                      boundOptionalMemoryText(entry.ID, 80),
		"scope":                   boundOptionalMemoryText(string(entry.Scope), 20),
		"tier":                    boundOptionalMemoryText(string(entry.Tier), 20),
		"kind":                    boundOptionalMemoryText(string(entry.Kind), 32),
		"semantic_key":            boundOptionalMemoryText(entry.SemanticKey, 80),
		"subject":                 boundOptionalMemoryText(entry.Subject, 80),
		"predicate":               boundOptionalMemoryText(entry.Predicate, 80),
		"cardinality":             boundOptionalMemoryText(entry.Cardinality, 20),
		"content":                 page,
		"content_offset":          offset,
		"next_offset":             nextOffset,
		"has_more":                hasMore,
		"content_tokens":          tokens.EstimateInt(entry.Content),
		"returned_content_tokens": tokens.EstimateInt(page),
		"content_truncated":       offset > 0 || hasMore,
		"status":                  boundOptionalMemoryText(string(entry.Status), 32),
		"importance":              entry.Importance,
		"confidence":              entry.Confidence,
		"sensitivity":             boundOptionalMemoryText(entry.Sensitivity, 32),
		"source_type":             boundOptionalMemoryText(entry.SourceType, 80),
		"revision":                entry.Revision,
		"created_at":              entry.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":              entry.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if entry.ValidFrom != nil {
		view["valid_from"] = entry.ValidFrom.UTC().Format(time.RFC3339)
	}
	if entry.ValidTo != nil {
		view["valid_to"] = entry.ValidTo.UTC().Format(time.RFC3339)
	}
	if entry.ExpiresAt != nil {
		view["expires_at"] = entry.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if entry.SourceRef != "" {
		view["source_reference"] = boundOptionalMemoryText(entry.SourceRef, 80)
	}
	if raw, err := json.Marshal(entry.Value); err == nil && string(raw) != "null" {
		if tokens.EstimateInt(string(raw)) <= 120 {
			view["value"] = entry.Value
		} else {
			view["value_truncated"] = true
			view["value_tokens"] = tokens.EstimateInt(string(raw))
		}
	}
	if len(entry.Qualifiers) > 0 {
		if raw, err := json.Marshal(entry.Qualifiers); err == nil && tokens.EstimateInt(string(raw)) <= 120 {
			view["qualifiers"] = entry.Qualifiers
		} else {
			view["qualifiers_truncated"] = true
		}
	}
	return view
}

func boundOptionalMemoryText(text string, budget int) string {
	return memory.BoundText(text, budget).Text
}

func shrinkMemoryViewContent(payload map[string]any, view memoryToolEntryView, budget int) bool {
	original, _ := view["content"].(string)
	if original == "" {
		return false
	}
	low, high := 0, tokens.EstimateInt(original)
	best := ""
	for low <= high {
		middle := low + (high-low)/2
		candidate := tokens.TakePrefixForTokenBudget(original, float64(middle))
		view["content"] = candidate
		view["returned_content_tokens"] = tokens.EstimateInt(candidate)
		view["content_truncated"] = true
		raw, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if tokens.EstimateInt(string(raw)) <= budget {
			best = candidate
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	if best == "" {
		return false
	}
	view["content"] = best
	view["returned_content_tokens"] = tokens.EstimateInt(best)
	view["content_truncated"] = true
	return true
}

func memoryToolInt(payload map[string]any, key string, fallback, min, max int) int {
	value := intField(payload, key, fallback)
	if value < min {
		return min
	}
	if max > 0 && value > max {
		return max
	}
	return value
}

func memoryToolQuery(payload map[string]any) (string, bool) {
	query := strings.TrimSpace(fmt.Sprint(payload["query"]))
	if query == "" || query == "<nil>" {
		return "", false
	}
	bounded := memory.BoundText(query, memory.DefaultMemoryQueryTokenBudget)
	return bounded.Text, true
}
