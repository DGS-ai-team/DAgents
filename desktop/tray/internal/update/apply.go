package update

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodectl"
	"github.com/DGS-ai-team/DAgents/shared/config"
	sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"
)

const (
	ExitUpToDate         = 3
	exitNodeBusy         = 4
	defaultApplyTimeout  = 20 * time.Minute
	defaultNodeStopWait  = 30 * time.Second
	defaultNodeStartWait = 45 * time.Second
)

// ApplyOptions 控制 Shell apply orchestration（F-U6）。
type ApplyOptions struct {
	CheckOnly bool
	Force     bool
	Out       io.Writer
	ErrOut    io.Writer
}

// ApplyResult 为 check/apply 结果摘要。
type ApplyResult struct {
	Status  sharedupdate.Status
	Message string
}

// Applier 执行 upgrade-readiness → 下载 → stop Node → 覆盖 bin → start Node。
type Applier struct {
	cfg        *config.Config
	layout     *nodectl.Layout
	checker    *Checker
	nodeClient *nodeclient.Client
	httpClient *http.Client

	mu sync.Mutex
}

// NewApplier 构造 Shell update orchestrator。
func NewApplier(cfg *config.Config, layout *nodectl.Layout, checker *Checker, nodeClient *nodeclient.Client) *Applier {
	return &Applier{
		cfg:        cfg,
		layout:     layout,
		checker:    checker,
		nodeClient: nodeClient,
		httpClient: &http.Client{Timeout: 15 * time.Minute},
	}
}

// Run 执行 --check 或完整 apply。
func (a *Applier) Run(ctx context.Context, opt ApplyOptions) (ApplyResult, int) {
	if a == nil || a.cfg == nil || a.layout == nil || a.checker == nil {
		return ApplyResult{}, 1
	}
	a.checker.checkOnce()
	status := a.checker.Snapshot()
	result := ApplyResult{Status: status, Message: status.Message}

	if opt.CheckOnly {
		a.printStatus(opt, status)
		if !status.ManageReachable {
			return result, 1
		}
		if !status.UpgradeAvailable {
			return result, ExitUpToDate
		}
		return result, 0
	}

	if !status.ManageReachable {
		fmt.Fprintln(optErr(opt), "update: Manage 不可达，无法升级")
		return result, 1
	}
	if !status.UpgradeAvailable {
		fmt.Fprintln(optOut(opt), "当前已是最新版本")
		return result, ExitUpToDate
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if ready, code, msg := a.ensureUpgradeReady(ctx); !ready {
		fmt.Fprintln(optErr(opt), msg)
		return result, code
	}

	if !opt.Force {
		ok, err := confirmUpgrade(opt, status.LatestVersion)
		if err != nil {
			fmt.Fprintf(optErr(opt), "confirm failed: %v\n", err)
			return result, 1
		}
		if !ok {
			fmt.Fprintln(optOut(opt), "已取消")
			return result, 1
		}
	}

	pkgPath, err := a.downloadPackage(ctx, status)
	if err != nil {
		fmt.Fprintf(optErr(opt), "download failed: %v\n", err)
		return result, 1
	}
	defer os.Remove(pkgPath)

	stopCtx, cancelStop := context.WithTimeout(ctx, defaultNodeStopWait)
	defer cancelStop()
	if err := nodectl.Stop(stopCtx, a.layout, a.cfg); err != nil {
		fmt.Fprintf(optErr(opt), "stop node failed: %v\n", err)
		return result, 1
	}

	if err := installReleasePackage(a.layout.Home, pkgPath); err != nil {
		fmt.Fprintf(optErr(opt), "install failed: %v\n", err)
		_ = nodectl.Start(context.Background(), a.layout, a.cfg, defaultNodeStartWait)
		return result, 1
	}

	startCtx, cancelStart := context.WithTimeout(ctx, defaultNodeStartWait)
	defer cancelStart()
	if err := nodectl.Start(startCtx, a.layout, a.cfg, defaultNodeStartWait); err != nil {
		fmt.Fprintf(optErr(opt), "start node failed: %v\n", err)
		return result, 1
	}

	a.checker.checkOnce()
	result.Status = a.checker.Snapshot()
	result.Message = fmt.Sprintf("已升级到 %s", status.LatestVersion)
	fmt.Fprintf(optOut(opt), "update complete: %s\n", status.LatestVersion)
	return result, 0
}

func (a *Applier) ensureUpgradeReady(ctx context.Context) (ready bool, exitCode int, message string) {
	if a.nodeClient == nil {
		return true, 0, ""
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	readiness, err := a.nodeClient.UpgradeReadiness(checkCtx)
	if err != nil {
		return true, 0, ""
	}
	if readiness.Ready {
		return true, 0, ""
	}
	if readiness.HasActiveTurn {
		return false, exitNodeBusy, fmt.Sprintf("Node 忙碌（%d 个活跃 turn），请稍后再试", readiness.ActiveTurnCount)
	}
	return false, exitNodeBusy, "Node 未就绪，暂无法升级"
}

func (a *Applier) downloadPackage(ctx context.Context, status sharedupdate.Status) (string, error) {
	if status.Asset == nil {
		return "", fmt.Errorf("update response missing asset")
	}
	downloadURL, _ := status.Asset["download_url"].(string)
	downloadURL = strings.TrimSpace(downloadURL)
	if downloadURL == "" {
		return "", fmt.Errorf("update response missing download_url")
	}
	expectedSHA, _ := status.Asset["sha256"].(string)
	runtimeDir := strings.TrimRight(a.layout.Home, `\`) + string(os.PathSeparator) + ".runtime"
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return "", err
	}
	pkgPath := fmt.Sprintf("%s%s%d.pkg", runtimeDir, string(os.PathSeparator), time.Now().UnixNano())
	if err := sharedupdate.DownloadPackage(ctx, sharedupdate.DownloadRequest{
		URL:            downloadURL,
		DestPath:       pkgPath,
		ExpectedSHA256: expectedSHA,
		AgentID:        a.cfg.AgentID,
		NodeToken:      a.cfg.Manage.NodeToken,
		Client:         a.httpClient,
	}); err != nil {
		return "", err
	}
	return pkgPath, nil
}

func (a *Applier) printStatus(opt ApplyOptions, status sharedupdate.Status) {
	out := optOut(opt)
	fmt.Fprintf(out, "当前版本: %s\n", status.CurrentVersion)
	fmt.Fprintf(out, "最新版本: %s\n", status.LatestVersion)
	fmt.Fprintf(out, "平台: %s  渠道: %s\n", status.Platform, status.Channel)
	if status.ManageReachable {
		fmt.Fprintln(out, "Manage: 可达")
	} else {
		fmt.Fprintln(out, "Manage: 不可达")
	}
	if msg := strings.TrimSpace(status.Message); msg != "" {
		fmt.Fprintln(out, msg)
	}
}

func confirmUpgrade(opt ApplyOptions, latest string) (bool, error) {
	fmt.Fprintf(optOut(opt), "升级到 %s？ [y/N] ", strings.TrimSpace(latest))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}

func optOut(opt ApplyOptions) io.Writer {
	if opt.Out != nil {
		return opt.Out
	}
	return os.Stdout
}

func optErr(opt ApplyOptions) io.Writer {
	if opt.ErrOut != nil {
		return opt.ErrOut
	}
	return os.Stderr
}
