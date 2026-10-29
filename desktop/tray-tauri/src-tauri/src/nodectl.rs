//! Node 启停与探活（对齐 Go `nodectl` 行为的精简版）。

use crate::config::ShellConfig;
use crate::layout::Layout;
use serde::{Deserialize, Serialize};
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

#[derive(Debug, Clone)]
pub struct Health {
    pub ok: bool,
    pub status: String,
    /// 仍从 `/health` 解析，菜单/tooltip 不再展示。
    #[allow(dead_code)]
    pub node_id: String,
    pub version: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
struct ManagedNode {
    pid: u32,
    executable: PathBuf,
    config: PathBuf,
    node_id: String,
    process_start: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ProcessIdentity {
    executable: PathBuf,
    process_start: String,
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
    let url = format!("{}/health", cfg.node_endpoint().trim_end_matches('/'));
    let resp = ureq::get(&url)
        .timeout(Duration::from_secs(3))
        .call()
        .map_err(|e| format!("health 请求失败: {e}"))?;
    if resp.status() != 200 {
        return Err(format!("health status {}", resp.status()));
    }
    let payload: HealthPayload = resp.into_json().map_err(|e| format!("health JSON: {e}"))?;
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

pub fn is_managed(layout: &Layout, cfg: &ShellConfig) -> bool {
    let Some(owner) = read_managed_node(&layout.managed_node_path).ok().flatten() else {
        return false;
    };
    if !owner_matches_layout(&owner, layout)
        || process_identity(owner.pid)
            .as_ref()
            .is_none_or(|identity| !owner_matches_process(&owner, identity))
    {
        return false;
    }
    matches!(probe(cfg), Ok(health) if health.ok && health.node_id == owner.node_id)
}

pub fn owned_process_is_live(layout: &Layout) -> Result<bool, String> {
    let Some(owner) = read_managed_node(&layout.managed_node_path)? else {
        return Ok(false);
    };
    if !owner_matches_layout(&owner, layout) {
        return Err("Node 归属记录与当前安装路径不符；保留记录且不操作进程".into());
    }
    match process_identity(owner.pid) {
        None => Ok(false),
        Some(identity) if owner_matches_process(&owner, &identity) => Ok(true),
        Some(_) => Err("Node 归属记录中的 PID 已被其他进程复用；保留记录且不操作进程".into()),
    }
}

fn owner_matches_layout(owner: &ManagedNode, layout: &Layout) -> bool {
    same_path(&owner.config, &layout.config_path) && same_path(&owner.executable, &layout.node_exe)
}

fn owner_matches_process(owner: &ManagedNode, identity: &ProcessIdentity) -> bool {
    same_path(&identity.executable, &owner.executable)
        && identity.process_start == owner.process_start
}

pub fn start(layout: &Layout, cfg: &ShellConfig, wait_ready: Duration) -> Result<(), String> {
    // 已有健康 Node（包括外部 Node）可以连接使用，但不认领其进程所有权。
    if is_running(cfg) {
        return Ok(());
    }
    if let Some(owner) = read_managed_node(&layout.managed_node_path)? {
        let owner_still_live = process_identity(owner.pid).is_some_and(|identity| {
            same_path(&identity.executable, &owner.executable)
                && identity.process_start == owner.process_start
        });
        if owner_still_live {
            return Err(
                "已有受监督 Node 进程仍在运行但未通过当前 endpoint 探活；不会启动第二个 Node"
                    .into(),
            );
        }
        // 只有 OS 进程身份已失效时才清理陈旧所有权记录；旧 pid 本身不构成停止授权。
        let _ = fs::remove_file(&layout.managed_node_path);
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

    let config_arg = layout.config_path.to_string_lossy().to_string();
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

    let mut child = cmd.spawn().map_err(|e| format!("启动 Node 失败: {e}"))?;
    let pid = child.id();

    thread::sleep(Duration::from_millis(500));
    let first_status = match child.try_wait() {
        Ok(status) => status,
        Err(error) => {
            reap_child(&mut child);
            return Err(format!("检查 Node 启动状态失败: {error}"));
        }
    };
    if let Some(status) = first_status {
        return Err(format!("Node 启动后立即退出: {status}"));
    }

    let health = match wait_healthy(cfg, wait_ready) {
        Ok(health) => health,
        Err(error) => {
            reap_child(&mut child);
            let _ = fs::remove_file(&layout.managed_node_path);
            return Err(error);
        }
    };
    if health.node_id.trim().is_empty() {
        reap_child(&mut child);
        return Err("Node 健康响应缺少 node_id，Desktop 无法记录进程归属".into());
    }
    let second_status = match child.try_wait() {
        Ok(status) => status,
        Err(error) => {
            reap_child(&mut child);
            return Err(format!("验证 Node 进程失败: {error}"));
        }
    };
    if let Some(status) = second_status {
        let _ = fs::remove_file(&layout.managed_node_path);
        return Err(format!("Node 在健康检查后退出: {status}"));
    }
    let identity = match process_identity(pid) {
        Some(identity) => identity,
        None => {
            reap_child(&mut child);
            return Err("无法核验刚启动的 Node 进程身份".into());
        }
    };
    if !same_path(&identity.executable, &layout.node_exe) {
        reap_child(&mut child);
        return Err("Node 进程映像与 Desktop 启动目标不一致".into());
    }
    let owner = ManagedNode {
        pid,
        executable: identity.executable,
        config: layout.config_path.clone(),
        node_id: health.node_id,
        process_start: identity.process_start,
    };
    if let Err(error) = write_managed_node(&layout.managed_node_path, &owner) {
        reap_child(&mut child);
        return Err(error);
    }
    Ok(())
}

pub fn stop(layout: &Layout, cfg: &ShellConfig) -> Result<(), String> {
    let Some(owner) = read_managed_node(&layout.managed_node_path)? else {
        // 无本地 owner 记录时这是明确 no-op；不能用健康 endpoint 或旧裸 PID 推断所有权。
        return Ok(());
    };
    if !same_path(&owner.config, &layout.config_path)
        || !same_path(&owner.executable, &layout.node_exe)
    {
        return Err("记录的 Node 归属与当前启动配置不符；为保护其他进程，未执行停止".into());
    }
    if let Ok(health) = probe(cfg) {
        if health.ok && health.node_id != owner.node_id {
            return Err("当前 endpoint 对应的 Node ID 与受监督进程不符；未执行停止".into());
        }
    }
    if process_identity(owner.pid).is_none() {
        let _ = fs::remove_file(&layout.managed_node_path);
        return Ok(());
    }
    match terminate_owned_process(&owner) {
        Ok(true) => {}
        Ok(false) => return Err("Node 进程身份已变化；为保护其他进程，未执行停止".into()),
        Err(error) => return Err(format!("停止受监督 Node 失败: {error}")),
    }
    let deadline = Instant::now() + Duration::from_secs(15);
    while Instant::now() < deadline {
        let same_process = process_identity(owner.pid).is_some_and(|identity| {
            same_path(&identity.executable, &owner.executable)
                && identity.process_start == owner.process_start
        });
        if !same_process {
            let _ = fs::remove_file(&layout.managed_node_path);
            return Ok(());
        }
        thread::sleep(Duration::from_millis(100));
    }
    Err("受监督 Node 进程在停止请求后仍存在".into())
}

pub fn restart(layout: &Layout, cfg: &ShellConfig, wait_ready: Duration) -> Result<(), String> {
    stop(layout, cfg)?;
    start(layout, cfg, wait_ready)
}

fn wait_healthy(cfg: &ShellConfig, wait: Duration) -> Result<Health, String> {
    let deadline = Instant::now() + wait;
    let mut last = "尚未探活".to_string();
    while Instant::now() < deadline {
        match probe(cfg) {
            Ok(h) if h.ok => return Ok(h),
            Ok(h) => last = format!("status={}", h.status),
            Err(e) => last = e,
        }
        thread::sleep(Duration::from_millis(400));
    }
    Err(format!("等待 /health 超时: {last}"))
}

fn read_managed_node(path: &Path) -> Result<Option<ManagedNode>, String> {
    match fs::read(path) {
        Ok(bytes) => serde_json::from_slice(&bytes)
            .map(Some)
            .map_err(|error| format!("Node 归属记录无法验证，已保留且未停止进程: {error}")),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(format!("读取 Node 归属记录失败，未停止进程: {error}")),
    }
}

fn write_managed_node(path: &Path, owner: &ManagedNode) -> Result<(), String> {
    let parent = path
        .parent()
        .ok_or_else(|| "Node 归属路径无父目录".to_string())?;
    fs::create_dir_all(parent).map_err(|e| format!("创建 Node 归属目录失败: {e}"))?;
    let temp = parent.join(format!(
        "managed-node-{}-{}.tmp",
        std::process::id(),
        owner.process_start
    ));
    let bytes = serde_json::to_vec(owner).map_err(|e| format!("编码 Node 归属失败: {e}"))?;
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&temp)
        .map_err(|e| format!("创建 Node 归属临时文件失败: {e}"))?;
    if let Err(error) = file.write_all(&bytes).and_then(|_| file.sync_all()) {
        let _ = fs::remove_file(&temp);
        return Err(format!("写入 Node 归属失败: {error}"));
    }
    drop(file);
    if let Err(error) = fs::rename(&temp, path) {
        let _ = fs::remove_file(&temp);
        return Err(format!("保存 Node 归属失败: {error}"));
    }
    Ok(())
}

fn same_path(left: &Path, right: &Path) -> bool {
    let left = fs::canonicalize(left).unwrap_or_else(|_| left.to_path_buf());
    let right = fs::canonicalize(right).unwrap_or_else(|_| right.to_path_buf());
    #[cfg(windows)]
    {
        left.to_string_lossy()
            .eq_ignore_ascii_case(&right.to_string_lossy())
    }
    #[cfg(not(windows))]
    {
        left == right
    }
}

fn reap_child(child: &mut Child) {
    if child.try_wait().ok().flatten().is_some() {
        return;
    }
    let _ = child.kill();
    let _ = child.wait();
}

fn process_identity(pid: u32) -> Option<ProcessIdentity> {
    #[cfg(windows)]
    {
        use windows_sys::Win32::Foundation::{CloseHandle, FILETIME};
        use windows_sys::Win32::System::Threading::{
            GetProcessTimes, OpenProcess, QueryFullProcessImageNameW,
            PROCESS_QUERY_LIMITED_INFORMATION,
        };
        let handle = unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid) };
        if handle.is_null() {
            return None;
        }
        let mut created = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut exited = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut kernel = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut user = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut name = vec![0u16; 32768];
        let mut name_len = name.len() as u32;
        let ok = unsafe {
            GetProcessTimes(handle, &mut created, &mut exited, &mut kernel, &mut user) != 0
                && QueryFullProcessImageNameW(handle, 0, name.as_mut_ptr(), &mut name_len) != 0
        };
        unsafe { CloseHandle(handle) };
        if !ok {
            return None;
        }
        name.truncate(name_len as usize);
        Some(ProcessIdentity {
            executable: PathBuf::from(String::from_utf16_lossy(&name)),
            process_start: filetime_value(created).to_string(),
        })
    }
    #[cfg(target_os = "linux")]
    {
        let root = PathBuf::from(format!("/proc/{pid}"));
        let executable = fs::read_link(root.join("exe")).ok()?;
        let stat = fs::read_to_string(root.join("stat")).ok()?;
        let rest = stat.rsplit_once(')')?.1;
        let process_start = rest.split_whitespace().nth(19)?.to_string();
        Some(ProcessIdentity {
            executable,
            process_start,
        })
    }
    #[cfg(target_os = "macos")]
    {
        let output = Command::new("ps")
            .args(["-p", &pid.to_string(), "-o", "lstart=", "-o", "comm="])
            .output()
            .ok()?;
        if !output.status.success() {
            return None;
        }
        let text = String::from_utf8(output.stdout).ok()?;
        let (process_start, executable) = text.trim().split_once("  ")?;
        Some(ProcessIdentity {
            executable: PathBuf::from(executable.trim()),
            process_start: process_start.trim().to_string(),
        })
    }
    #[cfg(not(any(windows, target_os = "linux", target_os = "macos")))]
    {
        let _ = pid;
        None
    }
}

#[cfg(windows)]
fn filetime_value(time: windows_sys::Win32::Foundation::FILETIME) -> u64 {
    ((time.dwHighDateTime as u64) << 32) | time.dwLowDateTime as u64
}

fn terminate_owned_process(owner: &ManagedNode) -> Result<bool, String> {
    #[cfg(windows)]
    {
        use windows_sys::Win32::Foundation::{CloseHandle, FILETIME, WAIT_OBJECT_0};
        use windows_sys::Win32::System::Threading::{
            GetProcessTimes, OpenProcess, QueryFullProcessImageNameW, TerminateProcess,
            WaitForSingleObject, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_TERMINATE,
        };
        const SYNCHRONIZE: u32 = 0x0010_0000;
        let handle = unsafe {
            OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_TERMINATE | SYNCHRONIZE,
                0,
                owner.pid,
            )
        };
        if handle.is_null() {
            return Ok(false);
        }
        let mut created = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut exited = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut kernel = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut user = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut name = vec![0u16; 32768];
        let mut name_len = name.len() as u32;
        let queried = unsafe {
            GetProcessTimes(handle, &mut created, &mut exited, &mut kernel, &mut user) != 0
                && QueryFullProcessImageNameW(handle, 0, name.as_mut_ptr(), &mut name_len) != 0
        };
        name.truncate(name_len as usize);
        let executable = PathBuf::from(String::from_utf16_lossy(&name));
        if !queried
            || filetime_value(created).to_string() != owner.process_start
            || !same_path(&executable, &owner.executable)
        {
            unsafe { CloseHandle(handle) };
            return Ok(false);
        }
        if unsafe { TerminateProcess(handle, 1) } == 0 {
            unsafe { CloseHandle(handle) };
            return Err("TerminateProcess 拒绝了受监督进程".into());
        }
        let waited = unsafe { WaitForSingleObject(handle, 15_000) } == WAIT_OBJECT_0;
        unsafe { CloseHandle(handle) };
        if waited {
            Ok(true)
        } else {
            Err("等待受监督进程退出超时".into())
        }
    }
    #[cfg(not(windows))]
    {
        if !process_identity(owner.pid).is_some_and(|identity| {
            identity.process_start == owner.process_start
                && same_path(&identity.executable, &owner.executable)
        }) {
            return Ok(false);
        }
        let status = Command::new("kill")
            .args(["-TERM", &owner.pid.to_string()])
            .status()
            .map_err(|e| format!("kill: {e}"))?;
        Ok(status.success())
    }
}

