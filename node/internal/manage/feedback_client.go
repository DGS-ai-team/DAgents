package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type FeedbackClient struct {
	baseURL, nodeID, token string
	client                 *http.Client
}

func NewFeedbackClient(c *ControlClient) *FeedbackClient {
	if c == nil || c.cfg == nil {
		return &FeedbackClient{}
	}
	raw := strings.TrimRight(strings.TrimSpace(c.cfg.Manage.URL), "/")
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		raw = ""
	} else {
		raw = u.String()
	}
	return &FeedbackClient{baseURL: raw, nodeID: c.cfg.NodeID, token: c.cfg.Manage.NodeToken, client: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *FeedbackClient) request(ctx context.Context, method, path string, body any, out *store.Feedback) error {
	if c == nil || c.baseURL == "" || c.client == nil || strings.TrimSpace(c.nodeID) == "" {
		return fmt.Errorf("manage unavailable or invalid destination")
	}
	var rd io.Reader
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return e
		}
		rd = bytes.NewReader(raw)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if e != nil {
		return e
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set(agentIDHeader, c.nodeID)
	if c.token != "" {
		req.Header.Set(tokenHeader, c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := c.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("manage feedback status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if e = json.Unmarshal(raw, out); e != nil {
		return e
	}
	if out.NodeID != c.nodeID {
		return fmt.Errorf("manage feedback response node_id mismatch")
	}
	return nil
}

type feedbackSubmitPayload struct {
	ClientFeedbackID string `json:"client_feedback_id"`
	NodeID           string `json:"node_id"`
	Category         string `json:"category"`
	Title            string `json:"title"`
	Body             string `json:"body"`
	CreatedAt        string `json:"created_at,omitempty"`
}

func (c *FeedbackClient) Submit(ctx context.Context, f store.Feedback) error {
	if f.NodeID != "" && f.NodeID != c.nodeID {
		return fmt.Errorf("feedback node_id does not match client")
	}
	if f.Destination != "" && strings.TrimRight(strings.TrimSpace(f.Destination), "/") != c.baseURL {
		return fmt.Errorf("feedback destination does not match client")
	}
	p := feedbackSubmitPayload{f.ClientFeedbackID, c.nodeID, f.Category, f.Title, f.Body, f.CreatedAt}
	var out store.Feedback
	if e := c.request(ctx, http.MethodPost, "/v1/feedback", p, &out); e != nil {
		return e
	}
	if out.ClientFeedbackID != f.ClientFeedbackID {
		return fmt.Errorf("manage feedback response client_feedback_id mismatch")
	}
	return nil
}
func (c *FeedbackClient) Get(ctx context.Context, id string) (*store.Feedback, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("feedback id required")
	}
	q := url.Values{"node_id": []string{c.nodeID}}
	var out store.Feedback
	if e := c.request(ctx, http.MethodGet, "/v1/feedback/"+url.PathEscape(id)+"?"+q.Encode(), nil, &out); e != nil {
		return nil, e
	}
	if out.ClientFeedbackID != id {
		return nil, fmt.Errorf("manage feedback response client_feedback_id mismatch")
	}
	if out.Destination != "" && strings.TrimRight(strings.TrimSpace(out.Destination), "/") != c.baseURL {
		return nil, fmt.Errorf("manage feedback response destination mismatch")
	}
	return &out, nil
}
func (c *FeedbackClient) Sync(ctx context.Context, id string) (*store.Feedback, error) {
	return c.Get(ctx, id)
}
