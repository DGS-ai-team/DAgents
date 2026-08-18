package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const executionTargetLinuxChannel = "linux_channel"

// LinuxChannelConfig contains non-secret connection settings. SecretRef is
// resolved only inside the provider and is never copied into execution events
// or tool results.
type LinuxChannelConfig struct {
	ID             string
	DisplayName    string
	Host           string
	Port           int
	Username       string
	CredentialID   string
	HostKeyPolicy  string
	HostKeyRef     string
	RemoteShell    string
	DefaultCWD     string
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
	Enabled        bool
	MaxSessions    int
}

// LinuxCredential is a reference to an authentication secret. SecretRef is
// deliberately opaque to the caller; the provider's resolver is the only
// component allowed to turn it into a password or private-key PEM.
type LinuxCredential struct {
	ID           string
	DisplayName  string
	AuthType     string
	SecretRef    string
	UsernameHint string
	Enabled      bool
}

// LinuxChannelResolver supplies channel and credential metadata from the
// durable store. It must not return plaintext credentials in the channel
// configuration.
type LinuxChannelResolver interface {
	ResolveLinuxChannel(ctx context.Context, channelID string) (LinuxChannelConfig, error)
	ResolveLinuxCredential(ctx context.Context, credentialID string) (LinuxCredential, error)
}

// LinuxChannelBinding is the effective per-agent selection. The provider
// only needs Enabled/CWD/Shell to establish an execution boundary; policy and
// approval fields remain available to the orchestration layer.
type LinuxChannelBinding struct {
	AgentID         string
	ChannelID       string
	Enabled         bool
	IsDefault       bool
	RemoteCWD       string
	Shell           string
	MaxConcurrency  int
	ApprovalMode    string
	AllowedCommands []string
	DeniedCommands  []string
}

type LinuxChannelBindingResolver interface {
	ResolveLinuxBinding(ctx context.Context, agentID, channelID string) (LinuxChannelBinding, error)
}

// LinuxSecretResolver resolves an opaque secret reference at connection time.
// Implementations may use an environment variable, OS keyring, or the local
// encrypted secret store. The returned value must never be logged or emitted.
type LinuxSecretResolver func(ctx context.Context, secretRef string) (string, error)

// LinuxHostKeyResolver creates a strict callback for a channel. Production
// callers should use knownhosts.New or a pinned-key callback. Returning nil is
// not allowed by the provider.
type LinuxHostKeyResolver func(ctx context.Context, cfg LinuxChannelConfig) (ssh.HostKeyCallback, error)

// LinuxShellProvider executes one non-PTY command in one SSH session. The SSH
// client is intentionally not exposed as a persistent shell: cwd, env, and
// process state do not leak between commands.
type LinuxShellProvider struct {
	resolver        LinuxChannelResolver
	bindingResolver LinuxChannelBindingResolver
	secretResolver  LinuxSecretResolver
	hostKey         LinuxHostKeyResolver
	agentSocket     string
}

var linuxProcessSequence uint64

func NewLinuxShellProvider(resolver LinuxChannelResolver, secretResolver LinuxSecretResolver) *LinuxShellProvider {
	return &LinuxShellProvider{resolver: resolver, secretResolver: secretResolver}
}

func (p *LinuxShellProvider) WithHostKeyResolver(resolver LinuxHostKeyResolver) *LinuxShellProvider {
	if p != nil {
		p.hostKey = resolver
	}
	return p
}

func (p *LinuxShellProvider) WithSSHAgentSocket(socket string) *LinuxShellProvider {
	if p != nil {
		p.agentSocket = strings.TrimSpace(socket)
	}
	return p
}

func (p *LinuxShellProvider) WithBindingResolver(resolver LinuxChannelBindingResolver) *LinuxShellProvider {
	if p != nil {
		p.bindingResolver = resolver
	}
	return p
}

