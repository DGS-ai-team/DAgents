package tools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
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
	concurrencyMu   sync.Mutex
	concurrency     map[string]*linuxChannelConcurrency
}

type linuxChannelConcurrency struct {
	limit   int
	active  int
	changed chan struct{}
}

var linuxProcessSequence uint64
var linuxRemoteRecoverySequence uint64

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

func (p *LinuxShellProvider) acquireChannelSlot(ctx context.Context, key string, limit int) (func(), error) {
	if p == nil {
		return nil, fmt.Errorf("linux shell provider is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 1
	}
	p.concurrencyMu.Lock()
	if p.concurrency == nil {
		p.concurrency = make(map[string]*linuxChannelConcurrency)
	}
	state := p.concurrency[key]
	if state == nil {
		state = &linuxChannelConcurrency{limit: limit, changed: make(chan struct{})}
		p.concurrency[key] = state
	} else if limit != state.limit {
		// A binding may be edited while the Node is running. Existing holders
		// keep their slots; the new limit applies as soon as capacity permits.
		state.limit = limit
	}
	for {
		if state.active < state.limit {
			state.active++
			p.concurrencyMu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					p.concurrencyMu.Lock()
					if state.active > 0 {
						state.active--
					}
					close(state.changed)
					state.changed = make(chan struct{})
					p.concurrencyMu.Unlock()
				})
			}, nil
		}
		changed := state.changed
		p.concurrencyMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
			p.concurrencyMu.Lock()
		}
	}
}

func linuxChannelConcurrencyLimit(channelMax, bindingMax int) int {
	limit := channelMax
	if limit <= 0 {
		limit = 1
	}
	if bindingMax > 0 && bindingMax < limit {
		limit = bindingMax
	}
	return limit
}

func linuxBindingCommandError(binding LinuxChannelBinding, command string) error {
	command = strings.TrimSpace(command)
	for _, rule := range binding.DeniedCommands {
		matched, err := linuxCommandRuleMatches(rule, command)
		if err != nil {
			return fmt.Errorf("invalid denied command rule %q: %w", rule, err)
		}
		if matched {
			return fmt.Errorf("command denied by Linux channel policy: %s", strings.TrimSpace(rule))
		}
	}
	if len(binding.AllowedCommands) == 0 {
		return nil
	}
	for _, rule := range binding.AllowedCommands {
		matched, err := linuxCommandRuleMatches(rule, command)
		if err != nil {
			return fmt.Errorf("invalid allowed command rule %q: %w", rule, err)
		}
		if matched {
			return nil
		}
	}
	return fmt.Errorf("command is not allowed by Linux channel policy")
}

func linuxCommandRuleMatches(rule, command string) (bool, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false, nil
	}
	if strings.ContainsAny(rule, "*?[") {
		return path.Match(rule, command)
	}
	return rule == command, nil
}

func validateLinuxBindingMode(binding LinuxChannelBinding) error {
	switch strings.ToLower(strings.TrimSpace(binding.ApprovalMode)) {
	case "", "auto", "allow", "never", "require_approval", "always", "deny":
		return nil
	default:
		return fmt.Errorf("unsupported Linux channel approval mode %q", binding.ApprovalMode)
	}
}

func linuxBindingApprovalAction(binding LinuxChannelBinding) policy.Action {
	switch strings.ToLower(strings.TrimSpace(binding.ApprovalMode)) {
	case "deny":
		return policy.ActionDeny
	case "require_approval", "always":
		return policy.ActionRequireApproval
	default:
		return policy.ActionAuto
	}
}

