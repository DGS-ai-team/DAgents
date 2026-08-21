package tools

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestLocalShellProviderLifecycle(t *testing.T) {
	provider := NewLocalShellProvider()
	command := "printf provider-ok"
	if runtime.GOOS == "windows" {
		command = "Write-Output provider-ok"
	}

	process, err := provider.Start(context.Background(), ExecRequest{
		Target:  ExecutionTarget{Kind: executionTargetLocal},
		Command: command,
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	process.SetOutput(&stdout, &stderr)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("wait: %v; stderr=%q", err, stderr.String())
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "provider-ok") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if process.ID() == "" || process.ExitStatus() == nil {
		t.Fatalf("process lifecycle state missing: id=%q exit=%v", process.ID(), process.ExitStatus())
	}
}

func TestLocalShellProviderRejectsUnsupportedTarget(t *testing.T) {
	_, err := NewLocalShellProvider().Start(context.Background(), ExecRequest{
		Target:  ExecutionTarget{Kind: "ssh", ID: "server-1"},
		Command: "echo ignored",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support target") {
		t.Fatalf("err=%v", err)
	}
}

func TestLocalShellProviderTestTarget(t *testing.T) {
	status, err := NewLocalShellProvider().Test(context.Background(), ExecutionTarget{Kind: executionTargetLocal})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available {
		t.Fatalf("status=%+v", status)
	}
}

func TestLocalShellProviderEmitsSequencedLifecycleEvents(t *testing.T) {
	provider := NewLocalShellProvider()
	command := "printf event-output"
	if runtime.GOOS == "windows" {
		command = "Write-Output event-output"
	}
	var mu sync.Mutex
	var events []ProcessEvent
	process, err := provider.Start(context.Background(), ExecRequest{
		Target:  ExecutionTarget{Kind: executionTargetLocal},
		Command: command,
		Context: ExecutionContext{
			SessionID:     "session-events",
			ToolCallID:    "call-events",
			CommandDigest: executionCommandDigest(command),
		},
		EventSink: func(event ProcessEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	process.SetOutput(&stdout, &stderr)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("wait: %v; stderr=%q", err, stderr.String())
	}
	_ = process.Close()

	mu.Lock()
	got := append([]ProcessEvent(nil), events...)
	mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("events=%+v, want started/output/exited", got)
	}
	if got[0].Type != ProcessEventStarted {
		t.Fatalf("first event=%+v", got[0])
	}
	if got[len(got)-1].Type != ProcessEventExited {
		t.Fatalf("last event=%+v", got[len(got)-1])
	}
	var output string
	var outputBytes int64
	for i, event := range got {
		if event.Seq != uint64(i+1) {
			t.Fatalf("event seq=%d at index=%d, events=%+v", event.Seq, i, got)
		}
		if event.Type == ProcessEventOutput {
			if event.OutputBytes < outputBytes {
				t.Fatalf("output bytes regressed from %d to %d", outputBytes, event.OutputBytes)
			}
			outputBytes = event.OutputBytes
			if event.Stream == "stdout" {
				output += string(event.Data)
			}
			if event.Stream != "stdout" && event.Stream != "stderr" {
				t.Fatalf("output stream=%q", event.Stream)
			}
		}
		if event.Context.SessionID != "session-events" || event.Context.ToolCallID != "call-events" {
			t.Fatalf("event context=%+v", event.Context)
		}
	}
	if !strings.Contains(output, "event-output") {
		t.Fatalf("output events=%q, all events=%+v", output, got)
	}
	if outputBytes != int64(len(output)) {
		t.Fatalf("output bytes=%d, output length=%d, events=%+v", outputBytes, len(output), got)
	}
	if got[len(got)-1].OutputBytes != outputBytes {
		t.Fatalf("exit output bytes=%d, output bytes=%d", got[len(got)-1].OutputBytes, outputBytes)
	}
	if got[0].Context.CommandDigest == "" {
		t.Fatalf("command digest missing from event context: %+v", got[0].Context)
	}
	if got[len(got)-1].Exit == nil || got[len(got)-1].Exit.Code != 0 {
		t.Fatalf("exit event=%+v", got[len(got)-1])
	}
}

type recordingShellProvider struct {
	local *LocalShellProvider
	req   ExecRequest
}

func (p *recordingShellProvider) Start(ctx context.Context, req ExecRequest) (Process, error) {
	p.req = req
	return p.local.Start(ctx, req)
}

func (p *recordingShellProvider) Test(ctx context.Context, target ExecutionTarget) (TargetStatus, error) {
	return p.local.Test(ctx, target)
}

func TestBashRunUsesConfiguredShellProvider(t *testing.T) {
	dir := t.TempDir()
	registry, err := NewRegistry(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingShellProvider{local: NewLocalShellProvider()}
	if err := registry.WithShellProvider(recorder); err != nil {
		t.Fatal(err)
	}
	command := "printf provider-route"
	if runtime.GOOS == "windows" {
		command = "Write-Output provider-route"
	}
	out, err := registry.Execute(context.Background(), "bash_run", `{"command":"`+command+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "provider-route") {
		t.Fatalf("out=%q", out)
	}
	if recorder.req.Target.Kind != executionTargetLocal {
		t.Fatalf("target=%+v", recorder.req.Target)
	}
	if recorder.req.Command != command {
		t.Fatalf("command=%q want %q", recorder.req.Command, command)
	}
	if recorder.req.CWD != dir {
		t.Fatalf("cwd=%q want %q", recorder.req.CWD, dir)
	}
}

func TestRegistryOpenTerminalRoutesLocalTarget(t *testing.T) {
	registry, err := NewRegistry(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	shell := "sh"
	if runtime.GOOS == "windows" {
		shell = "cmd"
	}
	terminal, err := registry.OpenTerminal(context.Background(), TerminalRequest{
		Target: ExecutionTarget{Kind: executionTargetLocal},
		Shell:  shell,
		Rows:   24,
		Cols:   80,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	if _, err := terminal.Output(); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Start(); err != nil {
		t.Fatal(err)
	}
}
