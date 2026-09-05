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
	"sync/atomic"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/desktopapi"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/events"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodectl"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/notify"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/pending"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/shelllog"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/singleinstance"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/uifocus"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/update"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/webui"
	"github.com/DGS-ai-team/DAgents/shared/config"
	sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"
	"github.com/getlantern/systray"
)

//go:embed assets/icon.ico
var iconData []byte

//go:embed assets/icon_pending.ico
var iconPendingData []byte

const (
	ensureNodeTimeout   = 45 * time.Second
	probeInterval       = 3 * time.Second
	maxPendingMenuSlots = 8
	iconBlinkInterval   = 600 * time.Millisecond
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("dagents-shell", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configFlag := fs.String("config", "", "path to config.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "update" {
		cfgPath, err := config.ResolveConfigPath(*configFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dagents-shell: %v\n", err)
			return 1
		}
		layout, err := nodectl.ResolveLayout(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dagents-shell: %v\n", err)
			return 1
		}
		return runUpdateCommand(rest[1:], desktopapi.ReadBridgeToken(layout.DesktopBridgeTokenFile))
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
	bridgeToken, err := desktopapi.EnsureBridgeToken(layout.DesktopBridgeTokenFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dagents-shell: desktop bridge: %v\n", err)
		return 1
	}
	_ = os.Setenv(desktopapi.BridgeURLEnv, "http://"+desktopapi.DefaultListenAddr)
	if closer, err := shelllog.Setup(layout.Home); err != nil {
		fmt.Fprintf(os.Stderr, "dagents-shell: shell log: %v\n", err)
	} else if closer != nil {
		defer closer.Close()
	}

	app := &trayApp{
		cfg:                cfg,
		layout:             layout,
		desktopBridgeToken: bridgeToken,
		nodeClient:         nodeclient.New(cfg.Local.Endpoint),
		pendingStore:       pending.NewStore(),
		notifier:           notify.New(cfg.Local.Endpoint, iconData),
	}
	systray.Run(app.onReady, app.onExit)
	return 0
}

type trayApp struct {
	cfg                *config.Config
	layout             *nodectl.Layout
	desktopBridgeToken string

	mu          sync.Mutex
	lastHealth  *nodectl.Health
	lastGood    *nodectl.Health
	lastErr     error
	holdStopped bool
	recovering  bool
	sup         supervisor

	nodeClient    *nodeclient.Client
	pendingStore  *pending.Store
	notifier      *notify.Notifier
	updateChecker *update.Checker
	updateApplier *update.Applier
	desktopAPI    *desktopapi.Server
	uiFocus       *uifocus.Store
	bgCancel      context.CancelFunc
	sseSub        *events.Subscriber
	sseCancel     context.CancelFunc

	pendingAgentIDs [maxPendingMenuSlots]string

	mStatus          *systray.MenuItem
	mPending         *systray.MenuItem
	mPendingSessions [maxPendingMenuSlots]*systray.MenuItem
	mOpenConsole     *systray.MenuItem
	mOpenManage      *systray.MenuItem
	mUpdate          *systray.MenuItem
	mStart           *systray.MenuItem
	mStop            *systray.MenuItem
	mRestart         *systray.MenuItem
	mQuit            *systray.MenuItem

	iconBlinkMu   sync.Mutex
	iconBlinkStop chan struct{}

	webuiOpenInFlight   atomic.Bool
	updateCheckInFlight atomic.Bool
	lifecycleInFlight   atomic.Bool
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
	a.mOpenManage = systray.AddMenuItem("打开 Manage", "用本机 node_id 打开 Manage Console")
	a.mOpenManage.Disable()
	a.mUpdate = systray.AddMenuItem("更新：检查中…", "查看版本与升级")
	a.mUpdate.Disable()
	systray.AddSeparator()
	a.mStart = systray.AddMenuItem("启动 Node", "后台启动 dagents-node")
	a.mStop = systray.AddMenuItem("停止 Node", "停止 dagents-node")
	a.mRestart = systray.AddMenuItem("重启 Node", "重启 dagents-node")
	a.mStart.Disable()
	a.mStop.Disable()
	a.mRestart.Disable()
	systray.AddSeparator()
	a.mQuit = systray.AddMenuItem("退出 Shell", "退出并停止 dagents-node")

	go a.ensureNodeOnStart()
	go a.pollLoop()
	go a.clickLoop()
	go a.pendingClickLoop()
	a.startBackgroundServices()
	a.startSSESubscriber()
}

func (a *trayApp) startBackgroundServices() {
	bgCtx, cancel := context.WithCancel(context.Background())
	a.bgCancel = cancel
	a.updateChecker = update.NewChecker(a.cfg, a.layout.Home, nil)
	a.updateChecker.SetUpgradeCallback(func(status sharedupdate.Status) {
		if a.notifier != nil {
			if err := a.notifier.PushUpdateAvailable(a.cfg.Local.Endpoint, status.LatestVersion); err != nil {
				log.Printf("update toast: %v", err)
			}
		}
		a.refreshUpdateUI()
	})
	a.updateApplier = update.NewApplier(a.cfg, a.layout, a.updateChecker, a.nodeClient)
	a.uiFocus = uifocus.NewStore()
	a.desktopAPI = desktopapi.New(a.updateChecker, a.updateApplier, a.uiFocus, a.desktopBridgeToken)
	go a.updateChecker.Start(bgCtx)
	go a.desktopAPI.Start(bgCtx)
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
	if !a.lifecycleInFlight.CompareAndSwap(false, true) {
		return
	}
	defer func() {
		a.lifecycleInFlight.Store(false)
		a.refreshStatus()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
	defer cancel()
	if err := nodectl.Start(ctx, a.layout, a.cfg, 30*time.Second); err != nil {
		log.Printf("ensure Node on start: %v", err)
		a.mu.Lock()
		a.lastErr = err
		a.mu.Unlock()
	}
}

func (a *trayApp) onExit() {
	a.stopIconBlink()
	if a.bgCancel != nil {
		a.bgCancel()
	}
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
		case <-a.mOpenManage.ClickedCh:
			a.openManage()
		case <-a.mUpdate.ClickedCh:
			if a.updateChecker.Snapshot().UpgradeAvailable {
				a.openUpdateSettings()
			} else {
				a.checkUpdateNow()
			}
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
			a.openPendingAgent(0)
		case <-a.mPendingSessions[1].ClickedCh:
			a.openPendingAgent(1)
		case <-a.mPendingSessions[2].ClickedCh:
			a.openPendingAgent(2)
		case <-a.mPendingSessions[3].ClickedCh:
			a.openPendingAgent(3)
		case <-a.mPendingSessions[4].ClickedCh:
			a.openPendingAgent(4)
		case <-a.mPendingSessions[5].ClickedCh:
			a.openPendingAgent(5)
		case <-a.mPendingSessions[6].ClickedCh:
			a.openPendingAgent(6)
		case <-a.mPendingSessions[7].ClickedCh:
			a.openPendingAgent(7)
		}
	}
}

func (a *trayApp) openConsole() {
	a.openWebUI(func(ctx context.Context) error {
		return webui.OpenConsole(ctx, a.layout, a.cfg)
	}, "open console")
}

func (a *trayApp) openManage() {
	if !a.lifecycleInFlight.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer func() {
			a.lifecycleInFlight.Store(false)
			a.refreshStatus()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
		defer cancel()
		if err := webui.OpenManage(ctx, a.layout, a.cfg); err != nil {
			log.Printf("open manage: %v", err)
			a.mu.Lock()
			a.lastErr = err
			a.mu.Unlock()
		}
	}()
}

func (a *trayApp) openUpdateSettings() {
	a.openWebUI(func(ctx context.Context) error {
		return webui.EnsureNodeAndOpenURL(ctx, a.layout, a.cfg, webui.SettingsAboutURL(a.cfg.Local.Endpoint))
	}, "open update settings")
}

func (a *trayApp) checkUpdateNow() {
	if !a.updateCheckInFlight.CompareAndSwap(false, true) {
		return
	}
	a.mUpdate.SetTitle("更新：检查中…")
	a.mUpdate.Disable()
	go func() {
		a.updateChecker.CheckNow()
		a.updateCheckInFlight.Store(false)
		a.refreshUpdateUI()
	}()
}

func (a *trayApp) refreshUpdateUI() {
	if a.mUpdate == nil || a.updateChecker == nil {
		return
	}
	status := a.updateChecker.Snapshot()
	if !status.ManageEnabled || !status.UpdateEnabled {
		a.mUpdate.SetTitle("更新：未启用")
		a.mUpdate.Disable()
		return
	}
	if status.LastCheckedAt == "" {
		a.mUpdate.SetTitle("更新：检查中…")
		a.mUpdate.Disable()
		return
	}
	if !status.ManageReachable {
		a.mUpdate.SetTitle("更新：检查失败（点击重试）")
		a.mUpdate.Enable()
		return
	}
	if status.UpgradeAvailable {
		a.mUpdate.SetTitle(fmt.Sprintf("更新：新版本 %s 可用", status.LatestVersion))
		a.mUpdate.Enable()
		return
	}
	a.mUpdate.SetTitle("更新：已是最新（点击重试）")
	a.mUpdate.Enable()
}

func (a *trayApp) openFirstPendingSession() {
	entries := a.pendingStore.Entries()
	if len(entries) == 0 {
		a.openConsole()
		return
	}
	a.openAgent(entries[0].AgentID)
}

func (a *trayApp) openPendingAgent(slot int) {
	if slot < 0 || slot >= maxPendingMenuSlots {
		return
	}
	agentID := a.pendingAgentIDs[slot]
	if agentID == "" {
		return
	}
	a.openAgent(agentID)
}

func (a *trayApp) openAgent(agentID string) {
	a.openWebUI(func(ctx context.Context) error {
		if err := webui.EnsureNodeAndOpen(ctx, a.layout, a.cfg, agentID); err != nil {
			return err
		}
		a.refreshPendingUI()
		return nil
	}, fmt.Sprintf("open agent %s", agentID))
}

func (a *trayApp) openWebUI(fn func(context.Context) error, label string) {
	if !a.webuiOpenInFlight.CompareAndSwap(false, true) {
		return
	}
	if !a.lifecycleInFlight.CompareAndSwap(false, true) {
		a.webuiOpenInFlight.Store(false)
		return
	}
	go func() {
		defer func() {
			a.webuiOpenInFlight.Store(false)
			a.lifecycleInFlight.Store(false)
			a.refreshStatus()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), ensureNodeTimeout)
		defer cancel()
		if err := fn(ctx); err != nil {
			log.Printf("%s: %v", label, err)
		}
	}()
}