// Preflight checks only durable channel/binding policy. It intentionally does
// not resolve credentials or open a socket; those checks remain at Start so a
// stale approval can never bypass current connection state.
func (p *LinuxShellProvider) Preflight(ctx context.Context, agentID, channelID, command string) (policy.Action, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.resolver == nil {
		return policy.ActionDeny, "Linux channel provider is unavailable", nil
	}
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return policy.ActionDeny, "Linux channel_id is required", nil
	}
	cfg, err := p.resolver.ResolveLinuxChannel(ctx, channelID)
	if err != nil {
		return policy.ActionDeny, fmt.Sprintf("Linux channel preflight failed: %v", err), nil
	}
	if err := validateLinuxChannelConfig(cfg); err != nil {
		return policy.ActionDeny, err.Error(), nil
	}
	if !cfg.Enabled {
		return policy.ActionDeny, fmt.Sprintf("Linux channel %q is disabled", cfg.ID), nil
	}
	if p.bindingResolver == nil {
		return policy.ActionAuto, "", nil
	}
	binding, err := p.bindingResolver.ResolveLinuxBinding(ctx, strings.TrimSpace(agentID), cfg.ID)
	if err != nil {
		return policy.ActionDeny, fmt.Sprintf("Linux channel binding preflight failed: %v", err), nil
	}
	if !binding.Enabled {
		return policy.ActionDeny, fmt.Sprintf("Linux channel %q is not enabled for agent %q", cfg.ID, agentID), nil
	}
	if err := validateLinuxBindingMode(binding); err != nil {
		return policy.ActionDeny, err.Error(), nil
	}
	if strings.EqualFold(strings.TrimSpace(binding.ApprovalMode), "deny") {
		return policy.ActionDeny, fmt.Sprintf("Linux channel %q is denied by binding policy", binding.ChannelID), nil
	}
	if err := linuxBindingCommandError(binding, command); err != nil {
		return policy.ActionDeny, err.Error(), nil
	}
	action := linuxBindingApprovalAction(binding)
	if action == policy.ActionRequireApproval {
		return action, fmt.Sprintf("Linux channel %q requires approval for this operation", cfg.ID), nil
	}
	return action, "", nil
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
	// knownhosts.New requires the file to exist. Creating the empty file here
	// keeps first-use on a new Linux channel deterministic while preserving the
	// strict host-key policy: an empty file still rejects an unknown host until
	// its key is explicitly added.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create known_hosts directory %q: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create known_hosts %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close known_hosts %q: %w", path, err)
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
	var binding LinuxChannelBinding
	if p.bindingResolver != nil {
		binding, err = p.bindingResolver.ResolveLinuxBinding(ctx, strings.TrimSpace(req.Context.AgentID), cfg.ID)
		if err != nil {
			return nil, err
		}
		if !binding.Enabled {
			return nil, fmt.Errorf("linux channel %q is not enabled for agent %q", cfg.ID, req.Context.AgentID)
		}
		if err := validateLinuxBindingMode(binding); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Command) != "" {
			if err := linuxBindingCommandError(binding, req.Command); err != nil {
				return nil, err
			}
		}
		if linuxBindingApprovalAction(binding) == policy.ActionRequireApproval && strings.TrimSpace(req.Context.ApprovalID) == "" {
			return nil, fmt.Errorf("linux channel %q requires approval before execution", cfg.ID)
		}
		if req.CWD == "" && strings.TrimSpace(binding.RemoteCWD) != "" {
			cfg.DefaultCWD = strings.TrimSpace(binding.RemoteCWD)
		}
		if strings.TrimSpace(binding.Shell) != "" {
			cfg.RemoteShell = strings.TrimSpace(binding.Shell)
		}
	}
	releaseSlot, err := p.acquireChannelSlot(ctx,
		strings.TrimSpace(req.Context.AgentID)+"\x00"+cfg.ID,
		linuxChannelConcurrencyLimit(cfg.MaxSessions, binding.MaxConcurrency),
	)
	if err != nil {
		return nil, fmt.Errorf("linux channel concurrency limit: %w", err)
	}
	slotTransferred := false
	defer func() {
		if !slotTransferred {
			releaseSlot()
		}
	}()
	req.Context.PolicyDecision = "linux_channel_allowed"
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
	clientConn, chans, requests, err := newSSHClientConnContext(ctx, conn, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), clientConfig)
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
	command, processGroupMode, recovery, err := buildLinuxRemoteCommandWithRecovery(cfg, req)
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, err
	}
	seq := atomic.AddUint64(&linuxProcessSequence, 1)
	slotTransferred = true
	return &linuxProcess{
		id:               fmt.Sprintf("linux-process-%d", seq),
		provider:         p,
		client:           client,
		session:          session,
		agentConn:        agentConn,
		command:          command,
		processGroupMode: processGroupMode,
		remoteRecovery:   recovery,
		ctx:              req.Context,
		sink:             req.EventSink,
		releaseSlot:      releaseSlot,
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
	stages := make([]TargetStageStatus, 0, 9)
	cfg, err := p.resolver.ResolveLinuxChannel(ctx, strings.TrimSpace(target.ID))
	if err != nil {
		return TargetStatus{}, err
	}
	if err := recordLinuxTestStage(&stages, "config", func() error {
		return validateLinuxChannelConfig(cfg)
	}); err != nil {
		return linuxTestFailure(stages, "invalid_channel_config", err), nil
	}
	if !cfg.Enabled {
		err := fmt.Errorf("linux channel %q is disabled", cfg.ID)
		return linuxTestFailure(stages, "channel_disabled", err), nil
	}
	cred, err := p.resolver.ResolveLinuxCredential(ctx, cfg.CredentialID)
	if err != nil {
		return TargetStatus{}, err
	}
	if err := recordLinuxTestStage(&stages, "credential", func() error {
		if !cred.Enabled {
			return fmt.Errorf("linux credential %q is disabled", cred.ID)
		}
		return nil
	}); err != nil {
		return linuxTestFailure(stages, "credential_unavailable", err), nil
	}
	auth, agentConn, err := p.authMethod(ctx, cred)
	if err != nil {
		recordLinuxTestStageError(&stages, "authentication", "authentication_failed", err)
		return linuxTestFailure(stages, "authentication_failed", err), nil
	}
	recordLinuxTestStage(&stages, "authentication", func() error { return nil })
	if agentConn != nil {
		defer agentConn.Close()
	}
	hostKey, err := p.hostKeyCallback(ctx, cfg)
	if err != nil {
		recordLinuxTestStageError(&stages, "host_key", "host_key_unavailable", err)
		return linuxTestFailure(stages, "host_key_unavailable", err), nil
	}
	recordLinuxTestStage(&stages, "host_key", func() error { return nil })
	var observedHostKeyFingerprint string
	baseHostKey := hostKey
	hostKey = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if key != nil {
			observedHostKeyFingerprint = ssh.FingerprintSHA256(key)
		}
		return baseHostKey(hostname, remote, key)
	}
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	if err := recordLinuxTestStage(&stages, "dns", func() error {
		if net.ParseIP(cfg.Host) != nil {
			return nil
		}
		addresses, err := net.DefaultResolver.LookupHost(ctx, cfg.Host)
		if err != nil {
			return err
		}
		if len(addresses) == 0 {
			return fmt.Errorf("host resolved to no addresses")
		}
		return nil
	}); err != nil {
		return linuxTestFailure(stages, "dns_failed", fmt.Errorf("resolve %q: %w", cfg.Host, err)), nil
	}
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		wrapped := fmt.Errorf("linux channel connect failed: %w", err)
		recordLinuxTestStageError(&stages, "tcp", "tcp_connect_failed", wrapped)
		return linuxTestFailure(stages, "tcp_connect_failed", wrapped), nil
	}
	recordLinuxTestStage(&stages, "tcp", func() error { return nil })
	clientConn, chans, requests, err := newSSHClientConnContext(ctx, conn, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         connectTimeout,
	})
	if err != nil {
		_ = conn.Close()
		code := classifyLinuxSSHHandshakeError(err)
		wrapped := fmt.Errorf("linux channel handshake failed: %w", err)
		recordLinuxTestStageError(&stages, "ssh_handshake", code, wrapped)
		failure := linuxTestFailure(stages, code, wrapped)
		failure.HostKeyFingerprint = observedHostKeyFingerprint
		return failure, nil
	}
	client := ssh.NewClient(clientConn, chans, requests)
	defer client.Close()
	recordLinuxTestStage(&stages, "ssh_handshake", func() error { return nil })
	session, err := client.NewSession()
	if err != nil {
		code := "session_open_failed"
		recordLinuxTestStageError(&stages, "session", code, err)
		return linuxTestFailure(stages, code, err), nil
	}
	defer session.Close()
	recordLinuxTestStage(&stages, "session", func() error { return nil })
	command, err := buildLinuxRemoteCommand(cfg, ExecRequest{Command: "printf dagents-channel-probe"})
	if err != nil {
		recordLinuxTestStageError(&stages, "shell", "shell_build_failed", err)
		return linuxTestFailure(stages, "shell_build_failed", err), nil
	}
	recordLinuxTestStage(&stages, "shell", func() error { return nil })
	if _, err := session.Output(command); err != nil {
		wrapped := fmt.Errorf("linux channel command test failed: %w", err)
		recordLinuxTestStageError(&stages, "command", "command_failed", wrapped)
		return linuxTestFailure(stages, "command_failed", wrapped), nil
	}
	recordLinuxTestStage(&stages, "command", func() error { return nil })
	return TargetStatus{
		Available:          true,
		Message:            "linux SSH connection and command test succeeded",
		HostKeyFingerprint: observedHostKeyFingerprint,
		Stages:             stages,
	}, nil
}

