package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func newFeedbackTestServer(t *testing.T, cfg *config.Config, remote string) *Server {
	t.Helper()
	fs, err := store.OpenFeedback(filepath.Join(cfg.RuntimeRoot, "feedback.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux(), feedbackStore: fs, control: manage.NewControlClient(cfg)}
	s.registerFeedbackRoutes()
	t.Cleanup(func() { _ = fs.Close() })
	return s
}

func feedbackPayload(id, body string) map[string]any {
	return map[string]any{"client_feedback_id": id, "category": "bug", "title": "title", "body": body}
}

func TestFeedbackAPIRealSQLiteDeliveryAndReplySync(t *testing.T) {
	var posts, gets atomic.Int32
	reply := atomic.Value{}
	reply.Store("")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			var p map[string]any
			_ = json.NewDecoder(r.Body).Decode(&p)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"client_feedback_id": p["client_feedback_id"], "node_id": "node-a", "category": "bug", "title": "title", "body": p["body"], "status": "open", "reply": "", "revision": 1})
			return
		}
		gets.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"client_feedback_id": "f1", "node_id": "node-a", "category": "bug", "title": "title", "body": "body", "status": "resolved", "reply": reply.Load(), "revision": 2})
	}))
	defer remote.Close()
	cfg := &config.Config{NodeID: "node-a", RuntimeRoot: t.TempDir(), Manage: config.ManageConfig{Enabled: true, URL: remote.URL, NodeToken: "token"}}
	s := newFeedbackTestServer(t, cfg, remote.URL)
	body, _ := json.Marshal(feedbackPayload("f1", "body"))
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytesReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f, _ := s.feedbackStore.Get(context.Background(), "f1")
		if f != nil && f.DeliveryStatus == "delivered" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	reply.Store("fixed")
	syncReq := httptest.NewRequest(http.MethodPost, "/v1/feedback/f1/sync", nil)
	syncRR := httptest.NewRecorder()
	s.mux.ServeHTTP(syncRR, syncReq)
	if syncRR.Code != 200 {
		t.Fatalf("sync status=%d", syncRR.Code)
	}
	f, _ := s.feedbackStore.Get(context.Background(), "f1")
	if f.Reply != "fixed" || f.Status != "resolved" {
		t.Fatalf("reply not applied: %+v", f)
	}
	if posts.Load() != 1 || gets.Load() < 2 {
		t.Fatalf("posts=%d gets=%d", posts.Load(), gets.Load())
	}
}

func TestFeedbackAPICutoverAndDuplicateProtection(t *testing.T) {
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer remote.Close()
	cfg := &config.Config{NodeID: "node-a", RuntimeRoot: t.TempDir(), Manage: config.ManageConfig{Enabled: true, URL: remote.URL}}
	s := newFeedbackTestServer(t, cfg, remote.URL)
	body, _ := json.Marshal(feedbackPayload("same", "body"))
	r := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytesReader(body))
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, r)
	if rr.Code != 201 {
		t.Fatal(rr.Code)
	}
	f, _ := s.feedbackStore.Get(context.Background(), "same")
	f.Reply = "keep"
	_ = s.feedbackStore.Save(context.Background(), *f)
	r2 := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytesReader(body))
	rr2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rr2, r2)
	if rr2.Code != 200 {
		t.Fatalf("duplicate=%d", rr2.Code)
	}
	r3 := httptest.NewRequest(http.MethodPost, "/v1/feedback", bytesReader(mustJSON(feedbackPayload("same", "changed"))))
	rr3 := httptest.NewRecorder()
	s.mux.ServeHTTP(rr3, r3)
	if rr3.Code != 409 {
		t.Fatalf("conflict=%d", rr3.Code)
	}
	cfg.Manage.URL = "http://changed.invalid"
	s.tryDeliverFeedback(context.Background(), *f)
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("cutover sent remote calls=%d", calls.Load())
	}
}

