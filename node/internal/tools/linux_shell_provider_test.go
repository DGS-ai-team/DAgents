package tools

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"golang.org/x/crypto/ssh"
)

type testLinuxResolver struct {
	channel    LinuxChannelConfig
	credential LinuxCredential
}

type testLinuxBindingResolver struct {
	binding LinuxChannelBinding
}

func (r testLinuxBindingResolver) ResolveLinuxBinding(context.Context, string, string) (LinuxChannelBinding, error) {
	return r.binding, nil
}

func (r testLinuxResolver) ResolveLinuxChannel(context.Context, string) (LinuxChannelConfig, error) {
	return r.channel, nil
}

func (r testLinuxResolver) ResolveLinuxCredential(context.Context, string) (LinuxCredential, error) {
	return r.credential, nil
}

func TestLinuxShellProviderValidatesStrictTargetAndConfiguration(t *testing.T) {
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "prod", Host: "127.0.0.1", Port: 22, Username: "deploy",
			CredentialID: "cred", HostKeyPolicy: "known_hosts", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "env:SSH_PASSWORD", Enabled: true},
	}, func(context.Context, string) (string, error) { return "secret", nil })
	if _, err := provider.Start(context.Background(), ExecRequest{
		Target: ExecutionTarget{Kind: executionTargetLocal, ID: "prod"}, Command: "id",
	}); err == nil || !strings.Contains(err.Error(), "does not support target") {
		t.Fatalf("unexpected target error: %v", err)
	}
	if _, err := provider.Start(context.Background(), ExecRequest{
		Target: ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "prod"}, Command: "id",
	}); err == nil || !strings.Contains(err.Error(), "known_hosts host key callback") {
		t.Fatalf("provider should fail closed without host key callback: %v", err)
	}
}

func TestLinuxShellProviderEnforcesBindingCommandPolicy(t *testing.T) {
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "prod", Host: "127.0.0.1", Port: 22, Username: "deploy",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: "SHA256:test", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "secret", nil }).WithBindingResolver(testLinuxBindingResolver{
		binding: LinuxChannelBinding{
			AgentID: "agent-1", ChannelID: "prod", Enabled: true,
			AllowedCommands: []string{"git *"}, DeniedCommands: []string{"git push *"},
		},
	})
	if _, err := provider.Start(context.Background(), ExecRequest{
		Target:  ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "prod"},
		Context: ExecutionContext{AgentID: "agent-1"}, Command: "git push origin main",
	}); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected denied command, got %v", err)
	}
	if _, err := provider.Start(context.Background(), ExecRequest{
		Target:  ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "prod"},
		Context: ExecutionContext{AgentID: "agent-1"}, Command: "uname -a",
	}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected allow-list rejection, got %v", err)
	}
}

func TestLinuxShellProviderPreflightAppliesBindingApproval(t *testing.T) {
	resolver := testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "prod", Host: "127.0.0.1", Port: 22, Username: "deploy",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: "SHA256:test", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "secret", Enabled: true},
	}
	provider := NewLinuxShellProvider(resolver, nil).WithBindingResolver(testLinuxBindingResolver{
		binding: LinuxChannelBinding{AgentID: "agent-1", ChannelID: "prod", Enabled: true, ApprovalMode: "require_approval"},
	})
	action, reason, err := provider.Preflight(context.Background(), "agent-1", "prod", "git status")
	if err != nil || action != policy.ActionRequireApproval || !strings.Contains(reason, "requires approval") {
		t.Fatalf("unexpected approval preflight: action=%q reason=%q err=%v", action, reason, err)
	}

	provider = NewLinuxShellProvider(resolver, nil).WithBindingResolver(testLinuxBindingResolver{
		binding: LinuxChannelBinding{AgentID: "agent-1", ChannelID: "prod", Enabled: true, DeniedCommands: []string{"git push *"}},
	})
	action, reason, err = provider.Preflight(context.Background(), "agent-1", "prod", "git push origin main")
	if err != nil || action != policy.ActionDeny || !strings.Contains(reason, "denied") {
		t.Fatalf("unexpected denied preflight: action=%q reason=%q err=%v", action, reason, err)
	}
}

func TestLinuxShellProviderRejectsUnapprovedStartBeforeSSH(t *testing.T) {
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "prod", Host: "127.0.0.1", Port: 22, Username: "deploy",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: "SHA256:test", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "secret", nil }).WithBindingResolver(testLinuxBindingResolver{
		binding: LinuxChannelBinding{AgentID: "agent-1", ChannelID: "prod", Enabled: true, ApprovalMode: "require_approval"},
	})
	_, err := provider.Start(context.Background(), ExecRequest{
		Target:  ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "prod"},
		Context: ExecutionContext{AgentID: "agent-1"},
		Command: "git status",
	})
	if err == nil || !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("expected pre-SSH approval rejection, got %v", err)
	}
}

