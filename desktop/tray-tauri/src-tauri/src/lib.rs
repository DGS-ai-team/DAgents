//! DAgents Shell — Tauri 系统托盘（替代 / 并行于 Go `desktop/tray`）。
//!
//! 核心能力：
//! - 启动时连接或启动 Node；退出 Desktop 不停止 Node
//! - 托盘菜单：状态 / 待办 / 更新 / 打开 DAgents / 启停重启 / 退出
//! - Desktop API、SSE 待办同步、Toast、Manage 更新检查
//! - 打开本机捆绑的 Desktop WebView；Node 仅提供服务 API
//! - 关闭窗口隐藏到托盘（不停 Node）
//! - health 轮询 + 基础自动恢复

macro_rules! safe_eprintln {
    ($($arg:tt)*) => {{
        diagnostics::stderr_best_effort(format_args!($($arg)*));
    }};
}

mod clipboard;
mod config;
mod desktopapi;
mod diagnostics;
mod events;
mod layout;
mod nodeclient;
mod nodectl;
mod notify;
mod pending;
mod screen_capture;
mod singleinstance;
mod terminal;
mod uifocus;
mod update;

use config::ShellConfig;
use layout::Layout;
use nodectl::Health;
use singleinstance::{acquire, InstanceError};
use std::collections::{HashMap, HashSet};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::tray::{MouseButton, TrayIconBuilder, TrayIconEvent};
use tauri::webview::WebviewWindowBuilder;
use tauri::{
    AppHandle, Emitter, Manager, RunEvent, State, Url, WebviewUrl, WebviewWindow, WindowEvent,
};

const ENSURE_TIMEOUT: Duration = Duration::from_secs(45);
const PROBE_INTERVAL: Duration = Duration::from_secs(3);
const WAIT_READY: Duration = Duration::from_secs(30);
const MAIN_WINDOW: &str = "desktop_mvp";
const MAX_PENDING_MENU_SLOTS: usize = 8;
const ICON_BLINK_INTERVAL: Duration = Duration::from_millis(600);

struct Shared {
    layout: Layout,
    cfg: Arc<ShellConfig>,
    node_client: Arc<nodeclient::Client>,
    pending_store: Arc<pending::Store>,
    notifier: Arc<notify::Notifier>,
    update_checker: Arc<update::Checker>,
    update_applier: Arc<update::Applier>,
    shell_stop: Arc<AtomicBool>,
    ui_focus: Arc<uifocus::Store>,
    sse_subscriber: Mutex<Option<events::Subscriber>>,
    hold_stopped: AtomicBool,
    node_running: AtomicBool,
    recovering: AtomicBool,
    lifecycle_busy: AtomicBool,
    blink_running: AtomicBool,
    update_check_in_flight: AtomicBool,
    last_err: Mutex<Option<String>>,
    status_item: Mutex<Option<MenuItem<tauri::Wry>>>,
    pending_submenu: Mutex<Option<Submenu<tauri::Wry>>>,
    pending_slots: Mutex<Vec<MenuItem<tauri::Wry>>>,
    pending_session_ids: Mutex<Vec<String>>,
    update_item: Mutex<Option<MenuItem<tauri::Wry>>>,
    open_manage_item: Mutex<Option<MenuItem<tauri::Wry>>>,
    start_item: Mutex<Option<MenuItem<tauri::Wry>>>,
    stop_item: Mutex<Option<MenuItem<tauri::Wry>>>,
    restart_item: Mutex<Option<MenuItem<tauri::Wry>>>,
    terminal_relays: Mutex<HashMap<String, terminal::RelayHandle>>,
}

struct LifecycleOperation {
    shared: Arc<Shared>,
    prior_hold_stopped: bool,
    keep_stopped: bool,
}

impl LifecycleOperation {
    fn begin(shared: Arc<Shared>) -> Result<Self, String> {
        shared
            .lifecycle_busy
            .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
            .map_err(|_| "Node 生命周期操作正在进行".to_string())?;
        let prior_hold_stopped = shared.hold_stopped.load(Ordering::SeqCst);
        Ok(Self {
            shared,
            prior_hold_stopped,
            keep_stopped: false,
        })
    }

    fn hold_node_stopped(&mut self) {
        self.shared.hold_stopped.store(true, Ordering::SeqCst);
        self.keep_stopped = true;
    }
}

impl Drop for LifecycleOperation {
    fn drop(&mut self) {
        if !self.keep_stopped {
            self.shared
                .hold_stopped
                .store(self.prior_hold_stopped, Ordering::SeqCst);
        }
        self.shared.lifecycle_busy.store(false, Ordering::SeqCst);
    }
}

