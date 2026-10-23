package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const browserServiceStartupTimeout = 30 * time.Second

// ServiceProcess is a browser sidecar process started and owned by this Node.
// The temporary config contains only effective browser settings, never Node secrets.
type ServiceProcess struct {
	cmd        *exec.Cmd
	endpoint   string
	configPath string
	logPath    string
	logFile    *os.File
	done       chan struct{}
	once       sync.Once
}

func (p *ServiceProcess) Endpoint() string {
	if p == nil {
		return ""
	}
	return p.endpoint
}

// Close stops only the sidecar process launched by this Node instance.
func (p *ServiceProcess) Close() error {
	if p == nil {
		return nil
	}
	var closeErr error
	p.once.Do(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			select {
			case <-p.done:
			default:
				if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !processAlreadyFinished(p.done) {
					closeErr = err
				}
			}
			if p.done != nil {
				select {
				case <-p.done:
				case <-time.After(3 * time.Second):
					if closeErr == nil {
						closeErr = fmt.Errorf("timed out waiting for browser sidecar to exit")
					}
				}
			}
		}
		if p.configPath != "" {
			_ = os.Remove(p.configPath)
		}
		if p.logFile != nil {
			_ = p.logFile.Close()
		}
	})
	return closeErr
}

func processAlreadyFinished(done <-chan struct{}) bool {
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// NewManagerWithAutoStart attaches to a healthy local service or starts the
// bundled sidecar at the fixed loopback address owned by this Node.
func NewManagerWithAutoStart(cfg *config.Config) (*Manager, *ServiceProcess, error) {
	if cfg == nil || !cfg.BrowserEnabled() {
		manager, err := NewManager(cfg, nil)
		return manager, nil, err
	}
	address, endpoint, parseErr := localBrowserServiceAddress(cfg.Browser.ServiceURL)
	if parseErr != nil {
		manager, err := NewManager(cfg, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("%w; automatic startup is available only for a local HTTP browser service: %v", err, parseErr)
		}
		return manager, nil, nil
	}
	if browserServiceHealthy(endpoint, 500*time.Millisecond) {
		manager, err := NewManager(cfg, nil)
		return manager, nil, err
	}
	pingErr := fmt.Errorf("browser service is not responding at %s", endpoint)
	if listener, dialErr := net.DialTimeout("tcp", address, 250*time.Millisecond); dialErr == nil {
		_ = listener.Close()
		return nil, nil, fmt.Errorf("%s already has a listener but it does not answer the Browser health check", endpoint)
	}
	process, startErr := startServiceProcess(cfg, address, endpoint)
	if startErr != nil {
		return nil, nil, fmt.Errorf("browser service is not responding at %s; could not start the bundled dagents-browser service: %v", endpoint, startErr)
	}
	if waitErr := waitForBrowserService(process.done, endpoint, browserServiceStartupTimeout); waitErr != nil {
		_ = process.Close()
		return nil, nil, fmt.Errorf("%v; bundled dagents-browser did not become ready: %v (log: %s)", pingErr, waitErr, process.logPath)
	}
	manager, err := NewManager(cfg, nil)
	if err != nil {
		_ = process.Close()
		return nil, nil, fmt.Errorf("browser service started but Node could not connect: %w", err)
	}
	return manager, process, nil
}

type browserServiceSettings struct {
	RuntimeRoot string `json:"runtime_root"`
	Browser     struct {
		Headed            bool     `json:"headed"`
		ChromePath        string   `json:"chrome_path,omitempty"`
		IgnoreHTTPSErrors bool     `json:"ignore_https_errors"`
		DefaultTimeoutMS  int      `json:"default_timeout_ms"`
		OutputDir         string   `json:"output_dir"`
		MaxSessions       int      `json:"max_sessions"`
		AllowedURLSchemes []string `json:"allowed_url_schemes"`
	} `json:"browser"`
}

func browserSidecarSettings(cfg *config.Config) (browserServiceSettings, error) {
	if cfg == nil {
		return browserServiceSettings{}, fmt.Errorf("node config is nil")
	}
	runtimeRoot, err := filepath.Abs(cfg.RuntimeDir())
	if err != nil {
		return browserServiceSettings{}, fmt.Errorf("resolve runtime root: %w", err)
	}
	settings := browserServiceSettings{RuntimeRoot: runtimeRoot}
	settings.Browser.Headed = cfg.BrowserHeaded()
	settings.Browser.ChromePath = strings.TrimSpace(cfg.Browser.ChromePath)
	settings.Browser.IgnoreHTTPSErrors = cfg.Browser.BrowserIgnoreHTTPSErrors()
	settings.Browser.DefaultTimeoutMS = cfg.BrowserDefaultTimeoutMS()
	settings.Browser.OutputDir = cfg.BrowserOutputDir()
	settings.Browser.MaxSessions = cfg.BrowserMaxSessions()
	settings.Browser.AllowedURLSchemes = cfg.BrowserAllowedURLSchemes()
	return settings, nil
}

func localBrowserServiceAddress(raw string) (address, endpoint string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("invalid service URL: %w", err)
	}
	if u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", "", fmt.Errorf("service URL must be a base http:// loopback URL")
	}
	host := strings.TrimSpace(u.Hostname())
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	} else {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", "", fmt.Errorf("%q is not a loopback host", host)
		}
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	portNumber, parseErr := net.LookupPort("tcp", port)
	if parseErr != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", fmt.Errorf("invalid service port %q", port)
	}
	address = net.JoinHostPort(host, port)
	endpoint = "http://" + address
	return address, endpoint, nil
}

