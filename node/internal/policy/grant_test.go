package policy

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGrantBoundsRealFileToolsAndDenyWins(t *testing.T) {
	root := t.TempDir()
	future := time.Now().Add(time.Hour)
	e := NewEngineFromMaps(Maps{Tools: map[string]ApprovalMode{"write_file": ModeDeny, "search_replace": ModeRule}, Grants: []Grant{{ID: "g", Tools: []string{"write_file", "search_replace"}, Workspace: root, ExpiresAt: future}}})
	if got := e.DecideTool("write_file", map[string]any{"path": filepath.Join(root, "ok.txt")}); got != ActionDeny { t.Fatalf("deny policy=%s", got) }
	if got := e.DecideTool("search_replace", map[string]any{"path": filepath.Join(root, "ok.txt")}); got != ActionAuto { t.Fatalf("grant=%s", got) }
	if got := e.DecideTool("search_replace", nil); got != ActionRequireApproval { t.Fatalf("missing path=%s", got) }
	if got := e.DecideTool("bash_run", map[string]any{"command": "cd / && rm -rf x"}); got != ActionRequireApproval { t.Fatalf("shell grant=%s", got) }
}

func TestGrantRejectsEscapeAndExpiry(t *testing.T) {
	root := t.TempDir()
	e := NewEngineFromMaps(Maps{Grants: []Grant{{ID: "g", Tools: []string{"write_file"}, Workspace: root, ExpiresAt: time.Now().Add(-time.Minute)}}})
	if got := e.DecideTool("write_file", map[string]any{"path": filepath.Join(root, "x")}); got != ActionRequireApproval { t.Fatalf("expired=%s", got) }
	e = NewEngineFromMaps(Maps{Grants: []Grant{{ID: "g", Tools: []string{"write_file"}, Workspace: root, ExpiresAt: time.Now().Add(time.Hour)}}})
	if got := e.DecideTool("write_file", map[string]any{"path": filepath.Join(root, "..", "escape")}); got != ActionRequireApproval { t.Fatalf("escape=%s", got) }
}
