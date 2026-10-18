//! Node-mediated Desktop update check and local installer orchestration.

use crate::config::ShellConfig;
use crate::layout::Layout;
use crate::nodeclient::Client;
use crate::nodectl;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, RwLock};
use std::thread;
use std::time::Duration;

pub const EXIT_UP_TO_DATE: i32 = 3;
pub const EXIT_NODE_BUSY: i32 = 4;
const DEFAULT_NODE_START_WAIT: Duration = Duration::from_secs(45);

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Status {
    pub current_version: String,
    pub latest_version: String,
    pub upgrade_available: bool,
    pub manage_reachable: bool,
    #[serde(default = "default_platform_supported")]
    pub platform_supported: bool,
    #[serde(default)]
    pub manage_enabled: bool,
    #[serde(default)]
    pub update_enabled: bool,
    #[serde(default, skip_serializing_if = "is_zero_u64")]
    pub check_interval_seconds: u64,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub last_checked_at: String,
    pub channel: String,
    pub platform: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub release_notes: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub message: String,
    pub apply_command: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub asset: Option<std::collections::HashMap<String, Value>>,
}

impl Default for Status {
    fn default() -> Self {
        Self {
            current_version: String::new(),
            latest_version: String::new(),
            upgrade_available: false,
            manage_reachable: false,
            platform_supported: true,
            manage_enabled: false,
            update_enabled: false,
            check_interval_seconds: 0,
            last_checked_at: String::new(),
            channel: String::new(),
            platform: String::new(),
            release_notes: String::new(),
            message: String::new(),
            apply_command: String::new(),
            asset: None,
        }
    }
}

#[derive(Debug, Clone, Default)]
pub struct ApplyOptions {
    pub check_only: bool,
    pub force: bool,
    pub skip_confirm: bool,
}

#[derive(Debug, Clone, Serialize, Default)]
pub struct ApplyResult {
    pub status: Status,
    pub message: String,
}

pub struct Checker {
    cfg: Arc<RwLock<Arc<ShellConfig>>>,
    home: PathBuf,
    node_client: Arc<Client>,
    status: Mutex<Status>,
    check_mu: Mutex<()>,
    started: AtomicBool,
    last_upgrade_toast: Mutex<String>,
    on_upgrade: Mutex<Option<Arc<dyn Fn(Status) + Send + Sync>>>,
}

impl Checker {
    pub fn new(cfg: Arc<ShellConfig>, home: PathBuf, node_client: Arc<Client>) -> Self {
        let current = read_install_version(&home);
        let status = Status {
            current_version: current.clone(),
            latest_version: current,
            channel: "node-managed".into(),
            platform: release_platform(),
            platform_supported: true,
            apply_command: "Desktop 本机安装".into(),
            update_enabled: cfg.update_check_enabled,
            check_interval_seconds: cfg.update_check_interval_seconds,
            message: if cfg.update_check_enabled {
                "等待检查"
            } else {
                "自动检查已关闭"
            }
            .into(),
            ..Status::default()
        };
        Self {
            cfg: Arc::new(RwLock::new(cfg)),
            home,
            node_client,
            status: Mutex::new(status),
            check_mu: Mutex::new(()),
            started: AtomicBool::new(false),
            last_upgrade_toast: Mutex::new(String::new()),
            on_upgrade: Mutex::new(None),
        }
    }

    pub fn set_upgrade_callback(&self, cb: impl Fn(Status) + Send + Sync + 'static) {
        if let Ok(mut guard) = self.on_upgrade.lock() {
            *guard = Some(Arc::new(cb));
        }
    }

    pub fn snapshot(&self) -> Status {
        self.status.lock().map(|s| s.clone()).unwrap_or_default()
    }

    pub fn start(self: &Arc<Self>, stop: Arc<AtomicBool>) {
        if self
            .started
            .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
            .is_err()
        {
            return;
        }
        let checker = Arc::clone(self);
        thread::spawn(move || {
            let mut status = if checker.config_snapshot().update_check_enabled {
                checker.check_once()
            } else {
                checker.snapshot()
            };
            while !stop.load(Ordering::SeqCst) {
                let interval = checker.config_snapshot().update_check_interval();
                sleep_until_stopped(&stop, interval);
                if !stop.load(Ordering::SeqCst) && checker.config_snapshot().update_check_enabled {
                    status = checker.check_once();
                }
            }
            let _ = status;
        });
    }

