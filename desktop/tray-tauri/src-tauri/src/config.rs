//! 从安装根 `config.yaml` 读取 Shell 需要的配置（与 Go shared/config 默认值对齐）。

use serde::Deserialize;
use std::fs;
use std::path::Path;
use std::time::Duration;

pub const DEFAULT_LISTEN_HOST: &str = "127.0.0.1";
pub const DEFAULT_LISTEN_PORT: u16 = 18765;
pub const DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS: u64 = 6 * 3600;
pub const DEFAULT_UPDATE_CHANNEL: &str = "stable";

#[derive(Debug, Clone)]
pub struct ShellConfig {
    pub node_id: String,
    pub endpoint: String,
    pub manage: ManageConfig,
}

/// Effective settings returned by Node after its bootstrap/YAML/SQLite merge.
/// Shell uses this DTO instead of opening node_settings.db itself.
#[derive(Debug, Clone, Default, Deserialize, PartialEq, Eq)]
pub struct RuntimeConfig {
    #[serde(default)]
    pub node_id: String,
    #[serde(default)]
    pub manage_enabled: bool,
    #[serde(default)]
    pub manage_url: String,
    #[serde(default)]
    pub manage_node_token: String,
    #[serde(default)]
    pub manage_update_enabled: bool,
    #[serde(default)]
    pub manage_update_check_interval_seconds: u64,
    #[serde(default)]
    pub manage_update_channel: String,
}

#[derive(Debug, Clone)]
pub struct ManageConfig {
    pub enabled: bool,
    pub url: String,
    pub node_token: String,
    pub update: ManageUpdateConfig,
}

#[derive(Debug, Clone)]
pub struct ManageUpdateConfig {
    pub enabled: Option<bool>,
    pub check_interval_seconds: u64,
    pub channel: String,
}

#[derive(Debug, Deserialize)]
struct RawConfig {
    #[serde(default)]
    node_id: Option<String>,
    #[serde(default)]
    agent_id: Option<String>,
    #[serde(default)]
    listen: RawListen,
    #[serde(default)]
    local: RawLocal,
    #[serde(default)]
    manage: RawManage,
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
    #[serde(default)]
    node_id: Option<String>,
    #[serde(default)]
    agent_id: Option<String>,
}

#[derive(Debug, Default, Deserialize)]
struct RawManage {
    #[serde(default)]
    enabled: bool,
    #[serde(default)]
    url: Option<String>,
    #[serde(default)]
    node_token: Option<String>,
    #[serde(default)]
    update: RawManageUpdate,
}

#[derive(Debug, Default, Deserialize)]
struct RawManageUpdate {
    #[serde(default)]
    enabled: Option<bool>,
    #[serde(default)]
    check_interval_seconds: Option<u64>,
    #[serde(default)]
    channel: Option<String>,
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
        let node_id = first_non_empty(&[
            raw.node_id.as_deref(),
            raw.agent_id.as_deref(),
            raw.local.node_id.as_deref(),
            raw.local.agent_id.as_deref(),
        ])
        .unwrap_or_default();
        let check_interval_seconds = raw
            .manage
            .update
            .check_interval_seconds
            .filter(|v| *v > 0)
            .unwrap_or(DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS);
        let channel = raw
            .manage
            .update
            .channel
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| DEFAULT_UPDATE_CHANNEL.to_string());
        let manage = ManageConfig {
            enabled: raw.manage.enabled,
            url: raw
                .manage
                .url
                .map(|s| s.trim().trim_end_matches('/').to_string())
                .unwrap_or_default(),
            node_token: raw
                .manage
                .node_token
                .map(|s| s.trim().to_string())
                .unwrap_or_default(),
            update: ManageUpdateConfig {
                enabled: raw.manage.update.enabled,
                check_interval_seconds,
                channel,
            },
        };

        Ok(Self {
            node_id,
            endpoint,
            manage,
        })
    }

    pub fn console_url(&self) -> String {
        format!("{}/ui/", self.endpoint.trim_end_matches('/'))
    }

    pub fn health_url(&self) -> String {
        format!("{}/health", self.endpoint.trim_end_matches('/'))
    }

    pub fn agent_url(&self, agent_id: &str) -> String {
        let base = self.console_url();
        let id = agent_id.trim();
        if id.is_empty() {
            return base;
        }
        format!("{base}agents/{}", path_escape(id))
    }

    pub fn settings_about_url(&self) -> String {
        format!("{}/ui/settings/about", self.endpoint.trim_end_matches('/'))
    }

    pub fn manage_update_enabled(&self) -> bool {
        if !self.manage.enabled {
            return false;
        }
        self.manage.update.enabled.unwrap_or(true)
    }

    pub fn manage_update_check_interval(&self) -> Duration {
        if self.manage.update.check_interval_seconds == 0 {
            return Duration::from_secs(DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS);
        }
        Duration::from_secs(self.manage.update.check_interval_seconds)
    }

    pub fn apply_runtime_config(&mut self, runtime: &RuntimeConfig) {
        if !runtime.node_id.trim().is_empty() {
            self.node_id = runtime.node_id.trim().to_string();
        }
        self.manage.enabled = runtime.manage_enabled;
        self.manage.url = runtime.manage_url.trim().trim_end_matches('/').to_string();
        self.manage.node_token = runtime.manage_node_token.trim().to_string();
        self.manage.update.enabled = Some(runtime.manage_update_enabled);
        if runtime.manage_update_check_interval_seconds > 0 {
            self.manage.update.check_interval_seconds =
                runtime.manage_update_check_interval_seconds;
        }
        if !runtime.manage_update_channel.trim().is_empty() {
            self.manage.update.channel = runtime.manage_update_channel.trim().to_string();
        }
    }
}

