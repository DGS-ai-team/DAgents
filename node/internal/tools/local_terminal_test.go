package tools

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLocalTerminalProviderRunsNativePTY(t *testing.T) {
	shell := "sh"
	input := "printf 'local-pty-ready\\n'\nexit\n"
	if runtime.GOOS == "windows" {
		shell = "cmd"
		input = "echo local-pty-ready\r\nexit\r\n"
	}
	events := make(chan ProcessEvent, 16)
	provider := NewLocalShellProvider()
	terminal, err := provider.OpenTerminal(context.Background(), TerminalRequest{
		Target: ExecutionTarget{Kind: executionTargetLocal, ID: executionTargetLocal},
		Shell:  shell,
		Rows:   30,
		Cols:   100,
		EventSink: func(event ProcessEvent) {
			events <- event
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	output, err := terminal.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := terminal.Start(); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Resize(context.Background(), 40, 120); err != nil {
		t.Fatal(err)
	}
	outputDone := make(chan string, 1)
	outputReady := make(chan struct{}, 1)
	go func() {
		var builder strings.Builder
		buffer := make([]byte, 4096)
		readySent := false
		for {
			n, err := output.Read(buffer)
			if n > 0 {
				builder.Write(buffer[:n])
				if runtime.GOOS == "windows" && !readySent && strings.Contains(builder.String(), ">") {
					readySent = true
					outputReady <- struct{}{}
				}
				if strings.Contains(builder.String(), "local-pty-ready") {
					outputDone <- builder.String()
					return
				}
			}
			if err != nil {
				outputDone <- builder.String()
				return
			}
		}
	}()
	if runtime.GOOS == "windows" {
		select {
		case <-outputReady:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for local PTY prompt")
		}
	}
	if err := terminal.Input(context.Background(), []byte(input)); err != nil {
		t.Fatal(err)
	}
	var outputText string
	select {
	case outputText = <-outputDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for local PTY output")
	}
	if err := terminal.Wait(); err != nil {
		t.Fatalf("wait error: %v", err)
	}
	if err := terminal.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outputText, "local-pty-ready") {
		t.Fatalf("PTY output=%q", outputText)
	}
	if exit := terminal.ExitStatus(); exit == nil || exit.Code != 0 {
		t.Fatalf("exit=%+v", exit)
	}
	var sawStarted, sawOutput, sawExited bool
	var lastSeq uint64
	for len(events) > 0 {
		event := <-events
		if event.Seq <= lastSeq {
			t.Fatalf("event sequence is not monotonic: previous=%d current=%d", lastSeq, event.Seq)
		}
		lastSeq = event.Seq
		switch event.Type {
		case ProcessEventStarted:
			sawStarted = true
		case ProcessEventOutput:
			if event.Stream != "pty" {
				t.Fatalf("PTY output stream=%q", event.Stream)
			}
			sawOutput = true
		case ProcessEventExited:
			sawExited = true
		}
	}
	if !sawStarted || !sawOutput || !sawExited {
		t.Fatalf("lifecycle events started=%v output=%v exited=%v", sawStarted, sawOutput, sawExited)
	}
}
