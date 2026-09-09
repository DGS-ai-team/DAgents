package hooks

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func realRiskStore(t *testing.T, agentIDs ...string) (*goals.Store, string, time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "goals.json")
	s, err := goals.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range agentIDs {
		if _, err := s.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, TotalTokenBudget: 1000}, 0, now); err != nil {
			t.Fatal(err)
		}
	}
	return s, path, now
}

func waitRiskObservation(t *testing.T, s *goals.Store, agent string, n int) {
	t.Helper()
	deadline := time.After(time.Second)
	for len(s.ListRiskObservations(agent, n)) < n {
		select {
		case <-deadline:
			t.Fatalf("agent %s observations=%d, want %d", agent, len(s.ListRiskObservations(agent, n)), n)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestGoalsRiskAdapterSeparatesAgentsAndPersistsActualUsage(t *testing.T) {
	s, path, _ := realRiskStore(t, "agent-a", "agent-b")
	host := riskHost{text: `{"level":"medium","reason":"review"}`, usage: &llm.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}}
	for _, agent := range []string{"agent-a", "agent-b"} {
		d := NewRiskDispatcher(RiskDispatcherConfig{Host: host, Store: GoalsRiskReviewStore{Store: s}, AgentID: agent})
		if !d.Submit(RiskObservationInput{RequestID: "same-request", ToolName: "read_file", ArgsJSON: []byte(`{"path":"x"}`), PolicyAction: "allow"}) {
			t.Fatal("submit rejected")
		}
		waitRiskObservation(t, s, agent, 1)
		d.Close()
	}
	reopened, err := goals.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"agent-a", "agent-b"} {
		obs := reopened.ListRiskObservations(agent, 10)
		if len(obs) != 1 || obs[0].ArgsDigest == "" || obs[0].ToolName != "read_file" {
			t.Fatalf("%s observations=%+v", agent, obs)
		}
		if got, _ := reopened.GetUsage(agent); got.RiskReviewTokens != 5 {
			t.Fatalf("%s usage=%+v", agent, got)
		}
	}
}

func TestGoalsRiskAdapterPendingRestartAndUnknownDoNotCallLLM(t *testing.T) {
	s, path, now := realRiskStore(t, "agent-a")
	called := make(chan string, 1)
	adapter := GoalsRiskReviewStore{Store: s}
	cfg := RiskDispatcherConfig{Host: riskHost{text: `{"level":"low"}`, called: called}, Store: adapter, AgentID: "agent-a"}
	if _, err := s.BeginRiskReview("agent-a", "risk:agent-a:pending", "pending", riskArgsDigest([]byte(`{}`))+":", cfg.EstimatedTokens, now); err != nil {
		t.Fatal(err)
	}
	restarted, err := goals.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	begin := make(chan struct{}, 1)
	cfg.Store = beginSignalStore{inner: GoalsRiskReviewStore{Store: restarted}, begin: begin}
	d := NewRiskDispatcher(cfg)
	if !d.Submit(RiskObservationInput{RequestID: "pending", ArgsJSON: []byte(`{}`)}) {
		t.Fatal("submit rejected")
	}
	select {
	case <-begin:
	case <-time.After(time.Second):
		t.Fatal("pending admission not attempted")
	}
	select {
	case <-called:
		t.Fatal("pending restart called LLM")
	default:
	}
	d.Close()
	unknownStore, unknownPath, _ := realRiskStore(t, "agent-a")
	obs := goals.RiskObservationRecord{AgentID: "agent-a", OperationID: "risk:agent-a:unknown", RequestID: "unknown", ArgsDigest: "digest"}
	if _, err := unknownStore.BeginRiskReview("agent-a", "risk:agent-a:unknown", "unknown", "fp2", 10, now); err != nil {
		t.Fatal(err)
	}
	if _, err := unknownStore.SettleRiskReviewWithObservation("agent-a", "risk:agent-a:unknown", 0, true, obs, now); err != nil {
		t.Fatal(err)
	}
	reopenedUnknown, err := goals.OpenStore(unknownPath)
	if err != nil {
		t.Fatal(err)
	}
	unknownHost := riskHost{text: `{"level":"low"}`, called: called}
	unknownBegin := make(chan struct{}, 1)
	d = NewRiskDispatcher(RiskDispatcherConfig{Host: unknownHost, Store: beginSignalStore{inner: GoalsRiskReviewStore{Store: reopenedUnknown}, begin: unknownBegin}, AgentID: "agent-a"})
	if !d.Submit(RiskObservationInput{RequestID: "new-after-unknown", ArgsJSON: []byte(`{}`)}) {
		t.Fatal("submit rejected")
	}
	select {
	case <-unknownBegin:
	case <-time.After(time.Second):
		t.Fatal("unknown admission not attempted")
	}
	select {
	case <-called:
		t.Fatal("unknown risk called LLM")
	default:
	}
	d.Close()
}

func TestGoalsRiskAdapterSettleFailureKeepsPendingReceipt(t *testing.T) {
	s, _, now := realRiskStore(t, "agent-a")
	called := make(chan string, 1)
	d := NewRiskDispatcher(RiskDispatcherConfig{Host: riskHost{text: `{"level":"low"}`, usage: &llm.Usage{TotalTokens: 2}, called: called}, Store: failingRiskStore{inner: GoalsRiskReviewStore{Store: s}}, AgentID: "agent-a"})
	if !d.Submit(RiskObservationInput{RequestID: "failure", ArgsJSON: []byte(`{}`)}) {
		t.Fatal("submit rejected")
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("host not called")
	}
	d.Close()
	// The dispatcher reserves output allowance plus the encoded input and
	// fixed prompt/framing allowance for this exact request.
	r, err := s.BeginRiskReview("agent-a", "risk:agent-a:failure", "failure", riskArgsDigest([]byte(`{}`))+"::", 393, now)
	if err != nil || r.Claimed {
		t.Fatalf("failed settlement did not retain pending receipt: %+v err=%v", r, err)
	}
}

type failingRiskStore struct{ inner GoalsRiskReviewStore }

func (s failingRiskStore) BeginRiskReview(a, o, r, f string, e int64, n time.Time) (RiskReviewReservation, error) {
	return s.inner.BeginRiskReview(a, o, r, f, e, n)
}
func (failingRiskStore) SettleRiskReviewWithObservation(string, string, int64, bool, RiskObservationRecord, time.Time) error {
	return errors.New("disk full")
}

type beginSignalStore struct {
	inner RiskReviewStore
	begin chan<- struct{}
}

func (s beginSignalStore) BeginRiskReview(a, o, r, f string, e int64, n time.Time) (RiskReviewReservation, error) {
	out, err := s.inner.BeginRiskReview(a, o, r, f, e, n)
	select {
	case s.begin <- struct{}{}:
	default:
	}
	return out, err
}
func (s beginSignalStore) SettleRiskReviewWithObservation(a, o string, u int64, x bool, r RiskObservationRecord, n time.Time) error {
	return s.inner.SettleRiskReviewWithObservation(a, o, u, x, r, n)
}
