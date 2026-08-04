//! Node 启停与探活（对齐 Go `nodectl` 行为的精简版）。

use crate::config::ShellConfig;
use crate::layout::Layout;
use serde::Deserialize;
use std::fs::{self, OpenOptions};
use std::io::{Read, Write};
use std::path::Path;
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

#[derive(Debug, Clone)]
pub struct Health {
    pub ok: bool,
    pub status: String,
    pub node_id: String,
    pub version: String,
}

#[derive(Debug, Deserialize)]
struct HealthPayload {
    #[serde(default)]
    status: String,
    #[serde(default)]
    node_id: String,
    #[serde(default)]
    version: String,
}

pub fn probe(cfg: &ShellConfig) -> Result<Health, String> {
    let url = cfg.health_url();
    let resp = ureq::get(&url)
        .timeout(Duration::from_secs(3))
        .call()
        .map_err(|e| format!("health 请求失败: {e}"))?;
    if resp.status() != 200 {
        return Err(format!("health status {}", resp.status()));
    }
    let payload: HealthPayload = resp
        .into_json()
        .map_err(|e| format!("health JSON: {e}"))?;
    Ok(Health {
        ok: payload.status == "ok",
        status: payload.status,
        node_id: payload.node_id,
        version: payload.version,
    })
}

pub fn is_running(cfg: &ShellConfig) -> bool {
    matches!(probe(cfg), Ok(h) if h.ok)
}

pub fn start(layout: &Layout, cfg: &ShellConfig, wait_ready: Duration) -> Result<(), String> {
    // 已有健康 Node（例如 IDE 单独调试）时直接成功，不要求本机 bin 下有可执行文件。
    if is_running(cfg) {
        return Ok(());
    }
    if !layout.node_exe.is_file() {
        return Err(format!("找不到 Node 二进制: {}", layout.node_exe.display()));
    }

    if let Some(parent) = layout.log_out.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("创建日志目录失败: {e}"))?;
    }
    let log_out = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&layout.log_out)
        .map_err(|e| format!("打开日志失败: {e}"))?;
    let log_err = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&layout.log_err)
        .map_err(|e| format!("打开错误日志失败: {e}"))?;

    let config_arg = layout.relative_config_arg()?;
    let mut cmd = Command::new(&layout.node_exe);
    cmd.arg("-config")
        .arg(&config_arg)
        .current_dir(&layout.home)
        .stdout(Stdio::from(log_out))
        .stderr(Stdio::from(log_err));

    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }

    let child = cmd.spawn().map_err(|e| format!("启动 Node 失败: {e}"))?;
    let pid = child.id();

    // 短暂观察是否立刻退出。
    thread::sleep(Duration::from_millis(500));
    // 无法非阻塞 wait 子进程且保留存活；依赖后续 health。写入 pid。
    if let Some(parent) = layout.pid_file.parent() {
        let _ = fs::create_dir_all(parent);
    }
    fs::write(&layout.pid_file, pid.to_string()).map_err(|e| format!("写 pid 失败: {e}"))?;

    wait_healthy(cfg, wait_ready).map_err(|e| {
        let _ = kill_pid(pid);
        let _ = fs::remove_file(&layout.pid_file);
        e
    })?;
    Ok(())
}

pub fn stop(layout: &Layout, cfg: &ShellConfig) -> Result<(), String> {
    if !is_running(cfg) {
        let _ = fs::remove_file(&layout.pid_file);
        return Ok(());
    }
    if let Some(pid) = read_pid(&layout.pid_file) {
        let _ = kill_pid(pid);
        thread::sleep(Duration::from_millis(300));
        let _ = force_kill_pid(pid);
    }

    let deadline = Instant::now() + Duration::from_secs(15);
    while Instant::now() < deadline {
        if !is_running(cfg) {
            let _ = fs::remove_file(&layout.pid_file);
            return Ok(());
        }
        thread::sleep(Duration::from_millis(500));
    }
    Err("停止后 Node 仍响应 /health".into())
}

pub fn restart(layout: &Layout, cfg: &ShellConfig, wait_ready: Duration) -> Result<(), String> {
    stop(layout, cfg)?;
    start(layout, cfg, wait_ready)
}

fn wait_healthy(cfg: &ShellConfig, wait: Duration) -> Result<(), String> {
    let deadline = Instant::now() + wait;
    let mut last = "尚未探活".to_string();
    while Instant::now() < deadline {
        match probe(cfg) {
            Ok(h) if h.ok => return Ok(()),
            Ok(h) => last = format!("status={}", h.status),
            Err(e) => last = e,
        }
        thread::sleep(Duration::from_millis(400));
    }
    Err(format!("等待 /health 超时: {last}"))
}

fn read_pid(path: &Path) -> Option<u32> {
    let mut f = fs::File::open(path).ok()?;
    let mut s = String::new();
    f.read_to_string(&mut s).ok()?;
    s.trim().parse().ok()
}

fn kill_pid(pid: u32) -> Result<(), String> {
    #[cfg(windows)]
    {
        let status = Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T"])
            .status()
            .map_err(|e| format!("taskkill: {e}"))?;
        if status.success() {
            Ok(())
        } else {
            Err(format!("taskkill exit {:?}", status.code()))
        }
    }
    #[cfg(not(windows))]
    {
        let status = Command::new("kill")
            .args(["-TERM", &pid.to_string()])
            .status()
            .map_err(|e| format!("kill: {e}"))?;
        if status.success() {
            Ok(())
        } else {
            Err(format!("kill exit {:?}", status.code()))
        }
    }
}

fn force_kill_pid(pid: u32) -> Result<(), String> {
    #[cfg(windows)]
    {
        let _ = Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T", "/F"])
            .status();
        Ok(())
    }
    #[cfg(not(windows))]
    {
        let _ = Command::new("kill")
            .args(["-KILL", &pid.to_string()])
            .status();
        Ok(())
    }
}

/// 打开系统默认浏览器（保留备用）。
#[allow(dead_code)]
pub fn open_url(url: &str) -> Result<(), String> {
    open::that(url).map_err(|e| format!("打开浏览器失败: {e}"))
}

/// 确保日志目录存在（启动 shell 时）。
pub fn ensure_runtime_dirs(layout: &Layout) -> Result<(), String> {
    if let Some(p) = layout.pid_file.parent() {
        fs::create_dir_all(p).map_err(|e| format!("创建 .runtime 失败: {e}"))?;
    }
    if let Some(p) = layout.log_out.parent() {
        fs::create_dir_all(p).map_err(|e| format!("创建日志目录失败: {e}"))?;
    }
    let _ = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&layout.log_out);
    Ok(())
}

pub fn append_shell_log(layout: &Layout, line: &str) {
    if let Some(parent) = layout.log_out.parent() {
        let path = parent.join(
            layout
                .log_out
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or("node.log")
                .replace("node-", "shell-"),
        );
        if let Ok(mut f) = OpenOptions::new().create(true).append(true).open(path) {
            let _ = writeln!(f, "{line}");
        }
    }
}
