//! 安装根与 Node 相关路径（对齐 Go `desktop/tray/internal/nodectl`）。

use std::env;
use std::path::{Path, PathBuf};
use std::time::SystemTime;

pub const ENV_HOME: &str = "DAGENTS_HOME";

#[derive(Debug, Clone)]
pub struct Layout {
    pub home: PathBuf,
    pub config_path: PathBuf,
    pub node_exe: PathBuf,
    pub pid_file: PathBuf,
    pub log_out: PathBuf,
    pub log_err: PathBuf,
    pub desktop_bridge_token_file: PathBuf,
}

impl Layout {
    pub fn resolve(config_flag: Option<&str>) -> Result<Self, String> {
        let home = resolve_home()?;
        let cfg = match config_flag.map(str::trim).filter(|s| !s.is_empty()) {
            Some(p) => {
                let path = PathBuf::from(p);
                if path.is_absolute() {
                    path
                } else {
                    home.join(path)
                }
            }
            None => home.join("config.yaml"),
        };
        let cfg = cfg.canonicalize().unwrap_or_else(|_| absolutize(&cfg));

        let runtime = home.join(".runtime");
        let log_dir = runtime.join("logs");
        let day = dated_stamp();

        #[cfg(windows)]
        let node_name = "dagents-node.exe";
        #[cfg(not(windows))]
        let node_name = "dagents-node";

        Ok(Self {
            node_exe: home.join("bin").join(node_name),
            pid_file: runtime.join("node.pid"),
            log_out: log_dir.join(format!("node-{day}.log")),
            log_err: log_dir.join(format!("node-{day}.err.log")),
            desktop_bridge_token_file: runtime.join("desktop-bridge.token"),
            home,
            config_path: cfg,
        })
    }

    pub fn relative_config_arg(&self) -> Result<String, String> {
        if let Ok(rel) = self.config_path.strip_prefix(&self.home) {
            return Ok(rel.to_string_lossy().replace('\\', "/"));
        }
        Ok(self.config_path.to_string_lossy().to_string())
    }
}

fn resolve_home() -> Result<PathBuf, String> {
    if let Ok(h) = env::var(ENV_HOME) {
        let t = h.trim();
        if !t.is_empty() {
            return Ok(absolutize(Path::new(t)));
        }
    }
    let exe = env::current_exe().map_err(|e| format!("无法解析可执行文件路径: {e}"))?;
    let dir = exe
        .parent()
        .ok_or_else(|| "可执行文件无父目录".to_string())?
        .to_path_buf();
    let base = dir.file_name().and_then(|n| n.to_str()).unwrap_or("");
    if base.eq_ignore_ascii_case("bin") {
        return Ok(dir.parent().map(|p| p.to_path_buf()).unwrap_or(dir));
    }
    env::current_dir().map_err(|e| format!("无法获取工作目录: {e}"))
}

fn absolutize(path: &Path) -> PathBuf {
    if path.is_absolute() {
        return path.to_path_buf();
    }
    env::current_dir()
        .map(|cwd| cwd.join(path))
        .unwrap_or_else(|_| path.to_path_buf())
}

fn dated_stamp() -> String {
    let secs = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    // UTC YYYY-MM-DD；与 Go 本地日历可能差一天，仅用于日志文件名。
    let days = secs / 86400;
    let (y, m, d) = civil_from_days(days as i64);
    format!("{y:04}-{m:02}-{d:02}")
}

/// Howard Hinnant civil_from_days (proleptic Gregorian).
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
