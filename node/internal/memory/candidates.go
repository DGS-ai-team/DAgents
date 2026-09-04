package memory

import (
	"context"
	"fmt"
	"sync"
)

// CandidatePipeline owns a bounded background extraction queue. One pipeline
// is created per Agent runtime, so its single worker also provides the
// per-Agent serialization boundary required by the memory contract.
//
// A nil extractor or consolidator makes the pipeline inert. This is the
// deliberate default: merely enabling Memory v2 does not spend LLM tokens on
// automatic extraction.
type CandidatePipeline struct {
	extractor    CandidateExtractor
	consolidator Consolidator
	jobs         chan ExtractionInput
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	onError      func(error)
	coreBudget   int

	mu       sync.RWMutex
	closed   bool
	onChange func(ConsolidationReport)
}

// NewCandidatePipeline creates an optional asynchronous pipeline. queueSize
// is bounded so compression cannot create unbounded memory work when a model
// or SQLite store is slow.
func NewCandidatePipeline(
	extractor CandidateExtractor,
	consolidator Consolidator,
	queueSize int,
	onError func(error),
) *CandidatePipeline {
	if queueSize <= 0 {
		queueSize = 16
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &CandidatePipeline{
		extractor:    extractor,
		consolidator: consolidator,
		jobs:         make(chan ExtractionInput, queueSize),
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
		onError:      onError,
		coreBudget:   defaultCoreBudget,
	}
	go p.run()
	return p
}

func (p *CandidatePipeline) SetCoreBudget(tokens int) {
	if p == nil || tokens <= 0 {
		return
	}
	p.mu.Lock()
	p.coreBudget = tokens
	p.mu.Unlock()
}

// SetOnChange attaches a metadata-only callback. It is intentionally separate
// from construction so the runtime can bind its publisher after allocation.
func (p *CandidatePipeline) SetOnChange(fn func(ConsolidationReport)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.onChange = fn
	p.mu.Unlock()
}

// Submit queues a defensive copy and returns false when automatic extraction
// is disabled or the bounded queue is full. The caller must treat false as a
// dropped optional background task; the active Turn is never affected.
func (p *CandidatePipeline) Submit(input ExtractionInput) bool {
	if p == nil || p.extractor == nil || p.consolidator == nil {
		return false
	}
	input = cloneExtractionInput(input)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return false
	}
	select {
	case p.jobs <- input:
		return true
	default:
		return false
	}
}

// Close cancels extraction and prevents late compression callbacks from
// sending on a closed channel. Extraction implementations must honor ctx.
func (p *CandidatePipeline) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.cancel()
	close(p.jobs)
	p.mu.Unlock()
	<-p.done
	return nil
}

func (p *CandidatePipeline) run() {
	defer close(p.done)
	for {
		select {
		case <-p.ctx.Done():
			return
		case input, ok := <-p.jobs:
			if !ok {
				return
			}
			p.process(input)
		}
	}
}

func (p *CandidatePipeline) process(input ExtractionInput) {
	candidates, err := p.extractor.Extract(p.ctx, input)
	if err != nil {
		p.reportError(fmt.Errorf("extract memory candidates: %w", err))
		return
	}
	candidates = normalizeCandidates(input, candidates)
	if len(candidates) == 0 {
		return
	}
	results, err := p.consolidator.Consolidate(p.ctx, candidates)
	if err != nil {
		p.reportError(fmt.Errorf("consolidate memory candidates: %w", err))
		return
	}
	report := buildConsolidationReport(results)
	if maintainer, ok := p.consolidator.(Maintainer); ok {
		p.mu.RLock()
		coreBudget := p.coreBudget
		p.mu.RUnlock()
		maintenanceReport, err := maintainer.Maintain(p.ctx, input.Scope, coreBudget)
		if err != nil {
			p.reportError(fmt.Errorf("maintain memory after consolidation: %w", err))
			return
		}
		// Maintenance may change the store even when every extracted candidate
		// was a duplicate. Preserve that fact in the metadata-only notification.
		if maintenanceReport.Changed {
			report.Changed = true
		}
		if maintenanceReport.StoreRevision > report.StoreRevision {
			report.StoreRevision = maintenanceReport.StoreRevision
		}
	}
	if report.Changed {
		p.mu.RLock()
		callback := p.onChange
		p.mu.RUnlock()
		if callback != nil {
			callback(report)
		}
	}
}

func (p *CandidatePipeline) reportError(err error) {
	if p.onError != nil && err != nil {
		p.onError(err)
	}
}

func normalizeCandidates(input ExtractionInput, candidates []Candidate) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		req := candidate.Request
		scope := input.Scope
		if scope != ScopeAgent && scope != ScopeGlobal {
			scope = ScopeAgent
		}
		// The model must not widen the Agent's configured scope. The extractor
		// output is treated as content, not as an authorization decision.
		req.Scope = scope
		if req.AgentID == "" {
			req.AgentID = input.AgentID
		}
		if req.SourceType == "" {
			req.SourceType = "model_inference"
		}
		if req.SourceSession == "" {
			req.SourceSession = input.SessionID
		}
		if req.SourceRef == "" {
			req.SourceRef = input.SourceFingerprint
		}
		// Automatic extraction starts in the Recall tier. Core promotion is a
		// later, deterministic maintenance decision, never an LLM privilege.
		req.Tier = TierRecall
		if req.Information == "" {
			continue
		}
		candidate.Request = req
		out = append(out, candidate)
	}
	return out
}

func cloneExtractionInput(in ExtractionInput) ExtractionInput {
	out := in
	out.Messages = append([]ExtractionMessage(nil), in.Messages...)
	for i := range out.Messages {
		out.Messages[i].ToolCalls = append([]ExtractionToolCall(nil), in.Messages[i].ToolCalls...)
	}
	return out
}

func buildConsolidationReport(results []WriteResult) ConsolidationReport {
	report := ConsolidationReport{}
	for _, result := range results {
		report.CandidateCount++
		if result.StoreRevision > report.StoreRevision {
			report.StoreRevision = result.StoreRevision
		}
		switch result.Outcome {
		case WriteAdded:
			report.Added++
			report.Changed = true
		case WriteDuplicate:
			report.Duplicates++
		case WritePendingConflict:
			report.PendingConflicts++
			report.Changed = true
		case WriteSuperseded:
			report.Superseded++
			report.Changed = true
		}
	}
	return report
}
