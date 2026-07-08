// Package nodeclient 为 Shell 提供 Node REST/SSE 客户端（F-E1/E12）。
package nodeclient

import (
	"net/http"
	"os"
	"strings"
	"time"
)

const clientTokenEnv = "DAGENTS_CLIENT_TOKEN"

// Client 连接 local.endpoint 的 HTTP 客户端。
type Client struct {
	base       string
	token      string
	httpClient *http.Client
}

// New 构造 Node 客户端；base 为 config local.endpoint。
func New(base string) *Client {
	return &Client{
		base:       strings.TrimRight(strings.TrimSpace(base), "/"),
		token:      strings.TrimSpace(os.Getenv(clientTokenEnv)),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Base 返回 Node endpoint。
func (c *Client) Base() string {
	if c == nil {
		return ""
	}
	return c.base
}

func (c *Client) setAuth(req *http.Request) {
	if c == nil || req == nil || c.token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
}