fn _assert_shared_send_sync() {
    fn assert_send_sync<T: Send + Sync>() {}
    assert_send_sync::<Shared>();
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let layout = match resolve_layout_from_args() {
        Ok(l) => l,
        Err(e) => {
            safe_eprintln!("dagents-shell: {e}");
            std::process::exit(1);
        }
    };

    let cli_args = std::env::args().skip(1).collect::<Vec<_>>();
    if args_have_flag(&cli_args, "--quit-desktop") {
        match singleinstance::request_quit_and_wait(&layout, Duration::from_secs(30)) {
            Ok(()) => std::process::exit(0),
            Err(error) => {
                safe_eprintln!("dagents-shell: {error}");
                std::process::exit(1);
            }
        }
    }

    if args_have_flag(&cli_args, "--desktop-status") {
        match singleinstance::is_running(&layout) {
            Ok(running) => {
                diagnostics::stdout_line_best_effort(format_args!("{{\"running\":{running}}}"));
                std::process::exit(0);
            }
            Err(error) => {
                safe_eprintln!("dagents-shell: {error}");
                std::process::exit(1);
            }
        }
    }

    let cli_only = [
        "--node-status",
        "--owned-node-status",
        "--stop-owned-node",
        "--start-owned-node",
        "--update-status",
        "--update-download",
        "--update-apply",
    ]
    .iter()
    .any(|flag| args_have_flag(&cli_args, flag))
        || update_args_from_args().is_some();
    if !cli_only {
        if let Err(error) = notify::register_app_identity() {
            diagnostics::stderr_best_effort(format_args!(
                "register Windows notification identity: {error}"
            ));
        }
    }
    let guard = if cli_only {
        None
    } else {
        match acquire(&layout) {
            Ok(guard) => Some(guard),
            Err(InstanceError::AlreadyRunning) => match singleinstance::request_show(&layout) {
                Ok(true) | Ok(false) => std::process::exit(0),
                Err(error) => {
                    safe_eprintln!("dagents-shell: 无法唤醒现有 Desktop: {error}");
                    std::process::exit(1);
                }
            },
            Err(InstanceError::Other(error)) => {
                safe_eprintln!("dagents-shell: single instance: {error}");
                std::process::exit(1);
            }
        }
    };

    let _ = nodectl::ensure_runtime_dirs(&layout);

    let cfg = match ShellConfig::load_or_create(&layout.desktop_config_path) {
        Ok(c) => c,
        Err(e) => {
            safe_eprintln!("dagents-shell: {e}");
            std::process::exit(1);
        }
    };
    let cfg = Arc::new(cfg);

    if args_have_flag(&cli_args, "--node-status") {
        match nodectl::probe(&cfg) {
            Ok(health) => {
                diagnostics::stdout_line_best_effort(format_args!(
                    "{}",
                    serde_json::json!({
                        "online": health.ok,
                        "status": health.status,
                        "node_id": health.node_id,
                        "version": health.version,
                    })
                ));
                std::process::exit(if health.ok { 0 } else { 1 });
            }
            Err(_) => {
                diagnostics::stdout_line_best_effort(format_args!("{{\"online\":false}}"));
                std::process::exit(1);
            }
        }
    }

    if args_have_flag(&cli_args, "--owned-node-status") {
        match nodectl::owned_process_is_live(&layout) {
            Ok(owned_process) => {
                let endpoint_ready = nodectl::is_running(&cfg);
                let managed = owned_process && nodectl::is_managed(&layout, &cfg);
                diagnostics::stdout_line_best_effort(format_args!(
                    "{{\"owned_process\":{owned_process},\"endpoint_ready\":{endpoint_ready},\"managed\":{managed}}}"
                ));
                std::process::exit(0);
            }
            Err(error) => {
                safe_eprintln!("dagents-shell: {error}");
                std::process::exit(1);
            }
        }
    }

    if args_have_flag(&cli_args, "--stop-owned-node") {
        if args_have_flag(&cli_args, "--require-idle") {
            let owned_live = match nodectl::owned_process_is_live(&layout) {
                Ok(value) => value,
                Err(error) => {
                    safe_eprintln!("dagents-shell: {error}");
                    std::process::exit(1);
                }
            };
            let check_readiness =
                match require_idle_readiness_check(owned_live, nodectl::is_running(&cfg)) {
                    Ok(check) => check,
                    Err(error) => {
                        safe_eprintln!("dagents-shell: {error}");
                        std::process::exit(1);
                    }
                };
            if check_readiness {
                let readiness = nodeclient::Client::new(config::NODE_ENDPOINT).upgrade_readiness();
                match readiness {
                    Ok(readiness) if readiness.ready => {}
                    Ok(readiness) => {
                        let reason = if readiness.has_active_turn {
                            format!("{} 个活跃回合", readiness.active_turn_count)
                        } else {
                            "Node 尚未报告升级就绪".into()
                        };
                        safe_eprintln!("dagents-shell: Node 当前不可安全停止（{reason}）");
                        std::process::exit(1);
                    }
                    Err(error) => {
                        safe_eprintln!("dagents-shell: 无法确认 Node 升级就绪: {error}");
                        std::process::exit(1);
                    }
                }
                if !nodectl::is_managed(&layout, &cfg) {
                    safe_eprintln!(
                        "dagents-shell: 受监督 Node 与当前 endpoint 不匹配；不能安全升级或卸载"
                    );
                    std::process::exit(1);
                }
            }
        }
        match nodectl::stop(&layout, &cfg) {
            Ok(()) => std::process::exit(0),
            Err(error) => {
                safe_eprintln!("dagents-shell: {error}");
                std::process::exit(1);
            }
        }
    }

    if args_have_flag(&cli_args, "--start-owned-node") {
        match nodectl::start(&layout, &cfg, WAIT_READY) {
            Ok(()) => std::process::exit(0),
            Err(error) => {
                safe_eprintln!("dagents-shell: 启动受监督 Node 失败: {error}");
                std::process::exit(1);
            }
        }
    }

    if let Some(update_args) = update_args_from_args() {
        let code = desktopapi::run_update_command(&update_args, layout, Arc::clone(&cfg));
        std::process::exit(code);
    }

    let guard = guard.expect("GUI path acquires the Desktop instance guard");

    nodectl::append_shell_log(
        &layout,
        &format!(
            "shell start endpoint={} config={}",
            config::NODE_ENDPOINT,
            layout.config_path.display()
        ),
    );

    // 单实例锁保留在主线程栈上：Windows Mutex HANDLE 非 Send，不可放入 Arc 跨线程。
    let _instance_guard = guard;

    let node_client = Arc::new(nodeclient::Client::new(config::NODE_ENDPOINT));
    let pending_store = Arc::new(pending::Store::new());
    let notifier = Arc::new(notify::Notifier::new(cfg.notification_enabled));
    let update_checker = Arc::new(update::Checker::new(
        Arc::clone(&cfg),
        layout.home.clone(),
        Arc::clone(&node_client),
    ));
    let update_applier = Arc::new(update::Applier::new(
        layout.clone(),
        Arc::clone(&update_checker),
        Arc::clone(&node_client),
    ));
    let ui_focus = Arc::new(uifocus::Store::new());
    let shell_stop = Arc::new(AtomicBool::new(false));

    let shared = Arc::new(Shared {
        layout,
        cfg,
        node_client,
        pending_store,
        notifier,
        update_checker,
        update_applier,
        shell_stop,
        ui_focus,
        sse_subscriber: Mutex::new(None),
        hold_stopped: AtomicBool::new(false),
        node_running: AtomicBool::new(false),
        recovering: AtomicBool::new(false),
        lifecycle_busy: AtomicBool::new(false),
        blink_running: AtomicBool::new(false),
        update_check_in_flight: AtomicBool::new(false),
        last_err: Mutex::new(None),
        status_item: Mutex::new(None),
        pending_submenu: Mutex::new(None),
        pending_slots: Mutex::new(Vec::new()),
        pending_session_ids: Mutex::new(vec![String::new(); MAX_PENDING_MENU_SLOTS]),
        update_item: Mutex::new(None),
        open_manage_item: Mutex::new(None),
        start_item: Mutex::new(None),
        stop_item: Mutex::new(None),
        restart_item: Mutex::new(None),
        terminal_relays: Mutex::new(HashMap::new()),
    });

    let app = tauri::Builder::default()
        .manage(Arc::clone(&shared))
        .invoke_handler(tauri::generate_handler![
            desktop_node_status,
            desktop_settings_get,
            desktop_settings_save,
            desktop_quit,
            desktop_shell_request,
            desktop_node_request,
            desktop_workspace_binary_request,
            desktop_media_binary_request,
            desktop_capture_screenshot,
            desktop_list_agents,
            desktop_hydrate_agent,
            desktop_resume_hitl,
            desktop_terminal_list,
            desktop_linux_terminal_channels,
            desktop_terminal_open,
            desktop_terminal_command
        ])
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_dialog::init())
        .setup({
            let shared = Arc::clone(&shared);
            move |app| {
                let window = WebviewWindowBuilder::new(
                    app,
                    MAIN_WINDOW,
                    WebviewUrl::App("index.html".into()),
                )
                .title("DAgents Desktop")
                .inner_size(1180.0, 760.0)
                .min_inner_size(800.0, 600.0)
                .resizable(true)
                .decorations(false)
                .visible(true)
                .skip_taskbar(false)
                .icon(
                    tauri::image::Image::from_bytes(include_bytes!("../icons/icon.png"))
                        .map_err(|e| format!("加载窗口图标失败: {e}"))?,
                )?
                .on_navigation(is_desktop_mvp_navigation_allowed)
                .build()?;

                let win_for_close = window.clone();
                let close_shared = Arc::clone(&shared);
                window.on_window_event(move |event| {
                    if let WindowEvent::CloseRequested { api, .. } = event {
                        detach_all_terminal_relays(&close_shared);
                        let _ = win_for_close.hide();
                        let _ = win_for_close.set_skip_taskbar(true);
                        api.prevent_close();
                    }
                });

                let status =
                    MenuItem::with_id(app, "status", "状态：检测中…", false, None::<&str>)?;
                // 待办项挂在子菜单下；空闲槽不加入菜单，避免顶层出现空白行（对齐 Go tray Hide）。
                let mut pending_slots = Vec::with_capacity(MAX_PENDING_MENU_SLOTS);
                for i in 0..MAX_PENDING_MENU_SLOTS {
                    let item = MenuItem::with_id(
                        app,
                        format!("pending_{i}"),
                        "打开…",
                        false,
                        None::<&str>,
                    )?;
                    pending_slots.push(item);
                }
                let pending = Submenu::with_id(app, "pending", "待办：无", false)?;
                let open =
                    MenuItem::with_id(app, "open_desktop", "打开 DAgents", true, None::<&str>)?;
                let open_manage =
                    MenuItem::with_id(app, "open_manage", "打开 Manage", false, None::<&str>)?;
                let update =
                    MenuItem::with_id(app, "update", "更新：检查中…", false, None::<&str>)?;
                let start = MenuItem::with_id(app, "start", "启动 Node", true, None::<&str>)?;
                let stop = MenuItem::with_id(app, "stop", "停止 Node", false, None::<&str>)?;
                let restart = MenuItem::with_id(app, "restart", "重启 Node", false, None::<&str>)?;
                let quit = MenuItem::with_id(app, "quit", "退出 Desktop", true, None::<&str>)?;
                let sep1 = PredefinedMenuItem::separator(app)?;
                let sep2 = PredefinedMenuItem::separator(app)?;
                let sep3 = PredefinedMenuItem::separator(app)?;

                {
                    let mut slot = shared.status_item.lock().unwrap();
                    *slot = Some(status.clone());
                }
                {
                    let mut slot = shared.pending_submenu.lock().unwrap();
                    *slot = Some(pending.clone());
                }
                {
                    let mut slot = shared.pending_slots.lock().unwrap();
                    *slot = pending_slots.clone();
                }
                {
                    let mut slot = shared.update_item.lock().unwrap();
                    *slot = Some(update.clone());
                }
                {
                    let mut slot = shared.open_manage_item.lock().unwrap();
                    *slot = Some(open_manage.clone());
                }
                {
                    let mut slot = shared.start_item.lock().unwrap();
                    *slot = Some(start.clone());
                }
                {
                    let mut slot = shared.stop_item.lock().unwrap();
                    *slot = Some(stop.clone());
                }
                {
                    let mut slot = shared.restart_item.lock().unwrap();
                    *slot = Some(restart.clone());
                }

                let items: Vec<&dyn tauri::menu::IsMenuItem<tauri::Wry>> = vec![
                    &status,
                    &pending,
                    &sep1,
                    &open,
                    &open_manage,
                    &update,
                    &sep2,
                    &start,
                    &stop,
                    &restart,
                    &sep3,
                    &quit,
                ];
                let menu = Menu::with_items(app, &items)?;

                let tray_shared = Arc::clone(&shared);
                let menu_shared = Arc::clone(&shared);
                let tray_app = app.handle().clone();
                // 使用高分辨率 PNG，避免 Windows 从小尺寸 ICO 帧放大导致托盘/标题栏模糊。
                let icon = tauri::image::Image::from_bytes(include_bytes!("../icons/icon.png"))
                    .map_err(|e| format!("加载托盘图标失败: {e}"))?;

                // TrayIcon 在最后一次 drop 时会从系统托盘移除，必须托管到 App 状态以保持存活。
                let tray = TrayIconBuilder::with_id("main")
                    .icon(icon)
                    .menu(&menu)
                    .tooltip("DAgents")
                    .show_menu_on_left_click(true)
                    .on_menu_event(move |app, event| {
                        handle_menu(&menu_shared, app, event.id.as_ref());
                    })
                    .on_tray_icon_event(move |_tray, event| {
                        // 双击显示 Desktop；左键单击仍由 show_menu_on_left_click 弹出菜单。
                        if let TrayIconEvent::DoubleClick {
                            button: MouseButton::Left,
                            ..
                        } = event
                        {
                            open_desktop_mvp(&tray_shared, &tray_app, None);
                        }
                    })
                    .build(app)?;
                app.manage(tray);
                let bg_stop = Arc::clone(&shared.shell_stop);
                shared.update_checker.set_upgrade_callback({
                    let shared = Arc::clone(&shared);
                    move |status| {
                        if shared.cfg.notification_enabled {
                            let _ = shared
                                .notifier
                                .push_update_available(&status.latest_version);
                        }
                        refresh_update_ui(&shared);
                    }
                });

                // Desktop 管理固定 loopback Node 的启动、恢复与更新检查。
                let boot = Arc::clone(&shared);
                let app_handle = app.handle().clone();
                let checker_stop = Arc::clone(&bg_stop);
                thread::spawn(move || {
                    ensure_node(&boot);
                    boot.update_checker.start(checker_stop);
                    refresh_status(&boot);
                    refresh_update_ui(&boot);
                    refresh_pending_ui(&boot, &app_handle);
                    loop {
                        thread::sleep(PROBE_INTERVAL);
                        refresh_status(&boot);
                        refresh_update_ui(&boot);
                        refresh_pending_ui(&boot, &app_handle);
                        maybe_recover(&boot);
                    }
                });

                let sub = events::Subscriber::new(
                    Arc::clone(&shared.node_client),
                    Arc::clone(&shared.pending_store),
                    {
                        let shared = Arc::clone(&shared);
                        let app = app.handle().clone();
                        move || refresh_pending_ui(&shared, &app)
                    },
                )
                .with_event_handler({
                    let app = app.handle().clone();
                    move |event| {
                        if let Some(window) = app.get_webview_window("desktop_mvp") {
                            let _ = window.emit("desktop-node-event", event);
                        }
                    }
                });
                sub.start();
                if let Ok(mut guard) = shared.sse_subscriber.lock() {
                    *guard = Some(sub);
                }

                Ok(())
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application");
    singleinstance::start_control_listener(app.handle().clone(), &shared.layout);
    app.run({
        let shared = Arc::clone(&shared);
        move |_app, event| match event {
            // 隐藏主窗后勿因「无可见窗口」退出；菜单「退出」走 app.exit(code)。
            RunEvent::ExitRequested { api, code, .. } => {
                if code.is_none() {
                    api.prevent_exit();
                }
            }
            RunEvent::Exit => {
                shared.shell_stop.store(true, Ordering::SeqCst);
                if let Ok(mut sub) = shared.sse_subscriber.lock() {
                    if let Some(sub) = sub.take() {
                        sub.stop();
                    }
                }
            }
            _ => {}
        }
    });
}

#[derive(Debug, Default, PartialEq, Eq)]
struct LayoutArgs {
    home: Option<String>,
    config: Option<String>,
    desktop_config: Option<String>,
}

fn parse_layout_args(args: &[String]) -> Result<LayoutArgs, String> {
    let mut parsed = LayoutArgs::default();
    let mut i = 0;
    while i < args.len() {
        let arg = args[i].as_str();
        let (flag, value): (&str, Option<String>) = if arg == "--home" {
            ("--home", None)
        } else if arg == "--config" || arg == "-config" {
            ("--config", None)
        } else if arg == "--desktop-config" {
            ("--desktop-config", None)
        } else if let Some(value) = arg.strip_prefix("--home=") {
            ("--home", Some(value.to_string()))
        } else if let Some(value) = arg.strip_prefix("--config=") {
            parsed.config = Some(value.to_string());
            i += 1;
            continue;
        } else if let Some(value) = arg.strip_prefix("--desktop-config=") {
            parsed.desktop_config = Some(value.to_string());
            i += 1;
            continue;
        } else {
            i += 1;
            continue;
        };

        let value = match value {
            Some(value) => value,
            None => {
                let next = args.get(i + 1).map(String::as_str);
                if next.is_none() || next.is_some_and(is_option_argument) {
                    return Err(format!("{flag} 需要路径参数"));
                }
                let value = next.unwrap().to_string();
                i += 1;
                value
            }
        };
        if flag == "--home" && value.trim().is_empty() {
            return Err("--home 需要非空路径".into());
        }
        match flag {
            "--home" => parsed.home = Some(value),
            "--config" => parsed.config = Some(value),
            "--desktop-config" => parsed.desktop_config = Some(value),
            _ => unreachable!(),
        }
        i += 1;
    }
    Ok(parsed)
}

fn is_option_argument(arg: &str) -> bool {
    arg.starts_with('-')
}

fn resolve_layout_from_args() -> Result<Layout, String> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let parsed = parse_layout_args(&args)?;
    let mut config_flag = parsed.config;
    let mut desktop_config_flag = parsed.desktop_config;
    if config_flag.is_none() {
        config_flag = std::env::var("DAGENTS_CONFIG").ok();
    }
    if desktop_config_flag.is_none() {
        desktop_config_flag = std::env::var(layout::ENV_DESKTOP_CONFIG).ok();
    }
    Layout::resolve(
        parsed.home.as_deref(),
        config_flag.as_deref(),
        desktop_config_flag.as_deref(),
    )
}

fn update_args_from_args() -> Option<Vec<String>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut i = 0;
    while i < args.len() {
        let a = &args[i];
        if a == "--config" || a == "-config" || a == "--home" || a == "--desktop-config" {
            i += 2;
            continue;
        }
        if a.starts_with("--home=")
            || a.starts_with("--config=")
            || a.starts_with("--desktop-config=")
        {
            i += 1;
            continue;
        }
        if a == "update" {
            return Some(args[i + 1..].to_vec());
        }
        i += 1;
    }
    None
}

