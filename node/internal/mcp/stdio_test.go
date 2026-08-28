package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMCPStdioHelper(t *testing.T) {
	if os.Getenv("DAGENTS_MCP_HELPER") != "1" {
		return
	}
	decoder := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for decoder.Scan() {
		var request map[string]any
		if err := json.Unmarshal(decoder.Bytes(), &request); err != nil {
			continue
		}
		method, _ := request["method"].(string)
		id := request["id"]
		if id == nil {
			continue
		}
		var result any
		switch method {
		case "initialize":
			result = map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "fake"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "echo", "description": "Echo text", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
			}}}
		case "tools/call":
			params, _ := request["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			text, _ := args["text"].(string)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
		default:
			result = map[string]any{}
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
}

func TestMCPStdioDiagnosticsHelper(t *testing.T) {
	if os.Getenv("DAGENTS_MCP_DIAGNOSTICS_HELPER") != "1" {
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "MCP_DIAGNOSTICS_MARKER")
	os.Exit(17)
}

func fakeConfig(t *testing.T) ServerConfig {
	t.Helper()
	t.Setenv("DAGENTS_MCP_HELPER", "1")
	return ServerConfig{
		ID:           "fake",
		Command:      os.Args[0],
		Args:         []string{"-test.run=TestMCPStdioHelper", "--"},
		EnvRefs:      map[string]string{"DAGENTS_MCP_HELPER": "DAGENTS_MCP_HELPER"},
		EnabledTools: []string{"echo"},
		Enabled:      true,
	}
}

