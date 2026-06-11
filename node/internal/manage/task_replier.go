package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

type taskReplier struct {
	cfg    *config.Config
	logger *slog.Logger
	client *http.Client
}

func newTaskReplier(cfg *config.Config, logger *slog.Logger) *taskReplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &taskReplier{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (r *taskReplier) manageURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(r.cfg.Manage.URL), "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (r *taskReplier) doJSON(ctx context.Context, method, url string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(agentIDHeader, r.cfg.AgentID)
	if token := strings.TrimSpace(r.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	return r.client.Do(req)
}

func (r *taskReplier) ack(ctx context.Context, taskID string) error {
	resp, err := r.doJSON(ctx, http.MethodPost, r.manageURL("/v1/a2a/tasks/"+taskID+"/ack"), map[string]string{
		"agent_id": r.cfg.AgentID,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ack status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	return nil
}

func (r *taskReplier) reply(ctx context.Context, task InboxTask, status, resultText, calleeSessionID, errorDetail string) error {
	body := map[string]string{
		"agent_id":          r.cfg.AgentID,
		"status":            status,
		"result_text":       resultText,
		"callee_session_id": calleeSessionID,
		"error_detail":      errorDetail,
	}
	resp, err := r.doJSON(ctx, http.MethodPost, r.manageURL("/v1/a2a/tasks/"+task.TaskID+"/reply"), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("reply status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	return nil
}

func (r *taskReplier) replyRequiresInput(ctx context.Context, task InboxTask, resultText, calleeSessionID string) error {
	return r.reply(ctx, task, "requires_input", resultText, calleeSessionID, "")
}

func (r *taskReplier) pollCallerInput(ctx context.Context, taskID string, wait time.Duration) (map[string]any, error) {
	q := url.Values{}
	q.Set("agent_id", r.cfg.AgentID)
	if wait > 0 {
		q.Set("wait", fmt.Sprintf("%.0f", wait.Seconds()))
	}
	rawURL := r.manageURL("/v1/a2a/tasks/"+taskID+"/caller_input") + "?" + q.Encode()
	resp, err := r.doJSON(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("caller_input status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	var out struct {
		Ready       bool           `json:"ready"`
		ResumeValue map[string]any `json:"resume_value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.Ready {
		return nil, nil
	}
	return out.ResumeValue, nil
}
