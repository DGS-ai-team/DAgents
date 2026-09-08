package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func reporterSummary() AutoEmployeeSummary {
	return AutoEmployeeSummary{AgentID: "auto/1", DisplayName: "摘要员工", Role: "整理资料", State: "waiting", Reason: "等待", LastResult: "已完成", AsOf: time.Now().UTC(), Usage: map[string]any{"tokens": 7}}
}

func TestAutoSummaryReporterSendsBoundIdentityAndWhitelist(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/registry/nodes/node one/auto-summary" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.Header.Get("x-dagents-a2a-token") != "secret" || r.Header.Get("x-dagents-agent-id") != "node one" {
			t.Errorf("identity headers missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		for _, forbidden := range []string{"role_objective", "prompt", "transcript", "workspace", "path", "artifact_body"} {
			if _, ok := body[forbidden]; ok {
				t.Errorf("privacy field %q sent", forbidden)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	r, err := NewAutoSummaryReporter(srv.URL, "node one", "secret", func(context.Context) ([]AutoEmployeeSummary, error) {
		return []AutoEmployeeSummary{reporterSummary()}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestAutoSummaryReporterRejectsRedirectAndFailure(t *testing.T) {
	var target atomic.Int32
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { target.Add(1) }))
	defer dst.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dst.URL, http.StatusFound) }))
	defer redirect.Close()
	r, err := NewAutoSummaryReporter(redirect.URL, "node-a", "secret", func(context.Context) ([]AutoEmployeeSummary, error) {
		return []AutoEmployeeSummary{reporterSummary()}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background()); err == nil || target.Load() != 0 {
		t.Fatalf("redirect result=%v target_calls=%d", err, target.Load())
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusInternalServerError) }))
	defer bad.Close()
	r, _ = NewAutoSummaryReporter(bad.URL, "node-a", "secret", func(context.Context) ([]AutoEmployeeSummary, error) {
		return []AutoEmployeeSummary{reporterSummary()}, nil
	})
	if err := r.Report(context.Background()); err == nil {
		t.Fatal("500 counted as success")
	}
}

func TestAutoSummaryReporterValidatesURLAndTimeout(t *testing.T) {
	for _, raw := range []string{"https://u:p@example.com", "https://example.com/path?q=secret", "https://example.com/path#frag"} {
		if _, err := NewAutoSummaryReporter(raw, "node", "token", func(context.Context) ([]AutoEmployeeSummary, error) {
			return []AutoEmployeeSummary{reporterSummary()}, nil
		}); err == nil {
			t.Fatalf("accepted unsafe URL %q", raw)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) }))
	defer srv.Close()
	r, err := NewAutoSummaryReporter(srv.URL, "node", "token", func(context.Context) ([]AutoEmployeeSummary, error) {
		return []AutoEmployeeSummary{reporterSummary()}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.Report(ctx); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("timeout error=%v", err)
	}
}