func (a *trayApp) setHoldStopped(v bool) {
	a.mu.Lock()
	a.holdStopped = v
	a.mu.Unlock()
}

func (a *trayApp) runAction(label string, fn func(context.Context) error) {
	if !a.lifecycleInFlight.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer func() {
			a.lifecycleInFlight.Store(false)
			a.refreshStatus()
		}()
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
	if !a.lifecycleInFlight.CompareAndSwap(false, true) {
		return
	}
	now := time.Now()
	a.mu.Lock()
	if !a.sup.shouldRecover(now, a.holdStopped, a.recovering) {
		a.mu.Unlock()
		a.lifecycleInFlight.Store(false)
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
			a.lifecycleInFlight.Store(false)
			a.refreshStatus()
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
	busy := a.lifecycleInFlight.Load()

	if showRunning {
		agentID := "…"
		if displayHealth != nil && displayHealth.NodeID != "" {
			agentID = displayHealth.NodeID
		}
		a.mStatus.SetTitle(fmt.Sprintf("状态：运行中 (%s)", agentID))
		if busy {
			a.mStart.Disable()
			a.mStop.Disable()
			a.mRestart.Disable()
		} else {
			a.mStart.Disable()
			a.mStop.Enable()
			a.mRestart.Enable()
		}
	} else {
		a.mStatus.SetTitle("状态：未运行")
		if busy {
			a.mStart.Disable()
			a.mStop.Disable()
			a.mRestart.Disable()
		} else {
			a.mStart.Enable()
			a.mStop.Disable()
			a.mRestart.Disable()
		}
	}
	manageAvailable := false
	if showRunning && a.nodeClient != nil {
		infoCtx, infoCancel := context.WithTimeout(context.Background(), 2*time.Second)
		info, infoErr := a.nodeClient.AgentInfo(infoCtx)
		infoCancel()
		manageAvailable = infoErr == nil && info.ManageEnabled && info.ManageURL != ""
	}
	if manageAvailable {
		a.mOpenManage.Enable()
	} else {
		a.mOpenManage.Disable()
	}
	a.refreshTooltip(showRunning)
	a.refreshPendingUI()
	a.refreshUpdateUI()
}

func (a *trayApp) refreshPendingUI() {
	if a.mPending == nil || a.pendingStore == nil {
		return
	}
	entries := a.pendingStore.Entries()
	sum := a.pendingStore.Summary()

	if sum.AgentCount == 0 {
		a.mPending.SetTitle("待办：无")
		a.mPending.Disable()
		a.stopIconBlink()
		a.setTrayIconNormal()
	} else {
		a.mPending.SetTitle("待办：" + sum.Label)
		a.mPending.Enable()
		a.startIconBlink()
	}

	for i := range a.mPendingSessions {
		a.mPendingSessions[i].Hide()
		a.mPendingSessions[i].Disable()
		a.pendingAgentIDs[i] = ""
	}
	limit := len(entries)
	if limit > maxPendingMenuSlots {
		limit = maxPendingMenuSlots
	}
	for i := 0; i < limit; i++ {
		e := entries[i]
		a.pendingAgentIDs[i] = e.AgentID
		a.mPendingSessions[i].SetTitle("打开 · " + e.SummaryLabel())
		a.mPendingSessions[i].Enable()
		a.mPendingSessions[i].Show()
	}

	if a.notifier != nil {
		retainIDs := map[string]struct{}{}
		toastEntries := make([]pending.Entry, 0, len(entries))
		for _, e := range entries {
			if a.uiFocus != nil && a.uiFocus.IsFocused(e.AgentID) {
				retainIDs[e.AgentID] = struct{}{}
				continue
			}
			toastEntries = append(toastEntries, e)
		}
		a.notifier.Sync(toastEntries, retainIDs)
	}
}

func (a *trayApp) refreshTooltip(showRunning bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	base := fmt.Sprintf("DAgents Shell @ %s", a.layout.Home)
	if a.pendingStore != nil {
		if sum := a.pendingStore.Summary(); sum.AgentCount > 0 {
			base += "\n" + sum.Label
		}
	}
	if showRunning && a.lastGood != nil {
		systray.SetTooltip(fmt.Sprintf("%s\nNode 运行中 · %s · %s", base, a.lastGood.NodeID, a.lastGood.Version))
		return
	}
	if a.lastErr != nil {
		systray.SetTooltip(fmt.Sprintf("%s\nNode 未运行 · %v", base, a.lastErr))
		return
	}
	systray.SetTooltip(base + "\nNode 未运行")
}

func (a *trayApp) setTrayIconNormal() {
	systray.SetIcon(iconData)
	systray.SetTitle("DAgents")
}

func (a *trayApp) setTrayIconPendingFrame(showPending bool) {
	if showPending && len(iconPendingData) > 0 {
		systray.SetIcon(iconPendingData)
	} else {
		systray.SetIcon(iconData)
	}
	systray.SetTitle("●")
}

func (a *trayApp) stopIconBlink() {
	a.iconBlinkMu.Lock()
	defer a.iconBlinkMu.Unlock()
	if a.iconBlinkStop != nil {
		close(a.iconBlinkStop)
		a.iconBlinkStop = nil
	}
}

func (a *trayApp) startIconBlink() {
	a.iconBlinkMu.Lock()
	if a.iconBlinkStop != nil {
		a.iconBlinkMu.Unlock()
		return
	}
	stop := make(chan struct{})
	a.iconBlinkStop = stop
	a.iconBlinkMu.Unlock()

	go a.iconBlinkLoop(stop)
}

func (a *trayApp) iconBlinkLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(iconBlinkInterval)
	defer ticker.Stop()
	showPending := true
	a.setTrayIconPendingFrame(showPending)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			showPending = !showPending
			a.setTrayIconPendingFrame(showPending)
		}
	}
}
