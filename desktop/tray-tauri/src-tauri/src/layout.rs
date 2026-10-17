//! 安装根与 Node 相关路径（对齐 Go `desktop/tray/internal/nodectl`）。

use std::env;
use std::path::{Path, PathBuf};
use std::time::SystemTime;

pub const ENV_HOME: &str = "DAGENTS_HOME";
pub const ENV_DESKTOP_CONFIG: &str = "DAGENTS_DESKTOP_CONFIG";

#[derive(Debug, Clone)]
pub struct Layout {
    pub home: PathBuf,
    pub config_path: PathBuf,
    pub desktop_config_path: PathBuf,
    pub managed_node_path: PathBuf,
    pub node_exe: PathBuf,
    pub log_out: PathBuf,
    pub log_err: PathBuf,
}

impl Layout {
    pub fn resolve(
        home_flag: Option<&str>,
        config_flag: Option<&str>,
        desktop_config_flag: Option<&str>,
    ) -> Result<Self, String> {
        let env_home = env::var_os(ENV_HOME).map(PathBuf::from);
        let exe = env::current_exe().map_err(|e| format!("无法解析可执行文件路径: {e}"))?;
        let cwd = env::current_dir().map_err(|e| format!("无法获取工作目录: {e}"))?;
        Self::resolve_at(
            home_flag,
            env_home.as_deref(),
            &exe,
            &cwd,
            config_flag,
            desktop_config_flag,
        )
    }

    fn resolve_at(
        home_flag: Option<&str>,
        env_home: Option<&Path>,
        exe: &Path,
        cwd: &Path,
        config_flag: Option<&str>,
        desktop_config_flag: Option<&str>,
    ) -> Result<Self, String> {
        let home = resolve_home(home_flag, env_home, exe, cwd)?;
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
        let cfg = cfg
            .canonicalize()
            .unwrap_or_else(|_| absolutize_at(&cfg, cwd));

        let desktop_cfg = match desktop_config_flag.map(str::trim).filter(|s| !s.is_empty()) {
            Some(p) => {
                let path = PathBuf::from(p);
                if path.is_absolute() {
                    path
                } else {
                    absolutize_at(&path, cwd)
                }
            }
            None => default_desktop_config_path()?,
        };

        let runtime = home.join(".runtime");
        let log_dir = runtime.join("logs");
        let day = dated_stamp();

        #[cfg(windows)]
        let node_name = "dagents-node.exe";
        #[cfg(not(windows))]
        let node_name = "dagents-node";

        Ok(Self {
            node_exe: home.join("bin").join(node_name),
            managed_node_path: runtime.join("managed-node.json"),
            log_out: log_dir.join(format!("node-{day}.log")),
            log_err: log_dir.join(format!("node-{day}.err.log")),
            home,
            config_path: cfg,
            desktop_config_path: desktop_cfg,
        })
    }
}

fn default_desktop_config_path() -> Result<PathBuf, String> {
    #[cfg(windows)]
    {
        let root = env::var_os("APPDATA")
            .or_else(|| env::var_os("LOCALAPPDATA"))
            .map(PathBuf::from)
            .ok_or_else(|| "无法解析当前用户的 AppData 目录".to_string())?;
        return Ok(root.join("DAgents").join("desktop").join("settings.json"));
    }
    #[cfg(target_os = "macos")]
    {
        let home = env::var_os("HOME")
            .map(PathBuf::from)
            .ok_or_else(|| "无法解析当前用户主目录".to_string())?;
        return Ok(home
            .join("Library")
            .join("Application Support")
            .join("DAgents")
            .join("desktop")
            .join("settings.json"));
    }
    #[cfg(not(any(windows, target_os = "macos")))]
    {
        let root = env::var_os("XDG_CONFIG_HOME")
            .map(PathBuf::from)
            .or_else(|| env::var_os("HOME").map(|home| PathBuf::from(home).join(".config")))
            .ok_or_else(|| "无法解析 XDG 配置目录".to_string())?;
        Ok(root.join("dagents").join("desktop").join("settings.json"))
    }
}

fn resolve_home(
    home_flag: Option<&str>,
    env_home: Option<&Path>,
    exe: &Path,
    cwd: &Path,
) -> Result<PathBuf, String> {
    if let Some(home) = home_flag {
        let home = home.trim();
        if home.is_empty() {
            return Err("--home 需要非空路径".to_string());
        }
        return Ok(absolutize_at(Path::new(home), cwd));
    }
    if let Some(home) = env_home {
        if !home.as_os_str().to_string_lossy().trim().is_empty() {
            return Ok(absolutize_at(home, cwd));
        }
    }
    let dir = exe
        .parent()
        .ok_or_else(|| "可执行文件无父目录".to_string())?
        .to_path_buf();
    let base = dir.file_name().and_then(|n| n.to_str()).unwrap_or("");
    if base.eq_ignore_ascii_case("bin") {
        return Ok(absolutize_at(
            &dir.parent().map(|p| p.to_path_buf()).unwrap_or(dir),
            cwd,
        ));
    }
    Ok(cwd.to_path_buf())
}