fn first_non_empty(values: &[Option<&str>]) -> Option<String> {
    values
        .iter()
        .filter_map(|v| v.map(str::trim))
        .find(|v| !v.is_empty())
        .map(ToString::to_string)
}

fn path_escape(value: &str) -> String {
    let mut out = String::with_capacity(value.len());
    for b in value.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char)
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
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

    #[test]
    fn loads_manage_update_defaults() {
        let path = write_temp(
            "node_id: node-1\nmanage:\n  enabled: true\n  url: http://manage.local/\n  node_token: tok\n",
        );
        let cfg = ShellConfig::load(&path).unwrap();
        let _ = fs::remove_file(&path);
        assert_eq!(cfg.node_id, "node-1");
        assert!(cfg.manage_update_enabled());
        assert_eq!(cfg.manage.url, "http://manage.local");
        assert_eq!(cfg.manage.node_token, "tok");
        assert_eq!(
            cfg.manage_update_check_interval(),
            Duration::from_secs(DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS)
        );
        assert_eq!(cfg.manage.update.channel, DEFAULT_UPDATE_CHANNEL);
    }

    #[test]
    fn manage_update_requires_manage_enabled() {
        let path = write_temp("manage:\n  update:\n    enabled: true\n");
        let cfg = ShellConfig::load(&path).unwrap();
        let _ = fs::remove_file(&path);
        assert!(!cfg.manage_update_enabled());
    }

    #[test]
    fn applies_node_runtime_config_over_bootstrap_values() {
        let path = write_temp("listen:\n  port: 19111\nmanage:\n  enabled: false\n");
        let mut cfg = ShellConfig::load(&path).unwrap();
        let _ = fs::remove_file(&path);
        cfg.apply_runtime_config(&RuntimeConfig {
            node_id: "node-runtime".into(),
            manage_enabled: true,
            manage_url: "http://manage.local/".into(),
            manage_node_token: "secret".into(),
            manage_update_enabled: true,
            manage_update_check_interval_seconds: 42,
            manage_update_channel: "beta".into(),
        });
        assert_eq!(cfg.node_id, "node-runtime");
        assert!(cfg.manage_update_enabled());
        assert_eq!(cfg.manage.url, "http://manage.local");
        assert_eq!(cfg.manage.node_token, "secret");
        assert_eq!(cfg.manage.update.check_interval_seconds, 42);
        assert_eq!(cfg.manage.update.channel, "beta");
    }

    #[test]
    fn builds_webui_urls() {
        let path = write_temp("local:\n  endpoint: http://127.0.0.1:18765\n");
        let cfg = ShellConfig::load(&path).unwrap();
        let _ = fs::remove_file(&path);
        assert_eq!(
            cfg.agent_url("agent 1"),
            "http://127.0.0.1:18765/ui/agents/agent%201"
        );
        assert_eq!(
            cfg.settings_about_url(),
            "http://127.0.0.1:18765/ui/settings/about"
        );
    }
}
