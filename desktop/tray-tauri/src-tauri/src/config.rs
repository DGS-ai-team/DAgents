//! Desktop-local preferences, stored separately from the supervised Node config.

use serde::{Deserialize, Serialize};
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::Path;
use url::Url;

pub const DEFAULT_NODE_ENDPOINT: &str = "http://127.0.0.1:18765";
pub const DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS: u64 = 6 * 3600;

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum ConnectionMode {
    #[default]
    Local,
    Tunnel,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(default)]
pub struct ShellConfig {
    pub connection_mode: ConnectionMode,
    pub node_endpoint: String,
    pub notification_enabled: bool,
    pub update_check_enabled: bool,
    pub update_check_interval_seconds: u64,
}

impl Default for ShellConfig {
    fn default() -> Self {
        Self {
            connection_mode: ConnectionMode::Local,
            node_endpoint: DEFAULT_NODE_ENDPOINT.into(),
            notification_enabled: true,
            update_check_enabled: true,
            update_check_interval_seconds: DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS,
        }
    }
}

impl ShellConfig {
    pub fn can_manage_local_node(&self) -> bool {
        self.connection_mode == ConnectionMode::Local
    }

    pub fn load_or_create(path: &Path) -> Result<Self, String> {
        if !path.exists() {
            let config = Self::default();
            config.save_atomic(path)?;
            return Ok(config);
        }
        let text = fs::read_to_string(path)
            .map_err(|e| format!("读取 Desktop 设置失败 {}: {e}", path.display()))?;
        let mut config: Self = serde_json::from_str(&text)
            .map_err(|e| format!("解析 Desktop 设置失败 {}: {e}", path.display()))?;
        config.node_endpoint = validate_node_endpoint(&config.node_endpoint)?;
        config.validate()?;
        Ok(config)
    }

    pub fn save_atomic(&self, path: &Path) -> Result<(), String> {
        let mut normalized = self.clone();
        normalized.node_endpoint = validate_node_endpoint(&normalized.node_endpoint)?;
        normalized.validate()?;
        let parent = path
            .parent()
            .ok_or_else(|| "Desktop 设置路径没有父目录".to_string())?;
        fs::create_dir_all(parent).map_err(|e| format!("创建 Desktop 设置目录失败: {e}"))?;
        let bytes = serde_json::to_vec_pretty(&normalized)
            .map_err(|e| format!("序列化 Desktop 设置失败: {e}"))?;
        let nonce = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos();
        let temp = parent.join(format!("settings.json.tmp-{}-{nonce}", std::process::id()));
        let mut temp_file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&temp)
            .map_err(|e| format!("创建 Desktop 设置临时文件失败: {e}"))?;
        temp_file
            .write_all(&bytes)
            .map_err(|e| format!("写入 Desktop 设置临时文件失败: {e}"))?;
        temp_file
            .sync_all()
            .map_err(|e| format!("同步 Desktop 设置临时文件失败: {e}"))?;
        drop(temp_file);
        if let Err(err) = fs::rename(&temp, path) {
            let _ = fs::remove_file(&temp);
            return Err(format!("原子替换 Desktop 设置失败: {err}"));
        }
        Ok(())
    }

    pub fn validate(&self) -> Result<(), String> {
        validate_node_endpoint(&self.node_endpoint)?;
        if !(60..=7 * 24 * 3600).contains(&self.update_check_interval_seconds) {
            return Err("Desktop 更新检查间隔必须为 60 秒至 7 天".into());
        }
        Ok(())
    }

    pub fn update_check_interval(&self) -> std::time::Duration {
        std::time::Duration::from_secs(self.update_check_interval_seconds)
    }
}

pub fn validate_node_endpoint(value: &str) -> Result<String, String> {
    let value = value.trim().trim_end_matches('/');
    let parsed = Url::parse(value).map_err(|_| "Node 地址格式无效".to_string())?;
    if parsed.scheme() != "http"
        || parsed.username() != ""
        || parsed.password().is_some()
        || parsed.query().is_some()
        || parsed.fragment().is_some()
        || !matches!(parsed.path(), "" | "/")
    {
        return Err("Desktop MVP 仅支持不含路径的本机 HTTP Node 地址".into());
    }
    let loopback = match parsed.host() {
        Some(url::Host::Ipv4(ip)) => ip.is_loopback(),
        Some(url::Host::Ipv6(ip)) => ip.is_loopback(),
        Some(url::Host::Domain(host)) => host.eq_ignore_ascii_case("localhost"),
        None => false,
    };
    if !loopback {
        return Err(
            "Node 地址必须是 loopback HTTP；隧道模式请填入预先建立的本机 SSH 转发地址".into(),
        );
    }
    Ok(parsed
        .origin()
        .ascii_serialization()
        .trim_end_matches('/')
        .to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temp_path() -> std::path::PathBuf {
        let n = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        std::env::temp_dir()
            .join(format!("dagents-desktop-settings-{n}"))
            .join("settings.json")
    }

    #[test]
    fn defaults_are_desktop_local_and_round_trip_atomically() {
        let path = temp_path();
        let initial = ShellConfig::load_or_create(&path).unwrap();
        assert_eq!(initial.node_endpoint, DEFAULT_NODE_ENDPOINT);
        assert_eq!(initial.connection_mode, ConnectionMode::Local);
        let mut changed = initial.clone();
        changed.notification_enabled = false;
        changed.update_check_interval_seconds = 900;
        changed.save_atomic(&path).unwrap();
        assert_eq!(ShellConfig::load_or_create(&path).unwrap(), changed);
        let _ = fs::remove_dir_all(path.parent().unwrap());
    }

    #[test]
    fn old_desktop_json_defaults_to_local_and_tunnel_stays_loopback_only() {
        let old: ShellConfig =
            serde_json::from_str(r#"{"node_endpoint":"http://127.0.0.1:18765"}"#).unwrap();
        assert_eq!(old.connection_mode, ConnectionMode::Local);

        let mut tunnel = old;
        tunnel.connection_mode = ConnectionMode::Tunnel;
        tunnel.node_endpoint = "http://localhost:18766".into();
        assert!(tunnel.validate().is_ok());
        tunnel.node_endpoint = "http://node.example:18766".into();
        assert!(tunnel.validate().is_err());
    }

    #[test]
    fn endpoint_requires_loopback_http_origin() {
        assert_eq!(
            validate_node_endpoint("http://localhost:18765/").unwrap(),
            "http://localhost:18765"
        );
        for invalid in [
            "https://127.0.0.1:18765",
            "http://example.com",
            "http://127.0.0.1:2/path",
        ] {
            assert!(
                validate_node_endpoint(invalid).is_err(),
                "accepted {invalid}"
            );
        }
    }
}
