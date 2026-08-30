//! DAgents Shell — Tauri 系统托盘（替代 / 并行于 Go `desktop/tray`）。
//!
//! 核心能力：
//! - 启动 ensure Node、退出 stop Node
//! - 托盘菜单：状态 / 待办 / 更新 / 打开控制台 / 启停重启 / 退出
//! - Desktop API、SSE 待办同步、Toast、Manage 更新检查
//! - 双击托盘图标在内嵌 WebView 中打开 Web UI（同源 `/ui/`）
//! - 关闭窗口隐藏到托盘（不停 Node）
//! - health 轮询 + 基础自动恢复

mod clipboard;
mod config;
mod desktopapi;
mod events;
mod layout;
mod nodeclient;
mod nodectl;
mod notify;
mod pending;
mod singleinstance;
mod uifocus;
mod update;

use config::ShellConfig;
use layout::Layout;
use nodectl::Health;
use singleinstance::{acquire, InstanceError};
use std::collections::HashSet;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::tray::{MouseButton, TrayIconBuilder, TrayIconEvent};
use tauri::webview::WebviewWindowBuilder;
use tauri::{AppHandle, Manager, RunEvent, Url, WebviewUrl, WindowEvent};

const ENSURE_TIMEOUT: Duration = Duration::from_secs(45);
const PROBE_INTERVAL: Duration = Duration::from_secs(3);
const WAIT_READY: Duration = Duration::from_secs(30);
const MAIN_WINDOW: &str = "main";
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
    desktop_api_stop: Arc<AtomicBool>,
    ui_focus: Arc<uifocus::Store>,
    sse_subscriber: Mutex<Option<events::Subscriber>>,
    hold_stopped: AtomicBool,
    node_running: AtomicBool,
    recovering: AtomicBool,
    lifecycle_busy: AtomicBool,
    blink_running: AtomicBool,
    update_check_in_flight: AtomicBool,
    webui_open_in_flight: AtomicBool,
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
            eprintln!("dagents-shell: {e}");
            std::process::exit(1);
        }
    };

    let _ = nodectl::ensure_runtime_dirs(&layout);

    let cfg = match ShellConfig::load(&layout.config_path) {
        Ok(c) => c,
        Err(e) => {
            eprintln!("dagents-shell: {e}");
            std::process::exit(1);
        }
    };
    let cfg = Arc::new(cfg);

    if let Some(update_args) = update_args_from_args() {
        let code = desktopapi::run_update_command(&update_args, layout, Arc::clone(&cfg));
        std::process::exit(code);
    }

    let guard = match acquire(&layout) {
        Ok(g) => g,
        Err(InstanceError::AlreadyRunning) => {
            eprintln!("dagents-shell: another instance is already running");
            std::process::exit(0);
        }
        Err(InstanceError::Other(e)) => {
            eprintln!("dagents-shell: single instance: {e}");
            std::process::exit(1);
        }
    };

    nodectl::append_shell_log(
        &layout,
        &format!(
            "shell start endpoint={} config={}",
            cfg.endpoint,
            layout.config_path.display()
        ),
    );

    // 单实例锁保留在主线程栈上：Windows Mutex HANDLE 非 Send，不可放入 Arc 跨线程。
    let _instance_guard = guard;

    let node_client = Arc::new(nodeclient::Client::new(&cfg.endpoint));
    let pending_store = Arc::new(pending::Store::new());
    let notifier = Arc::new(notify::Notifier::new(cfg.endpoint.clone()));
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
    let desktop_api_stop = Arc::new(AtomicBool::new(false));

    let shared = Arc::new(Shared {
        layout,
        cfg,
        node_client,
        pending_store,
        notifier,
        update_checker,
        update_applier,
        desktop_api_stop,
        ui_focus,
        sse_subscriber: Mutex::new(None),
        hold_stopped: AtomicBool::new(false),
        node_running: AtomicBool::new(false),
        recovering: AtomicBool::new(false),
        lifecycle_busy: AtomicBool::new(false),
        blink_running: AtomicBool::new(false),
        update_check_in_flight: AtomicBool::new(false),
        webui_open_in_flight: AtomicBool::new(false),
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
    });

    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .setup({
            let shared = Arc::clone(&shared);
            move |app| {
                let allowed_endpoint = shared.cfg.endpoint.clone();
                let window = WebviewWindowBuilder::new(
                    app,
                    MAIN_WINDOW,
                    WebviewUrl::App("index.html".into()),
                )
                .title("DAgents")
                .inner_size(1280.0, 800.0)
                .min_inner_size(800.0, 600.0)
                .resizable(true)
                .visible(false)
                .skip_taskbar(true)
                .icon(
                    tauri::image::Image::from_bytes(include_bytes!("../icons/icon.png"))
                        .map_err(|e| format!("加载窗口图标失败: {e}"))?,
                )?
                .on_navigation(move |url| is_navigation_allowed(url, &allowed_endpoint))
                .build()?;

                let win_for_close = window.clone();
                window.on_window_event(move |event| {
                    if let WindowEvent::CloseRequested { api, .. } = event {
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
                    MenuItem::with_id(app, "open_console", "打开控制台", true, None::<&str>)?;
                let open_manage =
                    MenuItem::with_id(app, "open_manage", "打开 Manage", false, None::<&str>)?;
                let update =
                    MenuItem::with_id(app, "update", "更新：检查中…", false, None::<&str>)?;
                let start = MenuItem::with_id(app, "start", "启动 Node", true, None::<&str>)?;
                let stop = MenuItem::with_id(app, "stop", "停止 Node", false, None::<&str>)?;
                let restart = MenuItem::with_id(app, "restart", "重启 Node", false, None::<&str>)?;
                let quit = MenuItem::with_id(app, "quit", "退出 Shell", true, None::<&str>)?;
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
                        // 双击打开 Web UI；左键单击仍由 show_menu_on_left_click 弹出菜单。
                        if let TrayIconEvent::DoubleClick {
                            button: MouseButton::Left,
                            ..
                        } = event
                        {
                            open_console(&tray_shared, &tray_app);
                        }
                    })
                    .build(app)?;
                app.manage(tray);

                let bg_stop = Arc::clone(&shared.desktop_api_stop);
                shared.update_checker.set_upgrade_callback({
                    let shared = Arc::clone(&shared);
                    move |status| {
                        let _ = shared
                            .notifier
                            .push_update_available(&status.latest_version);
                        refresh_update_ui(&shared);
                    }
                });

                // 启动 ensure Node + 轮询
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

                let api = Arc::new(desktopapi::Server::new(
                    Arc::clone(&shared.update_checker),
                    Arc::clone(&shared.update_applier),
                    Arc::clone(&shared.ui_focus),
                ));
                api.start(Arc::clone(&bg_stop));

                let sub = events::Subscriber::new(
                    Arc::clone(&shared.node_client),
                    Arc::clone(&shared.pending_store),
                    {
                        let shared = Arc::clone(&shared);
                        let app = app.handle().clone();
                        move || refresh_pending_ui(&shared, &app)
                    },
                );
                sub.start();
                if let Ok(mut guard) = shared.sse_subscriber.lock() {
                    *guard = Some(sub);
                }

                Ok(())
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run({
            let shared = Arc::clone(&shared);
            move |_app, event| match event {
                // 隐藏主窗后勿因「无可见窗口」退出；菜单「退出」走 app.exit(code)。
                RunEvent::ExitRequested { api, code, .. } => {
                    if code.is_none() {
                        api.prevent_exit();
                    }
                }
                RunEvent::Exit => {
                    shared.desktop_api_stop.store(true, Ordering::SeqCst);
                    if let Ok(mut sub) = shared.sse_subscriber.lock() {
                        if let Some(sub) = sub.take() {
                            sub.stop();
                        }
                    }
                    let _ = nodectl::stop(&shared.layout, &shared.cfg);
                }
                _ => {}
            }
        });
}

/// 仅允许：Tauri/asset 本地壳、Vite 开发服、配置的 Node endpoint（同源 `/ui/`）。
fn is_navigation_allowed(url: &Url, endpoint: &str) -> bool {
    match url.scheme() {
        "tauri" | "asset" | "about" | "data" => return true,
        "http" | "https" => {}
        _ => return false,
    }

    // 本地 stub（devUrl / 偶发 localhost）
    if matches!(url.host_str(), Some("localhost") | Some("127.0.0.1")) && url.port() == Some(1422) {
        return true;
    }

    let Ok(allowed) = Url::parse(endpoint) else {
        return false;
    };
    url.scheme() == allowed.scheme()
        && url.host() == allowed.host()
        && url.port_or_known_default() == allowed.port_or_known_default()
}

fn resolve_layout_from_args() -> Result<Layout, String> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut config_flag: Option<String> = None;
    let mut i = 0;
    while i < args.len() {
        let a = &args[i];
        if a == "--config" || a == "-config" {
            if i + 1 >= args.len() {
                return Err("--config 需要路径参数".into());
            }
            config_flag = Some(args[i + 1].clone());
            i += 2;
            continue;
        }
        if let Some(rest) = a.strip_prefix("--config=") {
            config_flag = Some(rest.to_string());
            i += 1;
            continue;
        }
        i += 1;
    }
    Layout::resolve(config_flag.as_deref())
}

fn update_args_from_args() -> Option<Vec<String>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut i = 0;
    while i < args.len() {
        let a = &args[i];
        if a == "--config" || a == "-config" {
            i += 2;
            continue;
        }
        if a.starts_with("--config=") {
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

fn handle_menu(shared: &Arc<Shared>, app: &AppHandle, id: &str) {
    match id {
        "pending" => open_first_pending(shared, app),
        "open_console" => open_console(shared, app),
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

fn open_console(shared: &Arc<Shared>, app: &AppHandle) {
    open_webui_url(shared, app, shared.cfg.console_url());
}

fn open_update_settings(shared: &Arc<Shared>, app: &AppHandle) {
    open_webui_url(shared, app, shared.cfg.settings_about_url());
}

fn handle_update_menu(shared: &Arc<Shared>, app: &AppHandle) {
    if shared.update_checker.snapshot().upgrade_available {
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
        open_agent(shared, app, &entry.session_id);
    } else {
        open_console(shared, app);
    }
}

fn open_pending_slot(shared: &Arc<Shared>, app: &AppHandle, slot: usize) {
    let session_id = shared
        .pending_session_ids
        .lock()
        .ok()
        .and_then(|ids| ids.get(slot).cloned())
        .unwrap_or_default();
    if !session_id.trim().is_empty() {
        open_agent(shared, app, &session_id);
    }
}

fn open_agent(shared: &Arc<Shared>, app: &AppHandle, agent_id: &str) {
    open_webui_url(shared, app, shared.cfg.agent_url(agent_id));
}

fn open_webui_url(shared: &Arc<Shared>, app: &AppHandle, url_str: String) {
    if shared
        .webui_open_in_flight
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    let shared = Arc::clone(shared);
    let app = app.clone();
    if shared
        .lifecycle_busy
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        shared.webui_open_in_flight.store(false, Ordering::SeqCst);
        return;
    }
    thread::spawn(move || {
        struct InFlightGuard(Arc<Shared>);
        impl Drop for InFlightGuard {
            fn drop(&mut self) {
                self.0.webui_open_in_flight.store(false, Ordering::SeqCst);
            }
        }
        let _guard = InFlightGuard(Arc::clone(&shared));
        struct LifecycleGuard(Arc<Shared>);
        impl Drop for LifecycleGuard {
            fn drop(&mut self) {
                self.0.lifecycle_busy.store(false, Ordering::SeqCst);
            }
        }
        let lifecycle_guard = LifecycleGuard(Arc::clone(&shared));
        if let Err(e) = nodectl::start(&shared.layout, &shared.cfg, WAIT_READY) {
            set_err(&shared, Some(e));
            drop(lifecycle_guard);
            refresh_status(&shared);
            return;
        }

        // 未完成首配时一律打开 /ui/ 并强制刷新，避免同 URL 仅聚焦导致看不到首配页。
        let force_onboarding = shared.node_client.node_profile_incomplete();
        let url_str = if force_onboarding {
            shared.cfg.console_url()
        } else {
            url_str
        };

        let url = match Url::parse(&url_str) {
            Ok(u) => u,
            Err(e) => {
                set_err(&shared, Some(format!("无效 Web UI URL {url_str}: {e}")));
                drop(lifecycle_guard);
                refresh_status(&shared);
                return;
            }
        };

        let (tx, rx) = mpsc::channel::<Result<(), String>>();
        let schedule = app.run_on_main_thread({
            let app = app.clone();
            move || {
                let result = (|| {
                    let Some(win) = app.get_webview_window(MAIN_WINDOW) else {
                        return Err("主窗口不存在".to_string());
                    };
                    let already_target = win
                        .url()
                        .ok()
                        .map(|u| {
                            u.scheme() == url.scheme()
                                && u.host() == url.host()
                                && u.port_or_known_default() == url.port_or_known_default()
                                && u.path() == url.path()
                        })
                        .unwrap_or(false);
                    if force_onboarding {
                        if already_target {
                            // 同 URL 时 navigate 可能被忽略；强制 remount 以重新跑 App 首配门闩。
                            win.eval("window.location.reload()")
                                .map_err(|e| format!("刷新失败: {e}"))?;
                        } else {
                            win.navigate(url.clone())
                                .map_err(|e| format!("导航失败: {e}"))?;
                        }
                    } else if !already_target {
                        win.navigate(url.clone())
                            .map_err(|e| format!("导航失败: {e}"))?;
                    }
                    win.set_skip_taskbar(false)
                        .map_err(|e| format!("显示任务栏失败: {e}"))?;
                    win.show().map_err(|e| format!("显示窗口失败: {e}"))?;
                    win.set_focus().map_err(|e| format!("聚焦失败: {e}"))?;
                    Ok(())
                })();
                let _ = tx.send(result);
            }
        });
        if let Err(e) = schedule {
            set_err(&shared, Some(format!("调度主线程失败: {e}")));
            drop(lifecycle_guard);
            refresh_status(&shared);
            return;
        }
        match rx.recv_timeout(Duration::from_secs(10)) {
            Ok(Ok(())) => set_err(&shared, None),
            Ok(Err(e)) => set_err(&shared, Some(e)),
            Err(_) => set_err(&shared, Some("打开 Web UI 超时".into())),
        }
        drop(lifecycle_guard);
        refresh_status(&shared);
    });
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
    shared.node_running.store(running, Ordering::SeqCst);
    let text = match health {
        Ok(h) if h.ok => format_running(&h),
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
            let _ = item.set_enabled(!busy && running);
        }
    }
    if let Ok(guard) = shared.restart_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_enabled(!busy && running);
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

fn format_running(h: &Health) -> String {
    if !h.version.is_empty() {
        format!("状态：运行中 v{}", h.version)
    } else {
        "状态：运行中".into()
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
    if shared.node_running.load(Ordering::SeqCst) {
        let _ = shared.update_checker.refresh_config();
    }
    let cfg = shared.update_checker.config_snapshot();
    let status = shared.update_checker.snapshot();
    let text_enabled = if !cfg.manage.enabled || !cfg.manage_update_enabled() {
        ("更新：未启用".to_string(), false)
    } else if status.last_checked_at.trim().is_empty() {
        ("更新：检查中…".to_string(), false)
    } else if !status.manage_reachable {
        ("更新：检查失败（点击重试）".to_string(), true)
    } else if status.upgrade_available {
        (format!("更新：新版本 {} 可用", status.latest_version), true)
    } else {
        ("更新：已是最新（点击重试）".to_string(), true)
    };
    if let Ok(guard) = shared.update_item.lock() {
        if let Some(item) = guard.as_ref() {
            let _ = item.set_text(text_enabled.0);
            let _ = item.set_enabled(text_enabled.1);
        }
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
            && !shared.desktop_api_stop.load(Ordering::SeqCst)
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
    use super::is_navigation_allowed;
    use tauri::Url;

    #[test]
    fn allows_endpoint_ui_and_blocks_external() {
        let ep = "http://127.0.0.1:18765";
        assert!(is_navigation_allowed(
            &Url::parse("http://127.0.0.1:18765/ui/").unwrap(),
            ep
        ));
        assert!(is_navigation_allowed(
            &Url::parse("http://127.0.0.1:18765/v1/agents").unwrap(),
            ep
        ));
        assert!(is_navigation_allowed(
            &Url::parse("http://localhost:1422/").unwrap(),
            ep
        ));
        assert!(is_navigation_allowed(
            &Url::parse("about:blank").unwrap(),
            ep
        ));
        assert!(!is_navigation_allowed(
            &Url::parse("https://example.com/").unwrap(),
            ep
        ));
        assert!(!is_navigation_allowed(
            &Url::parse("http://127.0.0.1:9999/ui/").unwrap(),
            ep
        ));
    }
}
