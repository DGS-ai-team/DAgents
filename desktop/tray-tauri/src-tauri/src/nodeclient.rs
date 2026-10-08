//! Node REST/SSE client used by the desktop shell.

use crate::config::RuntimeConfig;
use percent_encoding::{utf8_percent_encode, NON_ALPHANUMERIC};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;
use std::io::{BufRead, BufReader};
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

const CLIENT_TOKEN_ENV: &str = "DAGENTS_CLIENT_TOKEN";

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
    pub status: String,
    #[serde(default)]
    pub shell: String,
    #[serde(default)]
    pub created_at: String,
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
    ) -> Result<Value, String> {
        let method = method.trim().to_ascii_uppercase();
        if !matches!(method.as_str(), "GET" | "POST" | "PUT" | "PATCH" | "DELETE") {
            return Err("不支持的 Node API 方法".into());
        }
        let path = validate_desktop_api_path(path)?;
        if path == "/health" && method != "GET" {
            return Err("/health 只支持读取".into());
        }
        self.json_request(&method, &path, body)
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
        let req = self.authorize(ureq::get(&format!("{}/v1/agent/info", self.base)));
        let resp = req
            .timeout(Duration::from_secs(5))
            .call()
            .map_err(|e| format!("GET /v1/agent/info: {e}"))?;
        if resp.status() != 200 {
            return Err(format!("GET /v1/agent/info: status {}", resp.status()));
        }
        resp.into_json()
            .map_err(|e| format!("GET /v1/agent/info JSON: {e}"))
    }

    pub fn runtime_config(&self) -> Result<RuntimeConfig, String> {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let req = self.authorize(ureq::get(&format!(
            "{}/v1/desktop/runtime-config",
            self.base
        )));
        let resp = req
            .timeout(Duration::from_secs(5))
            .call()
            .map_err(|e| format!("GET /v1/desktop/runtime-config: {e}"))?;
        if resp.status() != 200 {
            return Err(format!(
                "GET /v1/desktop/runtime-config: status {}",
                resp.status()
            ));
        }
        resp.into_json()
            .map_err(|e| format!("GET /v1/desktop/runtime-config JSON: {e}"))
    }

    pub fn upgrade_readiness(&self) -> Result<UpgradeReadiness, String> {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let req = self.authorize(ureq::get(&format!(
            "{}/v1/agent/upgrade-readiness",
            self.base
        )));
        let resp = req
            .timeout(Duration::from_secs(5))
            .call()
            .map_err(|e| format!("GET /v1/agent/upgrade-readiness: {e}"))?;
        if resp.status() != 200 {
            return Err(format!(
                "GET /v1/agent/upgrade-readiness: status {}",
                resp.status()
            ));
        }
        resp.into_json()
            .map_err(|e| format!("upgrade-readiness JSON: {e}"))
    }

    /// 查询 Node 是否尚未完成 Web UI 首配。失败时返回 false（不强制改写目标 URL）。
    pub fn node_profile_incomplete(&self) -> bool {
        if self.base.is_empty() {
            return false;
        }
        let req = self.authorize(ureq::get(&format!("{}/v1/ui/bootstrap", self.base)));
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

    pub fn stream_events<F>(&self, stop: &AtomicBool, mut handler: F) -> Result<(), String>
    where
        F: FnMut(StreamEvent) -> bool,
    {
        if self.base.is_empty() {
            return Err("node client: empty base URL".into());
        }
        let req = self
            .authorize(ureq::get(&format!("{}/v1/streams?live=1", self.base)))
            .set("Accept", "text/event-stream");
        // 勿设 timeout(0)：ureq/Windows 会当成零读超时，空闲 SSE 立刻失败并重连刷屏。
        // 不设 overall timeout，与 Go tray `http.Client{Timeout: 0}`（无限）对齐。
        let resp = req.call().map_err(|e| format!("GET /v1/streams: {e}"))?;
        if resp.status() != 200 {
            return Err(format!("GET /v1/streams: status {}", resp.status()));
        }
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
