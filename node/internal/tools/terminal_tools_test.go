package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeTerminalBroker struct {
	opened        int
	input         string
	request       TerminalRequest
	info          TerminalSessionInfo
	read          TerminalOutput
	command       TerminalCommandRequest
	commandResult TerminalCommandResult
	terminated    bool
}

type fakeTerminalConfigResolver struct {
	config TerminalConfigInfo
	err    error
}

func (r fakeTerminalConfigResolver) ListTerminalConfigs(context.Context, string) ([]TerminalConfigInfo, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []TerminalConfigInfo{r.config}, nil
}

func (r fakeTerminalConfigResolver) ResolveTerminalConfig(context.Context, string, string) (TerminalConfigInfo, error) {
	if r.err != nil {
		return TerminalConfigInfo{}, r.err
	}
	return r.config, nil
}

func TestResolveLinuxChannelIDUsesTerminalConfigBinding(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetAgentID("agent-config-test")
	reg.SetTerminalConfigResolver(fakeTerminalConfigResolver{config: TerminalConfigInfo{
		ConfigID:   TerminalConfigLinuxPrefix + "channel-prod",
		TargetKind: executionTargetLinuxChannel,
		TargetID:   "channel-prod",
	}})

	got, err := reg.resolveLinuxChannelID(context.Background(), TerminalConfigLinuxPrefix+"channel-prod")
	if err != nil || got != "channel-prod" {
		t.Fatalf("resolved channel=%q err=%v", got, err)
	}
	legacy, err := reg.resolveLinuxChannelID(context.Background(), "channel-legacy")
	if err != nil || legacy != "channel-legacy" {
		t.Fatalf("legacy channel=%q err=%v", legacy, err)
	}
}

func TestResolveLinuxChannelIDRejectsNonLinuxConfig(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetTerminalConfigResolver(fakeTerminalConfigResolver{config: TerminalConfigInfo{
		ConfigID:   TerminalConfigLinuxPrefix + "wrong-target",
		TargetKind: executionTargetLocal,
		TargetID:   executionTargetLocal,
	}})
	if _, err := reg.resolveLinuxChannelID(context.Background(), TerminalConfigLinuxPrefix+"wrong-target"); err == nil || !strings.Contains(err.Error(), "not a Linux channel") {
		t.Fatalf("expected non-Linux config rejection, got %v", err)
	}
}

func (b *fakeTerminalBroker) Open(_ context.Context, agentID string, request TerminalRequest) (TerminalSessionInfo, error) {
	b.opened++
	b.request = request
	b.info = TerminalSessionInfo{ID: "terminal-test-1", AgentID: agentID, TargetKind: "local", Status: "running", CreatedAt: time.Now().UTC()}
	return b.info, nil
}
func (b *fakeTerminalBroker) List(_ string) []TerminalSessionInfo {
	return []TerminalSessionInfo{b.info}
}
func (b *fakeTerminalBroker) Lookup(_ string, _ string) (TerminalSessionInfo, error) {
	if b.info.ID == "" {
		b.info = TerminalSessionInfo{ID: "terminal-test-1", AgentID: "agent-terminal-test", TargetKind: "local", Status: "running", CreatedAt: time.Now().UTC()}
	}
	return b.info, nil
}
func (b *fakeTerminalBroker) ReadOutput(_ context.Context, _, _ string, _ uint64, _ int) (TerminalOutput, error) {
	return b.read, nil
}
func (b *fakeTerminalBroker) Input(_ context.Context, _, _ string, data []byte) error {
	b.input = string(data)
	return nil
}
func (b *fakeTerminalBroker) RunCommand(_ context.Context, _, terminalID string, request TerminalCommandRequest) (TerminalCommandResult, error) {
	b.command = request
	if b.commandResult.TerminalID == "" {
		b.commandResult = TerminalCommandResult{
			Status: "SUCCEEDED", TerminalID: terminalID, TargetKind: request.Target.Kind,
			ExitCode: 0, Stdout: "terminal-command-ok\r\n", StdoutBytes: len("terminal-command-ok\r\n"),
		}
	}
	return b.commandResult, nil
}
func (b *fakeTerminalBroker) Terminate(context.Context, string, string) (TerminalOutput, error) {
	b.terminated = true
	return TerminalOutput{Chunks: []TerminalOutputChunk{{Seq: 2, Data: []byte("stopped\r\n")}}, NextSeq: 2, Exited: true, Graceful: true, TerminationStatus: "confirmed"}, nil
}

