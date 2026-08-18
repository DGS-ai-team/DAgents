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
	opened     int
	input      string
	request    TerminalRequest
	info       TerminalSessionInfo
	read       TerminalOutput
	terminated bool
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
func (b *fakeTerminalBroker) ReadOutput(_ context.Context, _, _ string, _ uint64, _ int) (TerminalOutput, error) {
	return b.read, nil
}
func (b *fakeTerminalBroker) Input(_ context.Context, _, _ string, data []byte) error {
	b.input = string(data)
	return nil
}
func (b *fakeTerminalBroker) Terminate(context.Context, string, string) (TerminalOutput, error) {
	b.terminated = true
	return TerminalOutput{Chunks: []TerminalOutputChunk{{Seq: 2, Data: []byte("stopped\r\n")}}, NextSeq: 2, Exited: true, Graceful: true}, nil
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
	if readPayload["output"] != "hello\r\n" || readPayload["next_seq"] != float64(1) {
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
	if terminatedPayload["output"] != "stopped\r\n" || terminatedPayload["graceful"] != true {
		t.Fatalf("terminated=%s", terminated)
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
