package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// AutoEmployeeSummary is the deliberately small, privacy-preserving payload
// sent to Manage. It contains no prompt, transcript, path, or artifact body.
type AutoEmployeeSummary struct {
	AgentID             string          `json:"agent_id"`
	DisplayName         string          `json:"display_name,omitempty"`
	Role                string          `json:"role,omitempty"`
	WakeIntervalSeconds int64           `json:"wake_interval_seconds,omitempty"`
	TodoCounts          map[string]int  `json:"todo_counts,omitempty"`
	State               string          `json:"state"`
	Reason              string          `json:"reason,omitempty"`
	NextAt              *time.Time      `json:"next_at,omitempty"`
	ProfileRevision     int64           `json:"profile_revision,omitempty"`
	RuntimeRevision     int64           `json:"runtime_revision,omitempty"`
	AsOf                time.Time       `json:"as_of"`
	Dreaming            DreamingSummary `json:"dreaming"`
	// Deprecated in the wire contract; retained only for in-process callers
	// while old reporters are phased out.
	LastResult string         `json:"-"`
	Usage      map[string]any `json:"-"`
}

// DreamingSummary is a safe status-only projection for Manage. It carries no
// experience, handbook, Todo text, or failure detail.
type DreamingSummary struct {
	State       string     `json:"state"`
	NextAt      *time.Time `json:"next_at,omitempty"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
}

type AutoSummaryProvider func(context.Context) ([]AutoEmployeeSummary, error)

type AutoSummaryReporter struct {
	baseURL string
	nodeID  string
	token   string
	client  *http.Client
	provide AutoSummaryProvider
	mu      sync.Mutex
	cursor  int
}

func NewAutoSummaryReporter(rawURL, nodeID, token string, provide AutoSummaryProvider) (*AutoSummaryReporter, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid manage URL")
	}
	if strings.TrimSpace(nodeID) == "" || provide == nil {
		return nil, fmt.Errorf("node id and summary provider are required")
	}
	return &AutoSummaryReporter{baseURL: strings.TrimRight(u.String(), "/"), nodeID: strings.TrimSpace(nodeID), token: token, provide: provide, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (r *AutoSummaryReporter) Report(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("summary reporter unavailable")
	}
	summaries, err := r.provide(ctx)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		return nil
	}
	ordered := append([]AutoEmployeeSummary(nil), summaries...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].AgentID < ordered[j].AgentID })
	r.mu.Lock()
	start := r.cursor % len(ordered)
	count := len(ordered)
	if count > 64 {
		count = 64
	}
	batch := make([]AutoEmployeeSummary, 0, count)
	for n := 0; n < count; n++ {
		batch = append(batch, ordered[(start+n)%len(ordered)])
	}
	r.mu.Unlock()
	var firstErr error
	for i, summary := range batch {
		if err := ctx.Err(); err != nil {
			firstErr = err
			break
		}
		attemptCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := r.reportOne(attemptCtx, summary)
		cancel()
		r.mu.Lock()
		r.cursor = (start + i + 1) % len(ordered)
		r.mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *AutoSummaryReporter) reportOne(ctx context.Context, summary AutoEmployeeSummary) error {
	if strings.TrimSpace(summary.AgentID) == "" || strings.TrimSpace(summary.State) == "" || summary.AsOf.IsZero() {
		return fmt.Errorf("summary identity, state, and as_of are required")
	}
	if summary.AsOf.Location() == time.Local {
		summary.AsOf = summary.AsOf.UTC()
	}
	body, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	endpoint := r.baseURL + "/v1/registry/nodes/" + url.PathEscape(r.nodeID) + "/auto-summary"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-dagents-a2a-token", r.token)
	req.Header.Set("x-dagents-agent-id", r.nodeID)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return fmt.Errorf("summary report failed: http %d", resp.StatusCode)
	}
	if _, err := io.CopyN(io.Discard, resp.Body, 256*1024+1); err == nil {
		return fmt.Errorf("summary response too large")
	} else if err != io.EOF {
		return fmt.Errorf("read summary response: %w", err)
	}
	return nil
}
