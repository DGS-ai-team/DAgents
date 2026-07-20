package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// InboxTask 为 Manage GET /v1/a2a/inbox 返回的单条待处理任务。
type InboxTask struct {
	TaskID          string   `json:"task_id"`
	FromAgentID     string   `json:"from_agent_id"`
	Kind            string   `json:"kind"`
	Content         string   `json:"content"`
	BlobIDs         []string `json:"blob_ids"`
	CallerSessionID string   `json:"caller_session_id"`
	TraceID         string   `json:"trace_id"`
	CreatedAtUnix   int64    `json:"created_at_unix"`
	ExpiresAtUnix   int64    `json:"expires_at_unix"`
}

// InboxTaskHandler 在收到 inbox 任务时回调；返回 error 时 poller 记录日志并继续。
type InboxTaskHandler func(ctx context.Context, task InboxTask) error

// InboxPoller 对 Manage A2A inbox 做 long poll + 断线短 poll 降级。
type InboxPoller struct {
	cfg     *config.Config
	logger  *slog.Logger
	client  *http.Client
	handler InboxTaskHandler

	mu       sync.RWMutex
	running  bool
	failures int
}

// NewInboxPoller 构造 A2A inbox sidecar。
func NewInboxPoller(cfg *config.Config, logger *slog.Logger) *InboxPoller {
	if logger == nil {
		logger = slog.Default()
	}
	return &InboxPoller{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 90 * time.Second},
	}
}

// SetHandler 注入任务处理回调（通常为入队本地 session turn）。
func (p *InboxPoller) SetHandler(handler InboxTaskHandler) {
	p.handler = handler
}

// Start 启动后台 inbox 轮询；ctx 取消时退出。
func (p *InboxPoller) Start(ctx context.Context) {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()
	go p.run(ctx)
}

func (p *InboxPoller) run(ctx context.Context) {
	for {
		wait := p.cfg.ManageA2AInboxWait()
		if p.failures >= 3 {
			wait = 0
		}
		err := p.pollOnce(ctx, wait)
		if err != nil {
			p.failures++
			p.logger.Warn("a2a inbox poll failed", "error", err, "failures", p.failures)
		} else {
			p.failures = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.backoffInterval()):
		}
	}
}

func (p *InboxPoller) backoffInterval() time.Duration {
	if p.failures >= 3 {
		return p.cfg.ManageA2AInboxPollInterval()
	}
	return time.Second
}

func (p *InboxPoller) pollOnce(ctx context.Context, wait time.Duration) error {
	endpoint, err := p.inboxURL(wait)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set(agentIDHeader, p.cfg.NodeID)
	if token := strings.TrimSpace(p.cfg.Manage.NodeToken); token != "" {
		req.Header.Set(tokenHeader, token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("inbox status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	var body struct {
		Tasks []InboxTask `json:"tasks"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return err
	}
	handler := p.handler
	for _, task := range body.Tasks {
		if handler == nil {
			p.logger.Info("a2a inbox task received (no handler)", "task_id", task.TaskID, "from", task.FromAgentID)
			continue
		}
		if err := handler(ctx, task); err != nil {
			p.logger.Warn("a2a inbox task handler failed", "task_id", task.TaskID, "error", err)
		}
	}
	return nil
}

func (p *InboxPoller) inboxURL(wait time.Duration) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(p.cfg.Manage.URL), "/")
	if base == "" {
		return "", fmt.Errorf("manage.url is empty")
	}
	u, err := url.Parse(base + "/v1/a2a/inbox")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("agent_id", p.cfg.NodeID)
	q.Set("limit", "10")
	if wait > 0 {
		q.Set("wait", fmt.Sprintf("%.0f", wait.Seconds()))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
