//! Background SSE subscriber and event-driven pending projection.

use crate::nodeclient::Client;
use crate::pending::{self, Store};
use serde_json::Value;
use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

const RECONNECT_DELAY: Duration = Duration::from_secs(5);

pub struct Subscriber {
    client: Arc<Client>,
    store: Arc<Store>,
    on_change: Arc<dyn Fn() + Send + Sync>,
    on_event: Option<Arc<dyn Fn(crate::nodeclient::StreamEvent) + Send + Sync>>,
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
            on_event: None,
            stop: Arc::new(AtomicBool::new(false)),
            started: Mutex::new(false),
        }
    }

    pub fn with_event_handler(
        mut self,
        handler: impl Fn(crate::nodeclient::StreamEvent) + Send + Sync + 'static,
    ) -> Self {
        self.on_event = Some(Arc::new(handler));
        self
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
            on_event: self.on_event.clone(),
            stop: Arc::clone(&self.stop),
            cursor: StreamCursor::default(),
            ever_connected: false,
        }
    }
}

struct Parts {
    client: Arc<Client>,
    store: Arc<Store>,
    on_change: Arc<dyn Fn() + Send + Sync>,
    on_event: Option<Arc<dyn Fn(crate::nodeclient::StreamEvent) + Send + Sync>>,
    stop: Arc<AtomicBool>,
    cursor: StreamCursor,
    ever_connected: bool,
}

#[derive(Default)]
struct StreamCursor {
    seq: Option<i32>,
    epoch: String,
}

fn stream_loop(parts: Parts) {
    let mut parts = parts;
    while !parts.stop.load(Ordering::SeqCst) {
        if let Err(e) = connect_once(&mut parts) {
            if !parts.stop.load(Ordering::SeqCst) {
                crate::diagnostics::stderr_best_effort(format_args!("shell sse: {e}"));
            }
        }
        sleep_until_stopped(&parts.stop, RECONNECT_DELAY);
    }
}

fn connect_once(parts: &mut Parts) -> Result<(), String> {
    sync_agents(parts);
    let stop = Arc::clone(&parts.stop);
    let was_connected = parts.ever_connected;
    let connected = AtomicBool::new(false);
    let reconnect_handler = parts.on_event.clone();
    let after_seq = parts.cursor.seq;
    let client = Arc::clone(&parts.client);
    let on_event = parts.on_event.clone();
    let store = Arc::clone(&parts.store);
    let on_change = Arc::clone(&parts.on_change);
    let cursor = &mut parts.cursor;
    let result = client.stream_events_after(
        &stop,
        after_seq,
        || {
            connected.store(true, Ordering::SeqCst);
            if was_connected {
                if let Some(handler) = &reconnect_handler {
                    handler(transport_resync_event("stream_reconnected"));
                }
            }
        },
        |ev| {
            let epoch_changed = !cursor.epoch.is_empty()
                && !ev.stream_epoch.is_empty()
                && cursor.epoch != ev.stream_epoch;
            if epoch_changed && ev.event_type != "resync_required" {
                if let Some(handler) = &on_event {
                    handler(transport_resync_event("stream_epoch_changed"));
                }
                cursor.seq = None;
            }
            if !ev.stream_epoch.is_empty() {
                cursor.epoch = ev.stream_epoch.clone();
            }
            if ev.event_type == "resync_required" {
                cursor.seq = ev
                    .data
                    .get("seq")
                    .and_then(Value::as_i64)
                    .and_then(|seq| i32::try_from(seq).ok());
            } else if ev.seq > 0 && cursor.seq.is_none_or(|seq| ev.seq > seq) {
                cursor.seq = Some(ev.seq);
            }
            if let Some(handler) = &on_event {
                handler(ev.clone());
            }
            if pending::apply_notification_changed(&store, &ev) {
                on_change();
            }
            !stop.load(Ordering::SeqCst)
        },
    );
    if connected.load(Ordering::SeqCst) {
        parts.ever_connected = true;
    }
    result
}

fn transport_resync_event(reason: &str) -> crate::nodeclient::StreamEvent {
    let mut data = HashMap::new();
    data.insert("reason".into(), Value::String(reason.into()));
    data.insert("requires_hydrate".into(), Value::Bool(true));
    crate::nodeclient::StreamEvent {
        event_type: "resync_required".into(),
        session_id: String::new(),
        agent_id: String::new(),
        seq: 0,
        agent_seq: 0,
        event_version: 1,
        stream_epoch: String::new(),
        delivery: "transient".into(),
        data,
    }
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
        Err(e) => crate::diagnostics::stderr_best_effort(format_args!("shell sync agents: {e}")),
    }
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
    use crate::nodeclient::StreamEvent;
    use serde_json::Value;
    use std::collections::HashMap;

    #[test]
    fn applies_notification_events_without_polling() {
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
        let mut data = HashMap::<String, Value>::new();
        data.insert("has_pending_hitl".into(), Value::Bool(false));
        data.insert("pending_hitl_items".into(), Value::from(0));
        data.insert("has_unread".into(), Value::Bool(false));
        let ev = StreamEvent {
            event_type: "notification_changed".into(),
            agent_id: "a1".into(),
            session_id: "a1".into(),
            seq: 1,
            agent_seq: 1,
            event_version: 1,
            stream_epoch: "test".into(),
            delivery: "replayable".into(),
            data,
        };
        assert!(pending::apply_notification_changed(&store, &ev));
    }
}