    pub fn check_once(&self) -> Status {
        let _guard = self.check_mu.lock().unwrap_or_else(|e| e.into_inner());
        let cfg = self.config_snapshot();
        let mut status = match self.node_client.node_update().and_then(|v| {
            serde_json::from_value::<Status>(v).map_err(|e| format!("Node 更新状态格式无效: {e}"))
        }) {
            Ok(mut s) => {
                s.check_interval_seconds = cfg.update_check_interval_seconds;
                s.apply_command = "Desktop 本机安装".into();
                s
            }
            Err(error) => Status {
                current_version: read_install_version(&self.home),
                latest_version: read_install_version(&self.home),
                channel: "node-managed".into(),
                platform: release_platform(),
                check_interval_seconds: cfg.update_check_interval_seconds,
                apply_command: "Desktop 本机安装".into(),
                message: error,
                ..Status::default()
            },
        };
        if status.current_version.is_empty() {
            status.current_version = read_install_version(&self.home);
        }
        let should_notify = status.upgrade_available
            && status.update_enabled
            && status.manage_reachable
            && self
                .last_upgrade_toast
                .lock()
                .map(|mut last| {
                    let latest = status.latest_version.trim();
                    if latest.is_empty() || latest == last.as_str() {
                        false
                    } else {
                        *last = latest.to_string();
                        true
                    }
                })
                .unwrap_or(false);
        if !status.upgrade_available {
            if let Ok(mut last) = self.last_upgrade_toast.lock() {
                last.clear();
            }
        }
        if let Ok(mut state) = self.status.lock() {
            *state = status.clone();
        }
        if should_notify {
            if let Some(cb) = self.on_upgrade.lock().ok().and_then(|g| g.clone()) {
                cb(status.clone());
            }
        }
        status
    }

    pub fn config_snapshot(&self) -> Arc<ShellConfig> {
        self.cfg
            .read()
            .map(|c| Arc::clone(&*c))
            .unwrap_or_else(|e| Arc::clone(&*e.into_inner()))
    }
}

pub struct Applier {
    cfg: Arc<RwLock<Arc<ShellConfig>>>,
    layout: Layout,
    checker: Arc<Checker>,
    node_client: Arc<Client>,
    mu: Mutex<()>,
}

impl Applier {
    pub fn new(layout: Layout, checker: Arc<Checker>, node_client: Arc<Client>) -> Self {
        Self {
            cfg: Arc::clone(&checker.cfg),
            layout,
            checker,
            node_client,
            mu: Mutex::new(()),
        }
    }