func recordLinuxTestStage(stages *[]TargetStageStatus, name string, check func() error) error {
	started := time.Now()
	err := check()
	stage := TargetStageStatus{Name: name, Status: "passed", DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		stage.Status = "failed"
		stage.Message = err.Error()
	}
	*stages = append(*stages, stage)
	return err
}

func recordLinuxTestStageError(stages *[]TargetStageStatus, name, code string, err error) {
	started := time.Now()
	*stages = append(*stages, TargetStageStatus{
		Name: name, Status: "failed", Code: code, Message: err.Error(),
		DurationMS: time.Since(started).Milliseconds(),
	})
}

func linuxTestFailure(stages []TargetStageStatus, code string, err error) TargetStatus {
	return TargetStatus{Available: false, Message: err.Error(), ErrorCode: code, Stages: stages}
}

func classifyLinuxSSHHandshakeError(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unable to authenticate") || strings.Contains(message, "authentication") {
		return "authentication_failed"
	}
	if strings.Contains(message, "key is unknown") || strings.Contains(message, "key is not known") {
		return "host_key_unknown"
	}
	if strings.Contains(message, "knownhosts") || strings.Contains(message, "host key") {
		return "host_key_mismatch"
	}
	return "ssh_handshake_failed"
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

// openClient creates an authenticated SSH client for one Linux channel. It is
// shared by SFTP transfers and deliberately keeps the client private to the
// provider so credentials never cross the tools boundary.
func (p *LinuxShellProvider) openClient(ctx context.Context, channelID, agentID, approvalID string) (LinuxChannelConfig, *ssh.Client, net.Conn, error) {
	return p.openClientWithOptions(ctx, channelID, agentID, approvalID, true)
}