func TestTerminalToolsUseSharedSessionBroker(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetAgentID("agent-terminal-test")
	broker := &fakeTerminalBroker{read: TerminalOutput{
		Chunks:  []TerminalOutputChunk{{Seq: 1, Data: []byte("hello\r\n")}},
		NextSeq: 1,
	}}
	reg.SetTerminalSessionBroker(broker)

	configs, err := reg.Execute(context.Background(), "terminal_config_list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(configs, `"config_id":"local"`) {
		t.Fatalf("configs=%s", configs)
	}
	opened, err := reg.Execute(context.Background(), "terminal_open", `{"config_id":"local","shell":"powershell"}`)
	if err != nil {
		t.Fatal(err)
	}
	if broker.opened != 1 || !strings.Contains(opened, "terminal-test-1") {
		t.Fatalf("opened=%q broker=%+v", opened, broker)
	}
	if strings.Contains(configs, `"config_id":"local-wsl"`) {
		wslOpened, err := reg.Execute(context.Background(), "terminal_open", `{"config_id":"local-wsl"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(wslOpened, "terminal-test-1") || broker.request.Shell != "wsl" {
			t.Fatalf("wsl opened=%q request=%+v", wslOpened, broker.request)
		}
	}
	if _, err := reg.Execute(context.Background(), "terminal_input", `{"terminal_id":"terminal-test-1","data":"echo hi\n"}`); err != nil {
		t.Fatal(err)
	}
	if broker.input != "echo hi\n" {
		t.Fatalf("input=%q", broker.input)
	}
	read, err := reg.Execute(context.Background(), "terminal_read", `{"terminal_id":"terminal-test-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	var readPayload map[string]any
	if err := json.Unmarshal([]byte(read), &readPayload); err != nil {
		t.Fatal(err)
	}
	if readPayload["output"] != "hello\r\n" || readPayload["output_bytes"] != float64(len([]byte("hello\r\n"))) || readPayload["output_empty"] != false || readPayload["next_seq"] != float64(1) {
		t.Fatalf("read=%s", read)
	}
	terminated, err := reg.Execute(context.Background(), "terminal_terminate", `{"terminal_id":"terminal-test-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !broker.terminated {
		t.Fatal("expected terminate")
	}
	var terminatedPayload map[string]any
	if err := json.Unmarshal([]byte(terminated), &terminatedPayload); err != nil {
		t.Fatal(err)
	}
	if terminatedPayload["output"] != "stopped\r\n" || terminatedPayload["output_bytes"] != float64(len([]byte("stopped\r\n"))) || terminatedPayload["output_empty"] != false || terminatedPayload["graceful"] != true || terminatedPayload["termination_status"] != "confirmed" {
		t.Fatalf("terminated=%s", terminated)
	}
}

func TestTerminalInputDescriptionDocumentsJSONControls(t *testing.T) {
	definition := terminalInputToolDef()
	if !strings.Contains(definition.Function.Description, `JSON 字符串中的 \n 会解析为实际换行符`) {
		t.Fatalf("terminal_input description does not document JSON newline decoding: %q", definition.Function.Description)
	}
	properties, ok := definition.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("terminal_input properties are missing")
	}
	data, ok := properties["data"].(map[string]any)
	if !ok || !strings.Contains(data["description"].(string), `\u0003 表示 Ctrl+C`) {
		t.Fatalf("terminal_input data description does not document Ctrl+C: %#v", properties["data"])
	}
}

func TestTerminalReadWaitsBeforeSnapshot(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetAgentID("agent-terminal-wait-test")
	reg.SetTerminalSessionBroker(&fakeTerminalBroker{read: TerminalOutput{
		Chunks:  []TerminalOutputChunk{{Seq: 1, Data: []byte("done\r\n")}},
		NextSeq: 1,
	}})

	started := time.Now()
	if _, err := reg.Execute(context.Background(), "terminal_read", `{"terminal_id":"terminal-test-1","wait_seconds":1}`); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Fatalf("terminal_read returned before requested delay: %s", elapsed)
	}
}

func TestTerminalReadWaitCanBeCancelled(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetAgentID("agent-terminal-cancel-test")
	reg.SetTerminalSessionBroker(&fakeTerminalBroker{})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(25*time.Millisecond, cancel)
	_, err = reg.Execute(ctx, "terminal_read", `{"terminal_id":"terminal-test-1","wait_seconds":60}`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("terminal_read error=%v, want context.Canceled", err)
	}
}

func TestTerminalCommandRequiresExistingSessionAndReturnsStructuredResult(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetAgentID("agent-terminal-command-test")
	broker := &fakeTerminalBroker{info: TerminalSessionInfo{
		ID: "terminal-command-1", AgentID: "agent-terminal-command-test", TargetKind: executionTargetLocal, Status: "running",
	}}
	reg.SetTerminalSessionBroker(broker)
	result, err := reg.Execute(context.Background(), "terminal_command", `{"terminal_id":"terminal-command-1","command":"echo terminal-command-ok","timeout_ms":5000}`)
	if err != nil {
		t.Fatal(err)
	}
	var payload TerminalCommandResult
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "SUCCEEDED" || payload.ExitCode != 0 || !strings.Contains(strings.ToLower(payload.Stdout), "terminal-command-ok") {
		t.Fatalf("unexpected terminal command result: %+v", payload)
	}
	if broker.command.TerminalID != "terminal-command-1" || broker.command.Command != "echo terminal-command-ok" || broker.command.Timeout != 5*time.Second {
		t.Fatalf("command was not delegated to the shared session: %+v", broker.command)
	}
}

func TestNewRegistryDoesNotExposeDeprecatedLinuxTools(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range reg.Definitions() {
		switch def.Function.Name {
		case "linux_exec", "linux_file_upload", "linux_file_download":
			t.Fatalf("deprecated tool %q exposed without an old snapshot allowlist", def.Function.Name)
		}
	}
}
