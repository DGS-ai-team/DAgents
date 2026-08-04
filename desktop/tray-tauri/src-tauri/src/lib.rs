//! DAgents Shell — Tauri 系统托盘（替代 / 并行于 Go `desktop/tray`）。
//!
//! 核心能力（本版）：
//! - 启动 ensure Node、退出 stop Node
//! - 托盘菜单：状态 / 打开控制台 / 启停重启 / 退出
//! - **双击托盘图标**在内嵌 WebView 中打开 Web UI（同源 `/ui/`）
//! - 关闭窗口隐藏到托盘（不停 Node）
//! - health 轮询 + 基础自动恢复
//!
//! 待办 HITL、更新检查、Desktop API、Toast 等仍由 Go Shell 提供；迁移中可并存。

mod config;
mod layout;
mod nodectl;
mod singleinstance;

use config::ShellConfig;
use layout::Layout;
use nodectl::Health;
use singleinstance::{acquire, InstanceError};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, TrayIconBuilder, TrayIconEvent};
use tauri::webview::WebviewWindowBuilder;
use tauri::{AppHandle, Manager, RunEvent, Url, WebviewUrl, WindowEvent};

const ENSURE_TIMEOUT: Duration = Duration::from_secs(45);
const PROBE_INTERVAL: Duration = Duration::from_secs(3);
const WAIT_READY: Duration = Duration::from_secs(30);
const MAIN_WINDOW: &str = "main";

struct Shared {
    layout: Layout,
    cfg: ShellConfig,
    hold_stopped: AtomicBool,
    recovering: AtomicBool,
    last_err: Mutex<Option<String>>,
    status_item: Mutex<Option<MenuItem<tauri::Wry>>>,
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

