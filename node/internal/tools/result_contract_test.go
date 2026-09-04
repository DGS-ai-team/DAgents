package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyResultUsesOneAuthoritativeStatus(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		content  string
		rejected bool
		status   ResultStatus
	}{
		{name: "empty success", tool: "bash_run", content: "[BASH_RESULT] status=SUCCEEDED", status: ResultStatusSucceeded},
		{name: "shell failure", tool: "bash_run", content: "[BASH_RESULT] status=ERROR\nerror=exit", status: ResultStatusFailed},
		{name: "async accepted", tool: "browser_run_task", content: `{"ok":true,"detail":{"status":"accepted"}}`, status: ResultStatusQueued},
		{name: "browser detail failure", tool: "browser_run_task", content: `{"ok":true,"detail":{"status":"failed"},"error":"step failed"}`, status: ResultStatusFailed},
		{name: "policy denial", tool: "write_file", content: "rejected: policy_denied", rejected: true, status: ResultStatusDenied},
		{name: "persisted policy denial", tool: "write_file", content: "rejected: policy_denied", status: ResultStatusDenied},
		{name: "execution error is not denial", tool: "terminal_command", content: "ERROR: connection refused", rejected: true, status: ResultStatusFailed},
		{name: "cancelled", tool: "terminal_read", content: "流式输出被用户中断。", status: ResultStatusCancelled},
		{name: "timeout", tool: "browser_run_task", content: `{"ok":true,"wait_timed_out":true,"error":"use status"}`, status: ResultStatusTimedOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyResult(tt.tool, tt.content, tt.rejected)
			if got.Status != tt.status {
				t.Fatalf("status=%q want %q metadata=%+v", got.Status, tt.status, got)
			}
		})
	}
}

func TestResultEventFieldsExposeAuthoritativeStatus(t *testing.T) {
	fields := ResultEventFields("bash_run", "ERROR: exit 1", true)
	if fields["status"] != string(ResultStatusFailed) {
		t.Fatalf("status=%v", fields["status"])
	}
	if _, ok := fields["rejected"]; ok {
		t.Fatalf("tool result must not expose duplicate rejected field: %+v", fields)
	}
	if _, ok := fields["error"].(map[string]any); !ok {
		t.Fatalf("missing structured error: %+v", fields)
	}

	denied := ResultEventFields("write_file", "rejected: policy_denied", true)
	if denied["status"] != string(ResultStatusDenied) {
		t.Fatalf("denial fields=%+v", denied)
	}
}

func TestToolDefinitionsKeepCommonResultProtocolOutOfEachDescription(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	defs := reg.Definitions()
	if len(defs) == 0 {
		t.Fatal("expected builtin tool definitions")
	}
	common := strings.TrimSpace(ResultProtocolPrompt())
	if common == "" {
		t.Fatal("common result contract must remain available to prompt builders")
	}
	for _, def := range defs {
		if strings.Contains(def.Function.Description, common) {
			t.Fatalf("tool %q repeats the common result protocol", def.Function.Name)
		}
	}

	var terminalRead string
	for _, def := range defs {
		if def.Function.Name == "terminal_read" {
			terminalRead = def.Function.Description
			break
		}
	}
	if terminalRead == "" || !strings.Contains(terminalRead, "output_empty") {
		t.Fatalf("tool-specific evidence hint missing: %q", terminalRead)
	}
}

func TestResultContractJSONErrorsRemainMachineReadable(t *testing.T) {
	fields := ResultEventFields("browser_run_task", `{"ok":false,"error":"no companion"}`, false)
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"status":"failed"`) || !strings.Contains(string(raw), `"retryable":false`) {
		t.Fatalf("fields=%s", raw)
	}
}

func TestOneShotCommandResultsExposeStructuredOutcome(t *testing.T) {
	params := shellRunParams{shellType: shellBash, compress: BashCompressConfig{}}
	bash, _ := formatShellCompletedOutput(params, "", "", &ExitStatus{Code: 0}, nil)
	for _, part := range []string{"[BASH_RESULT] exit=0", "status=SUCCEEDED", "target=local", "exit_code=0", "stdout_bytes=0", "stderr_bytes=0", "output_truncated: false", "--- STDOUT ---", "--- STDERR ---"} {
		if !strings.Contains(bash, part) {
			t.Fatalf("bash result missing %q: %s", part, bash)
		}
	}

}

func TestCommandToolDescriptionsExplainEmptyOutputAndOutcome(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	definitions := make(map[string]string)
	for _, definition := range reg.Definitions() {
		definitions[definition.Function.Name] = definition.Function.Description
	}
	for name, description := range map[string]string{
		"bash_run":         definitions["bash_run"],
		"terminal_command": definitions["terminal_command"],
	} {
		if !strings.Contains(description, "exit_code") || !strings.Contains(description, "stdout_bytes") || !strings.Contains(description, "stdout") {
			t.Fatalf("%s description does not explain structured outcome: %s", name, description)
		}
	}
	if description := definitions["terminal_read"]; !strings.Contains(description, "output_empty") || !strings.Contains(description, "不代表终端未执行") {
		t.Fatalf("terminal_read description does not explain empty output: %s", description)
	}
}
