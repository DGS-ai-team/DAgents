//! Node REST/SSE client used by the desktop shell.

use serde::Deserialize;
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

#[derive(Debug, Clone, Deserialize, PartialEq, Eq)]
pub struct AgentSummary {
    #[serde(default)]
    pub agent_id: String,
    #[serde(default)]
    pub display_name: String,
    #[serde(default)]
    pub active: bool,
    #[serde(default)]
    pub run_turn_phase: String,
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

#[derive(Debug, Deserialize)]
struct ListAgentsResponse {
    #[serde(default)]
    agents: Vec<AgentSummary>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct StreamEvent {
    pub event_type: String,
    pub session_id: String,
    pub agent_id: String,
    pub seq: i32,
    pub data: HashMap<String, Value>,
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
        let resp = req
            .timeout(Duration::from_secs(0))
            .call()
            .map_err(|e| format!("GET /v1/streams: {e}"))?;
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

fn decode_stream_event(event_type: &str, event_id: &str, data: &str) -> Result<StreamEvent, String> {
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
        data: envelope.data,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_sse_event_type_and_id() {
        let raw = r#"{"agent_id":"a1","data":{"x":1}}"#;
        let ev = decode_stream_event("hitl_required", "42", raw).unwrap();
        assert_eq!(ev.event_type, "hitl_required");
        assert_eq!(ev.agent_id, "a1");
        assert_eq!(ev.seq, 42);
    }
}
