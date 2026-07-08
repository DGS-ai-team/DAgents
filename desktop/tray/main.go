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

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/events"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodectl"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/notify"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/pending"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/singleinstance"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/webui"
	"github.com/DGS-ai-team/DAgents/shared/config"
	"github.com/getlantern/systray"
)

//go:embed assets/icon.ico
var iconData []byte

//go:embed assets/icon_pending.ico
var iconPendingData []byte

const (
	ensureNodeTimeout    = 45 * time.Second
	probeInterval        = 3 * time.Second
	maxPendingMenuSlots  = 8
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

	app := &trayApp{
		cfg:          cfg,
		layout:       layout,
		nodeClient:   nodeclient.New(cfg.Local.Endpoint),
		pendingStore: pending.NewStore(),
		notifier:     notify.New(cfg.Local.Endpoint, iconData),
	}
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

	nodeClient   *nodeclient.Client
	pendingStore *pending.Store
	notifier     *notify.Notifier
	sseSub       *events.Subscriber
	sseCancel    context.CancelFunc

	pendingSessionIDs [maxPendingMenuSlots]string

	mStatus          *systray.MenuItem
	mPending         *systray.MenuItem
	mPendingSessions [maxPendingMenuSlots]*systray.MenuItem
	mOpenConsole     *systray.MenuItem
	mStart           *systray.MenuItem
	mStop            *systray.MenuItem
	mRestart         *systray.MenuItem
	mQuit            *systray.MenuItem
}

func (a *trayApp) onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("DAgents")
	a.refreshTooltip(false)

	a.mStatus = systray.AddMenuItem("状态：检测中…", "")
	a.mStatus.Disable()
	a.mPending = systray.AddMenuItem("待办：无", "有待办 HITL 或未读回复")
	a.mPending.Disable()
	for i := range a.mPendingSessions {
		a.mPendingSessions[i] = a.mPending.AddSubMenuItem("", "")
		a.mPendingSessions[i].Disable()
		a.mPendingSessions[i].Hide()
	}
	systray.AddSeparator()
	a.mOpenConsole = systray.AddMenuItem("打开控制台", "在浏览器中打开 Web UI")
	systray.AddSeparator()
	a.mStart = systray.AddMenuItem("启动 Node", "后台启动 dagents-node")
	a.mStop = systray.AddMenuItem("停止 Node", "停止 dagents-node")
	a.mRestart = systray.AddMenuItem("重启 Node", "重启 dagents-node")
	systray.AddSeparator()
	a.mQuit = systray.AddMenuItem("退出 Shell", "退出并停止 dagents-node")

	go a.ensureNodeOnStart()
	go a.pollLoop()
	go a.clickLoop()
	go a.pendingClickLoop()
	a.startSSESubscriber()
}

func (a *trayApp) startSSESubscriber() {
	ctx, cancel := context.WithCancel(context.Background())
	a.sseCancel = cancel
	a.sseSub = events.NewSubscriber(a.nodeClient, a.pendingStore, func() {
		a.refreshPendingUI()
	})
	a.sseSub.Start(ctx)
}

func (a *trayApp) stopSSESubscriber() {
	if a.sseSub != nil {
		a.sseSub.Stop()
	}
	if a.sseCancel != nil {
		a.sseCancel()
	}
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
	a.stopSSESubscriber()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := nodectl.Stop(ctx, a.layout, a.cfg); err != nil {
		log.Printf("stop Node on shell exit: %v", err)
	}
}

func (a *trayApp) clickLoop() {
	for {
		select {
		case <-a.mOpenConsole.ClickedCh:
			a.openConsole()
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

func (a *trayApp) pendingClickLoop() {
	for {
		select {
		case <-a.mPending.ClickedCh:
			a.openFirstPendingSession()
		case <-a.mPendingSessions[0].ClickedCh:
			a.openPendingSession(0)
		case <-a.mPendingSessions[1].ClickedCh:
			a.openPendingSession(1)
		case <-a.mPendingSessions[2].ClickedCh:
			a.openPendingSession(2)
		case <-a.mPendingSessions[3].ClickedCh:
			a.openPendingSession(3)
		case <-a.mPendingSessions[4].ClickedCh:
			a.openPendingSession(4)
		case <-a.mPendingSessions[5].ClickedCh:
			a.openPendingSession(5)
		case <-a.mPendingSessions[6].ClickedCh:
			a.openPendingSession(6)
		case <-a.mPendingSessions[7].ClickedCh:
			a.openPendingSession(7)
		}
	}
}

func (a *trayApp) openConsole() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
		defer cancel()
		if err := webui.OpenConsole(ctx, a.layout, a.cfg); err != nil {
			log.Printf("open console: %v", err)
		}
	}()
}

func (a *trayApp) openFirstPendingSession() {
	entries := a.pendingStore.Entries()
	if len(entries) == 0 {
		a.openConsole()
		return
	}
	a.openSession(entries[0].SessionID)
}

func (a *trayApp) openPendingSession(slot int) {
	if slot < 0 || slot >= maxPendingMenuSlots {
		return
	}
	sessionID := a.pendingSessionIDs[slot]
	if sessionID == "" {
		return
	}
	a.openSession(sessionID)
}

func (a *trayApp) openSession(sessionID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
		defer cancel()
		if err := webui.EnsureNodeAndOpen(ctx, a.layout, a.cfg, sessionID); err != nil {
			log.Printf("open session %s: %v", sessionID, err)
			return
		}
		a.refreshPendingUI()
	}()
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
	a.refreshPendingUI()
}

func (a *trayApp) refreshPendingUI() {
	if a.mPending == nil || a.pendingStore == nil {
		return
	}
	entries := a.pendingStore.Entries()
	sum := a.pendingStore.Summary()

	if sum.SessionCount == 0 {
		a.mPending.SetTitle("待办：无")
		a.mPending.Disable()
		systray.SetIcon(iconData)
		systray.SetTitle("DAgents")
	} else {
		a.mPending.SetTitle("待办：" + sum.Label)
		a.mPending.Enable()
		if len(iconPendingData) > 0 {
			systray.SetIcon(iconPendingData)
		}
		systray.SetTitle("●")
	}

	for i := range a.mPendingSessions {
		a.mPendingSessions[i].Hide()
		a.mPendingSessions[i].Disable()
		a.pendingSessionIDs[i] = ""
	}
	limit := len(entries)
	if limit > maxPendingMenuSlots {
		limit = maxPendingMenuSlots
	}
	for i := 0; i < limit; i++ {
		e := entries[i]
		a.pendingSessionIDs[i] = e.SessionID
		a.mPendingSessions[i].SetTitle("打开 · " + e.SummaryLabel())
		a.mPendingSessions[i].Enable()
		a.mPendingSessions[i].Show()
	}

	if a.notifier != nil {
		a.notifier.Sync(entries)
	}
}

func (a *trayApp) refreshTooltip(showRunning bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	base := fmt.Sprintf("DAgents Shell @ %s", a.layout.Home)
	if a.pendingStore != nil {
		if sum := a.pendingStore.Summary(); sum.SessionCount > 0 {
			base += "\n" + sum.Label
		}
	}
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