fn args_have_flag(args: &[String], flag: &str) -> bool {
    args.iter().any(|arg| arg == flag)
}

fn require_idle_readiness_check(
    owned_live: bool,
    endpoint_ready: bool,
) -> Result<bool, &'static str> {
    if !owned_live && endpoint_ready {
        return Err("当前 endpoint 为外部 Node，不能安全升级或卸载");
    }
    Ok(owned_live)
}

fn handle_menu(shared: &Arc<Shared>, app: &AppHandle, id: &str) {
    match id {
        "pending" => open_first_pending(shared, app),
        "open_desktop" => open_desktop_mvp(shared, app, None),
        "open_manage" => open_manage(shared),
        "update" => handle_update_menu(shared, app),
        "start" => {
            shared.hold_stopped.store(false, Ordering::SeqCst);
            run_action(shared, "启动", |s| {
                nodectl::start(&s.layout, &s.cfg, WAIT_READY)
            });
        }
        "stop" => {
            shared.hold_stopped.store(true, Ordering::SeqCst);
            run_action(shared, "停止", |s| nodectl::stop(&s.layout, &s.cfg));
        }
        "restart" => {
            shared.hold_stopped.store(false, Ordering::SeqCst);
            run_action(shared, "重启", |s| {
                nodectl::restart(&s.layout, &s.cfg, WAIT_READY)
            });
        }
        "quit" => {
            app.exit(0);
        }
        _ if id.starts_with("pending_") => {
            if let Ok(slot) = id.trim_start_matches("pending_").parse::<usize>() {
                open_pending_slot(shared, app, slot);
            }
        }
        _ => {}
    }
}

