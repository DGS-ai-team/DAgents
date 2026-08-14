package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

const maxHTTPResponseBytes = 8 << 20

// StreamableHTTPClient implements the MCP Streamable HTTP transport. Each
// request is an independent POST; the optional Mcp-Session-Id returned by
// initialize is sent on subsequent requests.
type StreamableHTTPClient struct {
	cfg        ServerConfig
	httpClient *http.Client
	headers    http.Header

	startMu sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	session string
	started bool
	closed  bool
}

func NewStreamableHTTPClient(cfg ServerConfig) (*StreamableHTTPClient, error) {
	normalized, err := ValidateServerConfig(cfg)
	if err != nil {
		return nil, err
	}
	if normalized.Transport != TransportStreamableHTTP {
		return nil, fmt.Errorf("mcp server %q is not configured for streamable http", normalized.ID)
	}
	headers, err := resolveHeaders(normalized.HeaderRefs, normalized.HeaderValues)
	if err != nil {
		return nil, err
	}
	return &StreamableHTTPClient{
		cfg:        normalized,
		httpClient: http.DefaultClient,
		headers:    headers,
	}, nil
}

func (c *StreamableHTTPClient) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c.startMu.Lock()
	defer c.startMu.Unlock()
	c.mu.Lock()
	if c.started && !c.closed {
		c.mu.Unlock()
		return nil
	}
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("mcp http client is closed")
	}
	c.mu.Unlock()

	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "dagents-node",
			"version": "0.1",
		},
	}
	var result map[string]any
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return err
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return err
	}
	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	return nil
}

func (c *StreamableHTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	var all []Tool
	var cursor string
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &result); err != nil {
			return nil, err
		}
		all = append(all, result.Tools...)
		if strings.TrimSpace(result.NextCursor) == "" || result.NextCursor == cursor {
			break
		}
		cursor = result.NextCursor
	}
	return all, nil
}

func (c *StreamableHTTPClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (CallResult, error) {
	if err := c.Start(ctx); err != nil {
		return CallResult{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return CallResult{}, fmt.Errorf("mcp tool name is required")
	}
	args := map[string]any{}
	if len(arguments) > 0 && string(arguments) != "null" {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return CallResult{}, fmt.Errorf("mcp tool arguments must be an object: %w", err)
		}
	}
	var result CallResult
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return CallResult{}, err
	}
	return result, nil
}

func (c *StreamableHTTPClient) call(ctx context.Context, method string, params any, out any) error {
	response, err := c.request(ctx, method, params)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("mcp %s failed (%d): %s", method, response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 {
		return fmt.Errorf("mcp %s returned an empty result", method)
	}
	if err := json.Unmarshal(response.Result, out); err != nil {
		return fmt.Errorf("decode mcp %s result: %w", method, err)
	}
	return nil
}

func (c *StreamableHTTPClient) notify(ctx context.Context, method string, params any) error {
	_, err := c.post(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: params}, false)
	return err
}

func (c *StreamableHTTPClient) request(ctx context.Context, method string, params any) (rpcResponse, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return rpcResponse{}, fmt.Errorf("mcp http client is closed")
	}
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	return c.post(ctx, rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}, true)
}

func (c *StreamableHTTPClient) post(ctx context.Context, request rpcRequest, expectResponse bool) (rpcResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(request)
	if err != nil {
		return rpcResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, fmt.Errorf("create mcp http request: %w", err)
	}
	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.hasSession() {
		req.Header.Set("Mcp-Session-Id", c.sessionID())
		req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return rpcResponse{}, fmt.Errorf("mcp http %s request: %w", request.Method, err)
	}
	defer response.Body.Close()
	if session := response.Header.Get("Mcp-Session-Id"); session != "" {
		c.mu.Lock()
		c.session = session
		c.mu.Unlock()
	}
	if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusNoContent {
		if expectResponse {
			return rpcResponse{}, fmt.Errorf("mcp http %s returned status %d without a response", request.Method, response.StatusCode)
		}
		return rpcResponse{}, nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxHTTPResponseBytes+1))
	if readErr != nil {
		return rpcResponse{}, fmt.Errorf("read mcp http %s response: %w", request.Method, readErr)
	}
	if len(raw) > maxHTTPResponseBytes {
		return rpcResponse{}, fmt.Errorf("mcp http %s response exceeds %d bytes", request.Method, maxHTTPResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return rpcResponse{}, fmt.Errorf("mcp http %s returned status %d: %s", request.Method, response.StatusCode, compactHTTPError(raw))
	}
	if !expectResponse && len(bytes.TrimSpace(raw)) == 0 {
		return rpcResponse{}, nil
	}
	decoded, err := decodeHTTPRPCResponse(raw, response.Header.Get("Content-Type"))
	if err != nil {
		return rpcResponse{}, fmt.Errorf("decode mcp http %s response: %w", request.Method, err)
	}
	if expectResponse && len(decoded.ID) > 0 {
		wantID, _ := json.Marshal(request.ID)
		if string(decoded.ID) != string(wantID) {
			return rpcResponse{}, fmt.Errorf("mcp http %s response id mismatch", request.Method)
		}
	}
	return decoded, nil
}

func (c *StreamableHTTPClient) hasSession() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session != ""
}

func (c *StreamableHTTPClient) sessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

func (c *StreamableHTTPClient) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func decodeHTTPRPCResponse(raw []byte, contentType string) (rpcResponse, error) {
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(raw), &response); err == nil {
		return response, nil
	}
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") || bytes.Contains(raw, []byte("data:")) {
		return decodeSSEResponse(raw)
	}
	return rpcResponse{}, fmt.Errorf("response is neither JSON nor MCP SSE")
}

func decodeSSEResponse(raw []byte) (rpcResponse, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), maxHTTPResponseBytes)
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if response, ok := decodeSSEData(data); ok {
				return response, nil
			}
			data = nil
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return rpcResponse{}, err
	}
	if response, ok := decodeSSEData(data); ok {
		return response, nil
	}
	return rpcResponse{}, fmt.Errorf("MCP SSE response did not contain a JSON-RPC message")
}

func decodeSSEData(data []string) (rpcResponse, bool) {
	if len(data) == 0 {
		return rpcResponse{}, false
	}
	var response rpcResponse
	if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &response); err != nil {
		return rpcResponse{}, false
	}
	return response, true
}

func resolveHeaders(refs, literals map[string]string) (http.Header, error) {
	headers := make(http.Header)
	for headerName, value := range literals {
		headerName = strings.TrimSpace(headerName)
		if headerName == "" || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("mcp literal header %q is invalid", headerName)
		}
		if http.CanonicalHeaderKey(headerName) == "" {
			return nil, fmt.Errorf("mcp literal header name %q is invalid", headerName)
		}
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("mcp literal header %q cannot be empty", headerName)
		}
		headers.Set(headerName, value)
	}
	for headerName, envName := range refs {
		headerName = strings.TrimSpace(headerName)
		envName = strings.TrimSpace(envName)
		if headerName == "" || envName == "" {
			return nil, fmt.Errorf("mcp header_refs cannot contain empty names")
		}
		value, ok := os.LookupEnv(envName)
		if !ok {
			return nil, fmt.Errorf("mcp header environment variable %q is not set", envName)
		}
		headers.Set(headerName, value)
	}
	return headers, nil
}

func compactHTTPError(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 512 {
		text = text[:512] + "..."
	}
	return text
}
