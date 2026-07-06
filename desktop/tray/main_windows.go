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
	"github.com/DGS-ai-team/DAgents/shared/config"
	"github.com/getlantern/systray"
)

//go:embed assets/icon.ico
var iconData []byte

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("dagents-tray", flag.ExitOnError)
	configFlag := fs.String("config", "", "path to config.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfgPath, err := config.ResolveConfigPath(*configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-tray: %v\n", err)
		return 1
	}
	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-tray: load config: %v\n", err)
		return 1
	}
	layout, err := nodectl.ResolveLayout(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-tray: %v\n", err)
		return 1
	}

	app := &trayApp{cfg: cfg, layout: layout}
	systray.Run(app.onReady, app.onExit)
	return 0
}

type trayApp struct {
	cfg    *config.Config
	layout *nodectl.Layout

	mu         sync.Mutex
	lastHealth *nodectl.Health
	lastErr    error

	mStatus *systray.MenuItem
	mStart  *systray.MenuItem
	mStop   *systray.MenuItem
	mRestart *systray.MenuItem
	mQuit   *systray.MenuItem
}

func (a *trayApp) onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("DAgents")
	a.refreshTooltip()

	a.mStatus = systray.AddMenuItem("状态：检测中…", "")
	a.mStatus.Disable()
	systray.AddSeparator()
	a.mStart = systray.AddMenuItem("启动 Node", "后台启动 dagents-node")
	a.mStop = systray.AddMenuItem("停止 Node", "停止 dagents-node")
	a.mRestart = systray.AddMenuItem("重启 Node", "重启 dagents-node")
	systray.AddSeparator()
	a.mQuit = systray.AddMenuItem("退出托盘", "退出托盘程序（不停止 Node）")

	go a.pollLoop()
	go a.clickLoop()
}

func (a *trayApp) onExit() {}

func (a *trayApp) clickLoop() {
	for {
		select {
		case <-a.mStart.ClickedCh:
			a.runAction("启动", func(ctx context.Context) error {
				return nodectl.Start(ctx, a.layout, a.cfg, 30*time.Second)
			})
		case <-a.mStop.ClickedCh:
			a.runAction("停止", func(ctx context.Context) error {
				return nodectl.Stop(ctx, a.layout, a.cfg)
			})
		case <-a.mRestart.ClickedCh:
			a.runAction("重启", func(ctx context.Context) error {
				return nodectl.Restart(ctx, a.layout, a.cfg, 30*time.Second)
			})
		case <-a.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func (a *trayApp) runAction(label string, fn func(context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			log.Printf("%s Node 失败: %v", label, err)
			a.mu.Lock()
			a.lastErr = err
			a.mu.Unlock()
		} else {
			a.mu.Lock()
			a.lastErr = nil
			a.mu.Unlock()
		}
		a.refreshStatus()
	}()
}

func (a *trayApp) pollLoop() {
	a.refreshStatus()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.refreshStatus()
	}
}

func (a *trayApp) refreshStatus() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	health, err := nodectl.Probe(ctx, a.cfg, nil)
	a.mu.Lock()
	a.lastHealth = health
	if err != nil {
		a.lastErr = err
	} else {
		a.lastErr = nil
	}
	a.mu.Unlock()

	running := err == nil && health != nil && health.OK
	if running {
		a.mStatus.SetTitle(fmt.Sprintf("状态：运行中 (%s)", health.AgentID))
		a.mStart.Disable()
		a.mStop.Enable()
		a.mRestart.Enable()
	} else {
		a.mStatus.SetTitle("状态：未运行")
		a.mStart.Enable()
		a.mStop.Disable()
		a.mRestart.Disable()
	}
	a.refreshTooltip()
}

func (a *trayApp) refreshTooltip() {
	a.mu.Lock()
	defer a.mu.Unlock()

	base := fmt.Sprintf("DAgents @ %s", a.layout.Home)
	if a.lastHealth != nil && a.lastHealth.OK {
		systray.SetTooltip(fmt.Sprintf("%s\nNode 运行中 · %s · %s", base, a.lastHealth.AgentID, a.lastHealth.Version))
		return
	}
	if a.lastErr != nil {
		systray.SetTooltip(fmt.Sprintf("%s\nNode 未运行 · %v", base, a.lastErr))
		return
	}
	systray.SetTooltip(base + "\nNode 未运行")
}