// DefaultLinuxHostKeyResolver uses an explicit channel path when provided or
// the current user's ~/.ssh/known_hosts. It deliberately has no insecure
// fallback and is suitable as the Node default.
func DefaultLinuxHostKeyResolver(_ context.Context, cfg LinuxChannelConfig) (ssh.HostKeyCallback, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.HostKeyPolicy), "pinned") {
		return nil, nil
	}
	path := strings.TrimSpace(cfg.HostKeyRef)
	if path == "" {
		home, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("resolve current user for known_hosts: %w", err)
		}
		path = filepath.Join(home.HomeDir, ".ssh", "known_hosts")
	}
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %q: %w", path, err)
	}
	return callback, nil
}

func (p *LinuxShellProvider) Start(ctx context.Context, req ExecRequest) (Process, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.resolver == nil {
		return nil, fmt.Errorf("linux channel resolver is unavailable")
	}
	if req.Target.Kind != executionTargetLinuxChannel {
		return nil, fmt.Errorf("linux shell provider does not support target %q", req.Target.Kind)
	}
	if strings.TrimSpace(req.Target.ID) == "" {
		return nil, fmt.Errorf("linux channel target id is required")
	}
	if req.TTY || req.PipeStdin {
		return nil, fmt.Errorf("linux shell provider does not support TTY or piped stdin yet")
	}
	if strings.TrimSpace(req.Command) == "" {
		return nil, fmt.Errorf("execution command is required")
	}
	if len(req.Argv) > 0 {
		return nil, fmt.Errorf("linux shell provider does not support argv yet")
	}

	cfg, err := p.resolver.ResolveLinuxChannel(ctx, strings.TrimSpace(req.Target.ID))
	if err != nil {
		return nil, err
	}
	if err := validateLinuxChannelConfig(cfg); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("linux channel %q is disabled", cfg.ID)
	}
	if p.bindingResolver != nil {
		binding, err := p.bindingResolver.ResolveLinuxBinding(ctx, strings.TrimSpace(req.Context.AgentID), cfg.ID)
		if err != nil {
			return nil, err
		}
		if !binding.Enabled {
			return nil, fmt.Errorf("linux channel %q is not enabled for agent %q", cfg.ID, req.Context.AgentID)
		}
		if req.CWD == "" && strings.TrimSpace(binding.RemoteCWD) != "" {
			cfg.DefaultCWD = strings.TrimSpace(binding.RemoteCWD)
		}
		if strings.TrimSpace(binding.Shell) != "" {
			cfg.RemoteShell = strings.TrimSpace(binding.Shell)
		}
	}
	if req.Timeout <= 0 && cfg.CommandTimeout > 0 {
		req.Timeout = cfg.CommandTimeout
	}
	cred, err := p.resolver.ResolveLinuxCredential(ctx, cfg.CredentialID)
	if err != nil {
		return nil, err
	}
	if !cred.Enabled {
		return nil, fmt.Errorf("linux credential %q is disabled", cred.ID)
	}
	auth, agentConn, err := p.authMethod(ctx, cred)
	if err != nil {
		return nil, err
	}
	hostKey, err := p.hostKeyCallback(ctx, cfg)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, err
	}

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux channel connect failed: %w", err)
	}
	clientConfig := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         connectTimeout,
	}
	clientConn, chans, requests, err := ssh.NewClientConn(conn, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), clientConfig)
	if err != nil {
		_ = conn.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux channel handshake failed: %w", err)
	}
	client := ssh.NewClient(clientConn, chans, requests)
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, fmt.Errorf("linux channel session failed: %w", err)
	}
	command, err := buildLinuxRemoteCommand(cfg, req)
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, err
	}
	seq := atomic.AddUint64(&linuxProcessSequence, 1)
	return &linuxProcess{
		id:        fmt.Sprintf("linux-process-%d", seq),
		client:    client,
		session:   session,
		agentConn: agentConn,
		command:   command,
		ctx:       req.Context,
		sink:      req.EventSink,
	}, nil
}

