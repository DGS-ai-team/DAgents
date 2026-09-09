package hooks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type riskWorkerStore struct {
	mu                      sync.Mutex
	seen                    map[string]bool
	beginCalls, settleCalls int
	last                    RiskObservationRecord
	lastUsed                int64
	lastUnknown             bool
	reject                  bool
	beginCh                 chan struct{}
}

func (s *riskWorkerStore) BeginRiskReview(_, operationID, _, _ string, _ int64, _ time.Time) (RiskReviewReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beginCalls++
	if s.beginCh != nil {
		select {
		case s.beginCh <- struct{}{}:
		default:
		}
	}
	if s.reject {
		return RiskReviewReservation{}, errors.New("budget")
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[operationID] {
		return RiskReviewReservation{}, nil
	}
	s.seen[operationID] = true
	return RiskReviewReservation{Claimed: true}, nil
}
func (s *riskWorkerStore) SettleRiskReviewWithObservation(_, _ string, used int64, unknown bool, obs RiskObservationRecord, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settleCalls++
	s.last = obs
	s.lastUsed = used
	s.lastUnknown = unknown
	return nil
}

func TestRiskDispatcherDeduplicatesAndPersistsActualUsage(t *testing.T) {
	store := &riskWorkerStore{beginCh: make(chan struct{}, 2)}
	host := riskHost{text: `{"level":"low","reason":"ok"}`, usage: &llm.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}}
	d := NewRiskDispatcher(RiskDispatcherConfig{Host: host, Store: store, AgentID: "agent-a", Capacity: 4})
	defer d.Close()
	in := RiskObservationInput{RequestID: "request-1", ToolName: "read_file", ArgsJSON: []byte(`{"path":"x"}`), PolicyAction: "allow"}
	if !d.Submit(in) || !d.Submit(in) {
		t.Fatal("submit unexpectedly rejected")
	}
	for i := 0; i < 2; i++ {
		select {
		case <-store.beginCh:
		case <-time.After(time.Second):
			t.Fatal("dedupe begin did not run")
		}
	}
	deadline := time.After(time.Second)
	for {
		store.mu.Lock()
		done := store.settleCalls
		store.mu.Unlock()
		if done == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("risk job did not settle")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	store.mu.Lock()
	got := store.last
	calls := store.beginCalls
	used := store.lastUsed
	unknown := store.lastUnknown
	store.mu.Unlock()
	if calls != 2 || got.PolicyAction != "allow" || got.UsageUnknown || got.RiskUnknown || used != 5 || unknown {
		t.Fatalf("store calls=%d observation=%+v used=%d unknown=%v", calls, got, used, unknown)
	}
}

func TestRiskDispatcherBudgetRejectDoesNotCallHost(t *testing.T) {
	called := make(chan string, 1)
	host := riskHost{text: `{"level":"low"}`, called: called}
	store := &riskWorkerStore{reject: true, beginCh: make(chan struct{}, 1)}
	d := NewRiskDispatcher(RiskDispatcherConfig{Host: host, Store: store, AgentID: "agent-a"})
	defer d.Close()
	if !d.Submit(RiskObservationInput{RequestID: "budget", ArgsJSON: []byte(`{}`), PolicyAction: "deny"}) {
		t.Fatal("submit rejected")
	}
	select {
	case <-store.beginCh:
	case <-time.After(time.Second):
		t.Fatal("budget admission was not attempted")
	}
	select {
	case got := <-called:
		t.Fatalf("host called after budget rejection: %s", got)
	default:
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.settleCalls != 0 {
		t.Fatal("budget rejection was settled")
	}
}

func TestRiskDispatcherMalformedOutputWithKnownUsageStillSettlesKnownUsage(t *testing.T) {
	store := &riskWorkerStore{beginCh: make(chan struct{}, 1)}
	host := riskHost{text: "not-json", usage: &llm.Usage{PromptTokens: 4, CompletionTokens: 1, TotalTokens: 5}}
	d := NewRiskDispatcher(RiskDispatcherConfig{Host: host, Store: store, AgentID: "agent-a"})
	defer d.Close()
	if !d.Submit(RiskObservationInput{RequestID: "malformed", ArgsJSON: []byte(`{}`), PolicyAction: "ask"}) {
		t.Fatal("submit rejected")
	}
	select {
	case <-store.beginCh:
	case <-time.After(time.Second):
		t.Fatal("admission did not run")
	}
	deadline := time.After(time.Second)
	for {
		store.mu.Lock()
		done, used, unknown, riskUnknown := store.settleCalls, store.lastUsed, store.lastUnknown, store.last.RiskUnknown
		store.mu.Unlock()
		if done == 1 {
			if used != 5 || unknown || !riskUnknown {
				t.Fatalf("used=%d unknown=%v riskUnknown=%v", used, unknown, riskUnknown)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("malformed result did not settle")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestRiskDispatcherQueueFullIsNonBlockingAndCloseCancels(t *testing.T) {
	entered := make(chan struct{}, 1)
	host := enteredBlockingHost{entered: entered}
	store := &riskWorkerStore{}
	d := NewRiskDispatcher(RiskDispatcherConfig{Host: host, Store: store, AgentID: "agent-a", Capacity: 1, Timeout: time.Hour})
	if !d.Submit(RiskObservationInput{RequestID: "one", ArgsJSON: []byte(`{}`)}) {
		t.Fatal("first submit rejected")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter host")
	}
	if d.Submit(RiskObservationInput{RequestID: "queued", ArgsJSON: []byte(`{}`)}) == false {
		t.Fatal("queue slot unexpectedly unavailable")
	}
	if d.Submit(RiskObservationInput{RequestID: "full", ArgsJSON: []byte(`{}`)}) {
		t.Fatal("full queue accepted job")
	}
	start := time.Now()
	for i := 0; i < 100; i++ {
		d.Submit(RiskObservationInput{RequestID: string(rune(i + 100)), ArgsJSON: []byte(`{}`)})
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("queue submit blocked")
	}
	d.Close()
}

type enteredBlockingHost struct{ entered chan<- struct{} }

func (h enteredBlockingHost) Snapshot() HostSnapshot             { return HostSnapshot{} }
func (h enteredBlockingHost) SessionStoreGet(string) (any, bool) { return nil, false }
func (h enteredBlockingHost) SessionStoreSet(string, any) error  { return nil }
func (h enteredBlockingHost) SessionStoreDelete(string) error    { return nil }
func (h enteredBlockingHost) LLMComplete(ctx context.Context, _ LLMCompleteRequest) (LLMCompleteResponse, error) {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return LLMCompleteResponse{}, ctx.Err()
}