fn ensure_mvp_window(window: &WebviewWindow) -> Result<(), String> {
    if window.label() == "desktop_mvp" {
        Ok(())
    } else {
        Err("Desktop API 仅对 Desktop MVP 窗口开放".into())
    }
}

async fn spawn_node_blocking<T: Send + 'static>(
    task: impl FnOnce() -> T + Send + 'static,
) -> Result<T, String> {
    tauri::async_runtime::spawn_blocking(task)
        .await
        .map_err(|error| format!("Node 请求任务失败: {error}"))
}

#[tauri::command]
async fn desktop_node_status(
    window: WebviewWindow,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, String> {
    ensure_mvp_window(&window)?;
    let client = Arc::clone(&shared.node_client);
    spawn_node_blocking(move || {
        let health = client.health();
        serde_json::json!({
            "online": health.online,
            "setup_required": health.setup_required,
            "version": health.version,
            "error": health.error,
        })
    })
    .await
}

#[tauri::command]
async fn desktop_capture_screenshot(window: WebviewWindow) -> Result<String, String> {
    use base64::Engine;

    ensure_mvp_window(&window)?;
    let monitor = window
        .current_monitor()
        .map_err(|error| format!("无法读取 Desktop 所在屏幕：{error}"))?
        .ok_or_else(|| "无法确定 Desktop 所在的屏幕。".to_string())?;
    let position = monitor.position();
    let size = monitor.size();
    let (x, y, width, height) = (position.x, position.y, size.width, size.height);
    window
        .hide()
        .map_err(|error| format!("无法暂时隐藏 Desktop：{error}"))?;
    thread::sleep(Duration::from_millis(140));
    let capture_result =
        spawn_node_blocking(move || screen_capture::capture_monitor_png(x, y, width, height)).await;
    let _ = window.show();
    let _ = window.set_focus();
    let png = capture_result??;
    Ok(base64::engine::general_purpose::STANDARD.encode(png))
}

#[tauri::command]
fn desktop_settings_get(
    window: WebviewWindow,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, String> {
    ensure_mvp_window(&window)?;
    let settings = config::ShellConfig::load_or_create(&shared.layout.desktop_config_path)?;
    Ok(serde_json::json!({
        "settings": settings,
    }))
}

#[tauri::command]
fn desktop_settings_save(
    window: WebviewWindow,
    settings: config::ShellConfig,
    shared: State<'_, Arc<Shared>>,
) -> Result<(), String> {
    ensure_mvp_window(&window)?;
    settings.save_atomic(&shared.layout.desktop_config_path)
}

#[tauri::command]
fn desktop_quit(app: AppHandle, window: WebviewWindow) -> Result<(), String> {
    ensure_mvp_window(&window)?;
    app.exit(0);
    Ok(())
}

#[tauri::command]
async fn desktop_shell_request(
    window: WebviewWindow,
    method: String,
    path: String,
    body: Option<serde_json::Value>,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, String> {
    ensure_mvp_window(&window)?;
    match (method.as_str(), path.as_str()) {
        ("GET", "/v1/platform/capabilities") => Ok(serde_json::json!({
            "desktop_shell": true,
            "clipboard_file_paths": cfg!(windows),
            "native_directory_picker": true,
            "window_focus": true,
            "update_apply": cfg!(windows)
        })),
        ("GET", "/v1/platform/clipboard/files") => {
            let paths = clipboard::file_paths()?;
            Ok(serde_json::json!({"paths": paths}))
        }
        ("POST", "/v1/platform/ui-focus") => {
            let body = body.unwrap_or_else(|| serde_json::json!({}));
            let source_id = body.get("source_id").and_then(|v| v.as_str()).unwrap_or("");
            let agent_id = body.get("agent_id").and_then(|v| v.as_str()).unwrap_or("");
            let ttl = body
                .get("ttl_seconds")
                .and_then(|v| v.as_u64())
                .unwrap_or(90);
            if source_id.trim().is_empty() {
                return Err("source_id is required".into());
            }
            shared
                .ui_focus
                .report(source_id, agent_id, Duration::from_secs(ttl));
            Ok(serde_json::json!({"ok": true}))
        }
        ("POST", "/v1/platform/update/apply") => {
            let force = body
                .as_ref()
                .and_then(|v| v.get("force"))
                .and_then(|v| v.as_bool())
                .unwrap_or(false);
            let mut lifecycle = LifecycleOperation::begin(Arc::clone(shared.inner()))?;
            let applier = Arc::clone(&shared.update_applier);
            let outcome = spawn_node_blocking(move || {
                applier.run(update::ApplyOptions {
                    force,
                    skip_confirm: true,
                    ..Default::default()
                })
            })
            .await;
            let (result, code) = match outcome {
                Ok(result) => result,
                Err(error) => return Err(error),
            };
            if code != 0 && code != update::EXIT_UP_TO_DATE {
                return Err(result.message);
            }
            if code == 0 {
                lifecycle.hold_node_stopped();
            }
            drop(lifecycle);
            refresh_status(shared.inner());
            serde_json::to_value(result).map_err(|e| format!("更新结果不可序列化: {e}"))
        }
        _ => Err(format!("未知 Desktop Shell 操作: {method} {path}")),
    }
}

#[tauri::command]
async fn desktop_node_request(
    window: WebviewWindow,
    method: String,
    path: String,
    body: Option<serde_json::Value>,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, DesktopNodeRequestError> {
    ensure_mvp_window(&window).map_err(DesktopNodeRequestError::from)?;
    let client = Arc::clone(&shared.node_client);
    spawn_node_blocking(move || client.desktop_api_request(&method, &path, body))
        .await
        .map_err(DesktopNodeRequestError::from)?
        .map_err(DesktopNodeRequestError::from)
}

#[tauri::command]
async fn desktop_workspace_binary_request(
    window: WebviewWindow,
    method: String,
    path: String,
    body_base64: Option<String>,
    if_match: Option<String>,
    if_none_match: Option<String>,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, String> {
    use base64::Engine;
    ensure_mvp_window(&window)?;
    let bytes = body_base64
        .as_deref()
        .map(|encoded| {
            let max_encoded = nodeclient::MAX_WORKSPACE_FILE_BYTES.div_ceil(3) * 4;
            if encoded.len() > max_encoded {
                return Err("工作区单文件不能超过 16 MiB".to_string());
            }
            base64::engine::general_purpose::STANDARD
                .decode(encoded)
                .map_err(|e| format!("文件内容编码无效: {e}"))
        })
        .transpose()?;
    let client = Arc::clone(&shared.node_client);
    let response = spawn_node_blocking(move || {
        client.workspace_binary_request(
            &method,
            &path,
            bytes,
            if_match.as_deref(),
            if_none_match.as_deref(),
        )
    })
    .await??;
    Ok(serde_json::json!({
        "status": response.status,
        "content_type": response.content_type,
        "etag": response.etag,
        "body_base64": base64::engine::general_purpose::STANDARD.encode(response.body),
    }))
}

#[tauri::command]
async fn desktop_media_binary_request(
    window: WebviewWindow,
    agent_id: String,
    media_id: String,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, String> {
    use base64::Engine;
    ensure_mvp_window(&window)?;
    let client = Arc::clone(&shared.node_client);
    let response =
        spawn_node_blocking(move || client.agent_media_binary_request(&agent_id, &media_id))
            .await??;
    Ok(serde_json::json!({
        "status": response.status,
        "content_type": response.content_type,
        "body_base64": base64::engine::general_purpose::STANDARD.encode(response.body),
    }))
}

#[derive(serde::Serialize)]
struct DesktopNodeRequestError {
    message: String,
    status: Option<u16>,
    code: Option<String>,
    details: Option<serde_json::Value>,
}

impl From<String> for DesktopNodeRequestError {
    fn from(message: String) -> Self {
        let status = message
            .split_once("状态 ")
            .and_then(|(_, tail)| tail.split(|ch: char| !ch.is_ascii_digit()).next())
            .and_then(|digits| digits.parse().ok());
        Self {
            message,
            status,
            code: None,
            details: None,
        }
    }
}

impl From<nodeclient::DesktopApiError> for DesktopNodeRequestError {
    fn from(error: nodeclient::DesktopApiError) -> Self {
        Self {
            message: error.message,
            status: error.status,
            code: error.code,
            details: error.details,
        }
    }
}

#[tauri::command]
async fn desktop_list_agents(
    window: WebviewWindow,
    shared: State<'_, Arc<Shared>>,
) -> Result<Vec<nodeclient::AgentSummary>, String> {
    ensure_mvp_window(&window)?;
    let client = Arc::clone(&shared.node_client);
    spawn_node_blocking(move || {
        if client.node_profile_incomplete() {
            return Err(
                "需要先完成本机 Node 首次初始化。请在 Desktop 设置中配置 Node 名称与模型。"
                    .to_string(),
            );
        }
        client.list_agents()
    })
    .await
    .map_err(|e| e)?
    .map_err(|e| {
        if e.contains("status 403") {
            "需要先完成本机 Node 首次初始化。请在 Desktop 设置中配置 Node 名称与模型。".to_string()
        } else {
            e
        }
    })
}

#[tauri::command]
async fn desktop_hydrate_agent(
    window: WebviewWindow,
    agent_id: String,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, String> {
    ensure_mvp_window(&window)?;
    let client = Arc::clone(&shared.node_client);
    spawn_node_blocking(move || client.hydrate(&agent_id)).await?
}

#[tauri::command]
async fn desktop_resume_hitl(
    window: WebviewWindow,
    agent_id: String,
    resume_value: serde_json::Value,
    shared: State<'_, Arc<Shared>>,
) -> Result<serde_json::Value, String> {
    ensure_mvp_window(&window)?;
    let client = Arc::clone(&shared.node_client);
    spawn_node_blocking(move || client.resume_hitl(&agent_id, resume_value)).await?
}

#[tauri::command]
async fn desktop_terminal_open(
    window: WebviewWindow,
    app: AppHandle,
    agent_id: String,
    connection_id: String,
    session_id: Option<String>,
    target_kind: Option<String>,
    target_id: Option<String>,
    shell: Option<String>,
    shared: State<'_, Arc<Shared>>,
) -> Result<String, String> {
    ensure_mvp_window(&window)?;
    let agent_id = agent_id.trim().to_string();
    if agent_id.is_empty() {
        return Err("找不到当前 Agent，已阻止跨 Agent 打开终端。".into());
    }
    let client = Arc::clone(&shared.node_client);
    let verified_id = agent_id.clone();
    let agent_exists = spawn_node_blocking(move || {
        client
            .list_agents()
            .map(|agents| agents.iter().any(|agent| agent.agent_id == verified_id))
    })
    .await??;
    if !agent_exists {
        return Err("找不到当前 Agent，已阻止跨 Agent 打开终端。".into());
    }
    let connection_id = connection_id.trim().to_string();
    if connection_id.is_empty()
        || connection_id.len() > 80
        || !connection_id
            .chars()
            .all(|ch| ch.is_ascii_alphanumeric() || ch == '-' || ch == '_')
    {
        return Err("终端连接标识无效".into());
    }
    let endpoint = Url::parse(config::NODE_ENDPOINT).map_err(|e| e.to_string())?;
    let session_id = session_id
        .as_deref()
        .map(str::trim)
        .filter(|id| !id.is_empty());
    let first_command = if let Some(id) = session_id {
        if target_kind.is_some() || target_id.is_some() || shell.is_some() {
            return Err("恢复终端会话时不能修改目标或 shell".into());
        }
        serde_json::json!({ "type": "resume", "session_id": id })
    } else {
        terminal::build_open_command(
            target_kind.as_deref(),
            target_id.as_deref(),
            shell.as_deref(),
        )?
    };
    let key = connection_id;
    let mut relays = shared
        .terminal_relays
        .lock()
        .map_err(|_| "终端连接表暂不可用".to_string())?;
    relays.retain(|_, relay| relay.alive.load(Ordering::Acquire));
    if relays.contains_key(&key) {
        return Err("该终端连接已存在".into());
    }
    drop(relays);
    let (sender, alive) = terminal::start(
        app,
        endpoint.to_string(),
        shared.node_client.client_token(),
        key.clone(),
        agent_id.clone(),
        first_command,
    );
    shared
        .terminal_relays
        .lock()
        .map_err(|_| "终端连接表暂不可用".to_string())?
        .insert(
            key.clone(),
            terminal::RelayHandle {
                agent_id,
                sender,
                alive,
            },
        );
    Ok(key)
}

#[tauri::command]
async fn desktop_linux_terminal_channels(
    window: WebviewWindow,
    agent_id: String,
    shared: State<'_, Arc<Shared>>,
) -> Result<Vec<nodeclient::LinuxTerminalChannel>, String> {
    ensure_mvp_window(&window)?;
    let agent_id = agent_id.trim().to_string();
    if agent_id.is_empty() {
        return Err("找不到当前 Agent，已阻止列出其他 Agent 的 SSH 主机。".into());
    }
    let client = Arc::clone(&shared.node_client);
    let verified_id = agent_id.clone();
    let agent_exists = spawn_node_blocking(move || {
        client
            .list_agents()
            .map(|agents| agents.iter().any(|agent| agent.agent_id == verified_id))
    })
    .await??;
    if !agent_exists {
        return Err("找不到当前 Agent，已阻止列出其他 Agent 的 SSH 主机。".into());
    }
    let client = Arc::clone(&shared.node_client);
    spawn_node_blocking(move || client.list_agent_linux_terminal_channels(&agent_id)).await?
}

#[tauri::command]
async fn desktop_terminal_list(
    window: WebviewWindow,
    agent_id: String,
    shared: State<'_, Arc<Shared>>,
) -> Result<Vec<nodeclient::TerminalSummary>, String> {
    ensure_mvp_window(&window)?;
    let agent_id = agent_id.trim();
    let client = Arc::clone(&shared.node_client);
    let verified_id = agent_id.to_string();
    let exists = spawn_node_blocking(move || {
        client
            .list_agents()
            .map(|agents| agents.iter().any(|agent| agent.agent_id == verified_id))
    })
    .await??;
    if !exists {
        return Err("找不到当前 Agent，已阻止跨 Agent 列举终端。".into());
    }
    let client = Arc::clone(&shared.node_client);
    let agent_id = agent_id.to_string();
    spawn_node_blocking(move || client.list_terminals(&agent_id)).await?
}

#[tauri::command]
fn desktop_terminal_command(
    window: WebviewWindow,
    connection_id: String,
    agent_id: String,
    command_type: String,
    data: Option<String>,
    rows: Option<u16>,
    cols: Option<u16>,
    shared: State<'_, Arc<Shared>>,
) -> Result<(), String> {
    use base64::Engine;
    ensure_mvp_window(&window)?;
    let id = connection_id.trim();
    let mut map = shared
        .terminal_relays
        .lock()
        .map_err(|_| "终端连接表暂不可用".to_string())?;
    map.retain(|_, relay| relay.alive.load(Ordering::Acquire));
    let relay = map
        .get(id)
        .ok_or_else(|| "终端会话已关闭或不属于当前 Desktop 窗口".to_string())?;
    if relay.agent_id != agent_id {
        return Err("终端会话不属于当前 Agent，已阻止跨 Agent 操作。".into());
    }
    let sender = relay.sender.clone();
    let command = match command_type.as_str() {
        "input" => {
            let encoded = base64::engine::general_purpose::STANDARD
                .encode(data.unwrap_or_default().as_bytes());
            serde_json::json!({ "type": "input", "data": encoded })
        }
        "resize" => serde_json::json!({
            "type": "resize",
            "rows": rows.unwrap_or(24).clamp(1, 200),
            "cols": cols.unwrap_or(80).clamp(1, 300)
        }),
        "close" | "terminate" | "detach" => serde_json::json!({ "type": command_type }),
        _ => return Err("不支持的终端动作".into()),
    };
    let should_remove = matches!(command_type.as_str(), "close" | "terminate" | "detach");
    if should_remove {
        map.remove(id);
    }
    drop(map);
    if sender
        .send(terminal::TerminalControl { message: command })
        .is_err()
        && !should_remove
    {
        return Err("终端连接已断开".to_string());
    }
    Ok(())
}

fn open_desktop_mvp(shared: &Arc<Shared>, app: &AppHandle, agent_id: Option<String>) {
    let shared = Arc::clone(shared);
    let app = app.clone();
    let main_app = app.clone();
    let _ = app.run_on_main_thread(move || {
        if let Some(win) = main_app.get_webview_window(MAIN_WINDOW) {
            let _ = win.set_skip_taskbar(false);
            let _ = win.show();
            let _ = win.set_focus();
            if let Some(agent_id) = agent_id.as_ref() {
                let _ = win.emit(
                    "desktop-open-agent",
                    serde_json::json!({"agentId": agent_id}),
                );
            }
            return;
        }
        nodectl::append_shell_log(&shared.layout, "无法打开 DAgents：主 Desktop 窗口尚未创建");
    });
}

fn detach_all_terminal_relays(shared: &Arc<Shared>) {
    let relays = match shared.terminal_relays.lock() {
        Ok(mut relays) => relays.drain().map(|(_, relay)| relay).collect::<Vec<_>>(),
        Err(_) => return,
    };
    for relay in relays {
        let _ = relay.sender.send(terminal::TerminalControl {
            message: serde_json::json!({ "type": "detach" }),
        });
    }
}

fn is_desktop_mvp_navigation_allowed(url: &Url) -> bool {
    desktop_mvp_navigation_allowed(url, cfg!(debug_assertions))
}

fn desktop_mvp_navigation_allowed(url: &Url, allow_vite_dev_server: bool) -> bool {
    if matches!(url.scheme(), "tauri" | "asset") {
        return true;
    }
    if allow_vite_dev_server
        && matches!(url.host_str(), Some("localhost") | Some("127.0.0.1"))
        && url.port() == Some(1422)
    {
        return true;
    }
    matches!(url.scheme(), "http" | "https")
        && matches!(
            url.host_str(),
            Some("tauri.localhost") | Some("asset.localhost")
        )
        && url.port().is_none()
}

fn open_update_settings(shared: &Arc<Shared>, app: &AppHandle) {
    let app_handle = app.clone();
    open_desktop_mvp(shared, app, None);
    let _ = app.run_on_main_thread(move || {
        if let Some(window) = app_handle.get_webview_window("desktop_mvp") {
            let _ = window.emit(
                "desktop-open-settings",
                serde_json::json!({"path":"/settings/desktop"}),
            );
        }
    });
}

fn handle_update_menu(shared: &Arc<Shared>, app: &AppHandle) {
    let status = shared.update_checker.snapshot();
    if status.upgrade_available || !status.platform_supported {
        open_update_settings(shared, app);
    } else {
        check_update_now(shared);
    }
}

fn check_update_now(shared: &Arc<Shared>) {
    if shared
        .update_check_in_flight
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    if let Ok(guard) = shared.update_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_text("更新：检查中…");
            let _ = item.set_enabled(false);
        }
    }
    let shared = Arc::clone(shared);
    thread::spawn(move || {
        let _ = shared.update_checker.check_once();
        shared.update_check_in_flight.store(false, Ordering::SeqCst);
        refresh_update_ui(&shared);
    });
}

fn open_first_pending(shared: &Arc<Shared>, app: &AppHandle) {
    let entries = shared.pending_store.entries();
    if let Some(entry) = entries.first() {
        open_agent(shared, app, &entry.agent_id);
    } else {
        open_desktop_mvp(shared, app, None);
    }
}

fn open_pending_slot(shared: &Arc<Shared>, app: &AppHandle, slot: usize) {
    let agent_id = shared
        .pending_session_ids
        .lock()
        .ok()
        .and_then(|ids| ids.get(slot).cloned())
        .unwrap_or_default();
    if !agent_id.trim().is_empty() {
        open_agent(shared, app, &agent_id);
    }
}

fn open_agent(shared: &Arc<Shared>, app: &AppHandle, agent_id: &str) {
    open_desktop_mvp(shared, app, Some(agent_id.to_string()));
}

fn open_manage(shared: &Arc<Shared>) {
    if shared
        .lifecycle_busy
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    let shared = Arc::clone(shared);
    thread::spawn(move || {
        struct LifecycleGuard(Arc<Shared>);
        impl Drop for LifecycleGuard {
            fn drop(&mut self) {
                self.0.lifecycle_busy.store(false, Ordering::SeqCst);
            }
        }
        let lifecycle_guard = LifecycleGuard(Arc::clone(&shared));
        match nodectl::open_manage(&shared.layout, &shared.cfg, WAIT_READY) {
            Ok(()) => set_err(&shared, None),
            Err(e) => set_err(&shared, Some(e)),
        }
        drop(lifecycle_guard);
        refresh_status(&shared);
    });
}

fn ensure_node(shared: &Arc<Shared>) {
    if shared
        .lifecycle_busy
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    match nodectl::start(&shared.layout, &shared.cfg, WAIT_READY) {
        Ok(()) => set_err(shared, None),
        Err(e) => {
            nodectl::append_shell_log(&shared.layout, &format!("ensure Node: {e}"));
            set_err(shared, Some(e));
        }
    }
    shared.lifecycle_busy.store(false, Ordering::SeqCst);
}

fn run_action(
    shared: &Arc<Shared>,
    label: &str,
    f: impl FnOnce(&Shared) -> Result<(), String> + Send + 'static,
) {
    if shared
        .lifecycle_busy
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    let shared = Arc::clone(shared);
    let label = label.to_string();
    thread::spawn(move || {
        struct ActionGuard(Arc<Shared>);
        impl Drop for ActionGuard {
            fn drop(&mut self) {
                self.0.lifecycle_busy.store(false, Ordering::SeqCst);
            }
        }
        let guard = ActionGuard(Arc::clone(&shared));
        match f(&shared) {
            Ok(()) => set_err(&shared, None),
            Err(e) => {
                nodectl::append_shell_log(&shared.layout, &format!("{label} Node 失败: {e}"));
                set_err(&shared, Some(e));
            }
        }
        drop(guard);
        refresh_status(&shared);
    });
}

fn set_err(shared: &Shared, err: Option<String>) {
    if let Ok(mut g) = shared.last_err.lock() {
        *g = err;
    }
}

fn refresh_status(shared: &Shared) {
    let health = nodectl::probe(&shared.cfg);
    let running = matches!(&health, Ok(h) if h.ok);
    let managed = running && nodectl::is_managed(&shared.layout, &shared.cfg);
    shared.node_running.store(running, Ordering::SeqCst);
    let text = match health {
        Ok(h) if h.ok => format_running(&h, managed),
        Ok(h) => format!("状态：异常 ({})", h.status),
        Err(_) => {
            let err = shared
                .last_err
                .lock()
                .ok()
                .and_then(|g| g.clone())
                .unwrap_or_else(|| "未运行".into());
            // 截断过长错误
            let short = if err.chars().count() > 24 {
                format!("{}…", err.chars().take(24).collect::<String>())
            } else {
                err
            };
            format!("状态：{short}")
        }
    };
    if let Ok(guard) = shared.status_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_text(text.clone());
        }
    }
    let busy = shared.lifecycle_busy.load(Ordering::SeqCst);
    if let Ok(guard) = shared.start_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_enabled(!busy && !running);
        }
    }
    if let Ok(guard) = shared.stop_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_enabled(!busy && managed);
        }
    }
    if let Ok(guard) = shared.restart_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_enabled(!busy && managed);
        }
    }
    let manage_available = if running {
        shared
            .node_client
            .agent_info()
            .map(|info| info.manage_enabled && !info.manage_url.trim().is_empty())
            .unwrap_or(false)
    } else {
        false
    };
    if let Ok(guard) = shared.open_manage_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_enabled(manage_available);
        }
    }
}