// openClientForRecovery is used only to stop a previously approved orphaned
// process. It still requires a live, enabled channel and binding, but does not
// ask for a second HITL approval for the safety-improving cancellation.
func (p *LinuxShellProvider) openClientForRecovery(ctx context.Context, channelID, agentID string) (LinuxChannelConfig, *ssh.Client, net.Conn, error) {
	return p.openClientWithOptions(ctx, channelID, agentID, "", false)
}

func (p *LinuxShellProvider) openClientWithOptions(ctx context.Context, channelID, agentID, approvalID string, requireApproval bool) (LinuxChannelConfig, *ssh.Client, net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.resolver == nil {
		return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel resolver is unavailable")
	}
	id := strings.TrimSpace(channelID)
	if id == "" {
		return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel target id is required")
	}
	cfg, err := p.resolver.ResolveLinuxChannel(ctx, id)
	if err != nil {
		return LinuxChannelConfig{}, nil, nil, err
	}
	if err := validateLinuxChannelConfig(cfg); err != nil {
		return LinuxChannelConfig{}, nil, nil, err
	}
	if !cfg.Enabled {
		return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel %q is disabled", cfg.ID)
	}
	if p.bindingResolver != nil {
		binding, err := p.bindingResolver.ResolveLinuxBinding(ctx, strings.TrimSpace(agentID), cfg.ID)
		if err != nil {
			return LinuxChannelConfig{}, nil, nil, err
		}
		if !binding.Enabled {
			return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel %q is not enabled for agent %q", cfg.ID, agentID)
		}
		if err := validateLinuxBindingMode(binding); err != nil {
			return LinuxChannelConfig{}, nil, nil, err
		}
		if strings.EqualFold(strings.TrimSpace(binding.ApprovalMode), "deny") {
			return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel %q is denied by binding policy", cfg.ID)
		}
		if requireApproval && linuxBindingApprovalAction(binding) == policy.ActionRequireApproval && strings.TrimSpace(approvalID) == "" {
			return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel %q requires approval before transfer", cfg.ID)
		}
	}
	cred, err := p.resolver.ResolveLinuxCredential(ctx, cfg.CredentialID)
	if err != nil {
		return LinuxChannelConfig{}, nil, nil, err
	}
	if !cred.Enabled {
		return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux credential %q is disabled", cred.ID)
	}
	auth, agentConn, err := p.authMethod(ctx, cred)
	if err != nil {
		return LinuxChannelConfig{}, nil, agentConn, err
	}
	hostKey, err := p.hostKeyCallback(ctx, cfg)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return LinuxChannelConfig{}, nil, nil, err
	}
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel connect failed: %w", err)
	}
	clientConn, chans, requests, err := newSSHClientConnContext(ctx, conn, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         connectTimeout,
	})
	if err != nil {
		_ = conn.Close()
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return LinuxChannelConfig{}, nil, nil, fmt.Errorf("linux channel handshake failed: %w", err)
	}
	return cfg, ssh.NewClient(clientConn, chans, requests), agentConn, nil
}

