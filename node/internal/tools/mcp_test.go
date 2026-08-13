package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
)

func TestRegistryMCPToolsExposeCallPurposeAndStripIt(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetBuiltinEnabledNone()
	var received map[string]any
	if err := reg.SetMCPTools([]MCPTool{{
		Name:        "mcp__fake__echo",
		Description: "echo",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
		Call: func(_ context.Context, args json.RawMessage) (mcp.CallResult, error) {
			if err := json.Unmarshal(args, &received); err != nil {
				return mcp.CallResult{}, err
			}
			return mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: "ok"}}}, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	defs := reg.Definitions()
	if len(defs) != 1 || defs[0].Function.Name != "mcp__fake__echo" {
		t.Fatalf("unexpected defs: %#v", defs)
	}
	props, _ := defs[0].Function.Parameters["properties"].(map[string]any)
	if _, ok := props[CallPurposeKey]; !ok {
		t.Fatalf("call_purpose missing: %#v", defs[0].Function.Parameters)
	}
	output, err := reg.Execute(context.Background(), "mcp__fake__echo", `{"text":"hello","call_purpose":"test"}`)
	if err != nil || output != "ok" {
		t.Fatalf("unexpected execute: output=%q err=%v", output, err)
	}
	if _, ok := received[CallPurposeKey]; ok {
		t.Fatalf("internal call_purpose leaked: %#v", received)
	}
	if !strings.EqualFold(received["text"].(string), "hello") {
		t.Fatalf("unexpected args: %#v", received)
	}
}