fn format_running(h: &Health, managed: bool) -> String {
    let version = if h.version.is_empty() {
        String::new()
    } else {
        format!(" v{}", h.version)
    };
    if managed {
        format!("状态：受监督运行{version}")
    } else {
        format!("状态：外部 Node 运行{version}")
    }
}

fn maybe_recover(shared: &Arc<Shared>) {
    if shared.hold_stopped.load(Ordering::SeqCst) {
        return;
    }
    if shared.node_running.load(Ordering::SeqCst) {
        return;
    }
    if shared
        .lifecycle_busy
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    if shared
        .recovering
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        shared.lifecycle_busy.store(false, Ordering::SeqCst);
        return;
    }
    let shared = Arc::clone(shared);
    thread::spawn(move || {
        struct RecoveryGuard(Arc<Shared>);
        impl Drop for RecoveryGuard {
            fn drop(&mut self) {
                self.0.lifecycle_busy.store(false, Ordering::SeqCst);
            }
        }
        let guard = RecoveryGuard(Arc::clone(&shared));
        let result = nodectl::start(&shared.layout, &shared.cfg, WAIT_READY);
        match result {
            Ok(()) => set_err(&shared, None),
            Err(e) => {
                nodectl::append_shell_log(&shared.layout, &format!("supervisor restart: {e}"));
                set_err(&shared, Some(e));
            }
        }
        shared.recovering.store(false, Ordering::SeqCst);
        drop(guard);
        refresh_status(&shared);
    });
    let _ = ENSURE_TIMEOUT;
}

