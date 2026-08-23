package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type diagnosticTestClient struct {
	diagnostics ClientDiagnostics
}

func (c diagnosticTestClient) Start(context.Context) error { return nil }
func (c diagnosticTestClient) ListTools(context.Context) ([]Tool, error) {
	return []Tool{{Name: "echo"}}, nil
}
func (c diagnosticTestClient) CallTool(context.Context, string, json.RawMessage) (CallResult, error) {
	return CallResult{}, errors.New("child transport exited")
}
func (c diagnosticTestClient) Close() error                   { return nil }
func (c diagnosticTestClient) Diagnostics() ClientDiagnostics { return c.diagnostics }

func TestClassifyClientOperationErrorPreservesStderrAndExitCode(t *testing.T) {
	exitCode := 17
	err := classifyClientOperationError("call", errors.New("request failed"), diagnosticTestClient{
		diagnostics: ClientDiagnostics{Stderr: "npm ERR! postinstall failed", ExitCode: &exitCode},
	})
	var operation *operationError
	if !errors.As(err, &operation) {
		t.Fatalf("expected operation error, got %T", err)
	}
	if operation.Stderr != "npm ERR! postinstall failed" || operation.ExitCode == nil || *operation.ExitCode != exitCode {
		t.Fatalf("diagnostics=%+v", operation)
	}
	if operation.FailureKind != "installation" || operation.Retryable {
		t.Fatalf("classification=%+v", operation)
	}
}

func TestManagerHealthAggregateAndStatusEvents(t *testing.T) {
	manager := NewManager(nil)
	var events []StatusEvent
	manager.SetStatusListener(func(event StatusEvent) {
		events = append(events, event)
	})
	if err := manager.Configure([]ServerConfig{
		{ID: "healthy", Transport: TransportStdio, Command: "healthy", Enabled: true},
		{ID: "disabled", Transport: TransportStdio, Command: "disabled", Enabled: false},
	}); err != nil {
		t.Fatal(err)
	}
	health := manager.Health()
	if health.Status != HealthDegraded || health.EnabledCount != 1 || health.ProblemCount != 1 {
		t.Fatalf("initial health=%+v", health)
	}

	manager.updateView("healthy", StatusChecking, "", nil)
	if got := manager.Health().Status; got != HealthChecking {
		t.Fatalf("checking health=%q", got)
	}
	manager.updateView("healthy", StatusReady, "", []Tool{{Name: "ping", Enabled: true}})
	health = manager.Health()
	if health.Status != HealthHealthy || health.HealthyCount != 1 || health.Revision != 2 {
		t.Fatalf("ready health=%+v", health)
	}
	manager.updateView("healthy", StatusError, "initialize failed", nil)
	health = manager.Health()
	if health.Status != HealthDegraded || health.ProblemCount != 1 || health.Revision != 3 {
		t.Fatalf("error health=%+v", health)
	}
	if len(events) != 3 || events[1].View.Status != StatusReady || events[2].Health.Status != HealthDegraded {
		t.Fatalf("status events=%+v", events)
	}
}

func TestManagerHealthIsUnconfiguredWhenOnlyDisabledServicesExist(t *testing.T) {
	manager := NewManager(nil)
	if err := manager.Configure([]ServerConfig{{
		ID: "disabled", Transport: TransportStdio, Command: "disabled", Enabled: false,
	}}); err != nil {
		t.Fatal(err)
	}
	health := manager.Health()
	if health.Status != HealthUnconfigured || health.ServerCount != 1 || health.EnabledCount != 0 {
		t.Fatalf("health=%+v", health)
	}
}
