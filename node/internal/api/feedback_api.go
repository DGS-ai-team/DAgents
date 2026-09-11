package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

type feedbackCreateInput struct {
	ClientFeedbackID    string `json:"client_feedback_id"`
	Category            string `json:"category"`
	Title               string `json:"title"`
	Body                string `json:"body"`
	ExpectedDestination string `json:"expected_destination"`
}

func (s *Server) allowNewFeedback(node string) bool {
	if s.feedbackRate == nil {
		s.feedbackRate = make(map[string][]time.Time)
	}
	s.feedbackRateMu.Lock()
	defer s.feedbackRateMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	xs := s.feedbackRate[node]
	j := 0
	for _, t := range xs {
		if t.After(cutoff) {
			xs[j] = t
			j++
		}
	}
	xs = xs[:j]
	if len(xs) >= 5 {
		s.feedbackRate[node] = xs
		return false
	}
	s.feedbackRate[node] = append(xs, now)
	return true
}

func (s *Server) registerFeedbackRoutes() {
	s.mux.HandleFunc("POST /v1/feedback", s.handleFeedbackCreate)
	s.mux.HandleFunc("GET /v1/feedback/target", s.handleFeedbackTarget)
	s.mux.HandleFunc("GET /v1/feedback", s.handleFeedbackList)
	s.mux.HandleFunc("GET /v1/feedback/{client_feedback_id}", s.handleFeedbackGet)
	s.mux.HandleFunc("POST /v1/feedback/{client_feedback_id}/sync", s.handleFeedbackSync)
}
func (s *Server) handleFeedbackCreate(w http.ResponseWriter, r *http.Request) {
	if s.feedbackStore == nil {
		http.Error(w, "feedback unavailable", http.StatusServiceUnavailable)
		return
	}
	var in feedbackCreateInput
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 110000)).Decode(&in) != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	if in.Category != "bug" && in.Category != "suggestion" && in.Category != "other" || strings.TrimSpace(in.Title) == "" || len(in.Title) > 256 || strings.TrimSpace(in.Body) == "" || len(in.Body) > 100000 {
		http.Error(w, "invalid feedback fields", 400)
		return
	}
	destination := strings.TrimRight(strings.TrimSpace(s.cfg.Manage.URL), "/")
	if in.ExpectedDestination != "" && strings.TrimRight(strings.TrimSpace(in.ExpectedDestination), "/") != destination {
		http.Error(w, "feedback destination changed", http.StatusConflict)
		return
	}
	f := store.Feedback{NodeID: s.cfg.NodeID, Category: in.Category, Title: in.Title, Body: in.Body}
	f.ClientFeedbackID = strings.TrimSpace(in.ClientFeedbackID)
	if strings.TrimSpace(f.ClientFeedbackID) == "" {
		b := make([]byte, 16)
		if _, e := rand.Read(b); e != nil {
			http.Error(w, "id generation failed", 500)
			return
		}
		f.ClientFeedbackID = hex.EncodeToString(b)
	}
	f.Status = "open"
	f.Reply = ""
	f.Revision = 1
	f.DeliveryStatus = "pending"
	f.LastError = ""
	f.Destination = destination
	if existing, _ := s.feedbackStore.Get(r.Context(), f.ClientFeedbackID); existing == nil && !s.allowNewFeedback(s.cfg.NodeID) {
		http.Error(w, "feedback rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	stored, created, e := s.feedbackStore.Create(r.Context(), f)
	if e != nil {
		status := 400
		if strings.Contains(e.Error(), "already exists") {
			status = 409
		}
		http.Error(w, e.Error(), status)
		return
	}
	f = *stored
	if !created {
		writeJSON(w, 200, f)
		return
	}
	if !s.cfg.Manage.Enabled || destination == "" {
		f.DeliveryStatus = "blocked"
		_ = s.feedbackStore.UpdateDelivery(r.Context(), f.ClientFeedbackID, "blocked", "manage disabled or unavailable")
		if fresh, e := s.feedbackStore.Get(r.Context(), f.ClientFeedbackID); e == nil && fresh != nil {
			f = *fresh
		}
		writeJSON(w, 201, f)
		return
	}
	s.tryDeliverFeedback(context.Background(), f)
	writeJSON(w, 201, f)
}

func (s *Server) syncFeedbackRecord(ctx context.Context, f store.Feedback) error {
	if s.feedbackStore == nil {
		return fmt.Errorf("feedback store unavailable")
	}
	if s.control == nil || !s.cfg.Manage.Enabled || f.NodeID != s.cfg.NodeID || f.Destination == "" || f.Destination != strings.TrimRight(strings.TrimSpace(s.cfg.Manage.URL), "/") {
		_ = s.feedbackStore.UpdateDelivery(ctx, f.ClientFeedbackID, "blocked", "destination or identity changed")
		return fmt.Errorf("feedback destination blocked")
	}
	c := manage.NewFeedbackClient(s.control)
	var err error
	if f.DeliveryStatus != "delivered" {
		err = c.Submit(ctx, f)
		if err != nil {
			_ = s.feedbackStore.UpdateDelivery(ctx, f.ClientFeedbackID, "pending", err.Error())
			return err
		}
		_ = s.feedbackStore.UpdateDelivery(ctx, f.ClientFeedbackID, "delivered", "")
	}
	out, e := c.Get(ctx, f.ClientFeedbackID)
	if e != nil {
		return e
	}
	return s.feedbackStore.ApplyRemote(ctx, f.ClientFeedbackID, out.Status, out.Reply, out.Revision)
}

func (s *Server) handleFeedbackTarget(w http.ResponseWriter, r *http.Request) {
	u := strings.TrimSpace(s.cfg.Manage.URL)
	configured := u != ""
	enabled := s.cfg.Manage.Enabled && configured
	if parsed, err := url.Parse(u); err == nil && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		u = strings.TrimRight(parsed.String(), "/")
	} else if configured {
		u = ""
		enabled = false
	}
	writeJSON(w, 200, map[string]any{"configured": configured, "enabled": enabled, "url": u, "node_id": s.cfg.NodeID})
}
func (s *Server) handleFeedbackList(w http.ResponseWriter, r *http.Request) {
	if s.feedbackStore == nil {
		http.Error(w, "feedback unavailable", http.StatusServiceUnavailable)
		return
	}
	v, e := s.feedbackStore.List(r.Context())
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) handleFeedbackGet(w http.ResponseWriter, r *http.Request) {
	if s.feedbackStore == nil {
		http.Error(w, "feedback unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("client_feedback_id")
	f, e := s.feedbackStore.Get(r.Context(), id)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	if f == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, 200, f)
}
func (s *Server) handleFeedbackSync(w http.ResponseWriter, r *http.Request) {
	if s.feedbackStore == nil {
		http.Error(w, "feedback unavailable", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("client_feedback_id")
	f, e := s.feedbackStore.Get(r.Context(), id)
	if e != nil || f == nil {
		http.NotFound(w, r)
		return
	}
	if s.control == nil {
		http.Error(w, "manage unavailable", http.StatusServiceUnavailable)
		return
	}
	if f.NodeID != s.cfg.NodeID || strings.TrimSpace(f.Destination) != strings.TrimSpace(s.cfg.Manage.URL) {
		http.Error(w, "feedback destination changed", http.StatusConflict)
		return
	}
	if !s.cfg.Manage.Enabled {
		http.Error(w, "manage disabled", http.StatusServiceUnavailable)
		return
	}
	if e := s.syncFeedbackRecord(r.Context(), *f); e != nil {
		http.Error(w, e.Error(), http.StatusBadGateway)
		return
	}
	fresh, e := s.feedbackStore.Get(r.Context(), id)
	if e != nil || fresh == nil {
		http.Error(w, "feedback unavailable", 500)
		return
	}
	writeJSON(w, 200, fresh)
}
func (s *Server) tryDeliverFeedback(ctx context.Context, f store.Feedback) {
	go func() { _ = s.syncFeedbackRecord(ctx, f) }()
}
