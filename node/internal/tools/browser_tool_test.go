package tools

import (
	"encoding/json"
	"testing"
)

func TestDecodeBrowserActionsValidatesProtocol(t *testing.T) {
	actions, err := decodeBrowserActions(json.RawMessage(`{"actions":[{"op":"observe"},{"op":"click","target":{"role":"button","name":"Go"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[1].Target["role"] != "button" {
		t.Fatalf("actions = %#v", actions)
	}
	if _, err := decodeBrowserActions(json.RawMessage(`{"actions":[{"op":"run_task"}]}`)); err == nil {
		t.Fatal("expected unsupported action error")
	}
	if _, err := decodeBrowserActions(json.RawMessage(`{"actions":[{"op":"observe","timeout_ms":120001}]}`)); err == nil {
		t.Fatal("expected timeout limit error")
	}
}

func TestBrowserToolDefinitionsAreExactlyTwo(t *testing.T) {
	defs := (&Registry{}).browserToolDefs()
	if len(defs) != 2 {
		t.Fatalf("definitions = %d", len(defs))
	}
	if defs[0].Function.Name != "browser_call" || defs[1].Function.Name != "browser_evaluate" {
		t.Fatalf("definitions = %#v", defs)
	}
}
