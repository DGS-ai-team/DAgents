package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStreamableHTTPClientInitializeListPaginationAndCall(t *testing.T) {
	t.Setenv("DAGENTS_TENCENT_DOCS_TOKEN", "test-token")
	var requests []rpcRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "test-token" {
			t.Errorf("authorization header = %q", got)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests = append(requests, request)
		w.Header().Set("Mcp-Session-Id", "session-1")
		switch request.Method {
		case "initialize":
			writeHTTPRPC(w, request.ID, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "tools/list":
			params, _ := request.Params.(map[string]any)
			if params["cursor"] == nil {
				writeHTTPRPC(w, request.ID, map[string]any{
					"tools":      []any{map[string]any{"name": "first", "inputSchema": map[string]any{"type": "object"}}},
					"nextCursor": "page-2",
				})
			} else {
				if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
					t.Errorf("session header = %q", got)
				}
				writeHTTPRPC(w, request.ID, map[string]any{
					"tools": []any{map[string]any{"name": "second", "description": "second tool", "inputSchema": map[string]any{"type": "object"}}},
				})
			}
		case "tools/call":
			if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Errorf("call session header = %q", got)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":", request.ID)
			_, _ = fmt.Fprint(w, ",\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"remote ok\"}]}}\n\n")
		default:
			t.Errorf("unexpected method %q", request.Method)
		}
	}))
	defer server.Close()

	client, err := NewStreamableHTTPClient(ServerConfig{
		ID:         "tencent",
		Transport:  TransportStreamableHTTP,
		URL:        server.URL,
		HeaderRefs: map[string]string{"Authorization": "DAGENTS_TENCENT_DOCS_TOKEN"},
		Enabled:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "first" || tools[1].Name != "second" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	result, err := client.CallTool(ctx, "first", json.RawMessage(`{"value":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "remote ok" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(requests) != 5 || requests[0].Method != "initialize" || requests[1].Method != "notifications/initialized" {
		t.Fatalf("unexpected request sequence: %#v", requests)
	}
}

func TestStreamableHTTPClientRequiresHeaderEnvironment(t *testing.T) {
	_, err := NewStreamableHTTPClient(ServerConfig{
		ID:         "remote",
		Transport:  TransportStreamableHTTP,
		URL:        "https://example.com/mcp",
		HeaderRefs: map[string]string{"Authorization": "MISSING_DAGENTS_TOKEN"},
		Enabled:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "MISSING_DAGENTS_TOKEN") {
		t.Fatalf("expected missing header env error, got %v", err)
	}
}

func TestStreamableHTTPClientSupportsLiteralHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer plaintext" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		writeHTTPRPC(w, request.ID, map[string]any{"tools": []any{}})
	}))
	defer server.Close()
	client, err := NewStreamableHTTPClient(ServerConfig{
		ID: "literal", Transport: TransportStreamableHTTP, URL: server.URL,
		HeaderValues: map[string]string{"Authorization": "Bearer plaintext"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerUsesStreamableHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		switch request.Method {
		case "initialize":
			writeHTTPRPC(w, request.ID, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "tools/list":
			writeHTTPRPC(w, request.ID, map[string]any{"tools": []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}})
		case "tools/call":
			writeHTTPRPC(w, request.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "pong"}}})
		default:
			t.Errorf("unexpected method %q", request.Method)
		}
	}))
	defer server.Close()
	mgr := NewManager(nil)
	if err := mgr.Configure([]ServerConfig{{
		ID: "remote", Transport: TransportStreamableHTTP, URL: server.URL, EnabledTools: []string{"ping"}, Enabled: true,
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tools, err := mgr.EffectiveTools(ctx, []Binding{{ServerID: "remote", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].QualifiedName != "mcp__remote__ping" {
		t.Fatalf("unexpected effective tools: %#v", tools)
	}
	result, err := tools[0].Call(ctx, json.RawMessage(`{}`))
	if err != nil || len(result.Content) != 1 || result.Content[0].Text != "pong" {
		t.Fatalf("unexpected manager call: result=%#v err=%v", result, err)
	}
}

func TestManagerRefreshKeepsServiceToolAllowlist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch request.Method {
		case "initialize":
			writeHTTPRPC(w, request.ID, map[string]any{})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "tools/list":
			writeHTTPRPC(w, request.ID, map[string]any{"tools": []any{
				map[string]any{"name": "allowed", "inputSchema": map[string]any{"type": "object"}},
				map[string]any{"name": "hidden", "inputSchema": map[string]any{"type": "object"}},
			}})
		}
	}))
	defer server.Close()
	mgr := NewManager(nil)
	cfg := ServerConfig{ID: "remote", Transport: TransportStreamableHTTP, URL: server.URL, EnabledTools: []string{"allowed"}, Enabled: true}
	if err := mgr.Configure([]ServerConfig{cfg}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	view, err := mgr.Refresh(ctx, "remote")
	if err != nil {
		t.Fatal(err)
	}
	if view.ToolCount != 2 || view.EnabledToolCount != 1 {
		t.Fatalf("unexpected tool counts: %#v", view)
	}
	defs, err := mgr.EffectiveTools(ctx, []Binding{{ServerID: "remote", Enabled: true}})
	if err != nil || len(defs) != 1 || defs[0].RemoteName != "allowed" {
		t.Fatalf("unexpected effective tools: defs=%#v err=%v", defs, err)
	}
}

func TestTencentDocsStreamableHTTPIntegration(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("TENCENT_DOC_KEY"))
	if token == "" {
		t.Skip("set TENCENT_DOC_KEY to run the live Tencent Docs MCP probe")
	}
	client, err := NewStreamableHTTPClient(ServerConfig{
		ID: "tencent-docs", Transport: TransportStreamableHTTP,
		URL:        "https://docs.qq.com/openapi/mcp",
		HeaderRefs: map[string]string{"Authorization": "TENCENT_DOC_KEY"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) == 0 {
		t.Fatal("Tencent Docs MCP returned no tools")
	}
	names := make([]string, 0, len(tools))
	hasUserInfo := false
	for _, tool := range tools {
		names = append(names, tool.Name)
		if tool.Name == "get_user_info" {
			hasUserInfo = true
		}
	}
	t.Logf("Tencent Docs MCP tools=%d names=%s", len(names), strings.Join(names, ", "))
	if !hasUserInfo {
		t.Fatal("Tencent Docs MCP did not advertise the expected read-only get_user_info tool")
	}
	result, err := client.CallTool(ctx, "get_user_info", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Tencent Docs MCP read-only tool call failed: %v", err)
	}
	t.Logf("Tencent Docs MCP get_user_info is_error=%t content_blocks=%d", result.IsError, len(result.Content))
}

func writeHTTPRPC(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
