//! Manage release check and install orchestration for the desktop shell.

use crate::config::{ShellConfig, DEFAULT_UPDATE_CHANNEL};
use crate::layout::Layout;
use crate::nodeclient::Client;
use crate::nodectl;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::fs;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

pub const EXIT_UP_TO_DATE: i32 = 3;
pub const EXIT_NODE_BUSY: i32 = 4;
const DEFAULT_NODE_STOP_WAIT: Duration = Duration::from_secs(30);
const DEFAULT_NODE_START_WAIT: Duration = Duration::from_secs(45);
const TOKEN_HEADER: &str = "x-dagents-a2a-token";
const AGENT_ID_HEADER: &str = "x-dagents-agent-id";

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct Status {
    pub current_version: String,
    pub latest_version: String,
    pub upgrade_available: bool,
    pub manage_reachable: bool,
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
    pub asset: Option<HashMap<String, Value>>,
    #[serde(default)]
    pub deprecated: bool,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub delegate: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub desktop_api: String,
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
    cfg: Arc<ShellConfig>,
    home: PathBuf,
    status: Mutex<Status>,
    last_upgrade_toast: Mutex<String>,
    on_upgrade: Mutex<Option<Arc<dyn Fn(Status) + Send + Sync>>>,
}

impl Checker {
    pub fn new(cfg: Arc<ShellConfig>, home: PathBuf) -> Self {
        let channel = cfg.manage.update.channel.trim();
        let channel = if channel.is_empty() {
            DEFAULT_UPDATE_CHANNEL.to_string()
        } else {
            channel.to_string()
        };
        let current = read_install_version(&home);
        let mut status = Status {
            current_version: current.clone(),
            latest_version: current,
            channel,
            platform: release_platform(),
            apply_command: "dagents update".into(),
            ..Status::default()
        };
        if !cfg.manage_update_enabled() {
            status.message = "Manage 未启用，无法检查更新".into();
        }
        Self {
            cfg,
            home,
            status: Mutex::new(status),
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
        if !self.cfg.manage_update_enabled() {
            return;
        }
        let checker = Arc::clone(self);
        thread::spawn(move || {
            checker.check_once();
            let interval = checker.cfg.manage_update_check_interval();
            while !stop.load(Ordering::SeqCst) {
                sleep_until_stopped(&stop, interval);
                if !stop.load(Ordering::SeqCst) {
                    checker.check_once();
                }
            }
        });
    }

    pub fn check_once(&self) -> Status {
        let status = self.fetch_check();
        let mut cb: Option<Arc<dyn Fn(Status) + Send + Sync>> = None;
        let mut notify = false;
        if let Ok(mut state) = self.status.lock() {
            *state = status.clone();
        }
        if let Ok(mut last) = self.last_upgrade_toast.lock() {
            if status.manage_reachable && status.upgrade_available {
                let latest = status.latest_version.trim();
                if !latest.is_empty() && latest != last.as_str() {
                    *last = latest.to_string();
                    notify = true;
                }
            } else if !status.upgrade_available {
                last.clear();
            }
        }
        if notify {
            cb = self.on_upgrade.lock().ok().and_then(|g| g.clone());
        }
        if let Some(cb) = cb {
            cb(status.clone());
        }
        status
    }

    fn fetch_check(&self) -> Status {
        check(CheckRequest {
            manage_url: self.cfg.manage.url.clone(),
            current_version: read_install_version(&self.home),
            platform: release_platform(),
            channel: self.cfg.manage.update.channel.clone(),
            agent_id: self.cfg.node_id.clone(),
            node_token: self.cfg.manage.node_token.clone(),
            apply_command: "dagents update".into(),
        })
    }
}

pub struct Applier {
    cfg: Arc<ShellConfig>,
    layout: Layout,
    checker: Arc<Checker>,
    node_client: Arc<Client>,
    mu: Mutex<()>,
}

impl Applier {
    pub fn new(
        cfg: Arc<ShellConfig>,
        layout: Layout,
        checker: Arc<Checker>,
        node_client: Arc<Client>,
    ) -> Self {
        Self {
            cfg,
            layout,
            checker,
            node_client,
            mu: Mutex::new(()),
        }
    }

