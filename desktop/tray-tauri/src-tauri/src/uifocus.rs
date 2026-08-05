//! Web UI 聚焦 Agent 记录，用于抑制同 Agent Toast。

use std::sync::Mutex;
use std::time::{Duration, Instant};

pub const DEFAULT_TTL: Duration = Duration::from_secs(90);

#[derive(Debug, Default)]
pub struct Store {
    inner: Mutex<FocusState>,
}

#[derive(Debug, Default)]
struct FocusState {
    agent_id: String,
    expires_at: Option<Instant>,
}

impl Store {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn report(&self, agent_id: &str, ttl: Duration) {
        let ttl = if ttl.is_zero() { DEFAULT_TTL } else { ttl };
        let agent_id = agent_id.trim();
        let Ok(mut state) = self.inner.lock() else {
            return;
        };
        if agent_id.is_empty() {
            state.agent_id.clear();
            state.expires_at = None;
            return;
        }
        state.agent_id = agent_id.to_string();
        state.expires_at = Some(Instant::now() + ttl);
    }

    pub fn is_focused(&self, agent_id: &str) -> bool {
        let agent_id = agent_id.trim();
        if agent_id.is_empty() {
            return false;
        }
        let Ok(state) = self.inner.lock() else {
            return false;
        };
        state.agent_id == agent_id
            && state
                .expires_at
                .map(|expires_at| Instant::now() < expires_at)
                .unwrap_or(false)
    }

    #[cfg(test)]
    pub fn focused_session(&self) -> String {
        let Ok(state) = self.inner.lock() else {
            return String::new();
        };
        if state
            .expires_at
            .map(|expires_at| Instant::now() < expires_at)
            .unwrap_or(false)
        {
            state.agent_id.clone()
        } else {
            String::new()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reports_and_expires_focus() {
        let store = Store::new();
        store.report(" a1 ", Duration::from_millis(20));
        assert!(store.is_focused("a1"));
        std::thread::sleep(Duration::from_millis(30));
        assert!(!store.is_focused("a1"));
    }

    #[test]
    fn clears_empty_focus() {
        let store = Store::new();
        store.report("a1", DEFAULT_TTL);
        store.report("", DEFAULT_TTL);
        assert_eq!(store.focused_session(), "");
    }
}
