package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type runnerSource struct {
	input    ExtractionInput
	seq      uint64
	complete bool
}

type noNewMaintenanceSource struct{}
type skipMaintenanceSource struct{ seq uint64 }

func (s skipMaintenanceSource) LoadMaintenanceMessages(_ context.Context, _ string, after uint64, _ int) (DurableMessageBatch, error) {
	if after >= s.seq {
		return DurableMessageBatch{}, nil
	}
	return DurableMessageBatch{Complete: true, SkipOnly: true, Sequence: s.seq, SkippedThrough: s.seq}, nil
}

func (noNewMaintenanceSource) LoadMaintenanceMessages(context.Context, string, uint64, int) (DurableMessageBatch, error) {
	return DurableMessageBatch{Complete: false}, nil
}

func (s runnerSource) LoadMaintenanceMessages(context.Context, string, uint64, int) (DurableMessageBatch, error) {
	return DurableMessageBatch{SessionID: s.input.SessionID, Sequence: s.seq, Messages: []llm.Message{{Role: "user", Content: "remember runner"}}, Complete: s.complete}, nil
}

func TestMaintenanceRunnerPersistsSkipOnlyWithoutExtractor(t *testing.T) {
	root := t.TempDir()
	gs, err := goals.OpenStore(filepath.Join(root, "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gs.SaveProfile(goals.AutoProfile{AgentID: "skip", Enabled: true, MaintenanceTokenBudget: 100}, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	ms, err := OpenLocalService(filepath.Join(root, "memory.db"), filepath.Join(root, "global.db"), ScopeAgent, "skip")
	if err != nil {
		t.Fatal(err)
	}
	ext := &runnerExtractor{}
	runner := &MaintenanceRunner{Source: skipMaintenanceSource{seq: 4000}, Extractor: ext, Memory: ms, Usage: gs}
	if seq, err := runner.RunOnce(context.Background(), "skip", MaintenanceCursor{}); err != nil || seq != 4000 {
		t.Fatalf("seq=%d err=%v", seq, err)
	}
	if ext.calls != 0 {
		t.Fatalf("extractor called %d times", ext.calls)
	}
	if len(gs.ListMaintenanceReceipts("skip")) != 0 {
		t.Fatal("skip-only created usage receipt")
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}
	ms, err = OpenLocalService(filepath.Join(root, "memory.db"), filepath.Join(root, "global.db"), ScopeAgent, "skip")
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	cur, err := ms.agent.GetMaintenanceCursor(context.Background(), "skip")
	if err != nil || cur.Sequence != 4000 {
		t.Fatalf("cursor=%+v err=%v", cur, err)
	}
	_, evidence, err := (&MaintenanceRunner{Source: skipMaintenanceSource{seq: 4000}, Extractor: ext, Memory: ms, Usage: gs}).RunOnceWithEvidence(context.Background(), "skip", cur)
	if err != nil || evidence.HasIncrement {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

type runnerExtractor struct{ calls int }

type gatedRunnerExtractor struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (e *gatedRunnerExtractor) ExtractWithUsage(context.Context, ExtractionInput) ([]Candidate, *llm.Usage, error) {
	e.calls.Add(1)
	close(e.started)
	<-e.release
	return []Candidate{{Request: RememberRequest{Information: "gated runner fact"}}}, &llm.Usage{TotalTokens: 3}, nil
}

type failOnceUsage struct {
	*goals.Store
	failed bool
}

type failEvidenceUsage struct{ *goals.Store }

func (f failEvidenceUsage) SaveMaintenanceResultWithEvidence(string, string, json.RawMessage, json.RawMessage, int64, int64, bool) error {
	return fmt.Errorf("injected evidence save failure")
}

func (f *failOnceUsage) SettleMaintenance(agentID, receiptID string, used int64, unknown bool, now time.Time) (goals.AgentUsage, error) {
	if !f.failed {
		f.failed = true
		return goals.AgentUsage{}, fmt.Errorf("injected settle failure")
	}
	return f.Store.SettleMaintenance(agentID, receiptID, used, unknown, now)
}

func (e *runnerExtractor) ExtractWithUsage(context.Context, ExtractionInput) ([]Candidate, *llm.Usage, error) {
	e.calls++
	return []Candidate{{Request: RememberRequest{Information: "runner fact"}}}, &llm.Usage{TotalTokens: 3}, nil
}

func TestMaintenanceRunnerPersistsResultAndDoesNotReextractSettled(t *testing.T) {
	root := t.TempDir()
	gs, err := goals.OpenStore(filepath.Join(root, "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "agent-1", Enabled: true, MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	ms, err := OpenLocalService(filepath.Join(root, "memory.db"), filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	ext := &runnerExtractor{}
	runner := &MaintenanceRunner{Source: runnerSource{input: ExtractionInput{AgentID: "agent-1", SessionID: "session-1"}, seq: 7, complete: true}, Extractor: ext, Memory: ms, Usage: gs}
	if seq, evidence, err := runner.RunOnceWithEvidence(context.Background(), "agent-1", MaintenanceCursor{}); err != nil || seq != 7 || !evidence.HasIncrement {
		t.Fatalf("run seq=%d err=%v", seq, err)
	}
	if ext.calls != 1 {
		t.Fatalf("extract calls=%d", ext.calls)
	}
	if _, err := runner.RunOnce(context.Background(), "agent-1", MaintenanceCursor{}); err != nil {
		t.Fatalf("settled recovery: %v", err)
	}
	if ext.calls != 1 {
		t.Fatalf("reextract calls=%d", ext.calls)
	}
	receipts := gs.ListMaintenanceReceipts("agent-1")
	if len(receipts) != 1 || len(receipts[0].EvidenceJSON) == 0 || !json.Valid(receipts[0].EvidenceJSON) {
		t.Fatalf("evidence receipt=%+v", receipts)
	}
	reopened, err := goals.OpenStore(filepath.Join(root, "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	reloaded := reopened.ListMaintenanceReceipts("agent-1")
	if len(reloaded) != 1 {
		t.Fatalf("reopened evidence receipt missing: %+v", reloaded)
	}
	var before, after any
	if err := json.Unmarshal(receipts[0].EvidenceJSON, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reloaded[0].EvidenceJSON, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("reopened evidence missing: %+v", reloaded)
	}
}

func TestMaintenanceRunnerEvidenceSaveFailureDoesNotAdvanceCursor(t *testing.T) {
	root := t.TempDir()
	gs, err := goals.OpenStore(filepath.Join(root, "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "agent-fail", Enabled: true, MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	ms, err := OpenLocalService(filepath.Join(root, "memory.db"), filepath.Join(root, "global.db"), ScopeAgent, "agent-fail")
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	ext := &runnerExtractor{}
	runner := &MaintenanceRunner{Source: runnerSource{input: ExtractionInput{AgentID: "agent-fail", SessionID: "source-session"}, seq: 7, complete: true}, Extractor: ext, Memory: ms, Usage: failEvidenceUsage{Store: gs}}
	if seq, err := runner.RunOnce(context.Background(), "agent-fail", MaintenanceCursor{}); err == nil || seq != 0 {
		t.Fatalf("seq=%d err=%v", seq, err)
	}
	cursor, err := ms.GetMaintenanceCursor(context.Background())
	if err != nil || cursor.Sequence != 0 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	if entries, err := ms.List(context.Background(), ScopeAgent, true); err != nil || len(entries) != 0 {
		t.Fatalf("memory entries=%d err=%v", len(entries), err)
	}
	if receipts := gs.ListMaintenanceReceipts("agent-fail"); len(receipts) != 1 || receipts[0].Status != "settled" || receipts[0].Unknown || receipts[0].UsedTokens != 3 {
		t.Fatalf("receipts=%+v", receipts)
	}
	if seq, err := runner.RunOnce(context.Background(), "agent-fail", MaintenanceCursor{}); err == nil || seq != 0 || ext.calls != 1 {
		t.Fatalf("unrecoverable repeat seq=%d err=%v extractor_calls=%d", seq, err, ext.calls)
	}
}

type countingRunnerSource struct {
	runnerSource
	calls atomic.Int32
}

func (s *countingRunnerSource) LoadMaintenanceMessages(ctx context.Context, agentID string, cursor uint64, limit int) (DurableMessageBatch, error) {
	s.calls.Add(1)
	return s.runnerSource.LoadMaintenanceMessages(ctx, agentID, cursor, limit)
}

func TestMaintenanceEvidenceReadsSourceOnceAndMatchesProcessedInput(t *testing.T) {
	root := t.TempDir()
	gs, err := goals.OpenStore(filepath.Join(root, "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "agent-1", Enabled: true, MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	ms, err := OpenLocalService(filepath.Join(root, "memory.db"), filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	source := &countingRunnerSource{runnerSource: runnerSource{input: ExtractionInput{AgentID: "agent-1", SessionID: "source-session"}, seq: 7, complete: true}}
	ext := &runnerExtractor{}
	runner := &MaintenanceRunner{Source: source, Extractor: ext, Memory: ms, Usage: gs}
	next, evidence, err := runner.RunOnceWithEvidence(context.Background(), "agent-1", MaintenanceCursor{})
	if err != nil || next != 7 {
		t.Fatalf("run seq=%d err=%v", next, err)
	}
	if source.calls.Load() != 1 {
		t.Fatalf("source reads=%d", source.calls.Load())
	}
	if ext.calls != 1 || !evidence.HasIncrement || evidence.SessionID != "source-session" || len(evidence.Messages) != 1 || evidence.Messages[0].Content != "remember runner" {
		t.Fatalf("extractor=%d evidence=%+v", ext.calls, evidence)
	}
}

func TestMaintenanceEvidenceTruncatesUTF8Safely(t *testing.T) {
	messages := []ExtractionMessage{{Role: "user", Content: strings.Repeat("你好", maintenanceEvidenceMaxMessageBytes)}}
	bounded, truncated := boundEvidenceMessages(messages)
	if !truncated || len(bounded) != 1 || !utf8.ValidString(bounded[0].Content) {
		t.Fatalf("bounded=%+v truncated=%v", bounded, truncated)
	}
	if len([]byte(bounded[0].Content)) > maintenanceEvidenceMaxMessageBytes {
		t.Fatalf("bounded bytes=%d", len([]byte(bounded[0].Content)))
	}
}

func TestMaintenanceEvidenceDropsUnboundedToolMetadataWithoutMutatingInput(t *testing.T) {
	original := ExtractionMessage{
		Role:       "assistant",
		Name:       strings.Repeat("n", maintenanceEvidenceMaxBytes*2),
		Content:    "保留这段文字",
		ToolCallID: "call-1",
		ToolCalls:  []ExtractionToolCall{{ID: "call-1", Name: "write_file", Arguments: strings.Repeat("x", maintenanceEvidenceMaxBytes*4)}},
	}
	input := []ExtractionMessage{original}
	bounded, truncated := boundEvidenceMessages(input)
	if !truncated || len(bounded) != 1 {
		t.Fatalf("bounded=%+v truncated=%v", bounded, truncated)
	}
	if bounded[0].Role != original.Role || bounded[0].Content != original.Content || bounded[0].Name != "" || bounded[0].ToolCallID != "" || len(bounded[0].ToolCalls) != 0 {
		t.Fatalf("unexpected bounded evidence=%+v", bounded[0])
	}
	if input[0].Name != original.Name || input[0].ToolCallID != original.ToolCallID || len(input[0].ToolCalls) != 1 || input[0].ToolCalls[0].Arguments != original.ToolCalls[0].Arguments {
		t.Fatal("source input was mutated")
	}
}

func TestMaintenanceEvidenceNormalizesUnboundedRole(t *testing.T) {
	originalRole := strings.Repeat("role-", maintenanceEvidenceMaxBytes*2)
	input := []ExtractionMessage{{Role: originalRole, Content: "evidence"}}
	bounded, truncated := boundEvidenceMessages(input)
	if len(bounded) != 1 || bounded[0].Role != "unknown" || !truncated {
		t.Fatalf("bounded=%+v truncated=%v", bounded, truncated)
	}
	if input[0].Role != originalRole {
		t.Fatal("source role was mutated")
	}
}

func TestMaintenanceRunnerConcurrentRunnersClaimReceiptOnce(t *testing.T) {
	root := t.TempDir()
	gs, err := goals.OpenStore(filepath.Join(root, "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "agent-1", Enabled: true, MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	ms, err := OpenLocalService(filepath.Join(root, "memory.db"), filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	ext := &gatedRunnerExtractor{started: make(chan struct{}), release: make(chan struct{})}
	source := runnerSource{input: ExtractionInput{AgentID: "agent-1", SessionID: "session-1"}, seq: 7, complete: true}
	runnerA := &MaintenanceRunner{Source: source, Extractor: ext, Memory: ms, Usage: gs}
	runnerB := &MaintenanceRunner{Source: source, Extractor: ext, Memory: ms, Usage: gs}
	var wg sync.WaitGroup
	var seqA int64
	var errA error
	wg.Add(1)
	go func() {
		defer wg.Done()
		seq, runErr := runnerA.RunOnce(context.Background(), "agent-1", MaintenanceCursor{})
		seqA, errA = int64(seq), runErr
	}()
	<-ext.started
	seqB, runErrB := runnerB.RunOnce(context.Background(), "agent-1", MaintenanceCursor{})
	if seqB != 0 || runErrB == nil || !strings.Contains(runErrB.Error(), "recovery_required") {
		t.Fatalf("concurrent pending run seq=%d err=%v", seqB, runErrB)
	}
	close(ext.release)
	wg.Wait()
	if seqA != 7 || errA != nil {
		t.Fatalf("claiming run seq=%d err=%v", seqA, errA)
	}
	if calls := ext.calls.Load(); calls != 1 {
		t.Fatalf("extract calls=%d want 1", calls)
	}
}

func TestMaintenanceRunnerSQLiteFailureReopenRecoversPersistedCandidates(t *testing.T) {
	root := t.TempDir()
	goalsPath := filepath.Join(root, "goals.json")
	memoryPath := filepath.Join(root, "memory.db")
	gs, err := goals.OpenStore(goalsPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "agent-1", Enabled: true, MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	ms, err := OpenLocalService(memoryPath, filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.agent.db.Exec(`CREATE TRIGGER fail_runner_cursor BEFORE INSERT ON maintenance_cursors BEGIN SELECT RAISE(ABORT, 'runner failure'); END`); err != nil {
		t.Fatal(err)
	}
	ext := &runnerExtractor{}
	runner := &MaintenanceRunner{Source: runnerSource{input: ExtractionInput{AgentID: "agent-1", SessionID: "session-1"}, seq: 7, complete: true}, Extractor: ext, Memory: ms, Usage: gs}
	if seq, err := runner.RunOnce(context.Background(), "agent-1", MaintenanceCursor{}); err == nil || seq != 0 {
		t.Fatalf("failed run seq=%d err=%v", seq, err)
	}
	if ext.calls != 1 {
		t.Fatalf("extract calls=%d", ext.calls)
	}
	if _, err := ms.agent.db.Exec(`DROP TRIGGER fail_runner_cursor`); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}
	gs2, err := goals.OpenStore(goalsPath)
	if err != nil {
		t.Fatal(err)
	}
	runner.Usage = gs2
	ms2, err := OpenLocalService(memoryPath, filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer ms2.Close()
	runner.Memory = ms2
	if seq, err := runner.RunOnce(context.Background(), "agent-1", MaintenanceCursor{}); err != nil || seq != 7 {
		t.Fatalf("recovery seq=%d err=%v", seq, err)
	}
	if ext.calls != 1 {
		t.Fatalf("recovery re-extracted calls=%d", ext.calls)
	}
	entries, err := ms2.List(context.Background(), ScopeAgent, true)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
	usage, ok := gs.GetUsage("agent-1")
	if !ok || usage.MaintenanceTokens != 3 {
		t.Fatalf("usage=%+v ok=%v", usage, ok)
	}
	cursor, err := ms2.agent.GetMaintenanceCursor(context.Background(), "agent-1")
	if err != nil || cursor.Sequence != 7 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
}

func TestMaintenanceRunnerSettleFailureReopenUsesPersistedUsage(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "goals.json")
	gs, err := goals.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: "agent-1", Enabled: true, MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	ms, err := OpenLocalService(filepath.Join(root, "memory.db"), filepath.Join(root, "global.db"), ScopeAgent, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	ext := &runnerExtractor{}
	usage := &failOnceUsage{Store: gs}
	runner := &MaintenanceRunner{Source: runnerSource{input: ExtractionInput{AgentID: "agent-1", SessionID: "s"}, seq: 7, complete: true}, Extractor: ext, Memory: ms, Usage: usage}
	if _, err := runner.RunOnce(context.Background(), "agent-1", MaintenanceCursor{}); err == nil {
		t.Fatal("expected settlement failure")
	}
	gs2, err := goals.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	runner.Usage = gs2
	runner.Source = noNewMaintenanceSource{}
	current, err := ms.agent.GetMaintenanceCursor(context.Background(), "agent-1")
	if err != nil || current.Sequence != 7 {
		t.Fatalf("persisted cursor=%+v err=%v", current, err)
	}
	if seq, err := runner.RunOnce(context.Background(), "agent-1", current); err != nil || seq != 7 {
		t.Fatalf("recovery seq=%d err=%v", seq, err)
	}
	if ext.calls != 1 {
		t.Fatalf("extract calls=%d", ext.calls)
	}
	u, ok := gs2.GetUsage("agent-1")
	if !ok || u.MaintenanceTokens != 3 {
		t.Fatalf("usage=%+v ok=%v", u, ok)
	}
}