func (p *LinuxShellProvider) Test(ctx context.Context, target ExecutionTarget) (TargetStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.resolver == nil {
		return TargetStatus{}, fmt.Errorf("linux channel resolver is unavailable")
	}
	if target.Kind != executionTargetLinuxChannel {
		return TargetStatus{Message: fmt.Sprintf("unsupported target %q", target.Kind)}, nil
	}
	cfg, err := p.resolver.ResolveLinuxChannel(ctx, strings.TrimSpace(target.ID))
	if err != nil {
		return TargetStatus{}, err
	}
	if err := validateLinuxChannelConfig(cfg); err != nil {
		return TargetStatus{Message: err.Error()}, nil
	}
	if !cfg.Enabled {
		return TargetStatus{Message: fmt.Sprintf("linux channel %q is disabled", cfg.ID)}, nil
	}
	cred, err := p.resolver.ResolveLinuxCredential(ctx, cfg.CredentialID)
	if err != nil {
		return TargetStatus{}, err
	}
	if !cred.Enabled {
		return TargetStatus{Message: fmt.Sprintf("linux credential %q is disabled", cred.ID)}, nil
	}
	auth, agentConn, err := p.authMethod(ctx, cred)
	if err != nil {
		return TargetStatus{}, err
	}
	if agentConn != nil {
		defer agentConn.Close()
	}
	hostKey, err := p.hostKeyCallback(ctx, cfg)
	if err != nil {
		return TargetStatus{}, err
	}
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return TargetStatus{}, fmt.Errorf("linux channel connect failed: %w", err)
	}
	clientConn, chans, requests, err := ssh.NewClientConn(conn, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         connectTimeout,
	})
	if err != nil {
		_ = conn.Close()
		return TargetStatus{}, fmt.Errorf("linux channel handshake failed: %w", err)
	}
	client := ssh.NewClient(clientConn, chans, requests)
	_ = client.Close()
	return TargetStatus{Available: true, Message: "linux SSH connection established"}, nil
}

func validateLinuxChannelConfig(cfg LinuxChannelConfig) error {
	if strings.TrimSpace(cfg.ID) == "" {
		return fmt.Errorf("linux channel id is required")
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("linux channel host is required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("linux channel port must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return fmt.Errorf("linux channel username is required")
	}
	if strings.TrimSpace(cfg.CredentialID) == "" {
		return fmt.Errorf("linux channel credential id is required")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.HostKeyPolicy)) {
	case "known_hosts", "pinned":
	default:
		return fmt.Errorf("linux channel host key policy must be known_hosts or pinned")
	}
	if strings.EqualFold(strings.TrimSpace(cfg.HostKeyPolicy), "pinned") && strings.TrimSpace(cfg.HostKeyRef) == "" {
		return fmt.Errorf("linux channel pinned host key reference is required")
	}
	return nil
}