// InspectRemoteProcess checks whether a remote process group is still alive
// without sending it a signal. It is used for restart-recovered jobs where
// Node can report remote state but must not silently mutate the process.
func (p *LinuxShellProvider) InspectRemoteProcess(ctx context.Context, agentID string, recovery RemoteProcessRecovery) (string, error) {
	return p.runRemoteRecovery(ctx, agentID, recovery, false)
}

// RecoverRemoteProcess terminates a remote process group only when the
// persisted PID file contains the exact Node-generated token. It returns a
// small status token and never returns remote command output.
func (p *LinuxShellProvider) RecoverRemoteProcess(ctx context.Context, agentID string, recovery RemoteProcessRecovery) (string, error) {
	return p.runRemoteRecovery(ctx, agentID, recovery, true)
}

func (p *LinuxShellProvider) runRemoteRecovery(ctx context.Context, agentID string, recovery RemoteProcessRecovery, terminate bool) (string, error) {
	if !isSafeRemoteJobToken(recovery.JobToken) || recovery.TargetID == "" || recovery.PIDFile != remoteJobPIDFile(recovery.JobToken) {
		return "", fmt.Errorf("remote recovery identity is invalid")
	}
	cfg, client, agentConn, err := p.openClientForRecovery(ctx, recovery.TargetID, agentID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	if agentConn != nil {
		defer agentConn.Close()
	}
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("remote recovery session failed: %w", err)
	}
	defer session.Close()
	steps := []string{
		"record=$(cat " + shellQuote(recovery.PIDFile) + " 2>/dev/null) || { printf 'missing'; exit 0; }",
		"pid=${record%%|*}",
		"token=${record#*|}",
		"case \"$pid\" in ''|*[!0-9]*) printf 'invalid_pid'; exit 0;; esac",
		"if [ \"$token\" != " + shellQuote(recovery.JobToken) + " ]; then printf 'token_mismatch'; exit 0; fi",
		"if ! kill -0 -\"$pid\" 2>/dev/null; then rm -f " + shellQuote(recovery.PIDFile) + "; printf 'not_running'; exit 0; fi",
	}
	if terminate {
		steps = append(steps,
			"if ! kill -TERM -\"$pid\" 2>/dev/null; then if ! kill -TERM \"$pid\" 2>/dev/null; then printf 'kill_failed'; exit 0; fi; fi",
			"sleep 1",
			"if kill -0 -\"$pid\" 2>/dev/null || kill -0 \"$pid\" 2>/dev/null; then if kill -KILL -\"$pid\" 2>/dev/null || kill -KILL \"$pid\" 2>/dev/null; then rm -f "+shellQuote(recovery.PIDFile)+"; printf 'force_terminated'; else printf 'force_failed'; fi; else rm -f "+shellQuote(recovery.PIDFile)+"; printf 'terminated'; fi",
		)
	} else {
		steps = append(steps, "printf 'running'")
	}
	script := strings.Join(steps, "; ")
	command, err := buildLinuxControlCommand(cfg, script)
	if err != nil {
		return "", err
	}
	output, err := session.CombinedOutput(command)
	status := strings.TrimSpace(string(output))
	if err != nil {
		return "", fmt.Errorf("remote recovery failed: %w", err)
	}
	switch status {
	case "missing":
		return "not_running", nil
	case "not_running", "terminated", "force_terminated", "running":
		return status, nil
	case "token_mismatch", "invalid_pid", "kill_failed", "force_failed":
		return "", fmt.Errorf("remote recovery returned %s", status)
	default:
		return "", fmt.Errorf("remote recovery returned an invalid status")
	}
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
	command, _, err := buildLinuxRemoteCommandWithMode(cfg, req)
	return command, err
}