    pub fn run(&self, opt: ApplyOptions) -> (ApplyResult, i32) {
        let status = self.checker.check_once();
        let mut result = ApplyResult {
            status: status.clone(),
            message: status.message.clone(),
        };
        if opt.check_only {
            print_status(&status);
            if !status.manage_reachable {
                return (result, 1);
            }
            return if status.upgrade_available {
                (result, 0)
            } else {
                (result, EXIT_UP_TO_DATE)
            };
        }
        if !status.manage_reachable {
            eprintln!("update: Manage 不可达，无法升级");
            return (result, 1);
        }
        if !status.upgrade_available {
            println!("当前已是最新版本");
            return (result, EXIT_UP_TO_DATE);
        }
        let Ok(_guard) = self.mu.lock() else {
            return (result, 1);
        };
        if let Some((code, message)) = self.ensure_upgrade_ready() {
            eprintln!("{message}");
            result.message = message;
            return (result, code);
        }
        if !opt.force && !opt.skip_confirm && !confirm_upgrade(&status.latest_version) {
            println!("已取消");
            result.message = "已取消".into();
            return (result, 1);
        }
        let pkg_path = match self.download_package(&status) {
            Ok(path) => path,
            Err(e) => {
                eprintln!("download failed: {e}");
                result.message = format!("download failed: {e}");
                return (result, 1);
            }
        };
        let install_result = (|| {
            nodectl::stop(&self.layout, &self.cfg)?;
            install_release_package(&self.layout.home, &pkg_path)?;
            nodectl::start(&self.layout, &self.cfg, DEFAULT_NODE_START_WAIT)
        })();
        let _ = fs::remove_file(&pkg_path);
        if let Err(e) = install_result {
            eprintln!("install failed: {e}");
            let _ = nodectl::start(&self.layout, &self.cfg, DEFAULT_NODE_STOP_WAIT);
            result.message = format!("install failed: {e}");
            return (result, 1);
        }
        result.status = self.checker.check_once();
        result.message = format!("已升级到 {}", status.latest_version);
        println!("update complete: {}", status.latest_version);
        (result, 0)
    }

    fn ensure_upgrade_ready(&self) -> Option<(i32, String)> {
        let readiness = match self.node_client.upgrade_readiness() {
            Ok(r) => r,
            Err(_) => return None,
        };
        if readiness.ready {
            return None;
        }
        let msg = if readiness.has_active_turn {
            format!(
                "Node 忙碌（{} 个活跃 turn），请稍后再试",
                readiness.active_turn_count
            )
        } else {
            "Node 未就绪，暂无法升级".into()
        };
        Some((EXIT_NODE_BUSY, msg))
    }

