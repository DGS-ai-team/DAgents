//! DAgents Setup — Tauri 安装向导后端。
//!
//! 发版产物为**便携单文件**（无 NSIS 外层）：双击即开向导 UI。
//! Windows：优先使用编译期嵌入的 Inno 包，其次资源目录旁路；`/VERYSILENT` 落地后
//! 可选 doctor / 启动 shell / 打开 Web UI。
//! 非 Windows：提供模拟安装，便于 UI 预览。

use serde::Serialize;
use std::fs;
#[cfg(windows)]
use std::io::Write;
use std::path::{Path, PathBuf};
#[cfg(windows)]
use std::process::Command;
use std::thread;
use std::time::Duration;
use tauri::{AppHandle, Manager, State};
use tauri_plugin_dialog::DialogExt;

/// 发版时由 build.rs 写入 Inno 字节；开发态为空。
const EMBEDDED_INNO: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/embedded_inno.exe"));

#[derive(Debug, thiserror::Error)]
pub enum SetupError {
    #[error("{0}")]
    Message(String),
}

impl Serialize for SetupError {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        serializer.serialize_str(&self.to_string())
    }
}

type SetupResult<T> = Result<T, SetupError>;

#[derive(Clone)]
struct AppState {
    default_dir: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct HostInfo {
    os: String,
    default_install_dir: String,
    has_payload: bool,
    payload_name: Option<String>,
    demo_mode: bool,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct InstallResult {
    ok: bool,
    install_dir: String,
    message: String,
    exit_code: Option<i32>,
}

fn err(msg: impl Into<String>) -> SetupError {
    SetupError::Message(msg.into())
}

fn default_install_dir() -> String {
    #[cfg(windows)]
    {
        std::env::var("LOCALAPPDATA")
            .map(|p| format!(r"{}\Programs\DAgents", p))
            .unwrap_or_else(|_| r"C:\Users\Public\DAgents".into())
    }
    #[cfg(not(windows))]
    {
        std::env::var("HOME")
            .map(|p| format!("{}/Applications/DAgents", p))
            .unwrap_or_else(|_| "/tmp/DAgents".into())
    }
}

fn resource_dir(app: &AppHandle) -> SetupResult<PathBuf> {
    app.path()
        .resource_dir()
        .map_err(|e| err(format!("无法解析资源目录: {e}")))
}

fn is_inno_payload_name(name: &str) -> bool {
    name.contains("dagents-local-assistant") && name.contains("installer")
}

/// 将编译期嵌入的 Inno 字节落到临时文件（便携单文件发版路径）。
#[cfg(windows)]
fn materialize_embedded_payload() -> SetupResult<Option<PathBuf>> {
    if EMBEDDED_INNO.is_empty() {
        return Ok(None);
    }
    let dir = std::env::temp_dir().join("dagents-setup-payload");
    fs::create_dir_all(&dir).map_err(|e| err(format!("创建临时目录失败: {e}")))?;
    let path = dir.join("dagents-local-assistant-windows-amd64-installer-embedded.exe");
    let need_write = match fs::metadata(&path) {
        Ok(meta) => meta.len() as usize != EMBEDDED_INNO.len(),
        Err(_) => true,
    };
    if need_write {
        let mut f = fs::File::create(&path).map_err(|e| err(format!("写入嵌入安装包失败: {e}")))?;
        f.write_all(EMBEDDED_INNO)
            .map_err(|e| err(format!("写入嵌入安装包失败: {e}")))?;
        f.flush()
            .map_err(|e| err(format!("写入嵌入安装包失败: {e}")))?;
    }
    Ok(Some(path))
}

/// 在资源目录（及常见开发旁路路径）中查找 Inno 安装包。
fn find_disk_payload(app: &AppHandle) -> Option<PathBuf> {
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Ok(dir) = resource_dir(app) {
        candidates.push(dir.join("resources"));
        candidates.push(dir);
    }
    // 开发态：packaging/bootstrapper/src-tauri/resources
    if let Ok(cwd) = std::env::current_dir() {
        candidates.push(cwd.join("resources"));
        candidates.push(cwd.join("src-tauri/resources"));
        candidates.push(cwd.join("../resources"));
    }
    // CI 旁路：仓库 dist/
    if let Ok(root) = std::env::var("DAGENTS_REPO_ROOT") {
        candidates.push(PathBuf::from(root).join("dist"));
    }

    for dir in candidates {
        if !dir.is_dir() {
            continue;
        }
        if let Ok(rd) = fs::read_dir(&dir) {
            let mut matches: Vec<PathBuf> = rd
                .filter_map(|e| e.ok())
                .map(|e| e.path())
                .filter(|p| {
                    p.extension()
                        .and_then(|x| x.to_str())
                        .map(|x| x.eq_ignore_ascii_case("exe"))
                        .unwrap_or(false)
                        && p.file_name()
                            .and_then(|n| n.to_str())
                            .map(is_inno_payload_name)
                            .unwrap_or(false)
                })
                .collect();
            matches.sort();
            if let Some(last) = matches.pop() {
                return Some(last);
            }
        }
    }
    None
}

/// 磁盘旁路优先（便于本地联调），否则使用嵌入 payload。
#[cfg(windows)]
fn find_payload(app: &AppHandle) -> Option<PathBuf> {
    if let Some(disk) = find_disk_payload(app) {
        return Some(disk);
    }
    materialize_embedded_payload().ok().flatten()
}

#[tauri::command]
fn get_host_info(app: AppHandle, state: State<'_, AppState>) -> HostInfo {
    let disk = find_disk_payload(&app);
    let has_embedded = !EMBEDDED_INNO.is_empty();
    let has_payload = disk.is_some() || has_embedded;
    let payload_name = disk
        .as_ref()
        .and_then(|p| p.file_name())
        .and_then(|n| n.to_str())
        .map(|s| s.to_string())
        .or_else(|| {
            if has_embedded {
                Some("dagents-local-assistant-installer-embedded.exe".into())
            } else {
                None
            }
        });
    HostInfo {
        os: std::env::consts::OS.to_string(),
        default_install_dir: state.default_dir.clone(),
        has_payload,
        payload_name,
        demo_mode: !cfg!(windows) || !has_payload,
    }
}

#[tauri::command]
fn pick_install_dir(app: AppHandle, current: Option<String>) -> Option<String> {
    let picker = app.dialog().file();
    let picker = if let Some(cur) = current.filter(|s| !s.trim().is_empty()) {
        picker.set_directory(PathBuf::from(cur))
    } else {
        picker
    };
    picker
        .blocking_pick_folder()
        .and_then(|p| p.into_path().ok())
        .map(|p| p.to_string_lossy().to_string())
}

#[cfg(windows)]
fn run_inno_silent(payload: &Path, install_dir: &Path, overwrite_policy: bool) -> SetupResult<i32> {
    let dir = install_dir
        .to_str()
        .ok_or_else(|| err("安装路径包含无效字符"))?;
    let mut args = vec![
        "/VERYSILENT".into(),
        "/SUPPRESSMSGBOXES".into(),
        "/NORESTART".into(),
        "/CLOSEAPPLICATIONS".into(),
        "/RESTARTAPPLICATIONS".into(),
        format!("/DIR={}", dir),
        "/SP-".into(),
    ];
    // 预留：后续用 Inno Tasks/自定义开关传递 overwrite_policy。
    if overwrite_policy {
        args.push("/TASKS=overwritepolicy".into());
    }

    let status = Command::new(payload)
        .args(&args)
        .status()
        .map_err(|e| err(format!("启动 Inno 安装包失败: {e}")))?;
    Ok(status.code().unwrap_or(-1))
}

fn simulate_install(install_dir: &Path) -> SetupResult<()> {
    fs::create_dir_all(install_dir).map_err(|e| err(format!("创建目录失败: {e}")))?;
    let marker = install_dir.join(".dagents-setup-demo");
    fs::write(
        &marker,
        format!(
            "demo install at {}\n",
            chrono_like_now()
        ),
    )
    .map_err(|e| err(format!("写入演示标记失败: {e}")))?;
    thread::sleep(Duration::from_millis(900));
    Ok(())
}

fn chrono_like_now() -> String {
    // 避免引入 chrono 依赖；用系统时间粗展示。
    format!("{:?}", std::time::SystemTime::now())
}

fn post_install_actions(install_dir: &Path, start_shell: bool, open_ui: bool) -> Vec<String> {
    let mut notes = Vec::new();
    let dagents_cmd = install_dir.join("dagents.cmd");
    if !dagents_cmd.is_file() {
        notes.push("未找到 dagents.cmd（演示模式或安装包未落地 CLI）".into());
        return notes;
    }

    #[cfg(windows)]
    {
        let status = Command::new("cmd")
            .args(["/C", dagents_cmd.to_str().unwrap_or("dagents.cmd"), "doctor"])
            .current_dir(install_dir)
            .status();
        match status {
            Ok(s) if s.success() => notes.push("dagents doctor 通过".into()),
            Ok(s) => notes.push(format!("dagents doctor 退出码 {:?}", s.code())),
            Err(e) => notes.push(format!("dagents doctor 失败: {e}")),
        }

        if start_shell {
            let _ = Command::new("cmd")
                .args([
                    "/C",
                    dagents_cmd.to_str().unwrap_or("dagents.cmd"),
                    "shell",
                    "--background",
                ])
                .current_dir(install_dir)
                .spawn();
            notes.push("已请求启动 DAgents Shell".into());
        }
    }

    #[cfg(not(windows))]
    {
        let _ = start_shell;
        notes.push("非 Windows：跳过 doctor / shell".into());
    }

    if open_ui {
        notes.push("可打开 http://127.0.0.1:18765/ui/ 完成连接配置".into());
    }
    notes
}

#[derive(serde::Deserialize)]
#[serde(rename_all = "camelCase")]
struct InstallOptions {
    install_dir: String,
    overwrite_policy: bool,
    start_shell: bool,
    open_ui: bool,
}

#[tauri::command]
fn run_install(app: AppHandle, opts: InstallOptions) -> SetupResult<InstallResult> {
    let install_dir = PathBuf::from(opts.install_dir.trim());
    if opts.install_dir.trim().is_empty() {
        return Err(err("请选择安装目录"));
    }

    #[cfg(windows)]
    {
        if let Some(payload) = find_payload(&app) {
            let code = run_inno_silent(&payload, &install_dir, opts.overwrite_policy)?;
            if code != 0 {
                return Ok(InstallResult {
                    ok: false,
                    install_dir: install_dir.display().to_string(),
                    message: format!("Inno 安装失败，退出码 {code}"),
                    exit_code: Some(code),
                });
            }
            let notes = post_install_actions(&install_dir, opts.start_shell, opts.open_ui);
            return Ok(InstallResult {
                ok: true,
                install_dir: install_dir.display().to_string(),
                message: format!("安装完成。{}", notes.join("；")),
                exit_code: Some(0),
            });
        }
    }

    // 演示 / 无 payload：模拟落地，便于设计与 Linux CI 预览。
    let _ = app;
    simulate_install(&install_dir)?;
    let mut notes = post_install_actions(&install_dir, opts.start_shell, opts.open_ui);
    if opts.overwrite_policy {
        notes.push("演示模式：已记录覆盖 policy 选项（实际生效需 Inno Tasks）".into());
    }
    Ok(InstallResult {
        ok: true,
        install_dir: install_dir.display().to_string(),
        message: format!(
            "演示安装完成（未嵌入 Inno 包或非 Windows）。{}",
            notes.join("；")
        ),
        exit_code: None,
    })
}

#[tauri::command]
fn open_web_ui(app: AppHandle) -> SetupResult<()> {
    tauri_plugin_opener::open_url("http://127.0.0.1:18765/ui/", None::<String>)
        .map_err(|e| err(format!("打开浏览器失败: {e}")))?;
    let _ = app;
    Ok(())
}

#[tauri::command]
fn open_tray(install_dir: String) -> SetupResult<()> {
    let dir = PathBuf::from(install_dir.trim());
    if dir.as_os_str().is_empty() {
        return Err(err("安装目录为空"));
    }
    let dagents_cmd = dir.join("dagents.cmd");
    #[cfg(windows)]
    {
        if !dagents_cmd.is_file() {
            return Err(err("未找到 dagents.cmd，无法启动托盘"));
        }
        Command::new("cmd")
            .args([
                "/C",
                dagents_cmd.to_str().unwrap_or("dagents.cmd"),
                "shell",
                "--background",
            ])
            .current_dir(&dir)
            .spawn()
            .map_err(|e| err(format!("启动托盘失败: {e}")))?;
        return Ok(());
    }
    #[cfg(not(windows))]
    {
        let _ = dagents_cmd;
        Err(err("托盘程序仅支持 Windows"))
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .manage(AppState {
            default_dir: default_install_dir(),
        })
        .invoke_handler(tauri::generate_handler![
            get_host_info,
            pick_install_dir,
            run_install,
            open_web_ui,
            open_tray
        ])
        .run(tauri::generate_context!())
        .expect("error while running DAgents Setup");
}
