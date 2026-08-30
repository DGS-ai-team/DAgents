//! Background SSE subscriber and periodic pending sync.

use crate::nodeclient::{Client, StreamEvent};
use crate::pending::{self, Store};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

const RECONNECT_DELAY: Duration = Duration::from_secs(5);
const SYNC_INTERVAL: Duration = Duration::from_secs(60);

pub struct Subscriber {
    client: Arc<Client>,
    store: Arc<Store>,
    on_change: Arc<dyn Fn() + Send + Sync>,
    stop: Arc<AtomicBool>,
    started: Mutex<bool>,
}

impl Subscriber {
    pub fn new(
        client: Arc<Client>,
        store: Arc<Store>,
        on_change: impl Fn() + Send + Sync + 'static,
    ) -> Self {
        Self {
            client,
            store,
            on_change: Arc::new(on_change),
            stop: Arc::new(AtomicBool::new(false)),
            started: Mutex::new(false),
        }
    }

    pub fn start(&self) {
        let Ok(mut started) = self.started.lock() else {
            return;
        };
        if *started {
            return;
        }
        *started = true;
        self.stop.store(false, Ordering::SeqCst);
        drop(started);

        let poll = self.clone_parts();
        thread::spawn(move || poll_loop(poll));
        let stream = self.clone_parts();
        thread::spawn(move || stream_loop(stream));
    }

    pub fn stop(&self) {
        self.stop.store(true, Ordering::SeqCst);
        if let Ok(mut started) = self.started.lock() {
            *started = false;
        }
    }

    fn clone_parts(&self) -> Parts {
        Parts {
            client: Arc::clone(&self.client),
            store: Arc::clone(&self.store),
            on_change: Arc::clone(&self.on_change),
            stop: Arc::clone(&self.stop),
        }
    }
}

struct Parts {
    client: Arc<Client>,
    store: Arc<Store>,
    on_change: Arc<dyn Fn() + Send + Sync>,
    stop: Arc<AtomicBool>,
}

fn stream_loop(parts: Parts) {
    while !parts.stop.load(Ordering::SeqCst) {
        if let Err(e) = connect_once(&parts) {
            if !parts.stop.load(Ordering::SeqCst) {
                eprintln!("shell sse: {e}");
            }
        }
        sleep_until_stopped(&parts.stop, RECONNECT_DELAY);
    }
}

fn poll_loop(parts: Parts) {
    sync_agents(&parts);
    let mut next = Instant::now() + SYNC_INTERVAL;
    while !parts.stop.load(Ordering::SeqCst) {
        let now = Instant::now();
        if now >= next {
            sync_agents(&parts);
            next = now + SYNC_INTERVAL;
        }
        sleep_until_stopped(&parts.stop, Duration::from_millis(250));
    }
}

fn connect_once(parts: &Parts) -> Result<(), String> {
    sync_agents(parts);
    let stop = Arc::clone(&parts.stop);
    parts.client.stream_events(&stop, |ev| {
        if should_sync_while_hitl_pending(&parts.store, &ev) || pending::should_sync_on_event(&ev) {
            sync_agents(parts);
        }
        !parts.stop.load(Ordering::SeqCst)
    })
}

fn sync_agents(parts: &Parts) {
    if parts.stop.load(Ordering::SeqCst) {
        return;
    }
    match parts.client.list_agents() {
        Ok(agents) => {
            if pending::sync_from_agents(&parts.store, &agents) {
                (parts.on_change)();
            }
        }
        Err(e) => eprintln!("shell sync agents: {e}"),
    }
}

fn should_sync_while_hitl_pending(store: &Store, ev: &StreamEvent) -> bool {
    store.has_pending_hitl()
        && matches!(ev.event_type.as_str(), "tool_result" | "tool_call")
        && pending::event_has_agent(ev)
}

fn sleep_until_stopped(stop: &AtomicBool, duration: Duration) {
    let deadline = Instant::now() + duration;
    while !stop.load(Ordering::SeqCst) && Instant::now() < deadline {
        thread::sleep(Duration::from_millis(100));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::Value;
    use std::collections::HashMap;

    #[test]
    fn syncs_tool_events_while_hitl_pending() {
        let store = Store::new();
        let mut incoming = HashMap::new();
        incoming.insert(
            "a1".into(),
            pending::Entry {
                agent_id: "a1".into(),
                session_id: "a1".into(),
                display_name: String::new(),
                hitl_items: 1,
                has_unread: false,
                event_type: "hitl_required".into(),
                updated_at: std::time::SystemTime::now(),
            },
        );
        store.replace_from_node(incoming);
        let ev = StreamEvent {
            event_type: "tool_result".into(),
            agent_id: "a1".into(),
            session_id: "a1".into(),
            seq: 1,
            agent_seq: 1,
            event_version: 1,
            stream_epoch: "test".into(),
            delivery: "replayable".into(),
            data: HashMap::<String, Value>::new(),
        };
        assert!(should_sync_while_hitl_pending(&store, &ev));
    }
}