/// 打开系统默认浏览器。
pub fn open_url(url: &str) -> Result<(), String> {
    open::that(url).map_err(|e| format!("打开浏览器失败: {e}"))
}

#[derive(Debug, Deserialize)]
struct AgentInfoPayload {
    #[serde(default)]
    node_id: String,
    #[serde(default)]
    manage_enabled: bool,
    #[serde(default)]
    manage_url: String,
    #[serde(default)]
    manage_registered: bool,
}

/// 从 Node `/v1/node/info` 解析 Manage Console URL（附带 node_id）。
pub fn manage_console_url(cfg: &ShellConfig) -> Result<String, String> {
    let info_url = format!("{}/v1/node/info", cfg.node_endpoint().trim_end_matches('/'));
    let resp = ureq::get(&info_url)
        .timeout(Duration::from_secs(3))
        .call()
        .map_err(|e| format!("读取 Node 信息失败: {e}"))?;
    if resp.status() != 200 {
        return Err(format!("agent/info status {}", resp.status()));
    }
    let info: AgentInfoPayload = resp
        .into_json()
        .map_err(|e| format!("agent/info JSON: {e}"))?;
    if !info.manage_enabled {
        return Err("本机 Node 未启用 Manage（设置 › 连接 Manage）".into());
    }
    let base = info.manage_url.trim().trim_end_matches('/');
    if base.is_empty() {
        return Err("Manage URL 为空".into());
    }
    let node_id = info.node_id.trim();
    if node_id.is_empty() {
        return Err("Node ID 为空".into());
    }
    if !info.manage_registered {
        // 仍可打开；Console 会提示未注册。
    }
    Ok(format!(
        "{base}/console/?node_id={}",
        urlencoding_node_id(node_id)
    ))
}