fn refresh_update_ui(shared: &Shared) {
    let cfg = shared.update_checker.config_snapshot();
    let status = shared.update_checker.snapshot();
    let text_enabled = update_menu_status_text(&status, cfg.update_check_enabled);
    if let Ok(guard) = shared.update_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_text(text_enabled.0);
            let _ = item.set_enabled(text_enabled.1);
        }
    }
}

fn update_menu_status_text(
    status: &crate::update::Status,
    update_check_enabled: bool,
) -> (String, bool) {
    if !update_check_enabled {
        ("更新：自动检查已关闭（点击手动检查）".to_string(), true)
    } else if !status.manage_reachable
        && !status.message.trim().is_empty()
        && status.message.trim() != "等待检查"
    {
        ("更新：Node 检查失败（点击重试）".to_string(), true)
    } else if status.last_checked_at.trim().is_empty() {
        ("更新：检查中…".to_string(), false)
    } else if !status.manage_reachable {
        ("更新：Node 检查失败（点击重试）".to_string(), true)
    } else if !status.update_enabled {
        ("更新：Node 已关闭更新".to_string(), false)
    } else if !status.platform_supported {
        ("更新：当前平台未发布安装包".to_string(), true)
    } else if status.upgrade_available {
        (format!("更新：新版本 {} 可用", status.latest_version), true)
    } else {
        ("更新：已是最新（点击重试）".to_string(), true)
    }
}

