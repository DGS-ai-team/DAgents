// Package webapi implements the independent DeepSeek Search and Fetch API client.
package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

var sharedHTTPClient = newHTTPClient()

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{Timeout: 35 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("web API redirect limit exceeded")
		}
		if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
			return fmt.Errorf("web API redirected to a different origin; authorization was withheld")
		}
		return nil
	}}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("web API base URL must be an http(s) URL with a host and no credentials, query, or fragment")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("web API key is required")
	}
	return &Client{
		baseURL: strings.TrimRight(u.String(), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		http:    sharedHTTPClient,
	}, nil
}

func (c *Client) Search(ctx context.Context, payload any, out any) error {
	return c.post(ctx, "tools/search", payload, out)
}

func (c *Client) Fetch(ctx context.Context, payload any, out any) error {
	return c.post(ctx, "tools/fetch", payload, out)
}

func (c *Client) post(ctx context.Context, endpoint string, payload, out any) error {
	if c == nil || c.http == nil {
		return fmt.Errorf("web API is unavailable")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode web API request: %w", err)
	}
	endpointURL, err := c.endpoint(endpoint)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("web API request cancelled: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create web API request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("web API request cancelled: %w", ctx.Err())
			}
			lastErr = fmt.Errorf("web API request failed: %s", redact(err.Error(), c.apiKey))
			if attempt < 2 && ctx.Err() == nil {
				if waitErr := waitRetry(ctx, attempt); waitErr != nil {
					return fmt.Errorf("web API request cancelled: %w", waitErr)
				}
				continue
			}
			return lastErr
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read web API response: %w", readErr)
		}
		if len(data) > maxResponseBytes {
			return fmt.Errorf("web API response exceeded %d bytes", maxResponseBytes)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode web API response: %w", err)
			}
			return nil
		}
		lastErr = providerError(resp.StatusCode, data, c.apiKey)
		if !retryable(resp.StatusCode) || attempt == 2 {
			return lastErr
		}
		if err := waitRetry(ctx, attempt); err != nil {
			return fmt.Errorf("web API request cancelled: %w", err)
		}
	}
	return lastErr
}

func (c *Client) endpoint(suffix string) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid web API base URL")
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/v1")
	u.Path = strings.TrimRight(path, "/") + "/v1/" + strings.TrimLeft(suffix, "/")
	u.RawPath = ""
	return u.String(), nil
}

func retryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func waitRetry(ctx context.Context, attempt int) error {
	d := time.Duration(attempt+1) * 250 * time.Millisecond
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func providerError(status int, body []byte, apiKey string) error {
	var parsed struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	detail := strings.TrimSpace(parsed.Message)
	if len(detail) == 0 && len(parsed.Error) > 0 && string(parsed.Error) != "null" {
		detail = string(parsed.Error)
	}
	if detail != "" {
		// Provider messages may echo user input or sensitive values; cap and strip controls.
		detail = strings.Map(func(r rune) rune {
			if r < 0x20 && r != '\t' {
				return -1
			}
			return r
		}, detail)
		detail = redact(detail, apiKey)
		if len(detail) > 300 {
			detail = detail[:300] + "…"
		}
		return fmt.Errorf("web API returned HTTP %d: %s", status, detail)
	}
	return fmt.Errorf("web API returned HTTP %d", status)
}

func redact(value, apiKey string) string {
	if apiKey == "" {
		return value
	}
	value = strings.ReplaceAll(value, apiKey, "[REDACTED]")
	value = strings.ReplaceAll(value, "Bearer "+apiKey, "Bearer [REDACTED]")
	return value
}
