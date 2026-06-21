package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestExternalHooksConfigFromShared(t *testing.T) {
	enabled := true
	cfg := config.HooksConfig{
		Enabled: &enabled,
		Entries: []config.HookEntryConfig{
			{
				Name:   "audit",
				Type:   "journal",
				Phases: []string{"turn.done", "invalid.phase"},
			},
			{
				Name:    "remote",
				Type:    "http",
				Phases:  []string{"tool.before_each"},
				URL:     "http://127.0.0.1:1/hooks",
				OnError: "abort",
			},
		},
	}
	out := ExternalHooksConfigFromShared(cfg, "/runtime")
	if !out.Enabled {
		t.Fatal("expected enabled")
	}
	if len(out.Entries) != 2 {
		t.Fatalf("entries = %d", len(out.Entries))
	}
	if out.Entries[0].Type != "journal" || len(out.Entries[0].Phases) != 1 {
		t.Fatalf("journal entry = %+v", out.Entries[0])
	}
	if out.Entries[1].OnError != OnErrorAbort {
		t.Fatalf("on_error = %q", out.Entries[1].OnError)
	}
}

func TestRegisterExternalEntries_journal(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(nil, RuntimeConfig{
		Duplicate: DefaultDuplicateConfig(),
		External: ExternalHooksConfig{
			RuntimeDir: dir,
			Entries: []ExternalHookEntry{
				{
					Name:   "audit-jsonl",
					Type:   "journal",
					Phases: []Phase{PhaseTurnDone},
				},
			},
		},
	})
	if len(reg.phaseHooksFor(PhaseTurnDone)) != 1 {
		t.Fatalf("hooks = %d", len(reg.phaseHooksFor(PhaseTurnDone)))
	}
	hc := BuildTurnDoneContext("sess-j", "agent-1", "stop")
	if _, err := reg.RunPhase(context.Background(), PhaseTurnDone, hc); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "logs", "hooks.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "audit-jsonl") || !strings.Contains(string(raw), "sess-j") {
		t.Fatalf("journal = %s", raw)
	}
}

func TestRegisterExternalEntries_commandRequiresEnabled(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(nil, RuntimeConfig{
		Duplicate: DefaultDuplicateConfig(),
		External: ExternalHooksConfig{
			Enabled:    false,
			RuntimeDir: dir,
			Entries: []ExternalHookEntry{
				{
					Name:         "cmd",
					Type:         "command",
					Phases:       []Phase{PhaseTurnDone},
					Command:      []string{script},
					AllowedPaths: []string{dir},
				},
			},
		},
	})
	if len(reg.phaseHooksFor(PhaseTurnDone)) != 0 {
		t.Fatal("command hook should not register when disabled")
	}
}

func TestRegisterExternalEntries_commandHook(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "mutate.sh")
	body := `#!/bin/sh
cat > /dev/null
echo '{"mutations":{"system_prompt":"from-command"}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig(), External: ExternalHooksConfig{
		Enabled: true,
		Entries: []ExternalHookEntry{{
			Name: "cmd", Type: "command", Phases: []Phase{PhasePromptBuild},
			Command: []string{script}, AllowedPaths: []string{dir},
		}},
	}})
	hc := BuildPromptBuildContext("s1", "a1", "base")
	out, err := reg.RunPhase(context.Background(), PhasePromptBuild, hc)
	if err != nil {
		t.Fatal(err)
	}
	if SystemPromptFrom(out, "") != "from-command" {
		t.Fatalf("prompt = %q", SystemPromptFrom(out, ""))
	}
}

func TestRegisterExternalEntries_httpHook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mutations": map[string]any{"metadata": map[string]any{"seen": true}},
		})
	}))
	defer srv.Close()

	entry := ExternalHooksConfigFromShared(config.HooksConfig{
		Enabled: ptrBool(true),
		Entries: []config.HookEntryConfig{{
			Name: "remote", Type: "http", Phases: []string{"turn.done"}, URL: srv.URL,
		}},
	}, ".")
	reg := NewRegistry(nil, RuntimeConfig{Duplicate: DefaultDuplicateConfig(), External: entry})
	hc := BuildTurnDoneContext("s", "a", "stop")
	out, err := reg.RunPhase(context.Background(), PhaseTurnDone, hc)
	if err != nil {
		t.Fatal(err)
	}
	if out.Metadata == nil || out.Metadata["seen"] != true {
		t.Fatalf("metadata = %+v", out.Metadata)
	}
}

func ptrBool(v bool) *bool { return &v }

func TestCommandPathAllowed(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "hook.sh")
	if !commandPathAllowed(exe, []string{dir}) {
		t.Fatal("expected allowed under root")
	}
	if commandPathAllowed("/usr/bin/bash", []string{dir}) {
		t.Fatal("expected denied outside root")
	}
}

func TestRedactionScript_example(t *testing.T) {
	script := filepath.Join("..", "..", "..", "packaging", "runtime", "hooks", "redaction.sh")
	if _, err := os.Stat(script); err != nil {
		t.Skip("packaging script not present:", err)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "redaction.sh")
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, raw, 0o755); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry(nil, RuntimeConfig{
		Duplicate: DefaultDuplicateConfig(),
		External: ExternalHooksConfig{
			Enabled: true,
			Entries: []ExternalHookEntry{{
				Name: "llm-redact", Type: "command", Phases: []Phase{PhaseLLMAfterCall},
				Command: []string{dest}, AllowedPaths: []string{dir},
			}},
		},
	})
	hc := BuildLLMAfterCallContext("s1", "a1", LLMAfterCallInput{
		AssistantContent: "token sk-abcdefghijklmnopqrstuvwxyz1234567890 ok",
		FinishReason:     "stop",
	})
	out, err := reg.RunPhase(context.Background(), PhaseLLMAfterCall, hc)
	if err != nil {
		t.Fatal(err)
	}
	merged := ApplyLLMAfterCallToResult(out, LLMAfterCallInput{
		AssistantContent: "token sk-abcdefghijklmnopqrstuvwxyz1234567890 ok",
	})
	if strings.Contains(merged.AssistantContent, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("expected redacted content, got %q", merged.AssistantContent)
	}
	if !strings.Contains(merged.AssistantContent, "sk-[REDACTED]") {
		t.Fatalf("expected sk-[REDACTED], got %q", merged.AssistantContent)
	}
}