func TestAutoSummaryReporterContinuesAfterOneAgentFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 2 {
			http.Error(w, "one failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	items := []AutoEmployeeSummary{reporterSummary(), reporterSummary(), reporterSummary()}
	items[1].AgentID, items[2].AgentID = "auto-2", "auto-3"
	r, err := NewAutoSummaryReporter(srv.URL, "node-a", "secret", func(context.Context) ([]AutoEmployeeSummary, error) { return items, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background()); err == nil {
		t.Fatal("agent failure was hidden")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d want 3", calls.Load())
	}
}

func TestAutoSummaryReporterRotatesLargeCatalogAcrossReports(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body AutoEmployeeSummary
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		mu.Lock()
		seen[body.AgentID]++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	items := make([]AutoEmployeeSummary, 100)
	for i := range items {
		items[i] = reporterSummary()
		items[i].AgentID = fmt.Sprintf("auto-%03d", i)
	}
	r, err := NewAutoSummaryReporter(srv.URL, "node-a", "secret", func(context.Context) ([]AutoEmployeeSummary, error) {
		return items, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(items) {
		t.Fatalf("rotated %d agents, want %d", len(seen), len(items))
	}
}

func TestAutoSummaryReporterTimeoutDoesNotStarveNextAgent(t *testing.T) {
	var calls atomic.Int32
	var seen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body AutoEmployeeSummary
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		if body.AgentID == "auto-0" && calls.Add(1) == 1 {
			time.Sleep(1500 * time.Millisecond)
			return
		}
		seen.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	items := []AutoEmployeeSummary{reporterSummary(), reporterSummary()}
	items[0].AgentID, items[1].AgentID = "auto-0", "auto-1"
	r, err := NewAutoSummaryReporter(srv.URL, "node-a", "secret", func(context.Context) ([]AutoEmployeeSummary, error) {
		return items, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background()); err == nil {
		t.Fatal("expected first agent timeout")
	}
	if seen.Load() != 1 {
		t.Fatalf("next agent was not attempted, seen=%d", seen.Load())
	}
	if err := r.Report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.Load() != 3 {
		t.Fatalf("timed out agent was not retried, successful=%d", seen.Load())
	}
}

func TestAutoSummaryReporterCancelledContextMakesNoRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	r, err := NewAutoSummaryReporter(srv.URL, "node-a", "secret", func(context.Context) ([]AutoEmployeeSummary, error) {
		return []AutoEmployeeSummary{reporterSummary()}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Report(ctx); err == nil {
		t.Fatal("cancelled report returned success")
	}
	if calls.Load() != 0 {
		t.Fatalf("cancelled report sent %d requests", calls.Load())
	}
}

func TestRegistrarAutoSummaryHookIsBoundedAndDoesNotFailHeartbeat(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	reg := NewRegistrar(testManageConfig(srv.URL, "secret"), nil)
	reg.SetAutoSummaryProvider(func(context.Context) ([]AutoEmployeeSummary, error) {
		return []AutoEmployeeSummary{reporterSummary()}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reg.reportAutoSummaries(ctx)
	if calls.Load() != 1 {
		t.Fatalf("summary calls=%d", calls.Load())
	}
}

func TestRegistrarRetainsSummaryRotationCursor(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body AutoEmployeeSummary
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		mu.Lock()
		seen[body.AgentID] = true
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	items := make([]AutoEmployeeSummary, 65)
	for i := range items {
		items[i] = reporterSummary()
		items[i].AgentID = fmt.Sprintf("auto-%03d", i)
	}
	reg := NewRegistrar(testManageConfig(srv.URL, "secret"), nil)
	reg.SetAutoSummaryProvider(func(context.Context) ([]AutoEmployeeSummary, error) { return items, nil })
	reg.reportAutoSummaries(context.Background())
	reg.reportAutoSummaries(context.Background())
	if len(seen) != len(items) {
		t.Fatalf("registrar reported %d agents, want %d", len(seen), len(items))
	}
}

func TestRegistrarRegisterPostsAutoSummaries(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/registry/agents":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"agent":{"status":"online"},"heartbeat_interval_seconds":60}`))
		case "/v1/registry/nodes/ops-01/auto-summary":
			var body AutoEmployeeSummary
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode summary: %v", err)
				return
			}
			mu.Lock()
			seen[body.AgentID] = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	items := []AutoEmployeeSummary{reporterSummary(), reporterSummary()}
	items[0].AgentID, items[1].AgentID = "auto-1", "auto-2"
	reg := NewRegistrar(testManageConfig(srv.URL, "secret"), nil)
	reg.SetAutoSummaryProvider(func(context.Context) ([]AutoEmployeeSummary, error) { return items, nil })
	if got := reg.register(context.Background()); got != 60*time.Second {
		t.Fatalf("interval=%s", got)
	}
	if len(seen) != 2 {
		t.Fatalf("summary agents=%d want 2", len(seen))
	}
}
