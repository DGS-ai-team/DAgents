package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func simplifiedOverview(t *testing.T, s *Server, query string) autoOverviewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleAutoOverview(w, httptest.NewRequest("GET", "/v1/auto/overview"+query, nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out autoOverviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func simplifiedProfile(id string, interval int64) autonomy.Profile {
	return autonomy.Profile{AgentID: id, WakeIntervalSeconds: interval, MaxToolRounds: 32, Timezone: "UTC"}
}

func TestAutoOverviewSimplifiedProjectionUsesAutonomyStateAndFilters(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	if err := s.autonomyStore.PutProfile(simplifiedProfile("auto-v2", 60), 0); err != nil {
		t.Fatal(err)
	}
	p, _ := s.autonomyStore.GetProfile("auto-v2")
	p.DreamingEnabled, p.DreamingTime = true, "03:00"
	if err := s.autonomyStore.PutProfile(p, p.Revision); err != nil {
		t.Fatal(err)
	}
	s.dreamingSched = NewDreamingScheduler(s.autonomyStore, s.agents, s.sessions, func(context.Context, string) error { return nil })
	if _, err := s.autonomyStore.CreateTodo("other-v2", "todo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.triggerStore.EnsureAutoDefault("auto-v2", 60, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	out := simplifiedOverview(t, s, "?search=auto-v2&status=standby")
	if len(out.Items) != 1 || out.Items[0].State != "standby" || out.Items[0].WakeIntervalSeconds != 60 {
		t.Fatalf("projection=%+v", out)
	}
	manageItems, err := s.autoSummaryProvider()(context.Background())
	if err != nil || len(manageItems) == 0 {
		t.Fatalf("manage projection err=%v items=%v", err, manageItems)
	}
	if manageItems[0].State != out.Items[0].State || manageItems[0].TodoCounts["pending"] != out.Items[0].TodoCounts["pending"] || (manageItems[0].NextAt == nil) != (out.Items[0].NextAt == nil) {
		t.Fatalf("node/manage projection drift: node=%+v manage=%+v", out.Items[0], manageItems[0])
	}
	if out.Items[0].Dreaming.State == "" || manageItems[0].Dreaming.State != out.Items[0].Dreaming.State {
		t.Fatalf("dreaming projection drift: node=%+v manage=%+v", out.Items[0].Dreaming, manageItems[0].Dreaming)
	}
	if got := simplifiedOverview(t, s, "?search=other-v2"); len(got.Items) != 1 || got.Items[0].TodoCounts["pending"] != 1 {
		t.Fatalf("todo projection=%+v", got)
	}
	if got := simplifiedOverview(t, s, "?search=normal-v2"); len(got.Items) != 0 {
		t.Fatalf("normal leaked=%+v", got)
	}
	all := simplifiedOverview(t, s, "")
	for _, state := range []string{"needs_attention", "working", "standby", "activation_off"} {
		if _, ok := all.Counts[state]; !ok {
			t.Fatalf("counts missing state %q: %+v", state, all.Counts)
		}
	}
}

func TestAutoOverviewSimplifiedTriggerAttentionAndNoInvalidNext(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	if err := s.autonomyStore.PutProfile(simplifiedProfile("auto-v2", 60), 0); err != nil {
		t.Fatal(err)
	}
	out := simplifiedOverview(t, s, "?search=auto-v2")
	if len(out.Items) != 1 || out.Items[0].State != "needs_attention" || out.Items[0].NextAt != nil {
		t.Fatalf("missing trigger=%+v", out)
	}
	_, err := s.triggerStore.EnsureAutoDefault("auto-v2", 60, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.triggerStore.EnsureAutoDefault("auto-v2", 0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	out = simplifiedOverview(t, s, "?search=auto-v2&status=needs_attention")
	if len(out.Items) != 1 || out.Items[0].NextAt != nil {
		t.Fatalf("disabled trigger=%+v", out)
	}
	if _, err := s.triggerStore.EnsureAutoDefault("auto-v2", 30, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	out = simplifiedOverview(t, s, "?search=auto-v2&status=needs_attention")
	if len(out.Items) != 1 || out.Items[0].NextAt != nil {
		t.Fatalf("interval mismatch=%+v", out)
	}
	d, ok := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID("auto-v2"))
	if !ok {
		t.Fatal("default trigger missing")
	}
	d.RecoveryRequired, d.RecoveryReason = true, "test"
	if err := s.triggerStore.ReplaceTrigger(*d); err != nil {
		t.Fatal(err)
	}
	out = simplifiedOverview(t, s, "?search=auto-v2&status=needs_attention")
	if len(out.Items) != 1 || out.Items[0].NextAt != nil {
		t.Fatalf("recovery trigger=%+v", out)
	}
}

func TestAutoOverviewSimplifiedManagePayloadIsPrivate(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	if _, err := s.autonomyStore.CreateTodo("auto-v2", strings.Repeat("secret", 8)); err != nil {
		t.Fatal(err)
	}
	items, err := s.autoSummaryProvider()(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(items)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "responsibility") || strings.Contains(string(b), "usage") {
		t.Fatalf("private data leaked: %s", b)
	}
}
