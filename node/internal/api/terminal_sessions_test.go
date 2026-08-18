package api

import (
	"bytes"
	"context"
	"io"
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
	if !out.Graceful || out.Forced || !out.Exited {
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

func TestTerminalSessionSplitsLargeOutputFramesForCursorReads(t *testing.T) {
	terminal := newInterruptibleTestTerminal()
	session, err := newTerminalSession("agent-frame", terminal, nil, tools.TerminalRequest{}, false)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), terminalOutputFrameMax*2+100)
	if err := terminal.write(payload); err != nil {
		t.Fatal(err)
	}
	waitForTerminalFrames(t, session, 3)

	first := session.snapshotOutput(0, terminalOutputFrameMax, true)
	if len(first.Chunks) != 1 || len(first.Chunks[0].Data) != terminalOutputFrameMax || first.NextSeq != 1 {
		t.Fatalf("first bounded read=%+v", first)
	}
	second := session.snapshotOutput(first.NextSeq, terminalOutputFrameMax, true)
	if len(second.Chunks) != 1 || len(second.Chunks[0].Data) != terminalOutputFrameMax || second.NextSeq != 2 {
		t.Fatalf("second bounded read=%+v", second)
	}
	third := session.snapshotOutput(second.NextSeq, terminalOutputFrameMax, true)
	if len(third.Chunks) != 1 || len(third.Chunks[0].Data) != 100 || third.NextSeq != 3 {
		t.Fatalf("third bounded read=%+v", third)
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
