//! DAgents Shell — Tauri 系统托盘（替代 / 并行于 Go `desktop/tray`）。
//!
//! 核心能力（本版）：
//! - 启动 ensure Node、退出 stop Node
//! - 托盘菜单：状态 / 打开控制台 / 启停重启 / 退出
//! - **双击托盘图标**打开 Web UI
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
use singleinstance::{acquire, InstanceError, InstanceGuard};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, RunEvent};

const ENSURE_TIMEOUT: Duration = Duration::from_secs(45);
const PROBE_INTERVAL: Duration = Duration::from_secs(3);
const WAIT_READY: Duration = Duration::from_secs(30);

struct Shared {
    layout: Layout,
    cfg: ShellConfig,
    hold_stopped: AtomicBool,
    recovering: AtomicBool,
    last_err: Mutex<Option<String>>,
    status_item: Mutex<Option<MenuItem<tauri::Wry>>>,
    _guard: InstanceGuard,
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
        &format!("shell start endpoint={} config={}", cfg.endpoint, layout.config_path.display()),
    );

    let shared = Arc::new(Shared {
        layout,
        cfg,
        hold_stopped: AtomicBool::new(false),
        recovering: AtomicBool::new(false),
        last_err: Mutex::new(None),
        status_item: Mutex::new(None),
        _guard: guard,
    });

    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .setup({
            let shared = Arc::clone(&shared);
            move |app| {
                let status = MenuItem::with_id(app, "status", "状态：检测中…", false, None::<&str>)?;
                let open = MenuItem::with_id(app, "open_console", "打开控制台", true, None::<&str>)?;
                let start = MenuItem::with_id(app, "start", "启动 Node", true, None::<&str>)?;
                let stop = MenuItem::with_id(app, "stop", "停止 Node", true, None::<&str>)?;
                let restart = MenuItem::with_id(app, "restart", "重启 Node", true, None::<&str>)?;
                let quit = MenuItem::with_id(app, "quit", "退出 Shell", true, None::<&str>)?;
                let sep1 = PredefinedMenuItem::separator(app)?;
                let sep2 = PredefinedMenuItem::separator(app)?;

                {
                    let mut slot = shared.status_item.lock().unwrap();
                    *slot = Some(status.clone());
                }

                let menu = Menu::with_items(
                    app,
                    &[&status, &sep1, &open, &sep2, &start, &stop, &restart, &quit],
                )?;

                let tray_shared = Arc::clone(&shared);
                let menu_shared = Arc::clone(&shared);
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
                            open_console(&tray_shared);
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
            move |_app, event| {
                if let RunEvent::Exit = event {
                    let _ = nodectl::stop(&shared.layout, &shared.cfg);
                }
            }
        });
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
        "open_console" => open_console(shared),
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

fn open_console(shared: &Arc<Shared>) {
    let shared = Arc::clone(shared);
    thread::spawn(move || {
        if let Err(e) = nodectl::start(&shared.layout, &shared.cfg, WAIT_READY) {
            set_err(&shared, Some(e));
            refresh_status(&shared);
            return;
        }
        if let Err(e) = nodectl::open_url(&shared.cfg.console_url()) {
            set_err(&shared, Some(e));
        } else {
            set_err(&shared, None);
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

fn run_action(shared: &Arc<Shared>, label: &str, f: impl FnOnce(&Shared) -> Result<(), String> + Send + 'static) {
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
    // tooltip 在 tray 上：通过 tray_by_id
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