fn refresh_pending_ui(shared: &Arc<Shared>, app: &AppHandle) {
    let entries = shared.pending_store.entries();
    let summary = shared.pending_store.summary();

    let submenu = shared.pending_submenu.lock().ok().and_then(|g| g.clone());
    let slots = shared.pending_slots.lock().ok().map(|g| g.clone());
    if let (Some(submenu), Some(slots)) = (submenu, slots) {
        for item in &slots {
            let _ = submenu.remove(item);
        }

        let mut ids = vec![String::new(); MAX_PENDING_MENU_SLOTS];
        if summary.session_count == 0 {
            let _ = submenu.set_text("待办：无");
            let _ = submenu.set_enabled(false);
        } else {
            let _ = submenu.set_text(format!("待办：{}", summary.label));
            let _ = submenu.set_enabled(true);
            let limit = entries.len().min(MAX_PENDING_MENU_SLOTS);
            for i in 0..limit {
                let entry = &entries[i];
                ids[i] = entry.session_id.clone();
                let _ = slots[i].set_text(format!("打开 · {}", entry.summary_label()));
                let _ = slots[i].set_enabled(true);
                let _ = submenu.append(&slots[i]);
            }
        }
        if let Ok(mut guard) = shared.pending_session_ids.lock() {
            *guard = ids;
        }
    }

    sync_notifier(shared, &entries);
    refresh_tooltip(shared, app);
    if summary.session_count > 0 {
        start_icon_blink(shared, app);
    } else {
        shared.blink_running.store(false, Ordering::SeqCst);
        set_tray_icon(app, false);
    }
}

