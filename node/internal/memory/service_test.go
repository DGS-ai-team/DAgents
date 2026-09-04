package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

func TestLocalServiceRecallDuplicateAndConflict(t *testing.T) {
	service, err := OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()

	first, err := service.Remember(ctx, RememberRequest{
		Information: "用户偏好中文回复", Tier: TierCore, Kind: KindPreference,
		SemanticKey: "user.language", Subject: "user", Predicate: "language", Cardinality: "single",
	})
	if err != nil || first.Outcome != WriteAdded {
		t.Fatalf("first remember outcome=%q err=%v", first.Outcome, err)
	}
	duplicate, err := service.Remember(ctx, RememberRequest{Information: "用户偏好中文回复", SemanticKey: "user.language", Cardinality: "single"})
	if err != nil || duplicate.Outcome != WriteDuplicate || duplicate.ExistingID != first.Entry.ID {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	conflict, err := service.Remember(ctx, RememberRequest{
		Information: "用户偏好英文回复", SemanticKey: "user.language", Subject: "user", Predicate: "language", Cardinality: "single",
	})
	if err != nil || conflict.Outcome != WritePendingConflict || conflict.Conflict == nil {
		t.Fatalf("conflict=%+v err=%v", conflict, err)
	}
	resolved, err := service.ResolveConflict(ctx, ScopeAgent, conflict.Conflict.ID, ConflictUseNew)
	if err != nil || resolved.Outcome != WriteSuperseded {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	entries, err := service.List(ctx, ScopeAgent, false)
	if err != nil || len(entries) != 1 || entries[0].Content != "用户偏好英文回复" {
		t.Fatalf("active entries=%+v err=%v", entries, err)
	}

	snapshot, err := service.Recall(ctx, RecallRequest{QueryText: "英文", IncludeCore: true})
	if err != nil || len(snapshot.Core) != 0 || len(snapshot.Recalled) != 1 || snapshot.Recalled[0].Content != "用户偏好英文回复" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestLocalServiceRecallDoesNotReintroduceTrimmedCore(t *testing.T) {
	service, err := OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	for _, item := range []struct {
		info       string
		importance int
	}{
		{info: "primary stable preference", importance: 100},
		{info: "secondary stable preference", importance: 50},
	} {
		if _, err := service.Remember(ctx, RememberRequest{Information: item.info, Tier: TierCore, Importance: item.importance}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := service.Recall(ctx, RecallRequest{
		QueryText: "secondary stable preference", IncludeCore: true,
		CoreBudget: 30, TokenBudget: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Core) != 1 || len(snapshot.Recalled) != 0 {
		t.Fatalf("trimmed core was reintroduced by recall: core=%+v recalled=%+v", snapshot.Core, snapshot.Recalled)
	}
}

func TestLocalServiceRecallNaturalLanguageQueryFindsMemoryTerms(t *testing.T) {
	service, err := OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	entry, err := service.Remember(ctx, RememberRequest{
		Information: "E2E_MEMORY_ISOLATED_20260902：这条记忆用于验证清空对话后仍可召回",
		Tier:        TierRecall,
		Kind:        KindFact,
		SemanticKey: "e2e.memory.isolated.20260902",
		Cardinality: "single",
	})
	if err != nil || entry.Entry == nil {
		t.Fatalf("remember=%+v err=%v", entry, err)
	}

	// Automatic Turn recall passes the complete root user message. It must not
	// require that entire instruction to be an exact FTS phrase.
	snapshot, err := service.Recall(ctx, RecallRequest{
		QueryText: "不要调用任何工具。请直接回答：E2E_MEMORY_ISOLATED_20260902 这条长期记忆的完整信息是什么？如果你能回答，请明确说明信息内容、kind、tier、semantic_key。",
		Limit:     6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Recalled) != 1 || snapshot.Recalled[0].ID != entry.Entry.ID {
		t.Fatalf("natural-language recall=%+v, want entry %q", snapshot.Recalled, entry.Entry.ID)
	}

	preference, err := service.Remember(ctx, RememberRequest{
		Information: "用户偏好中文回复",
		Tier:        TierRecall,
		Kind:        KindPreference,
		SemanticKey: "user.language",
		Cardinality: "single",
	})
	if err != nil || preference.Entry == nil {
		t.Fatalf("preference remember=%+v err=%v", preference, err)
	}
	chinese, err := service.Recall(ctx, RecallRequest{QueryText: "请告诉我之前记录的语言偏好是什么", Limit: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(chinese.Recalled) != 1 || chinese.Recalled[0].ID != preference.Entry.ID {
		t.Fatalf("CJK term recall=%+v, want entry %q", chinese.Recalled, preference.Entry.ID)
	}
}

func TestLocalServiceRecallRanksPreciseFieldsAboveRepeatedContent(t *testing.T) {
	service, err := OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	precise, err := service.Remember(ctx, RememberRequest{
		Information: "旅行地点偏好是南极星",
		Tier:        TierRecall,
		Kind:        KindPreference,
		SemanticKey: "travel.destination",
		Subject:     "旅行",
		Predicate:   "地点",
		Cardinality: "single",
	})
	if err != nil || precise.Entry == nil {
		t.Fatalf("precise remember=%+v err=%v", precise, err)
	}
	if _, err := service.Remember(ctx, RememberRequest{
		Information: strings.Repeat("偏好 ", 20000),
		Tier:        TierRecall,
		Kind:        KindExperience,
		SemanticKey: "verbose.noise",
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.Recall(ctx, RecallRequest{
		QueryText: "我之前说过的旅行地点偏好是什么？",
		Limit:     6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Recalled) == 0 || snapshot.Recalled[0].ID != precise.Entry.ID {
		t.Fatalf("precise memory should rank first, got=%+v want=%q", snapshot.Recalled, precise.Entry.ID)
	}
}

func TestLocalServiceRecallContextBudgetIncludesFraming(t *testing.T) {
	service, err := OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	for _, item := range []struct {
		text string
		tier Tier
	}{
		{text: "核心偏好：回答使用中文", tier: TierCore},
		{text: "召回事实：工作目录是 workspace/project", tier: TierRecall},
	} {
		if _, err := service.Remember(ctx, RememberRequest{Information: item.text, Tier: item.tier}); err != nil {
			t.Fatal(err)
		}
	}

	const budget = 120
	snapshot, err := service.Recall(ctx, RecallRequest{
		QueryText:          "工作目录是什么",
		IncludeCore:        true,
		CoreBudget:         2000,
		TokenBudget:        2000,
		ContextTokenBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TokenEstimate > budget {
		t.Fatalf("rendered memory context exceeded hard budget: got=%d budget=%d content=%q", snapshot.TokenEstimate, budget, snapshot.RenderedContent)
	}
	if snapshot.TokenEstimate != tokens.EstimateInt(snapshot.RenderedContent) {
		t.Fatalf("snapshot token estimate=%d does not match rendered content", snapshot.TokenEstimate)
	}
}

func TestLocalServiceModelOperationsCannotWidenConfiguredScope(t *testing.T) {
	service, err := OpenLocalService(t.TempDir()+"/agent.db", t.TempDir()+"/global.db", ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	ctx := context.Background()
	if _, err := service.Remember(ctx, RememberRequest{Scope: ScopeGlobal, Information: "不得写入全局"}); !errors.Is(err, ErrScopeForbidden) {
		t.Fatalf("Remember(global) error=%v, want ErrScopeForbidden", err)
	}
	if _, err := service.Search(ctx, SearchRequest{Scope: ScopeGlobal, Query: "不得读取全局"}); !errors.Is(err, ErrScopeForbidden) {
		t.Fatalf("Search(global) error=%v, want ErrScopeForbidden", err)
	}

	// The settings/control plane intentionally remains able to manage both
	// projections, then model access is still limited by the selected scope.
	if err := service.ReplaceAll(ctx, ScopeGlobal, []Entry{{ID: "global-1", Scope: ScopeGlobal, Content: "全局设置"}}); err != nil {
		t.Fatal(err)
	}
	service.SetScope(ScopeGlobal)
	if _, err := service.Get(ctx, ScopeGlobal, "global-1", false); err != nil {
		t.Fatalf("Get(global) after explicit scope selection: %v", err)
	}
}