fn absolutize_at(path: &Path, cwd: &Path) -> PathBuf {
    if path.is_absolute() {
        return path.to_path_buf();
    }
    cwd.join(path)
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

#[cfg(test)]
mod tests {
    use super::Layout;
    use std::path::Path;

    fn layout(
        home: Option<&str>,
        env_home: Option<&Path>,
        exe: &Path,
        cwd: &Path,
        config: Option<&str>,
    ) -> Layout {
        Layout::resolve_at(
            home,
            env_home,
            exe,
            cwd,
            config,
            Some("desktop-settings.json"),
        )
        .unwrap()
    }

    #[test]
    fn explicit_home_wins_over_environment_and_resolves_from_current_directory() {
        let temp = std::env::temp_dir();
        let cwd = temp.join("工作目录 含空格");
        let exe = temp
            .join("Other Install")
            .join("bin")
            .join("dagents-shell.exe");
        let env_home = temp.join("Shared").join("Other Install");
        let config = temp.join("Program Files").join("config.yaml");
        let home = "安装目录 含空格";
        let layout = layout(
            Some(home),
            Some(&env_home),
            &exe,
            &cwd,
            Some(config.to_str().unwrap()),
        );

        let expected_home = cwd.join(home);
        assert_eq!(layout.home, expected_home);
        assert_eq!(
            layout.node_exe,
            expected_home.join("bin").join(if cfg!(windows) {
                "dagents-node.exe"
            } else {
                "dagents-node"
            })
        );
        assert_eq!(
            layout.managed_node_path,
            expected_home.join(".runtime").join("managed-node.json")
        );
        assert!(layout
            .log_out
            .starts_with(expected_home.join(".runtime").join("logs")));
        assert!(layout
            .log_err
            .starts_with(expected_home.join(".runtime").join("logs")));
        assert_eq!(layout.config_path, config);
        assert_eq!(
            layout.desktop_config_path,
            cwd.join("desktop-settings.json")
        );
    }

    #[test]
    fn absolute_cli_home_wins_over_environment_home() {
        let temp = std::env::temp_dir();
        let cli_home = temp.join("CLI install root");
        let env_home = temp.join("Environment install root");
        let cwd = temp.join("unrelated cwd");
        let exe = cwd.join("dagents-shell.exe");
        let layout = layout(
            Some(cli_home.to_str().unwrap()),
            Some(&env_home),
            &exe,
            &cwd,
            None,
        );

        assert_eq!(layout.home, cli_home);
        assert_eq!(
            layout.node_exe,
            layout.home.join("bin").join(if cfg!(windows) {
                "dagents-node.exe"
            } else {
                "dagents-node"
            })
        );
    }

    #[test]
    fn environment_home_wins_over_executable_and_working_directory() {
        let temp = std::env::temp_dir();
        let cwd = temp.join("cwd");
        let exe = temp.join("Install").join("bin").join("dagents-shell.exe");
        let env_home = temp.join("Env Home").join("DAgents");
        let layout = layout(None, Some(&env_home), &exe, &cwd, None);
        assert_eq!(layout.home, env_home);
    }

    #[test]
    fn executable_bin_parent_precedes_working_directory() {
        let temp = std::env::temp_dir();
        let cwd = temp.join("cwd");
        let exe = temp
            .join("Install Root")
            .join("bin")
            .join("dagents-shell.exe");
        let layout = layout(None, None, &exe, &cwd, None);
        assert_eq!(layout.home, temp.join("Install Root"));
    }

    #[test]
    fn working_directory_remains_the_fallback_for_legacy_launches() {
        let temp = std::env::temp_dir();
        let cwd = temp.join("cwd");
        let exe = temp.join("portable").join("dagents-shell.exe");
        let layout = layout(None, None, &exe, &cwd, None);
        assert_eq!(layout.home, cwd);
    }

    #[test]
    fn blank_explicit_home_is_rejected() {
        let error = Layout::resolve_at(
            Some(" \t "),
            Some(Path::new("D:/Env Home")),
            Path::new("C:/Install/bin/dagents-shell.exe"),
            Path::new("C:/cwd"),
            None,
            Some("desktop-settings.json"),
        )
        .unwrap_err();
        assert!(error.contains("--home"));
    }
}
