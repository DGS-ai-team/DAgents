package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"sync"
	"time"
)

// RiskReviewReservation is the small admission result needed by the worker.
// The durable implementation owns the receipt and budget accounting.
type RiskReviewReservation struct{ Claimed bool }

// RiskReviewStore is implemented by the goals adapter. Keeping this interface
// here avoids coupling the hook queue to the persistence package.
type RiskReviewStore interface {
	BeginRiskReview(agentID, operationID, requestID, fingerprint string, estimated int64, now time.Time) (RiskReviewReservation, error)
	SettleRiskReviewWithObservation(agentID, operationID string, used int64, unknown bool, observation RiskObservationRecord, now time.Time) error
}

type RiskObservationRecord struct {
	AgentID, OperationID, RequestID, ToolName, ArgsDigest, PolicyAction, Level, Reason, Recommendation string
	RiskUnknown, UsageUnknown                                                                          bool
	Error                                                                                              string
}

type RiskDispatcherConfig struct {
	Host            Host
	Store           RiskReviewStore
	AgentID         string
	Capacity        int
	Timeout         time.Duration
	EstimatedTokens int64
	MaxArgsBytes    int
}

type riskJob struct{ in RiskObservationInput }

func riskArgsDigest(args []byte) string {
	sum := sha256.Sum256(args)
	return hex.EncodeToString(sum[:])
}

// RiskDispatcher evaluates observations asynchronously. Submit never waits
// for admission or the model and therefore cannot alter tool execution.
type RiskDispatcher struct {
	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
	queue  chan riskJob
	wg     sync.WaitGroup
	cfg    RiskDispatcherConfig
}

func NewRiskDispatcher(cfg RiskDispatcherConfig) *RiskDispatcher {
	if cfg.Capacity <= 0 || cfg.Capacity > 1024 {
		cfg.Capacity = 32
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 30*time.Second {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.EstimatedTokens <= 0 {
		cfg.EstimatedTokens = 256
	}
	if cfg.MaxArgsBytes <= 0 || cfg.MaxArgsBytes > 128<<10 {
		cfg.MaxArgsBytes = 32 << 10
	}
	cfg.AgentID = strings.TrimSpace(cfg.AgentID)
	ctx, cancel := context.WithCancel(context.Background())
	d := &RiskDispatcher{cancel: cancel, queue: make(chan riskJob, cfg.Capacity), cfg: cfg}
	d.wg.Add(1)
	go d.worker(ctx)
	return d
}

func (d *RiskDispatcher) Submit(in RiskObservationInput) bool {
	if d == nil || strings.TrimSpace(in.RequestID) == "" || strings.TrimSpace(d.cfg.AgentID) == "" || len(in.ArgsJSON) > d.cfg.MaxArgsBytes || len(in.ToolName)+len(in.PolicyAction)+len(in.RequestID) > 4096 {
		return false
	}
	in.AgentID = d.cfg.AgentID
	in.ArgsJSON = append([]byte(nil), in.ArgsJSON...)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	select {
	case d.queue <- riskJob{in: in}:
		return true
	default:
		return false
	}
}

func (d *RiskDispatcher) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		d.cancel()
	}
	d.mu.Unlock()
	d.wg.Wait()
}

func (d *RiskDispatcher) worker(ctx context.Context) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-d.queue:
			d.process(ctx, job.in)
		}
	}
}

func (d *RiskDispatcher) process(parent context.Context, in RiskObservationInput) {
	if d.cfg.Store == nil || d.cfg.Host == nil || parent.Err() != nil {
		return
	}
	operationID := "risk:" + d.cfg.AgentID + ":" + in.RequestID
	fingerprint := riskArgsDigest(in.ArgsJSON) + ":" + in.PolicyAction + ":" + in.ToolName
	// Reserve against this observation's actual input size. The fixed allowance
	// covers the risk system prompt and framing; EstimatedTokens is the output
	// allowance. This keeps small calls usable while conservatively charging
	// larger envelopes before the host is invoked.
	inputBytes := int64(len(in.ArgsJSON)) + int64(len(in.ToolName)) + int64(len(in.PolicyAction)) + int64(len(in.RequestID))
	if inputBytes > math.MaxInt64-128 || d.cfg.EstimatedTokens > math.MaxInt64-inputBytes-128 {
		return
	}
	estimated := d.cfg.EstimatedTokens + inputBytes + 128
	reservation, err := d.cfg.Store.BeginRiskReview(d.cfg.AgentID, operationID, in.RequestID, fingerprint, estimated, time.Now().UTC())
	if err != nil || !reservation.Claimed {
		return
	}
	obs := RiskEvaluator{Host: d.cfg.Host, Timeout: d.cfg.Timeout, MaxArgsBytes: d.cfg.MaxArgsBytes}.Evaluate(parent, in)
	used, unknown := int64(0), obs.UsageUnknown
	if obs.Usage != nil && !obs.UsageUnknown {
		used = int64(obs.Usage.TotalTokens)
		if used == 0 {
			used = int64(obs.Usage.PromptTokens + obs.Usage.CompletionTokens)
		}
	}
	_ = d.cfg.Store.SettleRiskReviewWithObservation(d.cfg.AgentID, operationID, used, unknown, RiskObservationRecord{
		AgentID: d.cfg.AgentID, OperationID: operationID, RequestID: in.RequestID, ToolName: in.ToolName,
		ArgsDigest: riskArgsDigest(in.ArgsJSON), PolicyAction: in.PolicyAction, Level: obs.Level,
		Reason: obs.Reason, Recommendation: obs.Recommendation, RiskUnknown: obs.RiskUnknown,
		UsageUnknown: obs.UsageUnknown, Error: obs.Error,
	}, time.Now().UTC())
}
