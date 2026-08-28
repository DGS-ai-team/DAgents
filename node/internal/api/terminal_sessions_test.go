package api

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type interruptibleTestTerminal struct {
	mu       sync.Mutex
	reader   *io.PipeReader
	writer   *io.PipeWriter
	input    []byte
	exit     *tools.ExitStatus
	waitDone chan struct{}
	closeOne sync.Once
}

type commandTestTerminal struct {
	*interruptibleTestTerminal
}

func (t *commandTestTerminal) Input(ctx context.Context, data []byte) error {
	if bytes.Equal(data, []byte{0x03}) {
		return t.interruptibleTestTerminal.Input(ctx, data)
	}
	t.mu.Lock()
	t.input = append(t.input, data...)
	t.mu.Unlock()
	text := string(data)
	startAt := strings.Index(text, "__DAGENTS_COMMAND_START_")
	if startAt < 0 {
		return nil
	}
	tokenStart := startAt + len("__DAGENTS_COMMAND_START_")
	closing := strings.Index(text[tokenStart:], "__")
	if closing < 0 {
		return nil
	}
	startEnd := tokenStart + closing + 2
	start := text[startAt:startEnd]
	token := strings.TrimSuffix(strings.TrimPrefix(start, "__DAGENTS_COMMAND_START_"), "__")
	endPrefix := "__DAGENTS_COMMAND_END_" + token + "_"
	_, err := t.writer.Write([]byte(start + "\r\ncommand-output\r\n" + endPrefix + "0__\r\n"))
	return err
}

func newInterruptibleTestTerminal() *interruptibleTestTerminal {
	reader, writer := io.Pipe()
	return &interruptibleTestTerminal{reader: reader, writer: writer, waitDone: make(chan struct{})}
}

func (t *interruptibleTestTerminal) ID() string { return "terminal-session-test" }

func (t *interruptibleTestTerminal) Input(_ context.Context, data []byte) error {
	t.mu.Lock()
	t.input = append(t.input, data...)
	if bytes.Equal(data, []byte{0x03}) && t.exit == nil {
		t.exit = &tools.ExitStatus{Code: 130}
		close(t.waitDone)
	}
	t.mu.Unlock()
	if bytes.Equal(data, []byte{0x03}) {
		_, _ = t.writer.Write([]byte("interrupt acknowledged\r\n"))
		_ = t.writer.Close()
	}
	return nil
}

func (t *interruptibleTestTerminal) Output() (io.ReadCloser, error)         { return t.reader, nil }
func (t *interruptibleTestTerminal) Resize(context.Context, int, int) error { return nil }
func (t *interruptibleTestTerminal) Start() error                           { return nil }

func (t *interruptibleTestTerminal) Wait() error {
	<-t.waitDone
	return nil
}

func (t *interruptibleTestTerminal) ExitStatus() *tools.ExitStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.exit == nil {
		return nil
	}
	exit := *t.exit
	return &exit
}

func (t *interruptibleTestTerminal) Terminate(context.Context) error {
	t.mu.Lock()
	if t.exit == nil {
		t.exit = &tools.ExitStatus{Code: -1}
		close(t.waitDone)
	}
	t.mu.Unlock()
	return t.Close()
}

func (t *interruptibleTestTerminal) Close() error {
	t.mu.Lock()
	if t.exit == nil {
		t.exit = &tools.ExitStatus{Code: -1}
		close(t.waitDone)
	}
	t.mu.Unlock()
	t.closeOne.Do(func() {
		_ = t.writer.Close()
		_ = t.reader.Close()
	})
	return nil
}

func (t *interruptibleTestTerminal) write(data []byte) error {
	_, err := t.writer.Write(data)
	return err
}

func (t *interruptibleTestTerminal) inputBytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]byte(nil), t.input...)
}

func waitForTerminalFrames(t *testing.T, session *terminalSession, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		got := session.nextSeq
		session.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("terminal output was not collected")
}