    let cfg = match ShellConfig::load(&layout.config_path) {
        Ok(c) => c,
        Err(e) => {
            eprintln!("dagents-shell: {e}");
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

    let shared = Arc::new(Shared {
        layout,
        cfg,
        hold_stopped: AtomicBool::new(false),
        recovering: AtomicBool::new(false),
        last_err: Mutex::new(None),
        status_item: Mutex::new(None),
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
                let open =
                    MenuItem::with_id(app, "open_console", "打开控制台", true, None::<&str>)?;
                let open_manage =
                    MenuItem::with_id(app, "open_manage", "打开 Manage", true, None::<&str>)?;
                let start = MenuItem::with_id(app, "start", "启动 Node", true, None::<&str>)?;
                let stop = MenuItem::with_id(app, "stop", "停止 Node", true, None::<&str>)?;
                let restart =
                    MenuItem::with_id(app, "restart", "重启 Node", true, None::<&str>)?;
                let quit = MenuItem::with_id(app, "quit", "退出 Shell", true, None::<&str>)?;
                let sep1 = PredefinedMenuItem::separator(app)?;
                let sep2 = PredefinedMenuItem::separator(app)?;

                {
                    let mut slot = shared.status_item.lock().unwrap();
                    *slot = Some(status.clone());
                }

                let menu = Menu::with_items(
                    app,
                    &[
                        &status,
                        &sep1,
                        &open,
                        &open_manage,
                        &sep2,
                        &start,
                        &stop,
                        &restart,
                        &quit,
                    ],
                )?;

                let tray_shared = Arc::clone(&shared);
                let menu_shared = Arc::clone(&shared);
                let tray_app = app.handle().clone();
                let icon = app
                    .default_window_icon()
                    .cloned()
                    .ok_or_else(|| "缺少默认窗口图标".to_string())?;

                let _tray = TrayIconBuilder::with_id("main")
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

                // 启动 ensure Node + 轮询
                let boot = Arc::clone(&shared);
                let app_handle = app.handle().clone();
                thread::spawn(move || {
                    ensure_node(&boot);
                    refresh_status(&boot);
                    loop {
                        thread::sleep(PROBE_INTERVAL);
                        refresh_status(&boot);
                        maybe_recover(&boot);
                        let _ = &app_handle;
                    }
                });

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
    if matches!(url.host_str(), Some("localhost") | Some("127.0.0.1"))
        && url.port() == Some(1422)
    {
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

fn handle_menu(shared: &Arc<Shared>, app: &AppHandle, id: &str) {
    match id {
        "open_console" => open_console(shared, app),
        "open_manage" => open_manage(shared),
        "start" => {
            shared.hold_stopped.store(false, Ordering::SeqCst);
            run_action(shared, "启动", |s| nodectl::start(&s.layout, &s.cfg, WAIT_READY));
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
        _ => {}
    }
}

fn open_console(shared: &Arc<Shared>, app: &AppHandle) {
    let shared = Arc::clone(shared);
    let app = app.clone();
    thread::spawn(move || {
        if let Err(e) = nodectl::start(&shared.layout, &shared.cfg, WAIT_READY) {
            set_err(&shared, Some(e));
            refresh_status(&shared);
            return;
        }

        let url_str = shared.cfg.console_url();
        let url = match Url::parse(&url_str) {
            Ok(u) => u,
            Err(e) => {
                set_err(&shared, Some(format!("无效控制台 URL {url_str}: {e}")));
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
                    // 已在同源 /ui/ 时仅聚焦，避免无谓刷新。
                    let already_console = win
                        .url()
                        .ok()
                        .map(|u| {
                            let path = u.path();
                            path == "/ui" || path.starts_with("/ui/")
                        })
                        .unwrap_or(false);
                    if !already_console {
                        win.navigate(url).map_err(|e| format!("导航失败: {e}"))?;
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
            refresh_status(&shared);
            return;
        }
        match rx.recv_timeout(Duration::from_secs(10)) {
            Ok(Ok(())) => set_err(&shared, None),
            Ok(Err(e)) => set_err(&shared, Some(e)),
            Err(_) => set_err(&shared, Some("打开控制台超时".into())),
        }
        refresh_status(&shared);
    });
}

fn open_manage(shared: &Arc<Shared>) {
    let shared = Arc::clone(shared);
    thread::spawn(move || {
        match nodectl::open_manage(&shared.layout, &shared.cfg, WAIT_READY) {
            Ok(()) => set_err(&shared, None),
            Err(e) => set_err(&shared, Some(e)),
        }
        refresh_status(&shared);
    });
}

fn ensure_node(shared: &Arc<Shared>) {
    match nodectl::start(&shared.layout, &shared.cfg, WAIT_READY) {
        Ok(()) => set_err(shared, None),
        Err(e) => {
            nodectl::append_shell_log(&shared.layout, &format!("ensure Node: {e}"));
            set_err(shared, Some(e));
        }
    }
}

fn run_action(
    shared: &Arc<Shared>,
    label: &str,
    f: impl FnOnce(&Shared) -> Result<(), String> + Send + 'static,
) {
    let shared = Arc::clone(shared);
    let label = label.to_string();
    thread::spawn(move || {
        match f(&shared) {
            Ok(()) => set_err(&shared, None),
            Err(e) => {
                nodectl::append_shell_log(&shared.layout, &format!("{label} Node 失败: {e}"));
                set_err(&shared, Some(e));
            }
        }
        refresh_status(&shared);
    });
}

fn set_err(shared: &Shared, err: Option<String>) {
    if let Ok(mut g) = shared.last_err.lock() {
        *g = err;
    }
}

fn refresh_status(shared: &Shared) {
    let text = match nodectl::probe(&shared.cfg) {
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
}

fn format_running(h: &Health) -> String {
    if !h.node_id.is_empty() {
        format!("状态：运行中 ({})", h.node_id)
    } else if !h.version.is_empty() {
        format!("状态：运行中 v{}", h.version)
    } else {
        "状态：运行中".into()
    }
}

fn maybe_recover(shared: &Arc<Shared>) {
    if shared.hold_stopped.load(Ordering::SeqCst) {
        return;
    }
    if nodectl::is_running(&shared.cfg) {
        return;
    }
    if shared
        .recovering
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    let shared = Arc::clone(shared);
    thread::spawn(move || {
        let result = nodectl::start(&shared.layout, &shared.cfg, WAIT_READY);
        match result {
            Ok(()) => set_err(&shared, None),
            Err(e) => {
                nodectl::append_shell_log(&shared.layout, &format!("supervisor restart: {e}"));
                set_err(&shared, Some(e));
            }
        }
        shared.recovering.store(false, Ordering::SeqCst);
        refresh_status(&shared);
    });
    let _ = ENSURE_TIMEOUT;
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
        assert!(is_navigation_allowed(&Url::parse("about:blank").unwrap(), ep));
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
