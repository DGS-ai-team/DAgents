//! Shell-side pending HITL/unread aggregation.

use crate::nodeclient::{AgentSummary, StreamEvent};
use std::collections::HashMap;
use std::sync::RwLock;
use std::time::SystemTime;

#[derive(Debug, Clone)]
pub struct Entry {
    pub agent_id: String,
    pub display_name: String,
    pub hitl_items: i32,
    pub has_unread: bool,
    pub event_type: String,
    pub updated_at: SystemTime,
    pub session_id: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Summary {
    pub agent_count: usize,
    pub session_count: usize,
    pub item_count: i32,
    pub label: String,
}

#[derive(Debug, Default)]
pub struct Store {
    by_agent: RwLock<HashMap<String, Entry>>,
}

impl Entry {
    pub fn active(&self) -> bool {
        self.hitl_items > 0 || self.has_unread
    }

    pub fn summary_label(&self) -> String {
        let label = self.display_label();
        match (self.hitl_items, self.has_unread) {
            (n, true) if n > 1 => format!("{label} · {n} 项 HITL + 新回复"),
            (n, true) if n > 0 => format!("{label} · HITL + 新回复"),
            (n, _) if n > 1 => format!("{label} · {n} 项待处理"),
            (1, _) => format!("{label} · 待处理"),
            (_, true) => format!("{label} · 新回复"),
            _ => label,
        }
    }

    fn item_count(&self) -> i32 {
        let mut n = self.hitl_items;
        if self.has_unread {
            n += 1;
        }
        n.max(0)
    }

    fn key(&self) -> String {
        let id = self.agent_id.trim();
        if id.is_empty() {
            self.session_id.trim().to_string()
        } else {
            id.to_string()
        }
    }

    fn display_label(&self) -> String {
        let name = self.display_name.trim();
        if !name.is_empty() {
            return name.to_string();
        }
        short_agent_id(&self.key())
    }
}

impl Store {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn replace_from_node(&self, incoming: HashMap<String, Entry>) -> bool {
        let Ok(mut guard) = self.by_agent.write() else {
            return false;
        };
        let next: HashMap<String, Entry> = incoming
            .into_iter()
            .filter(|(_, entry)| entry.active())
            .collect();
        if maps_equal(&guard, &next) {
            return false;
        }
        *guard = next;
        true
    }

    /// Apply Node's complete notification projection. Display metadata remains
    /// from the initial/reconnect agent snapshot.
    pub fn apply_notification(
        &self,
        agent_id: &str,
        has_pending_hitl: bool,
        pending_hitl_items: i32,
        has_unread: bool,
    ) -> bool {
        let id = agent_id.trim();
        if id.is_empty() {
            return false;
        }
        let Ok(mut guard) = self.by_agent.write() else {
            return false;
        };
        if !has_pending_hitl && !has_unread {
            return guard.remove(id).is_some();
        }
        let mut items = pending_hitl_items;
        if items <= 0 && has_pending_hitl {
            items = 1;
        }
        let event_type = if has_pending_hitl {
            "hitl_required"
        } else {
            ""
        };
        if let Some(entry) = guard.get(id) {
            if entry.hitl_items == items
                && entry.has_unread == has_unread
                && entry.event_type == event_type
            {
                return false;
            }
        }
        let entry = guard.entry(id.to_string()).or_insert_with(|| Entry {
            agent_id: id.to_string(),
            display_name: String::new(),
            hitl_items: 0,
            has_unread: false,
            event_type: String::new(),
            updated_at: SystemTime::now(),
            session_id: id.to_string(),
        });
        entry.hitl_items = items;
        entry.has_unread = has_unread;
        entry.event_type = event_type.into();
        entry.updated_at = SystemTime::now();
        true
    }

    pub fn entries(&self) -> Vec<Entry> {
        let Ok(guard) = self.by_agent.read() else {
            return Vec::new();
        };
        active_entries(&guard)
    }