func TestFeedbackAPIReopenAndDeliveredGETFailurePreservesState(t *testing.T) {
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer remote.Close()
	cfg := &config.Config{NodeID: "node-a", RuntimeRoot: t.TempDir(), Manage: config.ManageConfig{Enabled: true, URL: remote.URL}}
	path := filepath.Join(cfg.RuntimeRoot, "feedback.db")
	fs, e := store.OpenFeedback(path)
	if e != nil {
		t.Fatal(e)
	}
	f := store.Feedback{ClientFeedbackID: "reopen", NodeID: "node-a", Category: "bug", Title: "t", Body: "b", Status: "open", Revision: 1, DeliveryStatus: "pending", Destination: remote.URL}
	if _, _, e = fs.Create(context.Background(), f); e != nil {
		t.Fatal(e)
	}
	_ = fs.Close()
	fs, e = store.OpenFeedback(path)
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{cfg: cfg, feedbackStore: fs, control: manage.NewControlClient(cfg)}
	if e = s.syncFeedbackRecord(context.Background(), f); e == nil {
		t.Fatal("expected remote failure")
	}
	got, _ := fs.Get(context.Background(), "reopen")
	if got.DeliveryStatus != "pending" {
		t.Fatalf("status=%s", got.DeliveryStatus)
	}
	_ = fs.UpdateDelivery(context.Background(), "reopen", "delivered", "")
	got, _ = fs.Get(context.Background(), "reopen")
	if e = s.syncFeedbackRecord(context.Background(), *got); e == nil {
		t.Fatal("expected delivered GET failure")
	}
	got, _ = fs.Get(context.Background(), "reopen")
	if got.DeliveryStatus != "delivered" {
		t.Fatalf("delivered downgraded=%s", got.DeliveryStatus)
	}
	cfg.NodeID = "node-b"
	if e = s.syncFeedbackRecord(context.Background(), *got); e == nil {
		t.Fatal("expected node cutover block")
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected calls=%d", calls.Load())
	}
	_ = fs.Close()
}

func TestFeedbackAPILostResponseRetryKeepsOneRemoteRecord(t *testing.T) {
	var posts atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"client_feedback_id": "lost", "node_id": "node-a", "status": "open", "revision": 1})
			return
		}
		if r.Method != http.MethodPost {
			return
		}
		n := posts.Add(1)
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if n == 1 {
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"client_feedback_id":"lost","node_id":"node-a"}`))
			if h, ok := w.(http.Hijacker); ok {
				conn, _, _ := h.Hijack()
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"client_feedback_id": p["client_feedback_id"], "node_id": "node-a"})
	}))
	defer remote.Close()
	cfg := &config.Config{NodeID: "node-a", RuntimeRoot: t.TempDir(), Manage: config.ManageConfig{Enabled: true, URL: remote.URL}}
	fs, e := store.OpenFeedback(filepath.Join(cfg.RuntimeRoot, "f.db"))
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{cfg: cfg, feedbackStore: fs, control: manage.NewControlClient(cfg)}
	f := store.Feedback{ClientFeedbackID: "lost", NodeID: "node-a", Category: "bug", Title: "t", Body: "b", Destination: remote.URL}
	if _, _, e = fs.Create(context.Background(), f); e != nil {
		t.Fatal(e)
	}
	_ = s.syncFeedbackRecord(context.Background(), f)
	if e = s.syncFeedbackRecord(context.Background(), f); e != nil {
		t.Fatal(e)
	}
	if posts.Load() != 2 {
		t.Fatalf("posts=%d", posts.Load())
	}
	_ = fs.Close()
}

func TestFeedbackAPIRateLimitDoesNotPersist429(t *testing.T) {
	cfg := &config.Config{NodeID: "rate-node", RuntimeRoot: t.TempDir(), Manage: config.ManageConfig{Enabled: false}}
	s := newFeedbackTestServer(t, cfg, "")
	for i := 0; i < 5; i++ {
		b, _ := json.Marshal(feedbackPayload(fmt.Sprintf("rate-%d", i), "b"))
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/feedback", bytesReader(b)))
		if rr.Code != 201 {
			t.Fatalf("seed status=%d", rr.Code)
		}
	}
	b, _ := json.Marshal(feedbackPayload("rate-5", "b"))
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/feedback", bytesReader(b)))
	if rr.Code != 429 {
		t.Fatalf("limit status=%d", rr.Code)
	}
	if f, _ := s.feedbackStore.Get(context.Background(), "rate-5"); f != nil {
		t.Fatal("rate limited feedback was persisted")
	}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
func mustJSON(v any) []byte              { b, _ := json.Marshal(v); return b }
