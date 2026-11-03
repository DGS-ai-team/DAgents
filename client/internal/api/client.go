// Package api contains the local Node endpoints used by the maintenance CLI.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client connects to the local Agent Node.
type Client struct {
	base       string
	httpClient *http.Client
}

// New constructs a Node API client; base must not have a trailing slash.
func New(base string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: strings.TrimRight(base, "/"), httpClient: httpClient}
}

// ValidateLocalNodeEndpoint restricts the maintenance CLI to its local Node.
// Node configuration may advertise a reachable address to Manage; that is
// intentionally separate from the endpoint accepted by this CLI.
func ValidateLocalNodeEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid local Node endpoint: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("local Node endpoint must be a plain http loopback URL")
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return fmt.Errorf("local Node endpoint has an invalid port")
		}
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("maintenance CLI only connects to a loopback Node; remote endpoints are not supported")
	}
	return nil
}

// AgentUpdateStatus is the response from GET /v1/node/update.
type AgentUpdateStatus struct {
	CurrentVersion    string         `json:"current_version"`
	LatestVersion     string         `json:"latest_version"`
	UpgradeAvailable  bool           `json:"upgrade_available"`
	ManageReachable   bool           `json:"manage_reachable"`
	PlatformSupported *bool          `json:"platform_supported,omitempty"`
	LastCheckedAt     string         `json:"last_checked_at,omitempty"`
	Channel           string         `json:"channel"`
	Platform          string         `json:"platform"`
	ReleaseNotes      string         `json:"release_notes,omitempty"`
	Message           string         `json:"message,omitempty"`
	ApplyCommand      string         `json:"apply_command"`
	Asset             map[string]any `json:"asset,omitempty"`
}

// GetAgentUpdate requests the available Node update metadata.
func (c *Client) GetAgentUpdate(ctx context.Context) (*AgentUpdateStatus, error) {
	var status AgentUpdateStatus
	if err := c.getJSON(ctx, "/v1/node/update", &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// GetNodeUpdatePackage requests Node's verified release package. The caller
// owns the response body and must close it.
func (c *Client) GetNodeUpdatePackage(ctx context.Context) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/node/update/package", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	client := *c.httpClient
	if client.Timeout < 15*time.Minute {
		client.Timeout = 15 * time.Minute
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}
