//! Desktop-local preferences. Node and Browser addresses are fixed loopback
//! endpoints and are deliberately not persisted as user settings.

use serde::{Deserialize, Serialize};
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::Path;

pub const NODE_ENDPOINT: &str = "http://127.0.0.1:18765";
pub const DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS: u64 = 6 * 3600;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(default)]
pub struct ShellConfig {
    #[cfg(test)]
    #[serde(skip, default = "default_node_endpoint")]
    pub(crate) node_endpoint: String,
    pub notification_enabled: bool,
    pub update_check_enabled: bool,
    pub update_check_interval_seconds: u64,
}

impl Default for ShellConfig {
    fn default() -> Self {
        Self {
            #[cfg(test)]
            node_endpoint: default_node_endpoint(),
            notification_enabled: true,
            update_check_enabled: true,
            update_check_interval_seconds: DEFAULT_UPDATE_CHECK_INTERVAL_SECONDS,
        }
    }
}

impl ShellConfig {
    pub fn node_endpoint(&self) -> &str {
        #[cfg(test)]
        {
            return &self.node_endpoint;
        }
        #[cfg(not(test))]
        {
            NODE_ENDPOINT
        }
    }

    pub fn load_or_create(path: &Path) -> Result<Self, String> {
        if !path.exists() {
            let config = Self::default();
            config.save_atomic(path)?;
            return Ok(config);
        }
        let text = fs::read_to_string(path)
            .map_err(|e| format!("读取 Desktop 设置失败 {}: {e}", path.display()))?;
        let config: Self = serde_json::from_str(&text)
            .map_err(|e| format!("解析 Desktop 设置失败 {}: {e}", path.display()))?;
        config.validate()?;
        Ok(config)
    }

    pub fn save_atomic(&self, path: &Path) -> Result<(), String> {
        self.validate()?;
        let parent = path
            .parent()
            .ok_or_else(|| "Desktop 设置路径没有父目录".to_string())?;
        fs::create_dir_all(parent).map_err(|e| format!("创建 Desktop 设置目录失败: {e}"))?;
        let bytes =
            serde_json::to_vec_pretty(self).map_err(|e| format!("序列化 Desktop 设置失败: {e}"))?;
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
        if !(60..=7 * 24 * 3600).contains(&self.update_check_interval_seconds) {
            return Err("Desktop 更新检查间隔必须为 60 秒至 7 天".into());
        }
        Ok(())
    }

    pub fn update_check_interval(&self) -> std::time::Duration {
        std::time::Duration::from_secs(self.update_check_interval_seconds)
    }
}

#[cfg(test)]
fn default_node_endpoint() -> String {
    NODE_ENDPOINT.to_string()
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
    fn defaults_are_local_and_round_trip_atomically() {
        let path = temp_path();
        let initial = ShellConfig::load_or_create(&path).unwrap();
        assert_eq!(initial.node_endpoint(), "http://127.0.0.1:18765");
        let mut changed = initial.clone();
        changed.notification_enabled = false;
        changed.update_check_interval_seconds = 900;
        changed.save_atomic(&path).unwrap();
        assert_eq!(ShellConfig::load_or_create(&path).unwrap(), changed);
        let saved = fs::read_to_string(&path).unwrap();
        assert!(!saved.contains("node_endpoint"));
        assert!(!saved.contains("connection_mode"));
        let _ = fs::remove_dir_all(path.parent().unwrap());
    }

    #[test]
    fn legacy_tunnel_fields_are_ignored_and_cannot_change_local_endpoint() {
        let path = temp_path();
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(
            &path,
            r#"{"connection_mode":"tunnel","node_endpoint":"http://127.0.0.1:18766","notification_enabled":false}"#,
        )
        .unwrap();
        let config = ShellConfig::load_or_create(&path).unwrap();
        assert!(!config.notification_enabled);
        assert_eq!(config.node_endpoint(), "http://127.0.0.1:18765");
        config.save_atomic(&path).unwrap();
        let saved = fs::read_to_string(&path).unwrap();
        assert!(!saved.contains("tunnel"));
        assert!(!saved.contains("18766"));
        let _ = fs::remove_dir_all(path.parent().unwrap());
    }
}