    pub fn run(&self, opt: ApplyOptions) -> (ApplyResult, i32) {
        if !opt.check_only && !self.config_snapshot().can_manage_local_node() {
            let status = self.checker.snapshot();
            return (
                ApplyResult {
                    status,
                    message: "隧道模式只连接远程 Node，不支持从 Desktop 更新本机 Node 安装".into(),
                },
                1,
            );
        }
        let status = self.checker.check_once();
        let mut result = ApplyResult {
            status: status.clone(),
            message: status.message.clone(),
        };
        if !status.platform_supported {
            result.message = "当前平台没有可用的更新包".into();
            return (result, 1);
        }
        if opt.check_only {
            print_status(&status);
            return if status.upgrade_available {
                (result, 0)
            } else if status.manage_reachable {
                (result, EXIT_UP_TO_DATE)
            } else {
                (result, 1)
            };
        }
        if !status.update_enabled {
            result.message = "Node 已关闭更新".into();
            return (result, 1);
        }
        if !status.manage_reachable {
            result.message = if status.message.is_empty() {
                "Node 更新服务不可用".into()
            } else {
                status.message.clone()
            };
            return (result, 1);
        }
        if !status.upgrade_available {
            result.message = "当前已是最新版本".into();
            return (result, EXIT_UP_TO_DATE);
        }
        if !nodectl::is_managed(&self.layout, &self.config_snapshot()) {
            result.message = "当前 Node 由外部启动；Desktop 不会停止或升级外部进程".into();
            return (result, 1);
        }
        if !is_windows_installer_asset(&status) {
            result.message = "当前平台或更新包不是受支持的 Windows 安装程序".into();
            return (result, 1);
        }
        let Ok(_guard) = self.mu.lock() else {
            result.message = "更新程序正忙".into();
            return (result, 1);
        };
        if let Some((code, message)) = self.ensure_upgrade_ready() {
            result.message = message;
            return (result, code);
        }
        if !opt.force && !opt.skip_confirm && !confirm_upgrade(&status.latest_version) {
            result.message = "已取消".into();
            return (result, 1);
        }
        let asset = status.asset.as_ref();
        let expected_sha = asset
            .and_then(|a| a.get("sha256"))
            .and_then(Value::as_str)
            .unwrap_or("")
            .trim();
        if expected_sha.len() != 64 || !expected_sha.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            result.message = "Node 更新状态缺少有效 SHA-256".into();
            return (result, 1);
        }
        let package_dir = self
            .layout
            .desktop_config_path
            .parent()
            .unwrap_or(&self.layout.home)
            .join("updates");
        let nonce = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos();
        let package_path = package_dir.join(format!(
            "dagents-update-{}-{nonce}.exe",
            safe_version(&status.latest_version)
        ));
        let package = match self.node_client.download_update_package(
            &package_path,
            &status.latest_version,
            expected_sha,
        ) {
            Ok(package) => package,
            Err(error) => {
                result.message = error;
                return (result, 1);
            }
        };
        // 包下载期间可能有新的 Auto 或聊天回合开始；在停止 Node 前再确认一次。
        if let Some((code, message)) = self.ensure_upgrade_ready() {
            let _ = fs::remove_file(&package.path);
            result.message = message;
            return (result, code);
        }
        let cfg = self.config_snapshot();
        if let Err(error) = nodectl::stop(&self.layout, &cfg) {
            let _ = fs::remove_file(&package.path);
            result.message = format!("停止 Node 失败: {error}");
            return (result, 1);
        }
        if let Err(error) = launch_windows_installer(&package.path, &self.layout.home) {
            let _ = nodectl::start(&self.layout, &cfg, DEFAULT_NODE_START_WAIT);
            let _ = fs::remove_file(&package.path);
            result.message = error;
            return (result, 1);
        }
        result.message = format!("Windows 安装程序已启动，将升级到 {}", status.latest_version);
        (result, 0)
    }

    fn config_snapshot(&self) -> Arc<ShellConfig> {
        self.cfg
            .read()
            .map(|c| Arc::clone(&*c))
            .unwrap_or_else(|e| Arc::clone(&*e.into_inner()))
    }

    fn ensure_upgrade_ready(&self) -> Option<(i32, String)> {
        let readiness = match self.node_client.upgrade_readiness() {
            Ok(r) => r,
            Err(e) => return Some((EXIT_NODE_BUSY, format!("无法确认 Node 升级就绪: {e}"))),
        };
        if readiness.ready {
            return None;
        }
        let message = if readiness.has_active_turn {
            format!(
                "Node 忙碌（{} 个活跃 turn），请稍后再试",
                readiness.active_turn_count
            )
        } else {
            "Node 未就绪，暂无法升级".into()
        };
        Some((EXIT_NODE_BUSY, message))
    }
}

fn is_windows_installer_asset(status: &Status) -> bool {
    status.platform.to_ascii_lowercase().starts_with("windows-")
        && status
            .asset
            .as_ref()
            .and_then(|a| a.get("filename").and_then(Value::as_str))
            .map(|n| n.to_ascii_lowercase().ends_with(".exe"))
            .unwrap_or(false)
}

fn safe_version(version: &str) -> String {
    let sanitized: String = version
        .chars()
        .filter(|c| c.is_ascii_alphanumeric() || *c == '.' || *c == '-')
        .take(80)
        .collect();
    if sanitized.is_empty() {
        "latest".into()
    } else {
        sanitized
    }
}

