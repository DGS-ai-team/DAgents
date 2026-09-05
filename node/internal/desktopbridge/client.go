// Package desktopbridge provides the Node-side client for the optional
// desktop Shell. The browser never talks to this bridge directly; Node is the
// only component that may request native desktop capabilities.
package desktopbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	APIURLEnv = "DAGENTS_DESKTOP_API_URL"
	TokenEnv  = "DAGENTS_DESKTOP_BRIDGE_TOKEN"
)

var ErrUnavailable = errors.New("desktop Shell is unavailable")

// Client is deliberately small: it transports platform requests and does
// not contain Agent or UI business logic.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewFromEnv() *Client {
	return New(os.Getenv(APIURLEnv), os.Getenv(TokenEnv))
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) Available(ctx context.Context) bool {
	if !c.Configured() {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	client := c.http
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) DirectoryPicker(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(ctx, http.MethodPost, "/v1/desktop/dialog/directory", map[string]any{}, &out)
	return out, err
}

func (c *Client) ClipboardFiles(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(ctx, http.MethodGet, "/v1/desktop/clipboard/files", nil, &out)
	return out, err
}

func (c *Client) UIFocus(ctx context.Context, payload map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(ctx, http.MethodPost, "/v1/desktop/ui/focus", payload, &out)
	return out, err
}

func (c *Client) ApplyUpdate(ctx context.Context, force bool) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(ctx, http.MethodPost, "/v1/desktop/update/apply", map[string]any{"force": force}, &out)
	return out, err
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	if !c.Configured() {
		return ErrUnavailable
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	client := c.http
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("desktop bridge request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var detail struct {
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&detail)
		message := strings.TrimSpace(detail.Message)
		if message == "" {
			message = strings.TrimSpace(detail.Error.Message)
		}
		if message == "" {
			message = resp.Status
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			return fmt.Errorf("%w: %s", ErrUnavailable, message)
		}
		return fmt.Errorf("desktop bridge: %s", message)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode desktop bridge response: %w", err)
	}
	return nil
}