func TestStdioClientInitializeListAndCall(t *testing.T) {
	client, err := NewStdioClient(fakeConfig(t))
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
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	result, err := client.CallTool(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestStdioClientCapturesBoundedDiagnostics(t *testing.T) {
	client, err := NewStdioClient(ServerConfig{
		ID:      "diagnostics",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPStdioDiagnosticsHelper", "--"},
		EnvRefs: map[string]string{"DAGENTS_MCP_DIAGNOSTICS_HELPER": "DAGENTS_MCP_DIAGNOSTICS_HELPER"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	t.Setenv("DAGENTS_MCP_DIAGNOSTICS_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Start(ctx); err == nil {
		t.Fatal("expected stdio process failure")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	var diagnostics ClientDiagnostics
	for time.Now().Before(deadline) {
		diagnostics = client.Diagnostics()
		if diagnostics.ExitCode != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(diagnostics.Stderr, "MCP_DIAGNOSTICS_MARKER") {
		t.Fatalf("stderr=%q", diagnostics.Stderr)
	}
	if diagnostics.ExitCode == nil || *diagnostics.ExitCode != 17 {
		t.Fatalf("exit code=%v", diagnostics.ExitCode)
	}
}

func TestManagerEffectiveToolsNamespacedAndAllowlisted(t *testing.T) {
	mgr := NewManager(nil)
	if err := mgr.Configure([]ServerConfig{fakeConfig(t)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defs, err := mgr.EffectiveTools(ctx, []Binding{{ServerID: "fake", Enabled: true, ToolAllowlist: []string{"echo"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].QualifiedName != "mcp__fake__echo" {
		t.Fatalf("unexpected effective tools: %#v", defs)
	}
	result, err := defs[0].Call(ctx, json.RawMessage(`{"text":"ok"}`))
	if err != nil || len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("call failed: result=%#v err=%v", result, err)
	}
}

func TestManagerServiceToolAllowlistIsFailClosed(t *testing.T) {
	mgr := NewManager(nil)
	cfg := fakeConfig(t)
	cfg.EnabledTools = nil
	if err := mgr.Configure([]ServerConfig{cfg}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defs, err := mgr.EffectiveTools(ctx, []Binding{{ServerID: "fake", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected no tools before service enablement, got %#v", defs)
	}
	cfg.EnabledTools = []string{"echo"}
	if err := mgr.Configure([]ServerConfig{cfg}); err != nil {
		t.Fatal(err)
	}
	defs, err = mgr.EffectiveTools(ctx, []Binding{{ServerID: "fake", Enabled: true}})
	if err != nil || len(defs) != 1 || defs[0].RemoteName != "echo" {
		t.Fatalf("expected enabled echo tool, defs=%#v err=%v", defs, err)
	}
}

func TestManagerCallRejectsDisabledToolBeforeConnecting(t *testing.T) {
	mgr := NewManager(nil)
	cfg := fakeConfig(t)
	cfg.EnabledTools = nil
	if err := mgr.Configure([]ServerConfig{cfg}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := mgr.Call(ctx, "fake", "echo", json.RawMessage(`{"text":"should-not-run"}`)); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled-tool error before connect, got %v", err)
	}
}

func TestValidateServerConfigRejectsRawUnsupportedTransport(t *testing.T) {
	_, err := ValidateServerConfig(ServerConfig{ID: "x", Transport: "ftp", Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "unsupported mcp transport") {
		t.Fatalf("expected transport error, got %v", err)
	}
}

func TestValidateServerConfigInfersRemoteTransportFromURL(t *testing.T) {
	cfg, err := ValidateServerConfig(ServerConfig{ID: "remote", URL: "https://example.com/mcp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport != TransportStreamableHTTP {
		t.Fatalf("expected inferred streamable http transport, got %q", cfg.Transport)
	}
}

func TestQualifiedToolNameRejectsUnsafeNames(t *testing.T) {
	if _, err := QualifiedToolName("server", "has/slash"); err == nil {
		t.Fatal("expected unsafe tool name error")
	}
	if _, err := QualifiedToolName("server", ""); err == nil {
		t.Fatal("expected empty tool name error")
	}
}

func TestQualifiedToolNameTruncatesLongNamesWithStableHash(t *testing.T) {
	remote := fmt.Sprintf("%060s", "tool")
	got, err := QualifiedToolName("server", remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxLLMToolNameLength {
		t.Fatalf("qualified name length = %d, name=%q", len(got), got)
	}
	if got[:len("mcp__server__")] != "mcp__server__" {
		t.Fatalf("qualified name lost readable prefix: %q", got)
	}
	want, err := QualifiedToolName("server", remote)
	if err != nil || got != want {
		t.Fatalf("qualified name is not stable: got=%q want=%q err=%v", got, want, err)
	}
	other, err := QualifiedToolName("server", remote+"x")
	if err != nil || got == other {
		t.Fatalf("different remote names should not share long alias: got=%q other=%q err=%v", got, other, err)
	}
}

func TestQualifiedToolNameNormalizesDotsForLLMProviders(t *testing.T) {
	got, err := QualifiedToolName("tencent-docs", "doc.get")
	if err != nil {
		t.Fatal(err)
	}
	if got != "mcp__tencent-docs__doc_get" {
		t.Fatalf("qualified name = %q", got)
	}
}

func TestResolveEnvironmentScrubsInheritedSecrets(t *testing.T) {
	t.Setenv("MCP_TEST_SECRET", "must-not-inherit")
	t.Setenv("MCP_TEST_PATH", "ordinary-value")
	t.Setenv("MCP_TEST_REF", "explicit-ref-value")

	env, err := resolveEnvironment(
		map[string]string{"EXPLICIT_REF": "MCP_TEST_REF"},
		map[string]string{"EXPLICIT_LITERAL": "literal-value"},
	)
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string, len(env))
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid child environment entry %q", entry)
		}
		values[parts[0]] = parts[1]
	}
	if _, ok := values["MCP_TEST_SECRET"]; ok {
		t.Fatalf("inherited secret was not scrubbed: %#v", values)
	}
	if values["MCP_TEST_PATH"] != "ordinary-value" {
		t.Fatalf("ordinary inherited variable missing: %#v", values)
	}
	if values["EXPLICIT_REF"] != "explicit-ref-value" || values["EXPLICIT_LITERAL"] != "literal-value" {
		t.Fatalf("explicit MCP environment values missing: %#v", values)
	}

	for i := 1; i < len(env); i++ {
		if strings.SplitN(env[i-1], "=", 2)[0] > strings.SplitN(env[i], "=", 2)[0] {
			t.Fatalf("child environment names are not deterministic: %#v", env)
		}
	}
}

func TestResolveEnvironmentRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		refs map[string]string
		vals map[string]string
	}{
		{name: "empty literal name", vals: map[string]string{" ": "value"}},
		{name: "equals in literal name", vals: map[string]string{"BAD=NAME": "value"}},
		{name: "nul in literal value", vals: map[string]string{"BAD": "a\x00b"}},
		{name: "empty reference name", refs: map[string]string{"CHILD": " "}},
		{name: "nul in reference name", refs: map[string]string{"CHILD\x00": "SOURCE"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveEnvironment(tt.refs, tt.vals); err == nil {
				t.Fatal("expected invalid environment configuration error")
			}
		})
	}
}