fn launch_windows_installer(installer_path: &Path, home: &Path) -> Result<(), String> {
    Command::new(installer_path)
        .args([
            "/VERYSILENT",
            "/SUPPRESSMSGBOXES",
            "/NORESTART",
            "/CLOSEAPPLICATIONS",
        ])
        .current_dir(home)
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|err| format!("启动 Windows 安装程序失败: {err}"))
}

pub fn read_install_version(home: &Path) -> String {
    fs::read_to_string(home.join("VERSION"))
        .ok()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "dev".into())
}

pub fn release_platform() -> String {
    match (std::env::consts::OS, std::env::consts::ARCH) {
        ("linux", "aarch64") => "linux-arm64".into(),
        ("linux", _) => "linux-amd64".into(),
        ("windows", "x86") => "windows-386".into(),
        ("windows", _) => "windows-amd64".into(),
        (os, arch) => format!("{os}-{arch}"),
    }
}

fn confirm_upgrade(latest: &str) -> bool {
    print!("升级到 {}？ [y/N] ", latest.trim());
    let _ = std::io::Write::flush(&mut std::io::stdout());
    let mut line = String::new();
    if std::io::stdin().read_line(&mut line).is_err() {
        return false;
    }
    matches!(line.trim().to_ascii_lowercase().as_str(), "y" | "yes")
}

pub fn print_status(status: &Status) {
    println!("当前版本: {}", status.current_version);
    println!("最新版本: {}", status.latest_version);
    println!("平台: {}", status.platform);
    println!(
        "Node 更新服务: {}",
        if status.manage_reachable {
            "可用"
        } else {
            "不可用"
        }
    );
    println!("{}", status.message);
}

fn is_zero_u64(value: &u64) -> bool {
    *value == 0
}

fn default_platform_supported() -> bool {
    true
}

fn sleep_until_stopped(stop: &AtomicBool, duration: Duration) {
    let deadline = std::time::Instant::now() + duration;
    while !stop.load(Ordering::SeqCst) {
        let now = std::time::Instant::now();
        if now >= deadline {
            break;
        }
        thread::sleep((deadline - now).min(Duration::from_millis(500)));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitizes_installer_version_for_local_filename() {
        assert_eq!(safe_version("v1.2/../beta"), "v1.2..beta");
        assert_eq!(safe_version(""), "latest");
    }

    #[test]
    fn accepts_only_windows_exe_assets() {
        let mut asset = std::collections::HashMap::new();
        asset.insert("filename".into(), Value::String("setup.exe".into()));
        let status = Status {
            platform: "windows-amd64".into(),
            asset: Some(asset),
            ..Default::default()
        };
        assert!(is_windows_installer_asset(&status));
        let status = Status {
            platform: "linux-amd64".into(),
            ..status
        };
        assert!(!is_windows_installer_asset(&status));
    }

    #[test]
    fn platform_supported_defaults_true_for_legacy_node_status() {
        let status: Status = serde_json::from_str(
            r#"{"current_version":"0.7.0","latest_version":"0.7.0","upgrade_available":false,"manage_reachable":true,"channel":"stable","platform":"windows-386","apply_command":"dagents update"}"#,
        )
        .expect("legacy Node status should deserialize");
        assert!(status.platform_supported);
        assert!(Status::default().platform_supported);
    }

    #[test]
    fn platform_supported_preserves_explicit_false() {
        let status: Status = serde_json::from_str(
            r#"{"current_version":"0.7.0","latest_version":"0.7.0","upgrade_available":false,"manage_reachable":true,"platform_supported":false,"channel":"stable","platform":"windows-386","apply_command":"dagents update"}"#,
        )
        .expect("current Node status should deserialize");
        assert!(!status.platform_supported);
    }

    #[test]
    fn node_check_failure_keeps_platform_supported() {
        let home = std::env::temp_dir().join(format!(
            "dagents-update-check-failure-{}",
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_nanos()
        ));
        let checker = Checker::new(
            Arc::new(ShellConfig::default()),
            home,
            Arc::new(Client::new("http://127.0.0.1:1")),
        );
        let status = checker.check_once();
        assert!(!status.manage_reachable);
        assert!(status.platform_supported);
        assert!(!status.message.is_empty());
    }
}