    fn download_package(&self, status: &Status) -> Result<PathBuf, String> {
        let asset = status
            .asset
            .as_ref()
            .ok_or_else(|| "update response missing asset".to_string())?;
        let download_url = asset
            .get("download_url")
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .ok_or_else(|| "update response missing download_url".to_string())?;
        let expected_sha = asset
            .get("sha256")
            .and_then(Value::as_str)
            .unwrap_or("")
            .trim()
            .to_string();
        let runtime = self.layout.home.join(".runtime");
        fs::create_dir_all(&runtime).map_err(|e| e.to_string())?;
        let ext = if download_url.to_ascii_lowercase().contains(".zip") {
            "zip"
        } else {
            "pkg"
        };
        let pkg = runtime.join(format!("{}.{}", unix_nanos(), ext));
        download_package(DownloadRequest {
            url: download_url.to_string(),
            dest_path: pkg.clone(),
            expected_sha256: expected_sha,
            agent_id: self.cfg.node_id.clone(),
            node_token: self.cfg.manage.node_token.clone(),
        })?;
        Ok(pkg)
    }
}

#[derive(Debug)]
struct CheckRequest {
    manage_url: String,
    current_version: String,
    platform: String,
    channel: String,
    agent_id: String,
    node_token: String,
    apply_command: String,
}

fn check(req: CheckRequest) -> Status {
    let channel = if req.channel.trim().is_empty() {
        DEFAULT_UPDATE_CHANNEL.to_string()
    } else {
        req.channel.trim().to_string()
    };
    let current = req.current_version.trim().to_string();
    let apply_command = if req.apply_command.trim().is_empty() {
        "dagents update".to_string()
    } else {
        req.apply_command
    };
    let mut base = Status {
        current_version: current.clone(),
        latest_version: current,
        manage_reachable: false,
        channel: channel.clone(),
        platform: req.platform,
        apply_command,
        message: "无法连接 Manage，暂无法检查更新".into(),
        ..Status::default()
    };
    let endpoint = match check_url(&req.manage_url, &base.current_version, &base.platform, &channel)
    {
        Ok(url) => url,
        Err(e) => {
            base.message = e;
            return base;
        }
    };
    let mut http = ureq::get(&endpoint);
    if !req.agent_id.trim().is_empty() {
        http = http.set(AGENT_ID_HEADER, req.agent_id.trim());
    }
    if !req.node_token.trim().is_empty() {
        http = http.set(TOKEN_HEADER, req.node_token.trim());
    }
    let resp = match http.timeout(Duration::from_secs(20)).call() {
        Ok(resp) if (200..300).contains(&resp.status()) => resp,
        _ => return base,
    };
    let raw: Value = match resp.into_json() {
        Ok(raw) => raw,
        Err(_) => return base,
    };
    base.manage_reachable = true;
    base.last_checked_at = rfc3339_now();
    if let Some(latest) = raw.get("latest").and_then(Value::as_str) {
        if !latest.trim().is_empty() {
            base.latest_version = latest.trim().to_string();
        }
    }
    if let Some(notes) = raw.get("release_notes").and_then(Value::as_str) {
        base.release_notes = notes.to_string();
    }
    if let Some(upgrade) = raw.get("upgrade_available").and_then(Value::as_bool) {
        base.upgrade_available = upgrade;
    }
    if let Some(asset) = raw.get("asset").and_then(Value::as_object) {
        let map: HashMap<String, Value> = asset.clone().into_iter().collect();
        base.asset = Some(normalize_asset_urls(&req.manage_url, map));
    }
    base.message = if base.upgrade_available {
        format!("新版本 {} 可用", base.latest_version)
    } else if base.latest_version == base.current_version {
        "当前已是最新版本".into()
    } else {
        "暂无可用升级".into()
    };
    base
}

#[derive(Debug)]
struct DownloadRequest {
    url: String,
    dest_path: PathBuf,
    expected_sha256: String,
    agent_id: String,
    node_token: String,
}

fn download_package(req: DownloadRequest) -> Result<(), String> {
    let url = req.url.trim();
    if url.is_empty() {
        return Err("download url is empty".into());
    }
    let mut http = ureq::get(url);
    if !req.agent_id.trim().is_empty() {
        http = http.set(AGENT_ID_HEADER, req.agent_id.trim());
    }
    if !req.node_token.trim().is_empty() {
        http = http.set(TOKEN_HEADER, req.node_token.trim());
    }
    let resp = http
        .timeout(Duration::from_secs(15 * 60))
        .call()
        .map_err(|e| format!("download HTTP: {e}"))?;
    if !(200..300).contains(&resp.status()) {
        return Err(format!("download HTTP {}", resp.status()));
    }
    let tmp = req.dest_path.with_extension("part");
    let mut out = fs::File::create(&tmp).map_err(|e| e.to_string())?;
    let mut reader = resp.into_reader();
    let mut hasher = Sha256::new();
    let mut buf = [0_u8; 64 * 1024];
    loop {
        let n = reader.read(&mut buf).map_err(|e| e.to_string())?;
        if n == 0 {
            break;
        }
        hasher.update(&buf[..n]);
        out.write_all(&buf[..n]).map_err(|e| e.to_string())?;
    }
    out.flush().map_err(|e| e.to_string())?;
    if !req.expected_sha256.trim().is_empty() {
        let got = hex::encode(hasher.finalize());
        if got != req.expected_sha256.trim().to_ascii_lowercase() {
            let _ = fs::remove_file(&tmp);
            return Err(format!(
                "sha256 mismatch: expected {}, got {got}",
                req.expected_sha256.trim()
            ));
        }
    }
    fs::rename(&tmp, &req.dest_path).map_err(|e| e.to_string())
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
        ("windows", _) => "windows-amd64".into(),
        (os, arch) => format!("{os}-{arch}"),
    }
}

