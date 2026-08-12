//! Windows toast notifications plus cross-platform dedupe planning.

use crate::pending::Entry;
use std::collections::{HashMap, HashSet};
use std::sync::Mutex;

#[derive(Debug, Clone)]
pub struct SyncPlan {
    pub to_push: Vec<Entry>,
    pub next_last: HashMap<String, Entry>,
}

pub fn plan_sync(
    last: &HashMap<String, Entry>,
    toast_entries: &[Entry],
    retain_ids: &HashSet<String>,
) -> SyncPlan {
    let mut next = HashMap::with_capacity(last.len() + toast_entries.len());
    let mut to_push = Vec::new();
    for e in toast_entries {
        let id = entry_key(e);
        if id.is_empty() {
            continue;
        }
        let prev = last.get(&id);
        if prev
            .map(|p| p.active() && p.hitl_items == e.hitl_items && p.has_unread == e.has_unread)
            .unwrap_or(false)
        {
            next.insert(id, e.clone());
            continue;
        }
        to_push.push(e.clone());
        next.insert(id, e.clone());
    }
    for id in retain_ids {
        let id = id.trim();
        if id.is_empty() || next.contains_key(id) {
            continue;
        }
        if let Some(prev) = last.get(id) {
            next.insert(id.to_string(), prev.clone());
        }
    }
    SyncPlan { to_push, next_last: next }
}

pub struct Notifier {
    endpoint: String,
    last: Mutex<HashMap<String, Entry>>,
}

impl Notifier {
    pub fn new(endpoint: String) -> Self {
        Self {
            endpoint,
            last: Mutex::new(HashMap::new()),
        }
    }

    pub fn sync(&self, entries: &[Entry], retain_ids: &HashSet<String>) {
        let Ok(mut last) = self.last.lock() else {
            return;
        };
        let plan = plan_sync(&last, entries, retain_ids);
        for e in &plan.to_push {
            if let Err(err) = self.push_pending(e) {
                eprintln!("shell toast: {err}");
            }
        }
        *last = plan.next_last;
    }

    pub fn push_update_available(&self, latest_version: &str) -> Result<(), String> {
        push_update_toast(&self.endpoint, latest_version)
    }

    fn push_pending(&self, e: &Entry) -> Result<(), String> {
        let id = if e.agent_id.trim().is_empty() {
            e.session_id.trim()
        } else {
            e.agent_id.trim()
        };
        let target = agent_url(&self.endpoint, id);
        push_pending_toast(&target, &e.summary_label())
    }
}

fn entry_key(e: &Entry) -> String {
    let id = e.session_id.trim();
    if id.is_empty() {
        e.agent_id.trim().to_string()
    } else {
        id.to_string()
    }
}

fn agent_url(endpoint: &str, agent_id: &str) -> String {
    let base = format!("{}/ui/", endpoint.trim().trim_end_matches('/'));
    if agent_id.trim().is_empty() {
        base
    } else {
        format!("{base}agents/{}", path_escape(agent_id.trim()))
    }
}

#[cfg(windows)]
fn settings_about_url(endpoint: &str) -> String {
    format!("{}/ui/settings/about", endpoint.trim().trim_end_matches('/'))
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

#[cfg(windows)]
fn push_pending_toast(target: &str, message: &str) -> Result<(), String> {
    use winrt_notification::{Duration, Toast};
    Toast::new(Toast::POWERSHELL_APP_ID)
        .title("DAgents 待处理")
        .text1(message)
        .text2(target)
        .duration(Duration::Short)
        .show()
        .map_err(|e| e.to_string())
}

#[cfg(not(windows))]
fn push_pending_toast(_target: &str, _message: &str) -> Result<(), String> {
    Ok(())
}

#[cfg(windows)]
fn push_update_toast(endpoint: &str, latest_version: &str) -> Result<(), String> {
    use winrt_notification::{Duration, Toast};
    let latest = if latest_version.trim().is_empty() {
        "新版本"
    } else {
        latest_version.trim()
    };
    let target = settings_about_url(endpoint);
    Toast::new(Toast::POWERSHELL_APP_ID)
        .title("DAgents 新版本可用")
        .text1(&format!("可升级到 {latest}，点击查看详情"))
        .text2(&target)
        .duration(Duration::Short)
        .show()
        .map_err(|e| e.to_string())
}

#[cfg(not(windows))]
fn push_update_toast(_endpoint: &str, _latest_version: &str) -> Result<(), String> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::SystemTime;

    fn entry(id: &str, hitl: i32, unread: bool) -> Entry {
        Entry {
            agent_id: id.into(),
            session_id: id.into(),
            display_name: id.into(),
            hitl_items: hitl,
            has_unread: unread,
            event_type: String::new(),
            updated_at: SystemTime::now(),
        }
    }

    #[test]
    fn plan_sync_dedupes_unchanged_entries() {
        let mut last = HashMap::new();
        last.insert("a1".into(), entry("a1", 1, false));
        let plan = plan_sync(&last, &[entry("a1", 1, false), entry("a2", 0, true)], &HashSet::new());
        assert_eq!(plan.to_push.len(), 1);
        assert!(plan.next_last.contains_key("a1"));
        assert!(plan.next_last.contains_key("a2"));
    }

    #[test]
    fn plan_sync_retains_focused_ids() {
        let mut last = HashMap::new();
        last.insert("a1".into(), entry("a1", 1, false));
        let retain = HashSet::from(["a1".to_string()]);
        let plan = plan_sync(&last, &[], &retain);
        assert!(plan.next_last.contains_key("a1"));
        assert!(plan.to_push.is_empty());
    }
}