func startServiceProcess(cfg *config.Config, address, endpoint string) (*ServiceProcess, error) {
	executable, err := resolveBrowserExecutable()
	if err != nil {
		return nil, err
	}
	settings, err := browserSidecarSettings(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(settings.RuntimeRoot, "logs"), 0o700); err != nil {
		return nil, fmt.Errorf("create browser log directory: %w", err)
	}
	configFile, err := os.CreateTemp(settings.RuntimeRoot, ".dagents-browser-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("create browser runtime config: %w", err)
	}
	configPath := configFile.Name()
	cleanupConfig := true
	defer func() {
		_ = configFile.Close()
		if cleanupConfig {
			_ = os.Remove(configPath)
		}
	}()
	if err := configFile.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("protect browser runtime config: %w", err)
	}
	if err := json.NewEncoder(configFile).Encode(settings); err != nil {
		return nil, fmt.Errorf("write browser runtime config: %w", err)
	}
	logPath := filepath.Join(settings.RuntimeRoot, "logs", "browser-service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open browser service log: %w", err)
	}
	listen := address
	cmd := exec.Command(executable, "--config", configPath, "--listen", listen)
	cmd.Dir = settings.RuntimeRoot
	cmd.Stdout, cmd.Stderr = logFile, logFile
	configureBrowserCommand(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("launch %s: %w", filepath.Base(executable), err)
	}
	cleanupConfig = false
	process := &ServiceProcess{cmd: cmd, endpoint: endpoint, configPath: configPath, logPath: logPath, logFile: logFile, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(process.done)
		_ = os.Remove(configPath)
		_ = logFile.Close()
	}()
	return process, nil
}

func resolveBrowserExecutable() (string, error) {
	name := "dagents-browser"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if candidate, lookErr := exec.LookPath(name); lookErr == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("%s was not found beside dagents-node or on PATH", name)
}

func waitForBrowserService(done <-chan struct{}, endpoint string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if browserServiceHealthy(endpoint, 500*time.Millisecond) {
			return nil
		}
		select {
		case <-done:
			return fmt.Errorf("sidecar process exited before the health check succeeded")
		case <-deadline.C:
			return fmt.Errorf("health check timed out after %s", timeout)
		case <-ticker.C:
		}
	}
}

func browserServiceHealthy(endpoint string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/v2/browser/ping", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	var status struct {
		OK bool `json:"ok"`
	}
	return json.NewDecoder(resp.Body).Decode(&status) == nil && status.OK
}
