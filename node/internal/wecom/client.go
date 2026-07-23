// Package wecom 实现企业微信「消息推送」webhook（markdown_v2 / file）。
package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	maxMarkdownV2Bytes = 4096
	maxFileBytes       = 20 << 20 // 20 MiB
	defaultHTTPTimeout = 60 * time.Second
)

// Client 企业微信消息推送客户端（群机器人 webhook）。
type Client struct {
	apiBase    string
	webhookKey string
	http       *http.Client
}

// NewClientFromConfig 在 wecom.enabled 且密钥可用时创建客户端；否则返回 nil。
func NewClientFromConfig(cfg *config.Config) *Client {
	if cfg == nil || !cfg.WeComEnabled() {
		return nil
	}
	key := cfg.WeComWebhookKey()
	if key == "" {
		return nil
	}
	return NewClient(cfg.WeComAPIBase(), key, nil)
}

// NewClient 供测试或自定义 HTTP 客户端构造。
func NewClient(apiBase, webhookKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &Client{
		apiBase:    strings.TrimRight(strings.TrimSpace(apiBase), "/"),
		webhookKey: strings.TrimSpace(webhookKey),
		http:       httpClient,
	}
}

// Enabled 客户端是否可用。
func (c *Client) Enabled() bool {
	return c != nil && strings.TrimSpace(c.webhookKey) != ""
}

type apiResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Type    string `json:"type"`
	MediaID string `json:"media_id"`
}

// SendMarkdownV2 发送 markdown_v2 消息。
func (c *Client) SendMarkdownV2(ctx context.Context, content string) error {
	if !c.Enabled() {
		return fmt.Errorf("wecom not configured")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("content is required")
	}
	if len([]byte(content)) > maxMarkdownV2Bytes {
		return fmt.Errorf("content exceeds %d bytes (utf-8)", maxMarkdownV2Bytes)
	}
	body := map[string]any{
		"msgtype": "markdown_v2",
		"markdown_v2": map[string]any{
			"content": content,
		},
	}
	return c.sendJSON(ctx, body)
}

// SendFilePath 上传本地文件并以 file 消息发送；displayName 为空时用文件名。
func (c *Client) SendFilePath(ctx context.Context, absPath, displayName string) (mediaID string, err error) {
	if !c.Enabled() {
		return "", fmt.Errorf("wecom not configured")
	}
	absPath = strings.TrimSpace(absPath)
	if absPath == "" {
		return "", fmt.Errorf("path is required")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	if info.Size() <= 0 {
		return "", fmt.Errorf("file is empty")
	}
	if info.Size() > maxFileBytes {
		return "", fmt.Errorf("file exceeds 20MB limit (%d bytes)", info.Size())
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = filepath.Base(absPath)
	}
	if name == "" || name == "." || name == "/" {
		name = "file"
	}

	mediaID, err = c.uploadMedia(ctx, absPath, name)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"msgtype": "file",
		"file": map[string]any{
			"media_id": mediaID,
		},
	}
	if err := c.sendJSON(ctx, body); err != nil {
		return mediaID, err
	}
	return mediaID, nil
}

func (c *Client) sendURL() string {
	q := url.Values{}
	q.Set("key", c.webhookKey)
	return c.apiBase + "/cgi-bin/webhook/send?" + q.Encode()
}

func (c *Client) uploadURL() string {
	q := url.Values{}
	q.Set("key", c.webhookKey)
	q.Set("type", "file")
	return c.apiBase + "/cgi-bin/webhook/upload_media?" + q.Encode()
}

func (c *Client) sendJSON(ctx context.Context, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sendURL(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeAPIResponse(resp)
}

func (c *Client) uploadMedia(ctx context.Context, absPath, filename string) (string, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("media", filename)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.uploadURL(), &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out apiResponse
	if err := decodeAPIResponseInto(resp, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.MediaID) == "" {
		return "", fmt.Errorf("wecom upload missing media_id")
	}
	return out.MediaID, nil
}

func decodeAPIResponse(resp *http.Response) error {
	var out apiResponse
	return decodeAPIResponseInto(resp, &out)
}

func decodeAPIResponseInto(resp *http.Response, out *apiResponse) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("wecom decode response: %w (%s)", err, truncate(string(body), 200))
	}
	if out.ErrCode != 0 {
		msg := strings.TrimSpace(out.ErrMsg)
		if msg == "" {
			msg = "unknown"
		}
		return fmt.Errorf("wecom api errcode=%d errmsg=%s", out.ErrCode, msg)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