fn urlencoding_node_id(node_id: &str) -> String {
    let mut out = String::with_capacity(node_id.len());
    for b in node_id.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char)
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

/// ensure Node 后用系统浏览器打开 Manage Console（带 node_id）。
pub fn open_manage(layout: &Layout, cfg: &ShellConfig, wait_ready: Duration) -> Result<(), String> {
    start(layout, cfg, wait_ready)?;
    let url = manage_console_url(cfg)?;
    open_url(&url)
}

/// 确保日志目录存在（启动 shell 时）。
pub fn ensure_runtime_dirs(layout: &Layout) -> Result<(), String> {
    if let Some(p) = layout.managed_node_path.parent() {
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

#[cfg(test)]
mod lifecycle_tests {
    use super::*;

    fn fixture() -> (Layout, ManagedNode, ProcessIdentity) {
        let layout = Layout {
            home: PathBuf::from("C:/fixture"),
            config_path: PathBuf::from("C:/fixture/config.yaml"),
            desktop_config_path: PathBuf::from("C:/fixture/desktop.json"),
            managed_node_path: PathBuf::from("C:/fixture/.runtime/managed-node.json"),
            node_exe: PathBuf::from("C:/fixture/bin/dagents-node.exe"),
            log_out: PathBuf::from("C:/fixture/.runtime/node.log"),
            log_err: PathBuf::from("C:/fixture/.runtime/node.err"),
        };
        let identity = ProcessIdentity {
            executable: layout.node_exe.clone(),
            process_start: "windows-filetime-123".into(),
        };
        let owner = ManagedNode {
            pid: 1234,
            executable: identity.executable.clone(),
            config: layout.config_path.clone(),
            node_id: "node-fixture".into(),
            process_start: identity.process_start.clone(),
        };
        (layout, owner, identity)
    }

    #[test]
    fn ownership_requires_matching_executable_config_and_process_start() {
        let (layout, owner, identity) = fixture();
        assert!(owner_matches_layout(&owner, &layout));
        assert!(owner_matches_process(&owner, &identity));

        let mut reused_pid = identity.clone();
        reused_pid.process_start = "different-process".into();
        assert!(!owner_matches_process(&owner, &reused_pid));

        let mut alternate_config = layout.clone();
        alternate_config.config_path = PathBuf::from("C:/fixture/other.yaml");
        assert!(!owner_matches_layout(&owner, &alternate_config));

        let mut alternate_exe = identity;
        alternate_exe.executable = PathBuf::from("C:/other/dagents-node.exe");
        assert!(!owner_matches_process(&owner, &alternate_exe));
    }

    #[test]
    fn stop_without_owner_record_is_noop_and_does_not_probe_external_endpoint() {
        let (mut layout, _, _) = fixture();
        layout.managed_node_path = std::env::temp_dir().join(format!(
            "dagents-no-owner-{}-{}.json",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        let result = stop(&layout, &ShellConfig::default());
        assert!(result.is_ok());
        assert!(!layout.managed_node_path.exists());
    }

    #[test]
    fn invalid_owner_record_fails_closed_and_preserves_evidence() {
        let (mut layout, _, _) = fixture();
        layout.managed_node_path = std::env::temp_dir().join(format!(
            "dagents-invalid-owner-{}-{}.json",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::write(&layout.managed_node_path, b"{}").unwrap();
        let result = stop(&layout, &ShellConfig::default());
        assert!(result.is_err());
        assert_eq!(fs::read(&layout.managed_node_path).unwrap(), b"{}");
        let _ = fs::remove_file(&layout.managed_node_path);
    }

    #[test]
    fn start_connects_to_healthy_external_node_without_claiming_ownership() {
        use std::io::{Read, Write};
        use std::net::TcpListener;

        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut request = [0; 1024];
            let _ = stream.read(&mut request);
            let body = r#"{"status":"ok","node_id":"external-node","version":"test"}"#;
            write!(
                stream,
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            )
            .unwrap();
        });

        let (mut layout, _, _) = fixture();
        layout.managed_node_path = std::env::temp_dir().join(format!(
            "dagents-external-owner-{}-{}.json",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        let cfg = ShellConfig {
            test_node_endpoint: format!("http://{address}"),
            ..ShellConfig::default()
        };

        assert!(start(&layout, &cfg, Duration::from_millis(1)).is_ok());
        server.join().unwrap();
        assert!(!layout.managed_node_path.exists());
    }
}
