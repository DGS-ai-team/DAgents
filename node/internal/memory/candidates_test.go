package memory

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func openTestLocalService(t *testing.T) *LocalService {
	t.Helper()
	root := t.TempDir()
	service, err := OpenLocalService(filepath.Join(root, "agent.db"), filepath.Join(root, "global.db"), ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func TestLocalServiceConsolidateStagesThenPromotesCandidate(t *testing.T) {
	service := openTestLocalService(t)
	results, err := service.Consolidate(context.Background(), []Candidate{{Request: RememberRequest{
		Information: "项目默认分支是 dev",
		SemanticKey: "project.default_branch",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != WriteAdded || results[0].Entry == nil {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Entry.Status != StatusActive || results[0].Entry.Tier != TierRecall || results[0].Entry.SourceType != "model_inference" {
		t.Fatalf("candidate was not normalized/promoted: %+v", results[0].Entry)
	}
	entries, err := service.List(context.Background(), ScopeAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("active entries = %+v", entries)
	}
}

func TestLocalServiceConsolidateDoesNotAutoApproveConflict(t *testing.T) {
	service := openTestLocalService(t)
	_, err := service.Remember(context.Background(), RememberRequest{
		Information: "项目默认分支是 main", SemanticKey: "project.default_branch", Cardinality: "single",
	})
	if err != nil {
		t.Fatal(err)
	}
	results, err := service.Consolidate(context.Background(), []Candidate{{Request: RememberRequest{
		Information: "项目默认分支是 dev", SemanticKey: "project.default_branch", Cardinality: "single",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != WritePendingConflict || results[0].Conflict == nil {
		t.Fatalf("results = %+v", results)
	}
	entries, err := service.List(context.Background(), ScopeAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Content != "项目默认分支是 main" {
		t.Fatalf("active entries = %+v", entries)
	}
}

func TestCandidatePipelineIsBoundedAndPublishesMetadataOnlyReport(t *testing.T) {
	service := openTestLocalService(t)
	extractor := &testCandidateExtractor{}
	changed := make(chan ConsolidationReport, 1)
	pipeline := NewCandidatePipeline(extractor, service, 1, func(err error) {
		t.Errorf("pipeline error: %v", err)
	})
	pipeline.SetOnChange(func(report ConsolidationReport) { changed <- report })
	defer pipeline.Close()
	if !pipeline.Submit(ExtractionInput{AgentID: "agent-a", SessionID: "session-a", SourceFingerprint: "fp-a", Messages: []ExtractionMessage{{Role: "user", Content: "记住默认分支"}}}) {
		t.Fatal("first candidate batch was not accepted")
	}
	select {
	case report := <-changed:
		if report.CandidateCount != 1 || report.Added != 1 || !report.Changed || report.StoreRevision == 0 {
			t.Fatalf("report = %+v", report)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("candidate pipeline did not finish")
	}
	if got := extractor.Input(); got.AgentID != "agent-a" || got.SourceFingerprint != "fp-a" || len(got.Messages) != 1 {
		t.Fatalf("input was not frozen/preserved: %+v", got)
	}
}

func TestCandidatePipelineDisabledByMissingExtension(t *testing.T) {
	pipeline := NewCandidatePipeline(nil, nil, 1, nil)
	defer pipeline.Close()
	if pipeline.Submit(ExtractionInput{AgentID: "a"}) {
		t.Fatal("nil extension must keep automatic extraction disabled")
	}
}

func TestCandidatePipelineReportsMaintenanceOnlyChange(t *testing.T) {
	service := openTestLocalService(t)
	if _, err := service.Remember(context.Background(), RememberRequest{
		Information: "核心偏好", Tier: TierCore, Importance: 100,
	}); err != nil {
		t.Fatal(err)
	}
	changed := make(chan ConsolidationReport, 1)
	pipeline := NewCandidatePipeline(&testCandidateExtractor{text: "核心偏好"}, service, 1, func(err error) {
		t.Errorf("pipeline error: %v", err)
	})
	pipeline.SetCoreBudget(1)
	pipeline.SetOnChange(func(report ConsolidationReport) { changed <- report })
	defer pipeline.Close()
	if !pipeline.Submit(ExtractionInput{Scope: ScopeAgent, AgentID: "agent-a"}) {
		t.Fatal("candidate batch was not accepted")
	}
	select {
	case report := <-changed:
		if report.CandidateCount != 1 || report.Duplicates != 1 || !report.Changed || report.StoreRevision == 0 {
			t.Fatalf("report = %+v", report)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("maintenance-only change was not reported")
	}
	entries, err := service.List(context.Background(), ScopeAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Tier != TierRecall {
		t.Fatalf("entry was not maintained: %+v", entries)
	}
}

func TestMaintainDemotesCoreOverflowWithoutDeletingHistory(t *testing.T) {
	service := openTestLocalService(t)
	first, err := service.Remember(context.Background(), RememberRequest{Information: "核心偏好一", Tier: TierCore, Importance: 100})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Remember(context.Background(), RememberRequest{Information: "核心偏好二", Tier: TierCore, Importance: 10})
	if err != nil {
		t.Fatal(err)
	}
	if first.Entry == nil || second.Entry == nil {
		t.Fatal("missing entries")
	}
	report, err := service.Maintain(context.Background(), ScopeAgent, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.DemotedCore != 2 || !report.Changed || report.StoreRevision == 0 {
		t.Fatalf("report = %+v", report)
	}
	all, err := service.List(context.Background(), ScopeAgent, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("history entries = %+v", all)
	}
	for _, entry := range all {
		if entry.Tier != TierRecall || entry.Revision < 2 {
			t.Fatalf("entry was not demoted with revision: %+v", entry)
		}
	}
}

func TestLLMCandidateExtractorParsesBoundedJSON(t *testing.T) {
	extractor := NewLLMCandidateExtractor(&candidateExtractionLLM{
		response: "```json\n[{\"information\":\"用户偏好中文\",\"kind\":\"preference\",\"expires_at\":\"2030-01-01T00:00:00Z\"}]\n```",
	}, 1, 100)
	candidates, err := extractor.Extract(context.Background(), ExtractionInput{
		Messages: []ExtractionMessage{{Role: "user", Content: "请记住我偏好中文"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Request.Information != "用户偏好中文" || candidates[0].Request.Kind != KindPreference || candidates[0].Request.ExpiresAt == nil {
		t.Fatalf("candidates = %+v", candidates)
	}
}

type testCandidateExtractor struct {
	mu    sync.Mutex
	input ExtractionInput
	text  string
}

func (e *testCandidateExtractor) Extract(_ context.Context, input ExtractionInput) ([]Candidate, error) {
	e.mu.Lock()
	e.input = cloneExtractionInput(input)
	e.mu.Unlock()
	text := e.text
	if text == "" {
		text = "稳定的项目偏好"
	}
	return []Candidate{{Request: RememberRequest{Information: text}}}, nil
}

func (e *testCandidateExtractor) Input() ExtractionInput {
	e.mu.Lock()
	defer e.mu.Unlock()
	return cloneExtractionInput(e.input)
}

type candidateExtractionLLM struct {
	response string
}

func (c *candidateExtractionLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{}, nil
}

func (c *candidateExtractionLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return c.response, nil
}

func (c *candidateExtractionLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}
