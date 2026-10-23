package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const defaultBrowserServiceTimeout = 60 * time.Second

// RemoteDriver 经 loopback HTTP 调用本机 Playwright sidecar。
type RemoteDriver struct {
	baseURL    string
	httpClient *http.Client
}

// NewRemoteDriver 构造 remote 驱动；启动前会 ping 服务。
func NewRemoteDriver(cfg *config.Config) (*RemoteDriver, error) {
	if cfg == nil || !cfg.BrowserEnabled() {
		return nil, fmt.Errorf("browser is disabled")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Browser.ServiceURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("browser.service_url is required")
	}
	d := &RemoteDriver{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: defaultBrowserServiceTimeout,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := d.Call(ctx, Request{Op: "ping"})
	if err != nil {
		return nil, fmt.Errorf("browser service ping: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("browser service ping failed: %s", resp.Error)
	}
	return d, nil
}

// Call 实现 Driver 接口。
func (d *RemoteDriver) Call(ctx context.Context, req Request) (Response, error) {
	if d == nil {
		return Response{OK: false, Error: "remote driver is nil"}, nil
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	path := "/v2/browser/call"
	method := http.MethodPost
	var reader io.Reader = bytes.NewReader(body)
	if req.Op == "ping" {
		path = "/v2/browser/ping"
		method = http.MethodGet
		reader = nil
	} else if req.Op == "evaluate" {
		path = "/v2/browser/evaluate"
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, reader)
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return Response{OK: false, Error: err.Error()}, nil
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, err
	}
	if httpResp.StatusCode >= 400 {
		return Response{OK: false, Error: fmt.Sprintf("browser service HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(string(raw)))}, nil
	}
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Response{}, fmt.Errorf("decode browser service response: %w", err)
	}
	return resp, nil
}

// Close remote 驱动无本地资源。
func (d *RemoteDriver) Close() error {
	return nil
}