func buildLinuxControlCommand(cfg LinuxChannelConfig, script string) (string, error) {
	shell := strings.TrimSpace(cfg.RemoteShell)
	if shell == "" {
		shell = "bash"
	}
	if strings.ContainsAny(shell, " \t\r\n;|&") {
		return "", fmt.Errorf("invalid remote shell %q", shell)
	}
	return shell + " -lc " + shellQuote(script), nil
}

func buildLinuxRemoteCommandWithMode(cfg LinuxChannelConfig, req ExecRequest) (string, string, error) {
	command, mode, _, err := buildLinuxRemoteCommandWithRecovery(cfg, req)
	return command, mode, err
}

func buildLinuxRemoteCommandWithRecovery(cfg LinuxChannelConfig, req ExecRequest) (string, string, RemoteProcessRecovery, error) {
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
				return "", "", RemoteProcessRecovery{}, fmt.Errorf("invalid remote environment variable %q", key)
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
		return "", "", RemoteProcessRecovery{}, fmt.Errorf("invalid remote shell %q", shell)
	}
	command := shell + " -lc " + shellQuote(inner)
	// Run the remote shell in a dedicated session/process group when setsid is
	// available. The wrapper has no stdout marker, keeps the command's exit
	// status, and turns a signal delivered to the session leader into a group
	// termination. The fallback preserves compatibility with minimal images;
	// callers must treat termination as unconfirmed in that mode.
	jobToken := strings.TrimSpace(req.Context.BackgroundJobID)
	if !isSafeRemoteJobToken(jobToken) {
		jobToken = fmt.Sprintf("process-%d", atomic.AddUint64(&linuxRemoteRecoverySequence, 1))
	}
	pidFile := remoteJobPIDFile(jobToken)
	recovery := RemoteProcessRecovery{TargetID: cfg.ID, JobToken: jobToken, PIDFile: pidFile}
	groupWrapper := strings.Join([]string{
		"printf '%s|%s\\n' \"$$\" " + shellQuote(jobToken) + " > " + shellQuote(pidFile) + " || exit 125",
		"trap 'trap - INT TERM HUP; kill -TERM -- -$$ 2>/dev/null; rm -f " + pidFile + "' INT TERM HUP",
		"trap 'rm -f " + pidFile + "' EXIT",
		command + " & child=$!",
		`wait "$child"`,
	}, "; ")
	fallbackWrapper := strings.Join([]string{
		"trap 'trap - INT TERM HUP; kill -TERM -- -$$ 2>/dev/null; kill -TERM \"$child\" 2>/dev/null; rm -f " + pidFile + "' INT TERM HUP",
		command + " & child=$!",
		`wait "$child"`,
	}, "; ")
	wrapped := strings.Join([]string{
		"printf '%s|%s\\n' \"$$\" " + shellQuote(jobToken) + " > " + shellQuote(pidFile) + " || exit 125",
		"trap 'rm -f " + pidFile + "' EXIT",
		"if command -v setsid >/dev/null 2>&1; then exec setsid sh -c " + shellQuote(groupWrapper) + "; else " + fallbackWrapper + "; fi",
	}, "; ")
	return wrapped, "setsid_or_fallback", recovery, nil
}

func isSafeRemoteJobToken(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func remoteJobPIDFile(jobToken string) string {
	return "/tmp/dagents-job-" + jobToken + ".pid"
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