fn check_url(manage_url: &str, current: &str, platform: &str, channel: &str) -> Result<String, String> {
    let base = manage_url.trim().trim_end_matches('/');
    if base.is_empty() {
        return Err("manage.url is empty".into());
    }
    Ok(format!(
        "{base}/v1/releases/check?current={}&platform={}&channel={}",
        query_escape(current),
        query_escape(platform),
        query_escape(channel)
    ))
}

fn normalize_asset_urls(
    manage_base: &str,
    asset: HashMap<String, Value>,
) -> HashMap<String, Value> {
    let mut out = asset;
    let Some(raw) = out.get("download_url").and_then(Value::as_str) else {
        return out;
    };
    let raw = raw.trim();
    if raw.is_empty() || raw.starts_with("http://") || raw.starts_with("https://") {
        return out;
    }
    let base = manage_base.trim().trim_end_matches('/');
    let path = if raw.starts_with('/') {
        raw.to_string()
    } else {
        format!("/{raw}")
    };
    out.insert("download_url".into(), Value::String(format!("{base}{path}")));
    out
}

fn install_release_package(home: &Path, pkg_path: &Path) -> Result<(), String> {
    let staging = std::env::temp_dir().join(format!("dagents-update-{}", unix_nanos()));
    fs::create_dir_all(&staging).map_err(|e| e.to_string())?;
    let result = (|| {
        extract_package(pkg_path, &staging)?;
        let bundle = find_bundle_root(&staging)?;
        let bin_src = bundle.join("bin");
        if !bin_src.is_dir() {
            return Err(format!("release bundle missing bin/: {}", bin_src.display()));
        }
        copy_tree(&bin_src, &home.join("bin"))?;
        copy_if_exists(&bundle.join("dagents.cmd"), &home.join("dagents.cmd"))?;
        copy_if_exists(&bundle.join("VERSION"), &home.join("VERSION"))?;
        Ok(())
    })();
    let _ = fs::remove_dir_all(&staging);
    result
}

fn extract_package(pkg_path: &Path, dest: &Path) -> Result<(), String> {
    let lower = pkg_path.to_string_lossy().to_ascii_lowercase();
    if lower.ends_with(".zip") {
        unzip_file(pkg_path, dest)
    } else {
        let out = Command::new("tar")
            .arg("-xf")
            .arg(pkg_path)
            .arg("-C")
            .arg(dest)
            .output()
            .map_err(|e| format!("tar extract: {e}"))?;
        if out.status.success() {
            Ok(())
        } else {
            Err(format!(
                "tar extract: {}",
                String::from_utf8_lossy(&out.stderr).trim()
            ))
        }
    }
}

fn unzip_file(zip_path: &Path, dest: &Path) -> Result<(), String> {
    let file = fs::File::open(zip_path).map_err(|e| e.to_string())?;
    let mut archive = zip::ZipArchive::new(file).map_err(|e| e.to_string())?;
    let dest_clean = dest.canonicalize().unwrap_or_else(|_| dest.to_path_buf());
    for i in 0..archive.len() {
        let mut file = archive.by_index(i).map_err(|e| e.to_string())?;
        let Some(enclosed) = file.enclosed_name().map(|p| p.to_path_buf()) else {
            return Err(format!("zip entry escapes staging dir: {}", file.name()));
        };
        let target = dest.join(enclosed);
        let target_clean = target
            .parent()
            .and_then(|p| p.canonicalize().ok())
            .unwrap_or_else(|| dest_clean.clone());
        if !target_clean.starts_with(&dest_clean) {
            return Err(format!("zip entry escapes staging dir: {}", file.name()));
        }
        if file.is_dir() {
            fs::create_dir_all(&target).map_err(|e| e.to_string())?;
        } else {
            if let Some(parent) = target.parent() {
                fs::create_dir_all(parent).map_err(|e| e.to_string())?;
            }
            let mut out = fs::File::create(&target).map_err(|e| e.to_string())?;
            std::io::copy(&mut file, &mut out).map_err(|e| e.to_string())?;
        }
    }
    Ok(())
}

