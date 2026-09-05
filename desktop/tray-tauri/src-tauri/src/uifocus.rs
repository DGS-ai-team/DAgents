//! Web UI 聚焦 Agent 记录，用于抑制同 Agent Toast。

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

pub const DEFAULT_TTL: Duration = Duration::from_secs(90);

#[derive(Debug, Default)]
pub struct Store {
    inner: Mutex<FocusState>,
}

#[derive(Debug, Default)]
struct FocusState {
    claims: HashMap<String, FocusClaim>,
}

#[derive(Debug)]
struct FocusClaim {
    agent_id: String,
    expires_at: Instant,
}

impl Store {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn report(&self, source_id: &str, agent_id: &str, ttl: Duration) {
        let ttl = if ttl.is_zero() { DEFAULT_TTL } else { ttl };
        let source_id = source_id.trim();
        let agent_id = agent_id.trim();
        if source_id.is_empty() {
            return;
        }
        let Ok(mut state) = self.inner.lock() else {
            return;
        };
        prune_expired(&mut state.claims);
        if agent_id.is_empty() {
            state.claims.remove(source_id);
            return;
        }
        state.claims.insert(
            source_id.to_string(),
            FocusClaim {
                agent_id: agent_id.to_string(),
                expires_at: Instant::now() + ttl,
            },
        );
    }

    pub fn is_focused(&self, agent_id: &str) -> bool {
        let agent_id = agent_id.trim();
        if agent_id.is_empty() {
            return false;
        }
        let Ok(mut state) = self.inner.lock() else {
            return false;
        };
        prune_expired(&mut state.claims);
        state
            .claims
            .values()
            .any(|claim| claim.agent_id == agent_id)
    }

    #[cfg(test)]
    pub fn focused_session(&self) -> String {
        let Ok(mut state) = self.inner.lock() else {
            return String::new();
        };
        prune_expired(&mut state.claims);
        state
            .claims
            .values()
            .next()
            .map(|claim| claim.agent_id.clone())
            .unwrap_or_default()
    }
}

fn prune_expired(claims: &mut HashMap<String, FocusClaim>) {
    let now = Instant::now();
    claims.retain(|_, claim| now < claim.expires_at);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reports_and_expires_focus() {
        let store = Store::new();
        store.report("tab-a", " a1 ", Duration::from_millis(20));
        assert!(store.is_focused("a1"));
        std::thread::sleep(Duration::from_millis(30));
        assert!(!store.is_focused("a1"));
    }

    #[test]
    fn clears_empty_focus() {
        let store = Store::new();
        store.report("tab-a", "a1", DEFAULT_TTL);
        store.report("tab-a", "", DEFAULT_TTL);
        assert_eq!(store.focused_session(), "");
    }

    #[test]
    fn clearing_one_source_does_not_clear_another() {
        let store = Store::new();
        store.report("tab-a", "a1", DEFAULT_TTL);
        store.report("tab-b", "a1", DEFAULT_TTL);
        store.report("tab-a", "", DEFAULT_TTL);
        assert!(store.is_focused("a1"));
        store.report("tab-b", "", DEFAULT_TTL);
        assert!(!store.is_focused("a1"));
    }
}
