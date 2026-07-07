//go:build windows

package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodectl"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/singleinstance"
	"github.com/DGS-ai-team/DAgents/shared/config"
	"github.com/getlantern/systray"
)

//go:embed assets/icon.ico
var iconData []byte

const (
	ensureNodeTimeout = 45 * time.Second
	probeInterval     = 3 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("dagents-shell", flag.ExitOnError)
	configFlag := fs.String("config", "", "path to config.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfgPath, err := config.ResolveConfigPath(*configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-shell: %v\n", err)
		return 1
	}
	release, err := singleinstance.AcquireShell(cfgPath)
	if err == singleinstance.ErrAlreadyRunning {
		fmt.Fprintln(os.Stderr, "dagents-shell: another instance is already running")
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-shell: single instance: %v\n", err)
		return 1
	}
	defer release()

	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-shell: load config: %v\n", err)
		return 1
	}
	layout, err := nodectl.ResolveLayout(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-shell: %v\n", err)
		return 1
	}

	app := &trayApp{cfg: cfg, layout: layout}
	systray.Run(app.onReady, app.onExit)
	return 0
}

type trayApp struct {
	cfg    *config.Config
	layout *nodectl.Layout

	mu          sync.Mutex
	lastHealth  *nodectl.Health
	lastGood    *nodectl.Health
	lastErr     error
	holdStopped bool
	recovering  bool
	sup         supervisor

	mStatus  *systray.MenuItem
	mStart   *systray.MenuItem
	mStop    *systray.MenuItem
	mRestart *systray.MenuItem
	mQuit    *systray.MenuItem
}

func (a *trayApp) onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("DAgents")
	a.refreshTooltip(false)

	a.mStatus = systray.AddMenuItem("状态：检测中…", "")
	a.mStatus.Disable()
	systray.AddSeparator()
	a.mStart = systray.AddMenuItem("启动 Node", "后台启动 dagents-node")
	a.mStop = systray.AddMenuItem("停止 Node", "停止 dagents-node")
	a.mRestart = systray.AddMenuItem("重启 Node", "重启 dagents-node")
	systray.AddSeparator()
	a.mQuit = systray.AddMenuItem("退出 Shell", "退出并停止 dagents-node")

	go a.ensureNodeOnStart()
	go a.pollLoop()
	go a.clickLoop()
}

func (a *trayApp) ensureNodeOnStart() {
	ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
	defer cancel()
	if err := nodectl.Start(ctx, a.layout, a.cfg, 30*time.Second); err != nil {
		log.Printf("ensure Node on start: %v", err)
		a.mu.Lock()
		a.lastErr = err
		a.mu.Unlock()
	}
	a.refreshStatus()
}

func (a *trayApp) onExit() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := nodectl.Stop(ctx, a.layout, a.cfg); err != nil {
		log.Printf("stop Node on shell exit: %v", err)
	}
}

func (a *trayApp) clickLoop() {
	for {
		select {
		case <-a.mStart.ClickedCh:
			a.setHoldStopped(false)
			a.runAction("启动", func(ctx context.Context) error {
				return nodectl.Start(ctx, a.layout, a.cfg, 30*time.Second)
			})
		case <-a.mStop.ClickedCh:
			a.setHoldStopped(true)
			a.runAction("停止", func(ctx context.Context) error {
				return nodectl.Stop(ctx, a.layout, a.cfg)
			})
		case <-a.mRestart.ClickedCh:
			a.setHoldStopped(false)
			a.runAction("重启", func(ctx context.Context) error {
				return nodectl.Restart(ctx, a.layout, a.cfg, 30*time.Second)
			})
		case <-a.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func (a *trayApp) setHoldStopped(v bool) {
	a.mu.Lock()
	a.holdStopped = v
	a.mu.Unlock()
}

func (a *trayApp) runAction(label string, fn func(context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
		defer cancel()
		if err := fn(ctx); err != nil {
			log.Printf("%s Node 失败: %v", label, err)
			a.mu.Lock()
			a.lastErr = err
			a.mu.Unlock()
		} else {
			a.mu.Lock()
			a.lastErr = nil
			switch label {
			case "启动", "重启":
				a.sup.resetAfterManualStart()
			case "停止":
				a.sup.resetAfterManualStop()
			}
			a.mu.Unlock()
		}
		a.refreshStatus()
	}()
}

func (a *trayApp) pollLoop() {
	a.refreshStatus()
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for range ticker.C {
		a.refreshStatus()
		a.maybeRecoverNode()
	}
}

func (a *trayApp) maybeRecoverNode() {
	now := time.Now()
	a.mu.Lock()
	if !a.sup.shouldRecover(now, a.holdStopped, a.recovering) {
		a.mu.Unlock()
		return
	}
	a.sup.markRecoverAttempt(now)
	a.recovering = true
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.recovering = false
			a.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
		defer cancel()
		if err := nodectl.Start(ctx, a.layout, a.cfg, 30*time.Second); err != nil {
			log.Printf("supervisor restart Node: %v", err)
			a.mu.Lock()
			a.sup.recordRecoverFail()
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		a.sup.recordRecoverOK()
		a.mu.Unlock()
		a.refreshStatus()
	}()
}

func (a *trayApp) refreshStatus() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	health, err := nodectl.Probe(ctx, a.cfg, nil)
	probeOK := err == nil && health != nil && health.OK

	a.mu.Lock()
	a.lastHealth = health
	if probeOK {
		a.lastGood = health
	}
	if err != nil {
		a.lastErr = err
	} else {
		a.lastErr = nil
	}
	if probeOK {
		a.sup.recordProbeOK()
	} else {
		a.sup.recordProbeFail()
	}
	showRunning := a.sup.showRunning()
	displayHealth := a.lastGood
	a.mu.Unlock()

	if showRunning {
		agentID := "…"
		if displayHealth != nil && displayHealth.AgentID != "" {
			agentID = displayHealth.AgentID
		}
		a.mStatus.SetTitle(fmt.Sprintf("状态：运行中 (%s)", agentID))
		a.mStart.Disable()
		a.mStop.Enable()
		a.mRestart.Enable()
	} else {
		a.mStatus.SetTitle("状态：未运行")
		a.mStart.Enable()
		a.mStop.Disable()
		a.mRestart.Disable()
	}
	a.refreshTooltip(showRunning)
}

func (a *trayApp) refreshTooltip(showRunning bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	base := fmt.Sprintf("DAgents Shell @ %s", a.layout.Home)
	if showRunning && a.lastGood != nil {
		systray.SetTooltip(fmt.Sprintf("%s\nNode 运行中 · %s · %s", base, a.lastGood.AgentID, a.lastGood.Version))
		return
	}
	if a.lastErr != nil {
		systray.SetTooltip(fmt.Sprintf("%s\nNode 未运行 · %v", base, a.lastErr))
		return
	}
	systray.SetTooltip(base + "\nNode 未运行")
}