    pub fn summary(&self) -> Summary {
        let Ok(guard) = self.by_agent.read() else {
            return Summary::default();
        };
        if guard.is_empty() {
            return Summary::default();
        }
        let items: i32 = guard.values().map(Entry::item_count).sum();
        let n = guard.len();
        let label = if n == 1 {
            active_entries(&guard)
                .first()
                .map(Entry::summary_label)
                .unwrap_or_default()
        } else if items > n as i32 {
            format!("{n} 个 Agent · {items} 项待处理")
        } else {
            format!("{n} 个 Agent 待处理")
        };
        Summary {
            agent_count: n,
            session_count: n,
            item_count: items,
            label,
        }
    }
}

/// Apply one authoritative notification_changed SSE event.
pub fn apply_notification_changed(store: &Store, ev: &StreamEvent) -> bool {
    if ev.event_type != "notification_changed" || !event_has_agent(ev) {
        return false;
    }
    let has_unread = ev
        .data
        .get("has_unread")
        .and_then(|v| v.as_bool())
        .unwrap_or(false);
    let has_pending_hitl = ev
        .data
        .get("has_pending_hitl")
        .and_then(|v| v.as_bool())
        .unwrap_or(false);
    let pending_hitl_items = ev
        .data
        .get("pending_hitl_items")
        .and_then(|v| v.as_i64())
        .unwrap_or(0) as i32;
    store.apply_notification(
        &event_agent_id(ev),
        has_pending_hitl,
        pending_hitl_items,
        has_unread,
    )
}

pub fn event_has_agent(ev: &StreamEvent) -> bool {
    !event_agent_id(ev).is_empty()
}

pub fn sync_from_agents(store: &Store, agents: &[AgentSummary]) -> bool {
    let mut incoming = HashMap::new();
    let now = SystemTime::now();
    for ag in agents {
        let id = ag.agent_id.trim();
        if id.is_empty() || (!ag.has_unread && !ag.has_pending_hitl) {
            continue;
        }
        let mut items = ag.pending_hitl_items;
        if items <= 0 && ag.has_pending_hitl {
            items = 1;
        }
        incoming.insert(
            id.to_string(),
            Entry {
                agent_id: id.to_string(),
                session_id: id.to_string(),
                display_name: ag.display_name.trim().to_string(),
                hitl_items: items,
                has_unread: ag.has_unread,
                event_type: if ag.has_pending_hitl {
                    "hitl_required".into()
                } else {
                    String::new()
                },
                updated_at: now,
            },
        );
    }
    store.replace_from_node(incoming)
}

fn event_agent_id(ev: &StreamEvent) -> String {
    let id = ev.agent_id.trim();
    if id.is_empty() {
        ev.session_id.trim().to_string()
    } else {
        id.to_string()
    }
}

fn active_entries(map: &HashMap<String, Entry>) -> Vec<Entry> {
    let mut out: Vec<Entry> = map.values().filter(|e| e.active()).cloned().collect();
    out.sort_by(|a, b| {
        b.updated_at
            .cmp(&a.updated_at)
            .then_with(|| a.key().cmp(&b.key()))
    });
    out
}

fn maps_equal(a: &HashMap<String, Entry>, b: &HashMap<String, Entry>) -> bool {
    if a.len() != b.len() {
        return false;
    }
    a.iter().all(|(id, ea)| {
        b.get(id)
            .map(|eb| {
                ea.key() == eb.key()
                    && ea.display_name == eb.display_name
                    && ea.hitl_items == eb.hitl_items
                    && ea.has_unread == eb.has_unread
                    && ea.event_type == eb.event_type
            })
            .unwrap_or(false)
    })
}

fn short_agent_id(id: &str) -> String {
    let id = id.trim();
    if id.chars().count() <= 12 {
        return id.to_string();
    }
    format!("{}…", id.chars().take(8).collect::<String>())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::Value;

    fn agent(id: &str, name: &str, unread: bool, hitl: bool, items: i32) -> AgentSummary {
        AgentSummary {
            agent_id: id.into(),
            display_name: name.into(),
            has_unread: unread,
            has_pending_hitl: hitl,
            pending_hitl_items: items,
            active: false,
            has_active_turn: false,
            notify_seq: 0,
            ack_seq: 0,
        }
    }

    #[test]
    fn syncs_and_summarizes_agents() {
        let store = Store::new();
        assert!(sync_from_agents(
            &store,
            &[
                agent("a1", "Alpha", true, true, 2),
                agent("a2", "", false, true, 0)
            ]
        ));
        let sum = store.summary();
        assert_eq!(sum.agent_count, 2);
        assert_eq!(sum.item_count, 4);
        assert_eq!(sum.label, "2 个 Agent · 4 项待处理");
    }

    #[test]
    fn applies_notification_events_without_polling() {
        let store = Store::new();
        let mut incoming = HashMap::new();
        incoming.insert(
            "a1".into(),
            Entry {
                agent_id: "a1".into(),
                session_id: "a1".into(),
                display_name: String::new(),
                hitl_items: 1,
                has_unread: false,
                event_type: "hitl_required".into(),
                updated_at: SystemTime::now(),
            },
        );
        store.replace_from_node(incoming);
        let mut ev = StreamEvent {
            event_type: "notification_changed".into(),
            session_id: "a1".into(),
            agent_id: "a1".into(),
            seq: 0,
            agent_seq: 0,
            event_version: 1,
            stream_epoch: "test".into(),
            delivery: "replayable".into(),
            data: HashMap::from([
                ("has_pending_hitl".into(), Value::Bool(false)),
                ("pending_hitl_items".into(), Value::from(0)),
                ("has_unread".into(), Value::Bool(false)),
            ]),
        };
        assert!(apply_notification_changed(&store, &ev));
        ev.event_type = "tool_call".into();
        assert!(!apply_notification_changed(&store, &ev));
    }
}
