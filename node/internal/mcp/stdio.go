package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type StdioClient struct {
	cfg ServerConfig

	mu        sync.Mutex
	writeMu   sync.Mutex
	nextID    uint64
	pending   map[string]chan rpcResponse
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
	started   bool
	closed    bool
}

func NewStdioClient(cfg ServerConfig) (*StdioClient, error) {
	normalized, err := ValidateServerConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &StdioClient{
		cfg:     normalized,
		pending: make(map[string]chan rpcResponse),
		done:    make(chan struct{}),
	}, nil
}

func (c *StdioClient) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.started && !c.closed {
		c.mu.Unlock()
		return nil
	}
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("mcp stdio client is closed")
	}
	processCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processCtx, c.cfg.Command, c.cfg.Args...)
	if c.cfg.CWD != "" {
		cmd.Dir = c.cfg.CWD
	}
	env, err := resolveEnvironment(c.cfg.EnvRefs, c.cfg.EnvValues)
	if err != nil {
		cancel()
		c.mu.Unlock()
		return err
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		c.mu.Unlock()
		return fmt.Errorf("create mcp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		c.mu.Unlock()
		return fmt.Errorf("create mcp stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		c.mu.Unlock()
		return fmt.Errorf("start mcp server %q: %w", c.cfg.ID, err)
	}
	c.cmd, c.stdin, c.stdout, c.cancel = cmd, stdin, stdout, cancel
	c.started = true
	c.mu.Unlock()

	go c.readLoop()
	go c.waitLoop(cmd)
	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

func (c *StdioClient) initialize(ctx context.Context) error {
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
	return c.notify("notifications/initialized", map[string]any{})
}

func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
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

func (c *StdioClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (CallResult, error) {
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

func (c *StdioClient) call(ctx context.Context, method string, params any, out any) error {
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

func (c *StdioClient) request(ctx context.Context, method string, params any) (rpcResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if !c.started || c.closed {
		c.mu.Unlock()
		return rpcResponse{}, fmt.Errorf("mcp stdio client is not running")
	}
	c.nextID++
	id := c.nextID
	key := strconv.FormatUint(id, 10)
	ch := make(chan rpcResponse, 1)
	c.pending[key] = ch
	c.mu.Unlock()

	request := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(request)
	if err != nil {
		c.removePending(key)
		return rpcResponse{}, err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	_, writeErr := c.stdin.Write(data)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.removePending(key)
		return rpcResponse{}, fmt.Errorf("write mcp %s request: %w", method, writeErr)
	}

	select {
	case response := <-ch:
		if len(response.ID) == 0 && response.Error == nil && len(response.Result) == 0 {
			return rpcResponse{}, c.processError()
		}
		return response, nil
	case <-ctx.Done():
		c.removePending(key)
		return rpcResponse{}, ctx.Err()
	case <-c.done:
		c.removePending(key)
		return rpcResponse{}, c.processError()
	}
}

func (c *StdioClient) notify(method string, params any) error {
	data, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.stdin == nil {
		return fmt.Errorf("mcp stdio client is not running")
	}
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("write mcp notification: %w", err)
	}
	return nil
}

func (c *StdioClient) readLoop() {
	reader := bufio.NewReader(c.stdout)
	for {
		line, err := reader.ReadBytes('\n')
		line = []byte(strings.TrimSpace(string(line)))
		if len(line) > 0 {
			var response rpcResponse
			if decodeErr := json.Unmarshal(line, &response); decodeErr != nil {
				c.finish(fmt.Errorf("invalid JSON from mcp server %q: %w", c.cfg.ID, decodeErr))
				return
			}
			if len(response.ID) > 0 {
				key := string(response.ID)
				c.mu.Lock()
				ch := c.pending[key]
				delete(c.pending, key)
				c.mu.Unlock()
				if ch != nil {
					select {
					case ch <- response:
					default:
					}
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.finish(fmt.Errorf("read mcp server %q: %w", c.cfg.ID, err))
			} else {
				c.finish(fmt.Errorf("mcp server %q exited", c.cfg.ID))
			}
			return
		}
	}
}

func (c *StdioClient) waitLoop(cmd *exec.Cmd) {
	err := cmd.Wait()
	if err != nil {
		c.finish(fmt.Errorf("mcp server %q exited: %w", c.cfg.ID, err))
		return
	}
	c.finish(fmt.Errorf("mcp server %q exited", c.cfg.ID))
}

func (c *StdioClient) finish(err error) {
	c.closeOnce.Do(func() {
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		c.mu.Lock()
		c.closed = true
		for key, ch := range c.pending {
			delete(c.pending, key)
			select {
			case ch <- rpcResponse{Error: &rpcError{Code: -32000, Message: err.Error()}}:
			default:
			}
		}
		c.mu.Unlock()
		if c.cancel != nil {
			c.cancel()
		}
		close(c.done)
	})
}

func (c *StdioClient) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *StdioClient) processError() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err != nil {
		return c.err
	}
	return fmt.Errorf("mcp stdio client stopped")
}

func (c *StdioClient) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.errMu.Lock()
		c.err = context.Canceled
		c.errMu.Unlock()
		c.mu.Lock()
		c.closed = true
		for key, ch := range c.pending {
			delete(c.pending, key)
			select {
			case ch <- rpcResponse{Error: &rpcError{Code: -32000, Message: context.Canceled.Error()}}:
			default:
			}
		}
		stdin, stdout, cancel := c.stdin, c.stdout, c.cancel
		cmd := c.cmd
		c.mu.Unlock()
		if stdin != nil {
			_ = stdin.Close()
		}
		if stdout != nil {
			_ = stdout.Close()
		}
		if cancel != nil {
			cancel()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		close(c.done)
	})
	return nil
}

func resolveEnvironment(refs, literals map[string]string) ([]string, error) {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	for name, value := range literals {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("mcp literal environment variable name cannot be empty")
		}
		values[name] = value
	}
	for childName, sourceName := range refs {
		value, ok := os.LookupEnv(strings.TrimSpace(sourceName))
		if !ok {
			return nil, fmt.Errorf("mcp environment variable %q is not set", sourceName)
		}
		values[strings.TrimSpace(childName)] = value
	}
	env := make([]string, 0, len(values))
	for name, value := range values {
		env = append(env, name+"="+value)
	}
	return env, nil
}