fn sync_notifier(shared: &Shared, entries: &[pending::Entry]) {
    let mut retain = HashSet::new();
    let mut toast_entries = Vec::new();
    for entry in entries {
        let focus_id = if entry.agent_id.trim().is_empty() {
            entry.session_id.trim()
        } else {
            entry.agent_id.trim()
        };
        if shared.ui_focus.is_focused(focus_id) {
            retain.insert(entry.session_id.clone());
        } else {
            toast_entries.push(entry.clone());
        }
    }
    shared.notifier.sync(&toast_entries, &retain);
}

fn refresh_tooltip(shared: &Shared, app: &AppHandle) {
    let mut tooltip = format!("DAgents Shell @ {}", shared.layout.home.display());
    let summary = shared.pending_store.summary();
    if summary.session_count > 0 {
        tooltip.push('\n');
        tooltip.push_str(&summary.label);
    }
    match nodectl::probe(&shared.cfg) {
        Ok(h) if h.ok => {
            if h.version.is_empty() {
                tooltip.push_str("\nNode 运行中");
            } else {
                tooltip.push_str(&format!("\nNode 运行中 · v{}", h.version));
            }
        }
        _ => {
            let err = shared
                .last_err
                .lock()
                .ok()
                .and_then(|g| g.clone())
                .unwrap_or_else(|| "未运行".into());
            tooltip.push_str(&format!("\nNode 未运行 · {err}"));
        }
    }
    if let Some(tray) = app.tray_by_id("main") {
        let _ = tray.set_tooltip(Some(&tooltip));
    }
}

fn start_icon_blink(shared: &Arc<Shared>, app: &AppHandle) {
    if shared
        .blink_running
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    let shared = Arc::clone(shared);
    let app = app.clone();
    thread::spawn(move || {
        let mut pending_frame = true;
        while shared.blink_running.load(Ordering::SeqCst)
            && shared.pending_store.summary().session_count > 0
            && !shared.shell_stop.load(Ordering::SeqCst)
        {
            set_tray_icon(&app, pending_frame);
            pending_frame = !pending_frame;
            thread::sleep(ICON_BLINK_INTERVAL);
        }
        set_tray_icon(&app, false);
        shared.blink_running.store(false, Ordering::SeqCst);
    });
}

fn set_tray_icon(app: &AppHandle, pending: bool) {
    let bytes = if pending {
        include_bytes!("../icons/icon_pending.png").as_slice()
    } else {
        include_bytes!("../icons/icon.png").as_slice()
    };
    let Ok(icon) = tauri::image::Image::from_bytes(bytes) else {
        return;
    };
    if let Some(tray) = app.tray_by_id("main") {
        let _ = tray.set_icon(Some(icon));
    }
}

#[cfg(test)]
mod nav_tests {
    use super::{
        args_have_flag, desktop_mvp_navigation_allowed, parse_layout_args,
        require_idle_readiness_check, update_menu_status_text, LayoutArgs,
    };
    use crate::update::Status;
    use tauri::Url;

    #[test]
    fn mvp_vite_navigation_is_debug_only() {
        let app = Url::parse("tauri://localhost/index.html").unwrap();
        let vite = Url::parse("http://127.0.0.1:1422/").unwrap();
        let attacker = Url::parse("http://127.0.0.1:1423/").unwrap();
        assert!(desktop_mvp_navigation_allowed(&app, false));
        assert!(desktop_mvp_navigation_allowed(&vite, true));
        assert!(!desktop_mvp_navigation_allowed(&vite, false));
        assert!(!desktop_mvp_navigation_allowed(&attacker, true));
        let node_ui = Url::parse("http://127.0.0.1:18765/ui/").unwrap();
        assert!(!desktop_mvp_navigation_allowed(&node_ui, true));
    }

    #[test]
    fn unsupported_update_platform_is_not_reported_as_up_to_date() {
        let status = Status {
            manage_reachable: true,
            update_enabled: true,
            platform_supported: false,
            last_checked_at: "2026-10-17T00:00:00Z".into(),
            ..Status::default()
        };
        let (label, enabled) = update_menu_status_text(&status, true);
        assert_eq!(label, "更新：当前平台未发布安装包");
        assert!(enabled);
        assert!(!label.contains("已是最新"));
    }

    #[test]
    fn failed_update_check_is_not_reported_as_still_checking() {
        let status = Status {
            manage_reachable: false,
            platform_supported: true,
            message: "Node 请求失败".into(),
            ..Status::default()
        };
        let (label, enabled) = update_menu_status_text(&status, true);
        assert_eq!(label, "更新：Node 检查失败（点击重试）");
        assert!(enabled);
    }

    #[test]
    fn stop_owned_node_flag_is_detected_without_consuming_path_arguments() {
        let args = vec![
            "--stop-owned-node".into(),
            "--require-idle".into(),
            "--config".into(),
            "C:\\Program Files\\DAgents\\config.yaml".into(),
            "--desktop-config".into(),
            "C:\\Users\\测试\\settings.json".into(),
        ];
        assert!(args_have_flag(&args, "--stop-owned-node"));
        assert!(args_have_flag(&args, "--require-idle"));
        assert!(!args_have_flag(&args, "--start-owned-node"));
        assert!(!args_have_flag(&args, "--node-status"));
        assert!(!args_have_flag(&args, "--quit-desktop"));
    }

    #[test]
    fn require_idle_allows_an_offline_unowned_node_but_rejects_external_node() {
        assert_eq!(require_idle_readiness_check(false, false), Ok(false));
        assert_eq!(
            require_idle_readiness_check(false, true),
            Err("当前 endpoint 为外部 Node，不能安全升级或卸载")
        );
        assert_eq!(require_idle_readiness_check(true, false), Ok(true));
    }

    #[test]
    fn layout_args_keep_home_config_and_desktop_config_paths_separate() {
        let args = vec![
            "--config".into(),
            r"C:\Program Files\DAgents\config.yaml".into(),
            "--home".into(),
            r"C:\用户\DAgents 安装".into(),
            r"--desktop-config=C:\Users\测试\settings.json".into(),
        ];
        assert_eq!(
            parse_layout_args(&args).unwrap(),
            LayoutArgs {
                home: Some(r"C:\用户\DAgents 安装".into()),
                config: Some(r"C:\Program Files\DAgents\config.yaml".into()),
                desktop_config: Some(r"C:\Users\测试\settings.json".into()),
            }
        );

        let equals_args = vec![
            "--home=C:\\用户\\DAgents 安装".into(),
            "--config=C:\\Program Files\\DAgents\\config.yaml".into(),
        ];
        assert_eq!(
            parse_layout_args(&equals_args).unwrap(),
            LayoutArgs {
                home: Some(r"C:\用户\DAgents 安装".into()),
                config: Some(r"C:\Program Files\DAgents\config.yaml".into()),
                desktop_config: None,
            }
        );
    }

    #[test]
    fn layout_args_reject_missing_or_blank_home_without_consuming_other_flags() {
        let missing_before_config = vec!["--home".into(), "--config".into(), "C:\\cfg.yaml".into()];
        assert_eq!(
            parse_layout_args(&missing_before_config).unwrap_err(),
            "--home 需要路径参数"
        );

        let missing_before_control = vec!["--home".into(), "--quit-desktop".into()];
        assert_eq!(
            parse_layout_args(&missing_before_control).unwrap_err(),
            "--home 需要路径参数"
        );

        let missing_config = vec!["--config".into(), "--home".into(), "C:\\install".into()];
        assert_eq!(
            parse_layout_args(&missing_config).unwrap_err(),
            "--config 需要路径参数"
        );

        let blank_home = vec!["--home= \t".into()];
        assert_eq!(
            parse_layout_args(&blank_home).unwrap_err(),
            "--home 需要非空路径"
        );
    }
}
