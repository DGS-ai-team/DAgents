package agentruntime

import (
	"encoding/json"
	"testing"
)

func TestCompanionBrowserAgentID(t *testing.T) {
	if got := CompanionBrowserAgentID("agt-abc"); got != "agt-abc-browser" {
		t.Fatalf("got %q", got)
	}
	if got := CompanionBrowserAgentID("agt-abc-browser"); got != "agt-abc-browser" {
		t.Fatalf("idempotent got %q", got)
	}
	if !IsCompanionBrowserAgentID("agt-x-browser") {
		t.Fatal("expected companion id")
	}
	if IsCompanionBrowserAgentID("agt-x") {
		t.Fatal("parent should not match")
	}
}

func TestSnapshotHasBrowserGroup(t *testing.T) {
	snap := Snapshot{Defaults: map[string]any{
		"tools": map[string]any{"enabled_groups": []any{"fs", "browser"}},
	}}
	if !SnapshotHasBrowserGroup(snap) {
		t.Fatal("expected browser group")
	}
	snap2 := Snapshot{Defaults: map[string]any{
		"tools": map[string]any{"enabled_groups": []any{"fs"}},
	}}
	if SnapshotHasBrowserGroup(snap2) {
		t.Fatal("unexpected browser group")
	}
}

func TestWithCompanionMetaRoundTrip(t *testing.T) {
	base := json.RawMessage(`{"template_id":"t","defaults":{"tools":{"enabled_groups":["browser"]}}}`)
	out, err := WithCompanionMeta(base, CompanionMeta{BrowserAgentID: "agt-1-browser"})
	if err != nil {
		t.Fatal(err)
	}
	meta := ParseCompanionMeta(out)
	if meta.BrowserAgentID != "agt-1-browser" {
		t.Fatalf("meta=%+v", meta)
	}
	comp, err := WithCompanionMeta(json.RawMessage(`{}`), CompanionMeta{
		Role: "browser", OwnerAgentID: "agt-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !IsBrowserCompanionRecord(comp) {
		t.Fatal("expected companion record")
	}
}
