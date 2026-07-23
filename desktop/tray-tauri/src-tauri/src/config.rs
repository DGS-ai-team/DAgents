//! 从安装根 `config.yaml` 读取 local.endpoint / listen（与 Go shared/config 默认值对齐）。

use serde::Deserialize;
use std::fs;
use std::path::Path;

pub const DEFAULT_LISTEN_HOST: &str = "127.0.0.1";
pub const DEFAULT_LISTEN_PORT: u16 = 18765;

#[derive(Debug, Clone)]
pub struct ShellConfig {
    pub endpoint: String,
}

#[derive(Debug, Deserialize)]
struct RawConfig {
    #[serde(default)]
    listen: RawListen,
    #[serde(default)]
    local: RawLocal,
}

#[derive(Debug, Default, Deserialize)]
struct RawListen {
    #[serde(default)]
    host: Option<String>,
    #[serde(default)]
    port: Option<u16>,
}

#[derive(Debug, Default, Deserialize)]
struct RawLocal {
    #[serde(default)]
    endpoint: Option<String>,
}

impl ShellConfig {
    pub fn load(path: &Path) -> Result<Self, String> {
        let text = fs::read_to_string(path)
            .map_err(|e| format!("读取配置失败 {}: {e}", path.display()))?;
        let raw: RawConfig = serde_yaml::from_str(&text)
            .map_err(|e| format!("解析配置失败 {}: {e}", path.display()))?;

        let host = raw
            .listen
            .host
            .filter(|s| !s.trim().is_empty())
            .unwrap_or_else(|| DEFAULT_LISTEN_HOST.to_string());
        let port = raw.listen.port.unwrap_or(DEFAULT_LISTEN_PORT);
        let endpoint = raw
            .local
            .endpoint
            .filter(|s| !s.trim().is_empty())
            .unwrap_or_else(|| format!("http://{host}:{port}"));
        let endpoint = endpoint.trim_end_matches('/').to_string();

        Ok(Self { endpoint })
    }

    pub fn console_url(&self) -> String {
        format!("{}/ui/", self.endpoint.trim_end_matches('/'))
    }

    pub fn health_url(&self) -> String {
        format!("{}/health", self.endpoint.trim_end_matches('/'))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn write_temp(contents: &str) -> std::path::PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let path = std::env::temp_dir().join(format!("dagents-shell-cfg-{nanos}.yaml"));
        fs::write(&path, contents).unwrap();
        path
    }

    #[test]
    fn loads_endpoint_and_defaults() {
        let path = write_temp(
            "listen:\n  host: 127.0.0.1\n  port: 19000\nlocal:\n  endpoint: http://127.0.0.1:19000\n",
        );
        let cfg = ShellConfig::load(&path).unwrap();
        let _ = fs::remove_file(&path);
        assert_eq!(cfg.endpoint, "http://127.0.0.1:19000");
        assert_eq!(cfg.console_url(), "http://127.0.0.1:19000/ui/");
    }

    #[test]
    fn derives_endpoint_from_listen() {
        let path = write_temp("listen:\n  port: 19111\n");
        let cfg = ShellConfig::load(&path).unwrap();
        let _ = fs::remove_file(&path);
        assert_eq!(cfg.endpoint, "http://127.0.0.1:19111");
    }
}