fn find_bundle_root(staging: &Path) -> Result<PathBuf, String> {
    for entry in fs::read_dir(staging).map_err(|e| e.to_string())? {
        let entry = entry.map_err(|e| e.to_string())?;
        if entry.file_type().map_err(|e| e.to_string())?.is_dir() {
            return Ok(entry.path());
        }
    }
    if staging.join("bin").is_dir() {
        Ok(staging.to_path_buf())
    } else {
        Err(format!(
            "release bundle root not found under {}",
            staging.display()
        ))
    }
}

fn copy_tree(src: &Path, dest: &Path) -> Result<(), String> {
    fs::create_dir_all(dest).map_err(|e| e.to_string())?;
    for entry in fs::read_dir(src).map_err(|e| e.to_string())? {
        let entry = entry.map_err(|e| e.to_string())?;
        let src_path = entry.path();
        let dest_path = dest.join(entry.file_name());
        if entry.file_type().map_err(|e| e.to_string())?.is_dir() {
            copy_tree(&src_path, &dest_path)?;
        } else {
            fs::copy(&src_path, &dest_path).map_err(|e| e.to_string())?;
        }
    }
    Ok(())
}

fn copy_if_exists(src: &Path, dest: &Path) -> Result<(), String> {
    if src.is_file() {
        if let Some(parent) = dest.parent() {
            fs::create_dir_all(parent).map_err(|e| e.to_string())?;
        }
        fs::copy(src, dest).map_err(|e| e.to_string())?;
    }
    Ok(())
}

fn confirm_upgrade(latest: &str) -> bool {
    print!("升级到 {}？ [y/N] ", latest.trim());
    let _ = std::io::stdout().flush();
    let mut line = String::new();
    if std::io::stdin().read_line(&mut line).is_err() {
        return false;
    }
    matches!(line.trim().to_ascii_lowercase().as_str(), "y" | "yes")
}

pub fn print_status(status: &Status) {
    println!("当前版本: {}", status.current_version);
    println!("最新版本: {}", status.latest_version);
    println!("平台: {}  渠道: {}", status.platform, status.channel);
    if status.manage_reachable {
        println!("Manage: 可达");
    } else {
        println!("Manage: 不可达");
    }
    if !status.message.trim().is_empty() {
        println!("{}", status.message.trim());
    }
}

fn sleep_until_stopped(stop: &AtomicBool, duration: Duration) {
    let deadline = std::time::Instant::now() + duration;
    while !stop.load(Ordering::SeqCst) && std::time::Instant::now() < deadline {
        thread::sleep(Duration::from_millis(250));
    }
}

fn unix_nanos() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0)
}

fn query_escape(value: &str) -> String {
    let mut out = String::with_capacity(value.len());
    for b in value.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char)
            }
            b' ' => out.push('+'),
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

fn rfc3339_now() -> String {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let days = (secs / 86_400) as i64;
    let sod = secs % 86_400;
    let (year, month, day) = civil_from_days(days);
    format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}Z",
        sod / 3600,
        (sod / 60) % 60,
        sod % 60
    )
}

fn civil_from_days(z: i64) -> (i32, u32, u32) {
    let z = z + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = (yoe as i64) + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    (y as i32, m as u32, d as u32)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn platform_matches_go_slug() {
        assert!(!release_platform().is_empty());
    }

    #[test]
    fn builds_check_url() {
        let url = check_url("http://m/", "v 1", "windows-amd64", "stable").unwrap();
        assert_eq!(
            url,
            "http://m/v1/releases/check?current=v+1&platform=windows-amd64&channel=stable"
        );
    }

    #[test]
    fn normalizes_relative_asset_url() {
        let mut asset = HashMap::new();
        asset.insert("download_url".into(), Value::String("files/a.zip".into()));
        let out = normalize_asset_urls("http://m/base/", asset);
        assert_eq!(
            out.get("download_url").and_then(Value::as_str),
            Some("http://m/base/files/a.zip")
        );
    }

    #[test]
    fn defaults_missing_version_to_dev() {
        let dir = std::env::temp_dir().join(format!("dagents-version-{}", unix_nanos()));
        fs::create_dir_all(&dir).unwrap();
        assert_eq!(read_install_version(&dir), "dev");
        let _ = fs::remove_dir_all(&dir);
    }
}