func TestTerminalSessionRunCommandUsesExistingPTYAndConsumesTranscript(t *testing.T) {
	terminal := &commandTestTerminal{interruptibleTestTerminal: newInterruptibleTestTerminal()}
	session, err := newTerminalSession("agent-command", terminal, nil, tools.TerminalRequest{
		Target: tools.ExecutionTarget{Kind: "linux_channel", ID: "channel-test"}, Shell: "bash",
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	result, err := session.runCommand(context.Background(), tools.TerminalCommandRequest{
		TerminalID: "terminal-session-test", Command: "printf command-output", Timeout: time.Second, MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "SUCCEEDED" || result.ExitCode != 0 || result.Stdout != "command-output" {
		t.Fatalf("unexpected command result: %+v", result)
	}
	input := string(terminal.inputBytes())
	if !strings.Contains(input, "printf") || !strings.Contains(input, "__DAGENTS_COMMAND_START_") {
		t.Fatalf("command was not written to existing terminal input: %q", input)
	}
	read := session.snapshotOutput(0, terminalReplayBytes, true)
	if len(read.Chunks) != 0 {
		t.Fatalf("command transcript was returned a second time by terminal_read: %+v", read)
	}
	session.shutdown()
}

func TestParseTerminalCommandTranscriptDropsPTYPromptNoise(t *testing.T) {
	output, code, done, started := parseTerminalCommandTranscript([]byte(
		"__DAGENTS_COMMAND_START_token__\n"+
			"\x1b[?2004hdevuser@host:/$ \x1b[?2004l\r\r\n"+
			"\x1b[?2004h> \x1b[?2004l\r\r\n"+
			" 12:00:00 up 1 day, 1:02, 1 user, load average: 0.00, 0.00, 0.00\r\n"+
			"__DAGENTS_COMMAND_END_token_0__\n",
	), "__DAGENTS_COMMAND_START_token__", "__DAGENTS_COMMAND_END_token_")
	if !done || !started || code != 0 || !bytes.Contains(output, []byte("12:00:00 up 1 day")) || bytes.Contains(output, []byte("2004")) {
		t.Fatalf("unexpected parsed output=%q code=%d done=%v started=%v", output, code, done, started)
	}
}

func TestTerminalSessionTerminateInterruptsAndReturnsUnreadOutput(t *testing.T) {
	terminal := newInterruptibleTestTerminal()
	session, err := newTerminalSession("agent-test", terminal, nil, tools.TerminalRequest{}, false)
	if err != nil {
		t.Fatal(err)
	}

	if err := terminal.write([]byte("already read\r\n")); err != nil {
		t.Fatal(err)
	}
	waitForTerminalFrames(t, session, 1)
	read := session.snapshotOutput(0, terminalReplayBytes, true)
	if string(read.Chunks[0].Data) != "already read\r\n" {
		t.Fatalf("initial read=%q", read.Chunks[0].Data)
	}

	if err := terminal.write([]byte("remaining\r\n")); err != nil {
		t.Fatal(err)
	}
	out, err := session.terminate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !out.Graceful || out.Forced || !out.Exited || out.TerminationStatus != "confirmed" {
		t.Fatalf("termination status=%+v", out)
	}
	var output []byte
	for _, chunk := range out.Chunks {
		output = append(output, chunk.Data...)
	}
	if !bytes.Contains(output, []byte("remaining")) || bytes.Contains(output, []byte("already read")) {
		t.Fatalf("unread output=%q", output)
	}
	if !bytes.Equal(terminal.inputBytes(), []byte{0x03}) {
		t.Fatalf("input=%q, want Ctrl+C", terminal.inputBytes())
	}
	session.shutdown()
}

func TestTerminalSessionRegistryEnforcesAgentLimitAndIsNotPersistent(t *testing.T) {
	registry := newTerminalSessionRegistry(1)
	registry.setOpener(func(context.Context, string, tools.TerminalRequest) (tools.Terminal, error) {
		return newInterruptibleTestTerminal(), nil
	})
	first, err := registry.Open(context.Background(), "agent-limit", tools.TerminalRequest{ConfigID: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Open(context.Background(), "agent-limit", tools.TerminalRequest{ConfigID: "local"}); err == nil {
		t.Fatal("expected per-agent terminal limit")
	}
	if got := len(registry.List("agent-limit")); got != 1 {
		t.Fatalf("active sessions=%d", got)
	}
	if _, err := registry.Terminate(context.Background(), "agent-limit", first.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(registry.List("agent-limit")); got != 0 {
		t.Fatalf("sessions after terminate=%d", got)
	}

	// A new registry represents a Node restart; no session state is loaded.
	if got := len(newTerminalSessionRegistry(1).List("agent-limit")); got != 0 {
		t.Fatalf("sessions after restart=%d", got)
	}
}

func TestTerminalSessionRegistryReapsIdleSessions(t *testing.T) {
	if terminalIdleTimeout != 10*time.Minute {
		t.Fatalf("default idle timeout=%s", terminalIdleTimeout)
	}
	registry := newTerminalSessionRegistryWithOptions(2, 25*time.Millisecond)
	registry.setOpener(func(context.Context, string, tools.TerminalRequest) (tools.Terminal, error) {
		return newInterruptibleTestTerminal(), nil
	})
	if _, err := registry.Open(context.Background(), "agent-idle", tools.TerminalRequest{ConfigID: "local"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(registry.List("agent-idle")) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("idle terminal was not reaped")
}
