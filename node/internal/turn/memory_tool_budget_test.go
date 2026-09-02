package turn

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

func TestBuildMemorySearchOutputBoundsLongEntries(t *testing.T) {
	result := memory.SearchResult{
		Entry: memory.Entry{
			ID:          "mem-long",
			Scope:       memory.ScopeAgent,
			Tier:        memory.TierRecall,
			Kind:        memory.KindFact,
			SemanticKey: "long.fact",
			Content:     strings.Repeat("一段很长的历史内容 ", 10000),
			UpdatedAt:   time.Now().UTC(),
		},
		Score: 10,
	}

	raw, err := buildMemorySearchOutput("查找历史事实", "agent", []memory.SearchResult{result})
	if err != nil {
		t.Fatal(err)
	}
	if got := tokens.EstimateInt(raw); got > memory.DefaultMemorySearchTokenBudget {
		t.Fatalf("memory_search output exceeded budget: got=%d budget=%d", got, memory.DefaultMemorySearchTokenBudget)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	views, ok := payload["results"].([]any)
	if !ok || len(views) != 1 {
		t.Fatalf("results=%#v", payload["results"])
	}
	view, ok := views[0].(map[string]any)
	if !ok || view["content_truncated"] != true {
		t.Fatalf("long search result was not marked truncated: %#v", views[0])
	}
	if payload["result_count"] != float64(1) {
		t.Fatalf("result_count=%v want 1", payload["result_count"])
	}
}

func TestBuildMemoryGetOutputPaginatesAndBoundsLongEntries(t *testing.T) {
	entry := memory.Entry{
		ID:        "mem-page",
		Scope:     memory.ScopeAgent,
		Tier:      memory.TierRecall,
		Kind:      memory.KindExperience,
		Content:   strings.Repeat("分页内容 ", 10000),
		Status:    memory.StatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	firstRaw, err := buildMemoryGetOutput(entry, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := tokens.EstimateInt(firstRaw); got > memory.DefaultMemoryGetTokenBudget {
		t.Fatalf("memory_get output exceeded budget: got=%d budget=%d", got, memory.DefaultMemoryGetTokenBudget)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(firstRaw), &first); err != nil {
		t.Fatalf("invalid first page JSON: %v", err)
	}
	firstEntry, ok := first["entry"].(map[string]any)
	if !ok || firstEntry["has_more"] != true || firstEntry["content_truncated"] != true {
		t.Fatalf("first page metadata=%#v", first["entry"])
	}
	nextOffset, ok := firstEntry["next_offset"].(float64)
	if !ok || nextOffset <= 0 {
		t.Fatalf("next_offset=%v", firstEntry["next_offset"])
	}

	secondRaw, err := buildMemoryGetOutput(entry, int(nextOffset), 100)
	if err != nil {
		t.Fatal(err)
	}
	var second map[string]any
	if err := json.Unmarshal([]byte(secondRaw), &second); err != nil {
		t.Fatalf("invalid second page JSON: %v", err)
	}
	secondEntry, ok := second["entry"].(map[string]any)
	if !ok || secondEntry["content_offset"] != nextOffset {
		t.Fatalf("second page metadata=%#v", second["entry"])
	}
	if secondEntry["content"] == "" {
		t.Fatal("second page returned no content")
	}

	pathological := entry
	pathological.ID = strings.Repeat("id", 10000)
	pathological.SemanticKey = strings.Repeat("semantic-key ", 10000)
	pathological.Subject = strings.Repeat("subject ", 10000)
	pathological.Predicate = strings.Repeat("predicate ", 10000)
	pathological.SourceType = strings.Repeat("source ", 10000)
	metadataRaw, err := buildMemoryGetOutput(pathological, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := tokens.EstimateInt(metadataRaw); got > memory.DefaultMemoryGetTokenBudget {
		t.Fatalf("memory_get metadata exceeded budget: got=%d budget=%d", got, memory.DefaultMemoryGetTokenBudget)
	}
}

func TestMemoryForgetOutputDoesNotEmbedContent(t *testing.T) {
	result := memory.WriteResult{
		Outcome: memory.WriteSuperseded,
		Entry: &memory.Entry{
			ID:      "mem-forgotten",
			Scope:   memory.ScopeAgent,
			Tier:    memory.TierRecall,
			Kind:    memory.KindFact,
			Content: strings.Repeat("不应写回工具结果 ", 10000),
		},
		Superseded:    make([]string, 10000),
		StoreRevision: 7,
	}
	for i := range result.Superseded {
		result.Superseded[i] = strings.Repeat("superseded-id ", 1000)
	}
	raw := memoryForgetOutput(result)
	if strings.Contains(raw, "不应写回工具结果") {
		t.Fatal("memory_forget output embedded the deleted content")
	}
	if got := tokens.EstimateInt(raw); got > memory.DefaultMemorySearchTokenBudget {
		t.Fatalf("memory_forget output unexpectedly large: %d", got)
	}
}