func TestLinuxShellProviderRejectsUnapprovedTerminalBeforeSSH(t *testing.T) {
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "prod", Host: "127.0.0.1", Port: 22, Username: "deploy",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: "SHA256:test", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "secret", nil }).WithBindingResolver(testLinuxBindingResolver{
		binding: LinuxChannelBinding{AgentID: "agent-1", ChannelID: "prod", Enabled: true, ApprovalMode: "require_approval"},
	})
	_, err := provider.OpenTerminal(context.Background(), TerminalRequest{
		Target:  ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "prod"},
		Context: ExecutionContext{AgentID: "agent-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("expected pre-SSH terminal approval rejection, got %v", err)
	}
}

func TestLinuxChannelConcurrencyWaitsAndReleases(t *testing.T) {
	provider := NewLinuxShellProvider(nil, nil)
	release, err := provider.acquireChannelSlot(context.Background(), "agent\x00channel", 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := provider.acquireChannelSlot(ctx, "agent\x00channel", 1); err == nil {
		t.Fatal("expected second slot to wait and time out")
	}
	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	release2, err := provider.acquireChannelSlot(ctx2, "agent\x00channel", 1)
	if err != nil {
		t.Fatal(err)
	}
	release2()
}

func TestLinuxShellProviderTestReportsStructuredTCPFailure(t *testing.T) {
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "unreachable", Host: "127.0.0.1", Port: 1, Username: "deploy",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: "SHA256:test",
			ConnectTimeout: 100 * time.Millisecond, Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "test-secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "secret", nil })
	status, err := provider.Test(context.Background(), ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "unreachable"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Available || status.ErrorCode != "tcp_connect_failed" {
		t.Fatalf("unexpected failure status: %+v", status)
	}
	if len(status.Stages) < 6 || status.Stages[len(status.Stages)-1].Name != "tcp" || status.Stages[len(status.Stages)-1].Status != "failed" {
		t.Fatalf("expected failed tcp stage, got %+v", status.Stages)
	}
}

func TestBuildLinuxRemoteCommandQuotesCWDAndEnvironment(t *testing.T) {
	command, err := buildLinuxRemoteCommand(LinuxChannelConfig{
		RemoteShell: "bash", DefaultCWD: "/srv/it's-app",
	}, ExecRequest{
		Command: "printf '%s' \"$NAME\"",
		Env:     map[string]string{"NAME": "a'b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command, "cd") || !strings.Contains(command, "export NAME=") {
		t.Fatalf("command=%q", command)
	}
	if got := shellQuote("a'b"); got != "'a'\"'\"'b'" {
		t.Fatalf("shell quote=%q", got)
	}
	if _, err := buildLinuxRemoteCommand(LinuxChannelConfig{RemoteShell: "bash"}, ExecRequest{
		Command: "id", Env: map[string]string{"BAD-NAME": "x"},
	}); err == nil {
		t.Fatal("invalid environment name should be rejected")
	}
}

func TestBuildLinuxRemoteCommandUsesDedicatedProcessGroupWrapper(t *testing.T) {
	command, mode, err := buildLinuxRemoteCommandWithMode(LinuxChannelConfig{RemoteShell: "bash"}, ExecRequest{
		Command: "sleep 10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mode != "setsid_or_fallback" {
		t.Fatalf("process group mode=%q", mode)
	}
	for _, part := range []string{"command -v setsid", "exec setsid sh -c", "else", "kill -TERM", "\"$child\"", "sleep 10"} {
		if !strings.Contains(command, part) {
			t.Fatalf("remote command missing %q: %s", part, command)
		}
	}
}

func TestBuildLinuxRemoteCommandPersistsTokenForBackgroundRecovery(t *testing.T) {
	command, _, recovery, err := buildLinuxRemoteCommandWithRecovery(LinuxChannelConfig{ID: "prod", RemoteShell: "bash"}, ExecRequest{
		Command: "sleep 10",
		Context: ExecutionContext{BackgroundJobID: "job-a1b2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if recovery.TargetID != "prod" || recovery.JobToken != "job-a1b2" || recovery.PIDFile != remoteJobPIDFile("job-a1b2") {
		t.Fatalf("recovery=%+v", recovery)
	}
	for _, part := range []string{"printf", recovery.JobToken, recovery.PIDFile} {
		if !strings.Contains(command, part) {
			t.Fatalf("remote command missing %q: %s", part, command)
		}
	}
}

func TestBuildLinuxRemoteCommandCreatesRecoveryForSynchronousExecution(t *testing.T) {
	_, _, recovery, err := buildLinuxRemoteCommandWithRecovery(LinuxChannelConfig{ID: "prod", RemoteShell: "bash"}, ExecRequest{
		Command: "sleep 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if recovery.TargetID != "prod" || recovery.JobToken == "" || !isSafeRemoteJobToken(recovery.JobToken) {
		t.Fatalf("recovery=%+v", recovery)
	}
	if recovery.PIDFile != remoteJobPIDFile(recovery.JobToken) {
		t.Fatalf("recovery pid file=%q", recovery.PIDFile)
	}
}

func TestFormatLinuxExecResultReportsUnknownTermination(t *testing.T) {
	result := formatLinuxExecResult(nil, nil, &ExitStatus{Code: 1}, context.Canceled, false, "unknown")
	if !strings.Contains(result, "termination_status: unknown") {
		t.Fatalf("termination status missing: %q", result)
	}
}

func TestBuildTerminalInitQuotesAndSortsEnvironment(t *testing.T) {
	init, err := buildTerminalInit(LinuxChannelConfig{DefaultCWD: "/srv/it's-app"}, TerminalRequest{
		Env: map[string]string{"ZED": "last", "NAME": "a'b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(init); got != "cd '/srv/it'\"'\"'s-app' && export NAME='a'\"'\"'b' && export ZED='last'\n" {
		t.Fatalf("terminal init=%q", got)
	}
	if _, err := buildTerminalInit(LinuxChannelConfig{}, TerminalRequest{Env: map[string]string{"BAD-NAME": "x"}}); err == nil {
		t.Fatal("invalid environment name should be rejected")
	}
}

func TestLinuxExitStatus(t *testing.T) {
	if got := linuxExitStatus(nil); got == nil || got.Code != 0 {
		t.Fatalf("got=%+v", got)
	}
	if got := linuxExitStatus(context.Canceled); got == nil || got.Code != 1 || got.Error == "" {
		t.Fatalf("got=%+v", got)
	}
}

func TestLinuxShellProviderRunsAgainstTestSSHServer(t *testing.T) {
	addr, fingerprint := startTestSSHServer(t)
	host, portString, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portString, "%d", &port); err != nil {
		t.Fatal(err)
	}
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "test", Host: host, Port: port, Username: "test-user",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: fingerprint,
			RemoteShell: "bash", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "test-secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "test-password", nil })
	status, err := provider.Test(context.Background(), ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "test"})
	if err != nil || !status.Available {
		t.Fatalf("connection test status=%+v err=%v", status, err)
	}
	if len(status.Stages) != 10 {
		t.Fatalf("expected structured test stages, got %+v", status.Stages)
	}
	for _, stage := range status.Stages {
		if stage.Status != "passed" {
			t.Fatalf("stage failed: %+v", status.Stages)
		}
	}
	process, err := provider.Start(context.Background(), ExecRequest{
		Target:  ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "test"},
		Command: "printf remote-ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := process.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	stdoutDone := make(chan []byte, 1)
	stderrDone := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(stdout); stdoutDone <- data }()
	go func() { data, _ := io.ReadAll(stderr); stderrDone <- data }()
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := string(<-stdoutDone); got != "remote-ok\n" {
		t.Fatalf("stdout=%q", got)
	}
	if got := string(<-stderrDone); got != "" {
		t.Fatalf("stderr=%q", got)
	}
	if exit := process.ExitStatus(); exit == nil || exit.Code != 0 {
		t.Fatalf("exit=%+v", exit)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxShellProviderReportsUnknownHostKeyFingerprint(t *testing.T) {
	addr, fingerprint := startTestSSHServer(t)
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "unknown-key", Host: host, Port: port, Username: "test-user",
			CredentialID: "cred", HostKeyPolicy: "known_hosts", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "test-secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "test-password", nil }).WithHostKeyResolver(
		func(context.Context, LinuxChannelConfig) (ssh.HostKeyCallback, error) {
			return func(string, net.Addr, ssh.PublicKey) error {
				return fmt.Errorf("knownhosts: key is unknown")
			}, nil
		},
	)
	status, err := provider.Test(context.Background(), ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "unknown-key"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Available || status.ErrorCode != "host_key_unknown" {
		t.Fatalf("unexpected unknown-key status: %+v", status)
	}
	if status.HostKeyFingerprint != fingerprint {
		t.Fatalf("fingerprint=%q want=%q", status.HostKeyFingerprint, fingerprint)
	}
	last := status.Stages[len(status.Stages)-1]
	if last.Name != "ssh_handshake" || last.Code != "host_key_unknown" {
		t.Fatalf("unexpected failed stage: %+v", last)
	}
}

func TestLinuxShellProviderOpensPTY(t *testing.T) {
	addr, fingerprint := startTestSSHServer(t)
	host, portString, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portString, "%d", &port); err != nil {
		t.Fatal(err)
	}
	events := make(chan ProcessEvent, 8)
	provider := NewLinuxShellProvider(testLinuxResolver{
		channel: LinuxChannelConfig{
			ID: "test", Host: host, Port: port, Username: "test-user",
			CredentialID: "cred", HostKeyPolicy: "pinned", HostKeyRef: fingerprint,
			RemoteShell: "bash", Enabled: true,
		},
		credential: LinuxCredential{ID: "cred", AuthType: "password", SecretRef: "test-secret", Enabled: true},
	}, func(context.Context, string) (string, error) { return "test-password", nil })
	terminal, err := provider.OpenTerminal(context.Background(), TerminalRequest{
		Target:  ExecutionTarget{Kind: executionTargetLinuxChannel, ID: "test"},
		Context: ExecutionContext{AgentID: "agent-1", TurnID: "turn-1"},
		Rows:    30, Cols: 100,
		EventSink: func(event ProcessEvent) { events <- event },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	if err := terminal.Input(context.Background(), []byte("before-start\n")); err == nil {
		t.Fatal("input before start should fail")
	}
	output, err := terminal.Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := terminal.Output(); err == nil {
		t.Fatal("terminal output should only be acquired once")
	}
	if err := terminal.Resize(context.Background(), 40, 120); err == nil {
		t.Fatal("resize before start should fail")
	}
	reader := bufio.NewReader(output)
	if err := terminal.Start(); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Resize(context.Background(), 0, 120); err == nil {
		t.Fatal("zero terminal rows should be rejected")
	}
	if got := readPTYLine(t, reader); got != "pty-ready\n" {
		t.Fatalf("initial PTY output=%q", got)
	}
	if err := terminal.Resize(context.Background(), 40, 120); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Input(context.Background(), []byte("echo hello\n")); err != nil {
		t.Fatal(err)
	}
	if got := readPTYLine(t, reader); got != "received:echo hello\n" {
		t.Fatalf("echo output=%q", got)
	}
	if err := terminal.Input(context.Background(), []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Wait(); err != nil {
		t.Fatal(err)
	}
	if exit := terminal.ExitStatus(); exit == nil || exit.Code != 0 {
		t.Fatalf("exit=%+v", exit)
	}
	var sawStarted, sawPTYOutput, sawExited bool
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
			sawPTYOutput = true
		case ProcessEventExited:
			sawExited = true
		}
	}
	if !sawStarted || !sawPTYOutput || !sawExited {
		t.Fatalf("lifecycle events started=%v pty_output=%v exited=%v", sawStarted, sawPTYOutput, sawExited)
	}
	if err := terminal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := terminal.Close(); err != nil {
		t.Fatal(err)
	}
}

func readPTYLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	result := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		result <- line
	}()
	select {
	case line := <-result:
		return line
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PTY output")
		return ""
	}
}

func startTestSSHServer(t *testing.T) (string, string) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if string(password) != "test-password" {
			return nil, fmt.Errorf("invalid password")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConnection(conn, config)
		}
	}()
	return listener.Addr().String(), ssh.FingerprintSHA256(signer.PublicKey())
}

func serveTestSSHConnection(conn net.Conn, config *ssh.ServerConfig) {
	serverConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	go func() {
		for newChannel := range channels {
			if newChannel.ChannelType() != "session" {
				_ = newChannel.Reject(ssh.UnknownChannelType, "session only")
				continue
			}
			channel, requests, err := newChannel.Accept()
			if err != nil {
				continue
			}
			go func() {
				for request := range requests {
					switch request.Type {
					case "exec":
						_ = request.Reply(true, nil)
						var payload struct{ Command string }
						_ = ssh.Unmarshal(request.Payload, &payload)
						if strings.TrimSpace(payload.Command) == "bash -l" {
							go serveTestInteractivePTY(channel)
							continue
						}
						_, _ = channel.Write([]byte("remote-ok\n"))
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ ExitStatus uint32 }{0}))
						_ = channel.Close()
						return
					case "pty-req", "window-change":
						_ = request.Reply(true, nil)
					case "shell":
						_ = request.Reply(true, nil)
						go serveTestInteractivePTY(channel)
					default:
						_ = request.Reply(false, nil)
					}
				}
			}()
		}
	}()
	_ = serverConn.Wait()
}

func serveTestInteractivePTY(channel ssh.Channel) {
	_, _ = channel.Write([]byte("pty-ready\n"))
	reader := bufio.NewReader(channel)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if strings.TrimSpace(line) == "exit" {
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ ExitStatus uint32 }{0}))
			_ = channel.Close()
			return
		}
		_, _ = channel.Write([]byte("received:" + line))
	}
}