func (p *LinuxShellProvider) authMethod(ctx context.Context, cred LinuxCredential) ([]ssh.AuthMethod, net.Conn, error) {
	authType := strings.ToLower(strings.TrimSpace(cred.AuthType))
	switch authType {
	case "password":
		secret, err := p.resolveSecret(ctx, cred)
		if err != nil {
			return nil, nil, err
		}
		return []ssh.AuthMethod{ssh.Password(secret)}, nil, nil
	case "private_key":
		secret, err := p.resolveSecret(ctx, cred)
		if err != nil {
			return nil, nil, err
		}
		signer, err := ssh.ParsePrivateKey([]byte(secret))
		if err != nil {
			return nil, nil, fmt.Errorf("linux private key is invalid: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil, nil
	case "ssh_agent":
		socket := strings.TrimSpace(p.agentSocket)
		if socket == "" {
			socket = strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK"))
		}
		if socket == "" {
			return nil, nil, fmt.Errorf("SSH_AUTH_SOCK is not configured")
		}
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return nil, nil, fmt.Errorf("connect SSH agent: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}, conn, nil
	default:
		return nil, nil, fmt.Errorf("unsupported linux credential auth type %q", cred.AuthType)
	}
}

func (p *LinuxShellProvider) resolveSecret(ctx context.Context, cred LinuxCredential) (string, error) {
	if p == nil || p.secretResolver == nil {
		return "", fmt.Errorf("linux secret resolver is unavailable")
	}
	ref := strings.TrimSpace(cred.SecretRef)
	if ref == "" {
		return "", fmt.Errorf("linux credential %q secret reference is required", cred.ID)
	}
	secret, err := p.secretResolver(ctx, ref)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("linux credential %q secret is empty", cred.ID)
	}
	return secret, nil
}

func (p *LinuxShellProvider) hostKeyCallback(ctx context.Context, cfg LinuxChannelConfig) (ssh.HostKeyCallback, error) {
	if p != nil && p.hostKey != nil {
		callback, err := p.hostKey(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if callback != nil {
			return callback, nil
		}
		if !strings.EqualFold(strings.TrimSpace(cfg.HostKeyPolicy), "pinned") {
			return nil, fmt.Errorf("linux host key resolver returned an empty callback")
		}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.HostKeyPolicy), "pinned") {
		return pinnedHostKeyCallback(cfg.HostKeyRef), nil
	}
	return nil, errors.New("linux known_hosts host key callback is not configured")
}

func pinnedHostKeyCallback(reference string) ssh.HostKeyCallback {
	want := strings.TrimSpace(reference)
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		got := ssh.FingerprintSHA256(key)
		if want != got && strings.TrimPrefix(want, "SHA256:") != strings.TrimPrefix(got, "SHA256:") {
			return fmt.Errorf("linux host key mismatch for %s", hostname)
		}
		return nil
	}
}

func buildLinuxRemoteCommand(cfg LinuxChannelConfig, req ExecRequest) (string, error) {
	inner := strings.TrimSpace(req.Command)
	cwd := strings.TrimSpace(req.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(cfg.DefaultCWD)
	}
	if cwd != "" {
		inner = "cd " + shellQuote(cwd) + " && " + inner
	}
	if len(req.Env) > 0 {
		keys := make([]string, 0, len(req.Env))
		for key := range req.Env {
			if !validEnvName(key) {
				return "", fmt.Errorf("invalid remote environment variable %q", key)
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		prefix := make([]string, 0, len(keys))
		for _, key := range keys {
			prefix = append(prefix, "export "+key+"="+shellQuote(req.Env[key]))
		}
		inner = strings.Join(prefix, " && ") + " && " + inner
	}
	shell := strings.TrimSpace(cfg.RemoteShell)
	if shell == "" {
		shell = "bash"
	}
	if strings.ContainsAny(shell, " \t\r\n;|&") {
		return "", fmt.Errorf("invalid remote shell %q", shell)
	}
	return shell + " -lc " + shellQuote(inner), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func validEnvName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

type linuxProcess struct {
	mu        sync.Mutex
	id        string
	client    *ssh.Client
	session   *ssh.Session
	agentConn net.Conn
	command   string
	ctx       ExecutionContext
	sink      ProcessEventSink
	seq       uint64
	started   bool
	closed    bool
	stdout    io.Reader
	stderr    io.Reader
	stdoutSet bool
	stderrSet bool
	stdoutOut io.Writer
	stderrOut io.Writer
	eventMu   sync.Mutex
	exitMu    sync.RWMutex
	exit      *ExitStatus
}

func (p *linuxProcess) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *linuxProcess) StdoutPipe() (io.ReadCloser, error) {
	if p == nil || p.session == nil {
		return nil, fmt.Errorf("process is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.stdoutSet || p.stdoutOut != nil {
		return nil, fmt.Errorf("stdout pipe must be configured before process start")
	}
	reader, err := p.session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p.stdout = reader
	p.stdoutSet = true
	return &linuxProcessReader{Reader: reader, process: p, stream: "stdout"}, nil
}

func (p *linuxProcess) StderrPipe() (io.ReadCloser, error) {
	if p == nil || p.session == nil {
		return nil, fmt.Errorf("process is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.stderrSet || p.stderrOut != nil {
		return nil, fmt.Errorf("stderr pipe must be configured before process start")
	}
	reader, err := p.session.StderrPipe()
	if err != nil {
		return nil, err
	}
	p.stderr = reader
	p.stderrSet = true
	return &linuxProcessReader{Reader: reader, process: p, stream: "stderr"}, nil
}

func (p *linuxProcess) SetOutput(stdout, stderr io.Writer) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.stdoutSet || p.stderrSet {
		return
	}
	p.stdoutOut = stdout
	p.stderrOut = stderr
}

func (p *linuxProcess) Start() error {
	if p == nil || p.session == nil {
		return fmt.Errorf("process is nil")
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return fmt.Errorf("process %s already started", p.id)
	}
	p.started = true
	if p.stdoutOut != nil {
		if p.sink != nil {
			p.session.Stdout = &linuxProcessWriter{Writer: p.stdoutOut, process: p, stream: "stdout"}
		} else {
			p.session.Stdout = p.stdoutOut
		}
	}
	if p.stderrOut != nil {
		if p.sink != nil {
			p.session.Stderr = &linuxProcessWriter{Writer: p.stderrOut, process: p, stream: "stderr"}
		} else {
			p.session.Stderr = p.stderrOut
		}
	}
	p.mu.Unlock()
	if err := p.session.Start(p.command); err != nil {
		return err
	}
	p.emit(ProcessEventStarted, "", nil, nil)
	return nil
}

func (p *linuxProcess) Wait() error {
	if p == nil || p.session == nil {
		return fmt.Errorf("process is nil")
	}
	err := p.session.Wait()
	exit := linuxExitStatus(err)
	p.exitMu.Lock()
	p.exit = exit
	p.exitMu.Unlock()
	p.emit(ProcessEventExited, "", nil, exit)
	return err
}

func (p *linuxProcess) ExitStatus() *ExitStatus {
	if p == nil {
		return nil
	}
	p.exitMu.RLock()
	defer p.exitMu.RUnlock()
	if p.exit == nil {
		return nil
	}
	copy := *p.exit
	return &copy
}

func (p *linuxProcess) Terminate(_ context.Context) error {
	if p == nil {
		return nil
	}
	if p.ExitStatus() != nil {
		return nil
	}
	p.emit(ProcessEventTerminateRequested, "", nil, nil)
	return p.Close()
}

func (p *linuxProcess) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	session, client, agentConn := p.session, p.client, p.agentConn
	p.mu.Unlock()
	var first error
	if session != nil {
		if err := session.Close(); err != nil && !errors.Is(err, io.EOF) {
			first = err
		}
	}
	if client != nil {
		if err := client.Close(); err != nil && first == nil {
			first = err
		}
	}
	if agentConn != nil {
		_ = agentConn.Close()
	}
	return first
}

func (p *linuxProcess) emit(kind ProcessEventType, stream string, data []byte, exit *ExitStatus) {
	if p == nil || p.sink == nil {
		return
	}
	p.eventMu.Lock()
	defer p.eventMu.Unlock()
	seq := atomic.AddUint64(&p.seq, 1)
	if len(data) > 0 {
		data = append([]byte(nil), data...)
	}
	p.sink(ProcessEvent{Type: kind, ProcessID: p.id, Seq: seq, Context: p.ctx, Stream: stream, Data: data, Exit: exit})
}

func linuxExitStatus(err error) *ExitStatus {
	status := &ExitStatus{Code: 0}
	if err == nil {
		return status
	}
	status.Code = 1
	status.Error = err.Error()
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		status.Code = exitErr.ExitStatus()
	}
	return status
}

type linuxProcessWriter struct {
	io.Writer
	process *linuxProcess
	stream  string
}

func (w *linuxProcessWriter) Write(data []byte) (int, error) {
	n, err := w.Writer.Write(data)
	if n > 0 {
		w.process.emit(ProcessEventOutput, w.stream, data[:n], nil)
	}
	return n, err
}

type linuxProcessReader struct {
	io.Reader
	process *linuxProcess
	stream  string
}

func (r *linuxProcessReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	if n > 0 {
		r.process.emit(ProcessEventOutput, r.stream, data[:n], nil)
	}
	return n, err
}

func (r *linuxProcessReader) Close() error { return nil }
