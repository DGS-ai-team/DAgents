//! Node REST/SSE client used by the desktop shell.

use percent_encoding::{utf8_percent_encode, NON_ALPHANUMERIC};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::fs::{self, File, OpenOptions};
use std::io::{BufRead, BufReader, Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

const CLIENT_TOKEN_ENV: &str = "DAGENTS_CLIENT_TOKEN";
pub const MAX_WORKSPACE_FILE_BYTES: usize = 16 * 1024 * 1024;
const MAX_NODE_UPDATE_PACKAGE_BYTES: u64 = 1024 * 1024 * 1024;

fn disposition_filename(value: &str) -> Option<String> {
    let raw = value.split(';').find_map(|part| {
        let (key, value) = part.trim().split_once('=')?;
        key.trim()
            .eq_ignore_ascii_case("filename")
            .then_some(value.trim())
    })?;
    let name = raw.trim_matches('"').trim();
    if name.is_empty() || name.contains(['/', '\\', ':']) || name.chars().any(char::is_control) {
        return None;
    }
    Some(name.to_string())
}

fn create_update_output(dest: &Path) -> Result<File, String> {
    OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(dest)
        .map_err(|e| format!("创建更新包失败: {e}"))
}

pub struct DownloadedPackage {
    pub path: PathBuf,
}

pub struct WorkspaceBinaryResponse {
    pub status: u16,
    pub content_type: String,
    pub etag: String,
    pub body: Vec<u8>,
}

#[derive(Debug)]
pub struct DesktopApiError {
    pub status: Option<u16>,
    pub code: Option<String>,
    pub message: String,
    pub details: Option<Value>,
}

#[derive(Debug, Clone)]
pub struct Client {
    base: String,
    token: String,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct AgentSummary {
    #[serde(default)]
    pub agent_id: String,
    #[serde(default)]
    pub display_name: String,
    #[serde(default)]
    pub agent_type: String,
    #[serde(default)]
    pub active: bool,
    #[serde(default)]
    pub has_active_turn: bool,
    #[serde(default)]
    pub notify_seq: i32,
    #[serde(default)]
    pub ack_seq: i32,
    #[serde(default)]
    pub has_unread: bool,
    #[serde(default)]
    pub has_pending_hitl: bool,
    #[serde(default)]
    pub pending_hitl_items: i32,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct TerminalSummary {
    #[serde(default)]
    pub terminal_id: String,
    #[serde(default)]
    pub agent_id: String,
    #[serde(default)]
    pub config_id: String,
    #[serde(default)]
    pub target_kind: String,
    #[serde(default)]
    pub target_id: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub shell: String,
    #[serde(default)]
    pub cwd: String,
    #[serde(default)]
    pub created_at: String,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
pub struct LinuxTerminalChannel {
    pub channel_id: String,
    #[serde(default)]
    pub display_name: String,
    #[serde(default)]
    pub host: String,
    #[serde(default)]
    pub port: u16,
    #[serde(default)]
    pub username: String,
    #[serde(default)]
    pub is_default: bool,
}

#[derive(Debug, Clone, Deserialize, Default, PartialEq, Eq)]
pub struct AgentInfo {
    #[serde(default)]
    pub node_id: String,
    #[serde(default)]
    pub manage_enabled: bool,
    #[serde(default)]
    pub manage_url: String,
}

#[derive(Debug, Deserialize)]
struct ListAgentsResponse {
    #[serde(default)]
    agents: Vec<AgentSummary>,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct StreamEvent {
    pub event_type: String,
    pub session_id: String,
    pub agent_id: String,
    pub seq: i32,
    pub agent_seq: i32,
    pub event_version: i32,
    pub stream_epoch: String,
    pub delivery: String,
    pub data: HashMap<String, Value>,
}

#[derive(Debug, Clone, Serialize)]
pub struct NodeHealth {
    pub online: bool,
    pub setup_required: bool,
    pub version: String,
    pub error: Option<String>,
}

#[derive(Debug, Deserialize)]
struct StreamEnvelope {
    #[serde(default)]
    agent_id: String,
    #[serde(default, rename = "type")]
    event_type: String,
    #[serde(default)]
    seq: i32,
    #[serde(default)]
    agent_seq: i32,
    #[serde(default)]
    event_version: i32,
    #[serde(default)]
    stream_epoch: String,
    #[serde(default)]
    delivery: String,
    #[serde(default)]
    data: HashMap<String, Value>,
}

#[derive(Debug, Clone, Deserialize, Default)]
pub struct UpgradeReadiness {
    #[serde(default)]
    pub ready: bool,
    #[serde(default)]
    pub has_active_turn: bool,
    #[serde(default)]
    pub active_turn_count: i32,
    #[serde(default)]
    #[allow(dead_code)]
    pub active_session_ids: Vec<String>,
}

#[derive(Debug, Deserialize, Default)]
struct BootstrapOnboarding {
    #[serde(default)]
    onboarding: BootstrapOnboardingFlags,
}

#[derive(Debug, Deserialize, Default)]
struct BootstrapOnboardingFlags {
    /// 缺省视为已完成（与 Node 侧 nil=completed 对齐）；仅显式 false 触发首配。
    #[serde(default)]
    node_profile_completed: Option<bool>,
}

impl Client {
    pub fn new(base: &str) -> Self {
        Self {
            base: base.trim().trim_end_matches('/').to_string(),
            token: std::env::var(CLIENT_TOKEN_ENV)
                .unwrap_or_default()
                .trim()
                .to_string(),
        }
    }

    pub(crate) fn client_token(&self) -> Option<String> {
        (!self.token.is_empty()).then(|| self.token.clone())
    }

    pub fn list_agents(&self) -> Result<Vec<AgentSummary>, String> {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let req = self.authorize(ureq::get(&format!("{}/v1/agents", self.base)));
        let resp = req
            .timeout(Duration::from_secs(30))
            .call()
            .map_err(|e| format!("GET /v1/agents: {e}"))?;
        if resp.status() != 200 {
            return Err(format!("GET /v1/agents: status {}", resp.status()));
        }
        let out: ListAgentsResponse = resp
            .into_json()
            .map_err(|e| format!("GET /v1/agents JSON: {e}"))?;
        Ok(out.agents)
    }

    pub fn list_terminals(&self, agent_id: &str) -> Result<Vec<TerminalSummary>, String> {
        let id = utf8_percent_encode(agent_id.trim(), NON_ALPHANUMERIC).to_string();
        let req = self.authorize(ureq::get(&format!(
            "{}/v1/agents/{id}/terminals",
            self.base
        )));
        let resp = req
            .timeout(Duration::from_secs(10))
            .call()
            .map_err(|e| format!("GET /v1/agents/{id}/terminals: {e}"))?;
        if resp.status() != 200 {
            return Err(format!(
                "GET /v1/agents/{id}/terminals: status {}",
                resp.status()
            ));
        }
        let value: serde_json::Value = resp
            .into_json()
            .map_err(|e| format!("GET /v1/agents/{id}/terminals JSON: {e}"))?;
        serde_json::from_value(value.get("terminals").cloned().unwrap_or_default())
            .map_err(|e| format!("GET /v1/agents/{id}/terminals decode: {e}"))
    }

    pub fn list_agent_linux_terminal_channels(
        &self,
        agent_id: &str,
    ) -> Result<Vec<LinuxTerminalChannel>, String> {
        let id = utf8_percent_encode(agent_id.trim(), NON_ALPHANUMERIC).to_string();
        let channels_url = format!("{}/v1/linux/channels", self.base);
        let bindings_url = format!("{}/v1/agents/{id}/linux-channels", self.base);
        let channels: serde_json::Value = self
            .authorize(ureq::get(&channels_url))
            .timeout(Duration::from_secs(10))
            .call()
            .map_err(|e| format!("GET /v1/linux/channels: {e}"))?
            .into_json()
            .map_err(|e| format!("GET /v1/linux/channels JSON: {e}"))?;
        let bindings: serde_json::Value = self
            .authorize(ureq::get(&bindings_url))
            .timeout(Duration::from_secs(10))
            .call()
            .map_err(|e| format!("GET /v1/agents/{id}/linux-channels: {e}"))?
            .into_json()
            .map_err(|e| format!("GET /v1/agents/{id}/linux-channels JSON: {e}"))?;
        Ok(Self::linux_terminal_channels_from_payloads(
            &channels, &bindings,
        ))
    }

    fn linux_terminal_channels_from_payloads(
        channels: &serde_json::Value,
        bindings: &serde_json::Value,
    ) -> Vec<LinuxTerminalChannel> {
        let channel_rows = channels
            .get("channels")
            .and_then(serde_json::Value::as_array)
            .cloned()
            .unwrap_or_default();
        let binding_rows = bindings
            .get("bindings")
            .and_then(serde_json::Value::as_array)
            .cloned()
            .unwrap_or_default();
        let mut result = Vec::new();
        for binding in binding_rows {
            if binding.get("enabled").and_then(serde_json::Value::as_bool) != Some(true) {
                continue;
            }
            let Some(channel_id) = binding
                .get("channel_id")
                .and_then(serde_json::Value::as_str)
            else {
                continue;
            };
            let Some(channel) = channel_rows.iter().find(|channel| {
                channel
                    .get("channel_id")
                    .and_then(serde_json::Value::as_str)
                    == Some(channel_id)
                    && channel.get("enabled").and_then(serde_json::Value::as_bool) == Some(true)
            }) else {
                continue;
            };
            result.push(LinuxTerminalChannel {
                channel_id: channel_id.to_string(),
                display_name: channel
                    .get("display_name")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or("")
                    .to_string(),
                host: channel
                    .get("host")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or("")
                    .to_string(),
                port: channel
                    .get("port")
                    .and_then(serde_json::Value::as_u64)
                    .unwrap_or(22) as u16,
                username: channel
                    .get("username")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or("")
                    .to_string(),
                is_default: binding
                    .get("is_default")
                    .and_then(serde_json::Value::as_bool)
                    .unwrap_or(false),
            });
        }
        result
    }

    pub fn health(&self) -> NodeHealth {
        let url = format!("{}/health", self.base);
        let result = self
            .authorize(ureq::get(&url))
            .timeout(Duration::from_secs(4))
            .call();
        match result {
            Ok(resp) if resp.status() == 200 => {
                let body: Value = resp.into_json().unwrap_or(Value::Null);
                let version = body
                    .get("version")
                    .and_then(Value::as_str)
                    .unwrap_or("")
                    .to_string();
                let setup_required = self.node_profile_incomplete();
                NodeHealth {
                    online: true,
                    setup_required,
                    version,
                    error: None,
                }
            }
            Ok(resp) => NodeHealth {
                online: false,
                setup_required: false,
                version: String::new(),
                error: Some(format!("Node 返回状态 {}", resp.status())),
            },
            Err(e) => NodeHealth {
                online: false,
                setup_required: false,
                version: String::new(),
                error: Some(e.to_string()),
            },
        }
    }

    pub fn hydrate(&self, agent_id: &str) -> Result<Value, String> {
        let path = format!("/v1/agents/{}/hydrate", path_segment(agent_id)?);
        self.json_request("GET", &path, None)
    }

    #[cfg(test)]
    pub fn send_message(&self, agent_id: &str, content: &str) -> Result<Value, String> {
        self.json_request(
            "POST",
            "/v1/messages",
            Some(serde_json::json!({
                "agent_id": agent_id, "request_type": "message", "content": content
            })),
        )
    }

    pub fn resume_hitl(&self, agent_id: &str, resume_value: Value) -> Result<Value, String> {
        self.json_request(
            "POST",
            "/v1/messages",
            Some(serde_json::json!({
                "agent_id": agent_id, "request_type": "resume", "resume_value": resume_value
            })),
        )
    }

    /// Settings pages use the same authenticated Node client as the workspace.
    /// The route is relative to this client's fixed endpoint; a WebView cannot
    /// use this command to fetch an arbitrary origin.
    pub fn desktop_api_request(
        &self,
        method: &str,
        path: &str,
        body: Option<Value>,
    ) -> Result<Value, DesktopApiError> {
        let method = method.trim().to_ascii_uppercase();
        if !matches!(method.as_str(), "GET" | "POST" | "PUT" | "PATCH" | "DELETE") {
            return Err(DesktopApiError {
                status: None,
                code: Some("method_not_allowed".into()),
                message: "不支持的 Node API 方法".into(),
                details: None,
            });
        }
        let path = validate_desktop_api_path(path).map_err(|message| DesktopApiError {
            status: None,
            code: Some("path_not_allowed".into()),
            message,
            details: None,
        })?;
        if path == "/health" && method != "GET" {
            return Err(DesktopApiError {
                status: None,
                code: Some("method_not_allowed".into()),
                message: "/health 只支持读取".into(),
                details: None,
            });
        }
        if self.base.is_empty() {
            return Err(DesktopApiError {
                status: None,
                code: Some("node_unavailable".into()),
                message: "Node 地址为空".into(),
                details: None,
            });
        }
        let url = format!("{}{}", self.base, path);
        let req = self
            .authorize(ureq::request(&method, &url))
            .timeout(Duration::from_secs(60));
        let response = match body {
            Some(value) => req.send_json(value),
            None => req.call(),
        };
        match response {
            Ok(resp) => {
                let text = resp.into_string().map_err(|error| DesktopApiError {
                    status: None,
                    code: Some("response_read_failed".into()),
                    message: format!("{method} {path} body: {error}"),
                    details: None,
                })?;
                if text.trim().is_empty() {
                    Ok(Value::Null)
                } else {
                    serde_json::from_str(&text).map_err(|error| DesktopApiError {
                        status: None,
                        code: Some("response_decode_failed".into()),
                        message: format!("{method} {path} JSON: {error}"),
                        details: None,
                    })
                }
            }
            Err(ureq::Error::Status(status, response)) => {
                let text = response.into_string().unwrap_or_default();
                let body = serde_json::from_str::<Value>(&text).unwrap_or(Value::Null);
                let payload = body
                    .get("error")
                    .filter(|value| value.is_object())
                    .unwrap_or(&body);
                Err(DesktopApiError {
                    status: Some(status),
                    code: payload
                        .get("code")
                        .and_then(Value::as_str)
                        .map(str::to_string),
                    message: payload
                        .get("message")
                        .or_else(|| payload.get("error"))
                        .and_then(Value::as_str)
                        .map(str::to_string)
                        .unwrap_or_else(|| text.chars().take(240).collect()),
                    details: payload.get("details").cloned(),
                })
            }
            Err(error) => Err(DesktopApiError {
                status: None,
                code: Some("node_request_failed".into()),
                message: format!("{method} {path}: {error}"),
                details: None,
            }),
        }
    }

    pub fn workspace_binary_request(
        &self,
        method: &str,
        path: &str,
        body: Option<Vec<u8>>,
        if_match: Option<&str>,
        if_none_match: Option<&str>,
    ) -> Result<WorkspaceBinaryResponse, String> {
        let method = method.trim().to_ascii_uppercase();
        if !matches!(method.as_str(), "GET" | "PUT" | "POST") {
            return Err("工作区文件只支持 GET、PUT、POST".into());
        }
        let path = validate_workspace_binary_path(path, &method)?;
        if body
            .as_ref()
            .is_some_and(|bytes| bytes.len() > MAX_WORKSPACE_FILE_BYTES)
        {
            return Err("工作区单文件不能超过 16 MiB".into());
        }
        if method == "GET" && body.is_some() {
            return Err("读取工作区文件不能携带内容".into());
        }
        let url = format!("{}{}", self.base, path);
        let mut request = self
            .authorize(ureq::request(&method, &url))
            .timeout(Duration::from_secs(30));
        if let Some(value) = if_match.filter(|value| !value.trim().is_empty()) {
            request = request.set("If-Match", value);
        }
        if let Some(value) = if_none_match.filter(|value| !value.trim().is_empty()) {
            request = request.set("If-None-Match", value);
        }
        if let Some(bytes) = body.as_deref() {
            request = request.set("Content-Type", "application/octet-stream");
            let response = match request.send_bytes(bytes) {
                Ok(response) => response,
                Err(ureq::Error::Status(_, response)) => response,
                Err(error) => return Err(format!("{method} {path}: {error}")),
            };
            read_workspace_binary_response(response)
        } else {
            let response = match request.call() {
                Ok(response) => response,
                Err(ureq::Error::Status(_, response)) => response,
                Err(error) => return Err(format!("{method} {path}: {error}")),
            };
            read_workspace_binary_response(response)
        }
    }

    pub fn agent_media_binary_request(
        &self,
        agent_id: &str,
        media_id: &str,
    ) -> Result<WorkspaceBinaryResponse, String> {
        for (label, value) in [("agent", agent_id), ("media", media_id)] {
            if value.is_empty()
                || value.len() > 128
                || !value
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_'))
            {
                return Err(format!("invalid {label} id for media request"));
            }
        }
        let agent = utf8_percent_encode(agent_id, NON_ALPHANUMERIC);
        let media = utf8_percent_encode(media_id, NON_ALPHANUMERIC);
        let path = format!("/v1/agents/{agent}/media/{media}");
        let url = format!("{}{}", self.base, path);
        let response = match self
            .authorize(ureq::get(&url))
            .timeout(Duration::from_secs(20))
            .call()
        {
            Ok(response) => response,
            Err(ureq::Error::Status(_, response)) => response,
            Err(error) => return Err(format!("GET {path}: {error}")),
        };
        read_workspace_binary_response(response)
    }

    fn json_request(&self, method: &str, path: &str, body: Option<Value>) -> Result<Value, String> {
        if self.base.is_empty() {
            return Err("Node 地址为空".into());
        }
        let url = format!("{}{}", self.base, path);
        let req = self
            .authorize(ureq::request(method, &url))
            .timeout(Duration::from_secs(60));
        let response = match body {
            Some(value) => req.send_json(value),
            None => req.call(),
        };
        match response {
            Ok(resp) => {
                let text = resp
                    .into_string()
                    .map_err(|e| format!("{method} {path} body: {e}"))?;
                if text.trim().is_empty() {
                    Ok(Value::Null)
                } else {
                    serde_json::from_str(&text).map_err(|e| format!("{method} {path} JSON: {e}"))
                }
            }
            Err(ureq::Error::Status(code, resp)) => {
                let body = resp.into_string().unwrap_or_default();
                let detail = serde_json::from_str::<Value>(&body)
                    .ok()
                    .and_then(|v| {
                        v.get("error")
                            .or_else(|| v.get("message"))
                            .and_then(Value::as_str)
                            .map(str::to_string)
                    })
                    .unwrap_or_else(|| body.chars().take(240).collect());
                Err(format!(
                    "{method} {path}: 状态 {code}{}",
                    if detail.is_empty() {
                        String::new()
                    } else {
                        format!(" · {detail}")
                    }
                ))
            }
            Err(e) => Err(format!("{method} {path}: {e}")),
        }
    }

    pub fn agent_info(&self) -> Result<AgentInfo, String> {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let req = self.authorize(ureq::get(&format!("{}/v1/node/info", self.base)));
        let resp = req
            .timeout(Duration::from_secs(5))
            .call()
            .map_err(|e| format!("GET /v1/node/info: {e}"))?;
        if resp.status() != 200 {
            return Err(format!("GET /v1/node/info: status {}", resp.status()));
        }
        resp.into_json()
            .map_err(|e| format!("GET /v1/node/info JSON: {e}"))
    }

    pub fn node_update(&self) -> Result<Value, String> {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let req = self.authorize(ureq::get(&format!("{}/v1/node/update", self.base)));
        let resp = req
            .timeout(Duration::from_secs(30))
            .call()
            .map_err(|e| format!("GET /v1/node/update: {e}"))?;
        if resp.status() != 200 {
            return Err(format!("GET /v1/node/update: status {}", resp.status()));
        }
        resp.into_json()
            .map_err(|e| format!("GET /v1/node/update JSON: {e}"))
    }

    pub fn upgrade_readiness(&self) -> Result<UpgradeReadiness, String> {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let req = self.authorize(ureq::get(&format!(
            "{}/v1/node/upgrade-readiness",
            self.base
        )));
        let resp = req
            .timeout(Duration::from_secs(5))
            .call()
            .map_err(|e| format!("GET /v1/node/upgrade-readiness: {e}"))?;
        if resp.status() != 200 {
            return Err(format!(
                "GET /v1/node/upgrade-readiness: status {}",
                resp.status()
            ));
        }
        resp.into_json()
            .map_err(|e| format!("GET /v1/node/upgrade-readiness JSON: {e}"))
    }

    pub fn download_update_package(
        &self,
        dest: &Path,
        expected_version: &str,
        expected_sha256: &str,
    ) -> Result<DownloadedPackage, String> {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let request = self.authorize(ureq::get(&format!("{}/v1/node/update/package", self.base)));
        let response = request
            .timeout(Duration::from_secs(600))
            .call()
            .map_err(|e| format!("GET /v1/node/update/package: {e}"))?;
        if response.status() != 200 {
            return Err(format!(
                "GET /v1/node/update/package: status {}",
                response.status()
            ));
        }
        let expected_size = response
            .header("Content-Length")
            .and_then(|v| v.parse::<u64>().ok())
            .ok_or_else(|| "更新包缺少有效 Content-Length".to_string())?;
        if expected_size == 0 || expected_size > MAX_NODE_UPDATE_PACKAGE_BYTES {
            return Err("更新包大小超出允许范围".into());
        }
        let expected_sha = response
            .header("X-Node-Package-SHA256")
            .unwrap_or("")
            .trim()
            .to_ascii_lowercase();
        if expected_sha.len() != 64 || !expected_sha.bytes().all(|b| b.is_ascii_hexdigit()) {
            return Err("Node 返回的更新包 SHA-256 无效".into());
        }
        let version = response
            .header("X-Node-Package-Version")
            .unwrap_or("")
            .trim()
            .to_string();
        if version.is_empty() {
            return Err("Node 未返回更新包版本".into());
        }
        if version != expected_version.trim() {
            return Err("更新状态与下载包版本不一致，请重新检查".into());
        }
        if !expected_sha256.trim().is_empty()
            && expected_sha != expected_sha256.trim().to_ascii_lowercase()
        {
            return Err("更新状态与下载包 SHA-256 不一致，请重新检查".into());
        }
        response
            .header("Content-Disposition")
            .and_then(disposition_filename)
            .filter(|name| name.to_ascii_lowercase().ends_with(".exe"))
            .ok_or_else(|| "Node 更新包不是受支持的 Windows 安装程序".to_string())?;
        if let Some(parent) = dest.parent() {
            fs::create_dir_all(parent).map_err(|e| e.to_string())?;
        }
        let mut output = create_update_output(dest)?;
        let result = (|| {
            let mut reader = response.into_reader();
            let mut hasher = Sha256::new();
            let mut total = 0u64;
            let mut buffer = [0u8; 64 * 1024];
            loop {
                let count = reader
                    .read(&mut buffer)
                    .map_err(|e| format!("读取Node更新包失败: {e}"))?;
                if count == 0 {
                    break;
                }
                total += count as u64;
                if total > expected_size || total > MAX_NODE_UPDATE_PACKAGE_BYTES {
                    return Err("更新包实际大小超过声明值".into());
                }
                hasher.update(&buffer[..count]);
                output
                    .write_all(&buffer[..count])
                    .map_err(|e| format!("写入更新包失败: {e}"))?;
            }
            if total != expected_size {
                return Err("更新包下载不完整".into());
            }
            let actual_sha = hex::encode(hasher.finalize());
            if actual_sha != expected_sha {
                return Err("更新包 SHA-256 校验失败".into());
            }
            output
                .sync_all()
                .map_err(|e| format!("同步更新包失败: {e}"))?;
            Ok(DownloadedPackage {
                path: dest.to_path_buf(),
            })
        })();
        if result.is_err() {
            let _ = fs::remove_file(dest);
        }
        result
    }

    /// 查询 Node 首次配置状态。失败时不阻止 Desktop 显示 Node 错误。
    pub fn node_profile_incomplete(&self) -> bool {
        if self.base.is_empty() {
            return false;
        }
        let req = self.authorize(ureq::get(&format!("{}/v1/settings", self.base)));
        let Ok(resp) = req.timeout(Duration::from_secs(5)).call() else {
            return false;
        };
        if resp.status() != 200 {
            return false;
        }
        let Ok(body) = resp.into_json::<BootstrapOnboarding>() else {
            return false;
        };
        body.onboarding.node_profile_completed == Some(false)
    }

    #[cfg(test)]
    pub fn stream_events<F>(&self, stop: &AtomicBool, mut handler: F) -> Result<(), String>
    where
        F: FnMut(StreamEvent) -> bool,
    {
        self.stream_events_after(stop, None, || {}, &mut handler)
    }

    /// Subscribe to the live stream initially, then resume from the last
    /// process-global cursor after reconnect. `on_connected` runs only after
    /// the HTTP stream is established, so callers can reconcile durable state
    /// without treating an offline retry as a successful resync.
    pub fn stream_events_after<F, C>(
        &self,
        stop: &AtomicBool,
        after_seq: Option<i32>,
        on_connected: C,
        mut handler: F,
    ) -> Result<(), String>
    where
        F: FnMut(StreamEvent) -> bool,
        C: FnOnce(),
    {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let url = match after_seq {
            Some(seq) => format!("{}/v1/streams?after_seq={seq}", self.base),
            None => format!("{}/v1/streams?live=1", self.base),
        };
        let req = self
            .authorize(ureq::get(&url))
            .set("Accept", "text/event-stream");
        // 勿设 timeout(0)：ureq/Windows 会当成零读超时，空闲 SSE 立刻失败并重连刷屏。
        // 不设 overall timeout，与 Go tray `http.Client{Timeout: 0}`（无限）对齐。
        let resp = req.call().map_err(|e| format!("GET /v1/streams: {e}"))?;
        if resp.status() != 200 {
            return Err(format!("GET /v1/streams: status {}", resp.status()));
        }
        on_connected();
        parse_sse(stop, resp.into_reader(), &mut handler)
    }

    fn authorize<'a>(&self, req: ureq::Request) -> ureq::Request {
        if self.token.is_empty() {
            req
        } else {
            req.set("Authorization", &format!("Bearer {}", self.token))
        }
    }
}

fn read_workspace_binary_response(
    response: ureq::Response,
) -> Result<WorkspaceBinaryResponse, String> {
    let status = response.status();
    let content_type = response
        .header("Content-Type")
        .unwrap_or("application/octet-stream")
        .to_string();
    let etag = response.header("ETag").unwrap_or("").to_string();
    let mut body = Vec::new();
    response
        .into_reader()
        .take((MAX_WORKSPACE_FILE_BYTES + 1) as u64)
        .read_to_end(&mut body)
        .map_err(|error| format!("读取工作区响应失败: {error}"))?;
    if body.len() > MAX_WORKSPACE_FILE_BYTES {
        return Err("Node 文件响应超过 16 MiB".into());
    }
    Ok(WorkspaceBinaryResponse {
        status,
        content_type,
        etag,
        body,
    })
}

pub fn validate_workspace_binary_path(path: &str, method: &str) -> Result<String, String> {
    let normalized = validate_desktop_api_path(path)?;
    let url = url::Url::parse(&format!("http://localhost{normalized}"))
        .map_err(|_| "工作区文件路径无效".to_string())?;
    let parts: Vec<_> = url.path().split('/').collect();
    if parts.len() != 6
        || parts[1] != "v1"
        || parts[2] != "agents"
        || parts[3].is_empty()
        || parts[4] != "workspace"
    {
        return Err("工作区文件路径无效".into());
    }
    let endpoint = parts[5];
    if !matches!(
        (method, endpoint),
        ("GET", "file") | ("PUT", "file") | ("POST", "upload")
    ) {
        return Err("工作区文件路径不允许此操作".into());
    }
    let relative = url
        .query_pairs()
        .find(|(key, _)| key == "path")
        .map(|(_, value)| value.into_owned())
        .unwrap_or_default();
    if relative.contains('\\')
        || relative.starts_with('/')
        || relative.contains(':')
        || relative
            .split('/')
            .any(|segment| matches!(segment, ".." | "."))
    {
        return Err("工作区路径必须是安全的相对路径".into());
    }
    if endpoint == "file" && relative.trim().is_empty() {
        return Err("文件路径不能为空".into());
    }
    Ok(normalized)
}

fn validate_desktop_api_path(path: &str) -> Result<String, String> {
    let path = path.trim();
    if path == "/health" {
        return Ok(path.to_string());
    }
    if !path.starts_with("/v1/")
        || path.contains('\\')
        || path.contains('#')
        || path.chars().any(char::is_control)
    {
        return Err("无效的 Node API 路径".into());
    }
    let parsed = url::Url::parse(&format!("http://localhost{path}"))
        .map_err(|_| "无效的 Node API 路径".to_string())?;
    if !parsed.path().starts_with("/v1/") {
        return Err("Node API 路径不能越过 /v1/".into());
    }
    Ok(format!(
        "{}{}",
        parsed.path(),
        parsed
            .query()
            .map(|query| format!("?{query}"))
            .unwrap_or_default()
    ))
}

fn path_segment(value: &str) -> Result<String, String> {
    let value = value.trim();
    if value.is_empty()
        || value.contains('/')
        || value.contains('\\')
        || value == "."
        || value == ".."
    {
        return Err("无效的 Agent ID".into());
    }
    Ok(value
        .bytes()
        .map(|b| {
            if b.is_ascii_alphanumeric() || b"-_.~".contains(&b) {
                (b as char).to_string()
            } else {
                format!("%{b:02X}")
            }
        })
        .collect())
}

fn parse_sse<R, F>(stop: &AtomicBool, reader: R, handler: &mut F) -> Result<(), String>
where
    R: std::io::Read,
    F: FnMut(StreamEvent) -> bool,
{
    let reader = BufReader::new(reader);
    let mut event_type = String::new();
    let mut event_id = String::new();
    let mut data = String::new();
    for line in reader.lines() {
        if stop.load(Ordering::SeqCst) {
            return Ok(());
        }
        let line = line.map_err(|e| format!("read sse: {e}"))?;
        if line.is_empty() {
            if !flush_sse(&mut event_type, &mut event_id, &mut data, handler)? {
                return Ok(());
            }
            continue;
        }
        if line.starts_with(':') {
            continue;
        }
        if let Some(rest) = line.strip_prefix("event:") {
            event_type = rest.trim().to_string();
        } else if let Some(rest) = line.strip_prefix("id:") {
            event_id = rest.trim().to_string();
        } else if let Some(rest) = line.strip_prefix("data:") {
            data = rest.trim().to_string();
        }
    }
    let _ = flush_sse(&mut event_type, &mut event_id, &mut data, handler)?;
    Ok(())
}

fn flush_sse<F>(
    event_type: &mut String,
    event_id: &mut String,
    data: &mut String,
    handler: &mut F,
) -> Result<bool, String>
where
    F: FnMut(StreamEvent) -> bool,
{
    if data.is_empty() {
        event_type.clear();
        event_id.clear();
        return Ok(true);
    }
    let ev = decode_stream_event(event_type, event_id, data)?;
    event_type.clear();
    event_id.clear();
    data.clear();
    Ok(handler(ev))
}

fn decode_stream_event(
    event_type: &str,
    event_id: &str,
    data: &str,
) -> Result<StreamEvent, String> {
    let envelope: StreamEnvelope =
        serde_json::from_str(data).map_err(|e| format!("decode sse data: {e}"))?;
    let typ = if event_type.trim().is_empty() {
        envelope.event_type
    } else {
        event_type.trim().to_string()
    };
    let seq = if envelope.seq == 0 {
        event_id.trim().parse().unwrap_or(0)
    } else {
        envelope.seq
    };
    let agent_id = envelope.agent_id.trim().to_string();
    Ok(StreamEvent {
        event_type: typ,
        session_id: agent_id.clone(),
        agent_id,
        seq,
        agent_seq: envelope.agent_seq,
        event_version: envelope.event_version,
        stream_epoch: envelope.stream_epoch,
        delivery: envelope.delivery,
        data: envelope.data,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::TcpListener;

    #[test]
    fn failed_exclusive_package_create_preserves_existing_file() {
        let path = std::env::temp_dir().join(format!(
            "dagents-package-{}-{}.exe",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::write(&path, b"existing").unwrap();
        assert!(create_update_output(&path).is_err());
        assert_eq!(fs::read(&path).unwrap(), b"existing");
        let _ = fs::remove_file(path);
    }

    #[test]
    fn desktop_api_path_stays_inside_node_v1() {
        assert_eq!(
            validate_desktop_api_path("/v1/setup/config?section=llm").unwrap(),
            "/v1/setup/config?section=llm"
        );
        assert_eq!(validate_desktop_api_path("/health").unwrap(), "/health");
        for path in [
            "https://example.com/v1/setup/config",
            "//example.com/v1/setup/config",
            "/v1/../../health",
            "/v1/setup\\config",
            "/v1/setup/config#fragment",
            "/other",
        ] {
            assert!(validate_desktop_api_path(path).is_err(), "accepted {path}");
        }
    }

    #[test]
    fn sse_resume_uses_global_cursor_and_signals_successful_connection() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            let (mut socket, _) = listener.accept().unwrap();
            let mut request = [0u8; 1024];
            let count = socket.read(&mut request).unwrap();
            let request = String::from_utf8_lossy(&request[..count]);
            assert!(
                request.starts_with("GET /v1/streams?after_seq=17 HTTP/1.1"),
                "{request}"
            );
            socket
                .write_all(
                    b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
                )
                .unwrap();
        });

        let client = Client::new(&format!("http://{address}"));
        let stop = AtomicBool::new(false);
        let mut connected = false;
        client
            .stream_events_after(&stop, Some(17), || connected = true, |_| true)
            .unwrap();
        server.join().unwrap();
        assert!(connected, "connection callback should run after HTTP 200");
    }

    #[test]
    fn terminal_list_preserves_existing_target_and_directory_metadata() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut request = [0_u8; 2048];
            let size = stream.read(&mut request).unwrap();
            let request = String::from_utf8_lossy(&request[..size]);
            assert!(request.starts_with("GET /v1/agents/agent%2Da/terminals "));
            let body = serde_json::json!({"terminals":[{
                "terminal_id":"terminal-abc12345",
                "agent_id":"agent-a",
                "config_id":"linux-config-1",
                "target_kind":"linux_channel",
                "target_id":"staging-host",
                "status":"running",
                "shell":"/bin/bash",
                "cwd":"/workspace/project",
                "created_at":"2026-10-16T00:00:00Z"
            }]})
            .to_string();
            write!(stream, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}", body.len(), body).unwrap();
        });
        let client = Client {
            base: format!("http://{address}"),
            token: String::new(),
        };
        let terminals = client.list_terminals("agent-a").unwrap();
        server.join().unwrap();
        assert_eq!(terminals.len(), 1);
        let terminal = &terminals[0];
        assert_eq!(terminal.agent_id, "agent-a");
        assert_eq!(terminal.config_id, "linux-config-1");
        assert_eq!(terminal.target_kind, "linux_channel");
        assert_eq!(terminal.target_id, "staging-host");
        assert_eq!(terminal.cwd, "/workspace/project");
        let serialized = serde_json::to_value(terminal).unwrap();
        for (key, value) in [
            ("agent_id", "agent-a"),
            ("config_id", "linux-config-1"),
            ("target_kind", "linux_channel"),
            ("target_id", "staging-host"),
            ("cwd", "/workspace/project"),
        ] {
            assert_eq!(serialized[key], value);
        }
    }

    #[test]
    fn linux_terminal_channels_are_limited_to_enabled_agent_bindings() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            for _ in 0..2 {
                let (mut stream, _) = listener.accept().unwrap();
                let mut request = [0_u8; 2048];
                let size = stream.read(&mut request).unwrap();
                let request = String::from_utf8_lossy(&request[..size]);
                let body = if request.starts_with("GET /v1/linux/channels ") {
                    serde_json::json!({"channels":[
                        {"channel_id":"enabled-host","enabled":true,"display_name":"staging","host":"staging.example","port":22,"username":"deploy","credential_id":"credential-secret-ref"},
                        {"channel_id":"disabled-host","enabled":false,"display_name":"disabled"},
                        {"channel_id":"unbound-host","enabled":true,"display_name":"unbound"}
                    ]})
                } else {
                    assert!(request.starts_with("GET /v1/agents/agent%2Da/linux-channels "));
                    serde_json::json!({"bindings":[
                        {"channel_id":"enabled-host","enabled":true,"is_default":true},
                        {"channel_id":"disabled-host","enabled":true},
                        {"channel_id":"unbound-host","enabled":false}
                    ]})
                };
                let body = body.to_string();
                write!(stream, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}", body.len(), body).unwrap();
            }
        });
        let client = Client {
            base: format!("http://{address}"),
            token: String::new(),
        };
        let channels = client
            .list_agent_linux_terminal_channels("agent-a")
            .unwrap();
        server.join().unwrap();
        assert_eq!(channels.len(), 1);
        assert_eq!(channels[0].channel_id, "enabled-host");
        assert_eq!(channels[0].host, "staging.example");
        assert!(channels[0].is_default);
        let serialized = serde_json::to_value(&channels[0]).unwrap();
        assert!(serialized.get("credential_id").is_none());
        assert!(serialized.get("secret").is_none());
    }

    #[test]
    fn workspace_binary_path_is_limited_to_relative_workspace_files() {
        assert!(validate_workspace_binary_path(
            "/v1/agents/agent-1/workspace/file?path=src%2Fmain.rs",
            "GET"
        )
        .is_ok());
        assert!(validate_workspace_binary_path(
            "/v1/agents/agent-1/workspace/upload?path=assets",
            "POST"
        )
        .is_ok());
        for (method, path) in [
            (
                "GET",
                "/v1/agents/agent-1/workspace/file?path=%2e%2e%2Fsecret.txt",
            ),
            (
                "GET",
                "/v1/agents/agent-1/workspace/file?path=C%3A%5Csecret.txt",
            ),
            ("PUT", "/v1/agents/agent-1/workspace/upload?path=new.txt"),
            ("GET", "/v1/agents/agent-1/context?path=notes.txt"),
            ("POST", "/v1/agents/agent-1/workspace/file?path=notes.txt"),
        ] {
            assert!(
                validate_workspace_binary_path(path, method).is_err(),
                "accepted {method} {path}"
            );
        }
    }

    #[test]
    fn decodes_sse_event_type_and_id() {
        let raw = r#"{"agent_id":"a1","data":{"x":1}}"#;
        let ev = decode_stream_event("hitl_required", "42", raw).unwrap();
        assert_eq!(ev.event_type, "hitl_required");
        assert_eq!(ev.agent_id, "a1");
        assert_eq!(ev.seq, 42);
    }

    /// Opt-in live smoke against the developer's disposable local Node:
    /// DAGENTS_MVP_SMOKE=1 cargo test live_local_node_vertical_slice_smoke -- --nocapture
    #[test]
    fn live_local_node_vertical_slice_smoke() {
        if std::env::var("DAGENTS_MVP_SMOKE").as_deref() != Ok("1") {
            return;
        }
        use std::sync::mpsc;
        use std::sync::Arc;
        let client = Arc::new(Client::new("http://127.0.0.1:18765"));
        assert!(client.health().online, "local Node health failed");
        let agents = client.list_agents().expect("list agents");
        let agent = agents
            .iter()
            .find(|agent| agent.display_name == "MVP 对话助手")
            .or_else(|| agents.iter().find(|agent| agent.agent_type == "normal"))
            .expect("normal agent available in local test Node");
        let agent_id = agent.agent_id.clone();
        let before = client.hydrate(&agent_id).expect("hydrate history");
        assert_eq!(before["agent_id"].as_str(), Some(agent_id.as_str()));

        let (tx, rx) = mpsc::channel();
        let stream_client = Arc::clone(&client);
        let stream_agent_id = agent_id.clone();
        let marker = format!(
            "Desktop MVP 实链 smoke {}，请原样回复。",
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        );
        let stream_marker = marker.clone();
        let stop = Arc::new(AtomicBool::new(false));
        let stream_stop = Arc::clone(&stop);
        let _stream_thread = std::thread::spawn(move || {
            let mut streamed_text = String::new();
            let _ = stream_client.stream_events(&stream_stop, |event| {
                if event.agent_id != stream_agent_id {
                    return true;
                }
                if event.event_type == "assistant" {
                    if let Some(delta) = event.data.get("content").and_then(Value::as_str) {
                        streamed_text.push_str(delta);
                    }
                }
                let finished = event.event_type == "turn_finished";
                let ours = finished && streamed_text.contains(&stream_marker);
                if finished {
                    let _ = tx.send(ours);
                }
                !ours
            });
        });
        std::thread::sleep(Duration::from_millis(250));
        client
            .send_message(&agent_id, &marker)
            .expect("enqueue real message");
        let deadline = std::time::Instant::now() + Duration::from_secs(40);
        let mut completed = false;
        while std::time::Instant::now() < deadline {
            match rx.recv_timeout(Duration::from_millis(500)) {
                Ok(true) => {
                    completed = true;
                    break;
                }
                Ok(false) | Err(mpsc::RecvTimeoutError::Timeout) => {}
                Err(mpsc::RecvTimeoutError::Disconnected) => break,
            }
        }
        stop.store(true, Ordering::SeqCst);
        assert!(completed, "no turn_finished SSE received from Node");
        let mut after = client
            .hydrate(&agent_id)
            .expect("hydrate completed history");
        let hydrate_deadline = std::time::Instant::now() + Duration::from_secs(8);
        while std::time::Instant::now() < hydrate_deadline {
            let transcript = after["transcript"].as_array().cloned().unwrap_or_default();
            if transcript.iter().any(|item| {
                item["kind"].as_str() == Some("user")
                    && item["text"].as_str() == Some(marker.as_str())
            }) && transcript.iter().any(|item| {
                item["kind"].as_str() == Some("assistant")
                    && item["text"].as_str() == Some(marker.as_str())
            }) {
                return;
            }
            std::thread::sleep(Duration::from_millis(200));
            after = client
                .hydrate(&agent_id)
                .expect("hydrate persisted history");
        }
        let transcript = after["transcript"].as_array().expect("transcript array");
        assert!(
            transcript.iter().any(|item| {
                item["kind"].as_str() == Some("user")
                    && item["text"].as_str() == Some(marker.as_str())
            }),
            "marker={marker:?}; last transcript={:?}",
            transcript.iter().rev().take(4).collect::<Vec<_>>()
        );
        assert!(transcript.iter().any(|item| {
            item["kind"].as_str() == Some("assistant")
                && item["text"].as_str() == Some(marker.as_str())
        }));
    }
}
