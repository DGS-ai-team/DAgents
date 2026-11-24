//! Background SSE subscriber and event-driven pending projection.

use crate::nodeclient::Client;
use crate::pending::{self, Store};
use serde_json::Value;
use std::collections::{HashMap, VecDeque};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};
use tauri::{AppHandle, Emitter, Manager};

const RECONNECT_DELAY: Duration = Duration::from_secs(5);
const MAX_PRE_READY_EVENTS: usize = 512;
const MAX_PRE_READY_BYTES: usize = 8 * 1024 * 1024;

#[derive(Default)]
struct EventBridgeState {
    webview_ready: bool,
    stream_connected: bool,
    overflowed: bool,
    queued_bytes: usize,
    queued: VecDeque<crate::nodeclient::StreamEvent>,
    active_agent_id: Option<String>,
    active_agent_generation: u64,
    webview_generation: u64,
}

struct ReadyBatch {
    requires_resync: bool,
    events: Vec<crate::nodeclient::StreamEvent>,
}

impl EventBridgeState {
    fn push(&mut self, event: crate::nodeclient::StreamEvent) -> Option<ReadyBatch> {
        if is_background_conversation_delta(self.active_agent_id.as_deref(), &event) {
            return None;
        }
        if self.webview_ready && self.stream_connected {
            return Some(ReadyBatch {
                requires_resync: false,
                events: vec![event],
            });
        }
        let event_bytes = serde_json::to_vec(&event)
            .map(|payload| payload.len())
            .unwrap_or(MAX_PRE_READY_BYTES + 1);
        if !self.overflowed {
            if self.queued.len() >= MAX_PRE_READY_EVENTS
                || event_bytes > MAX_PRE_READY_BYTES
                || self.queued_bytes.saturating_add(event_bytes) > MAX_PRE_READY_BYTES
            {
                self.queued.clear();
                self.queued_bytes = 0;
                self.overflowed = true;
            } else {
                self.queued_bytes += event_bytes;
                self.queued.push_back(event);
            }
        }
        None
    }

    fn set_stream_connected(&mut self) -> Option<ReadyBatch> {
        let was_connected = self.stream_connected;
        self.stream_connected = true;
        if !was_connected && self.webview_ready {
            return self.take_ready_batch(true);
        }
        None
    }

    fn set_webview_ready(&mut self) -> Option<ReadyBatch> {
        let was_ready = self.webview_ready;
        self.webview_ready = true;
        if !was_ready && self.stream_connected {
            return self.take_ready_batch(true);
        }
        None
    }

    fn set_webview_loading(&mut self) {
        self.webview_generation = self.webview_generation.saturating_add(1);
        self.webview_ready = false;
        self.queued.clear();
        self.queued_bytes = 0;
        self.overflowed = false;
        self.active_agent_id = None;
        self.active_agent_generation = 0;
    }

    fn set_active_agent(
        &mut self,
        agent_id: &str,
        generation: u64,
        webview_generation: u64,
    ) -> bool {
        if webview_generation != self.webview_generation {
            return false;
        }
        let id = agent_id.trim();
        let next_id = (!id.is_empty()).then(|| id.to_string());
        if generation < self.active_agent_generation
            || (generation == self.active_agent_generation && self.active_agent_id != next_id)
        {
            return false;
        }
        self.active_agent_id = next_id;
        self.active_agent_generation = generation;
        true
    }

    fn take_ready_batch(&mut self, requires_resync: bool) -> Option<ReadyBatch> {
        if !self.webview_ready || !self.stream_connected {
            return None;
        }
        let overflowed = self.overflowed;
        let events = if overflowed {
            Vec::new()
        } else {
            self.queued.drain(..).collect()
        };
        self.overflowed = false;
        self.queued_bytes = 0;
        Some(ReadyBatch {
            requires_resync: requires_resync || overflowed,
            events,
        })
    }
}

fn is_background_conversation_delta(
    active_agent_id: Option<&str>,
    event: &crate::nodeclient::StreamEvent,
) -> bool {
    event.event_type == "conversation_delta"
        && event.delivery == "ephemeral"
        && active_agent_id != Some(event.agent_id.as_str())
}

/// Bridges the native SSE reader to the WebView without dropping startup or
/// reload events before the JavaScript listener and the SSE connection are both ready.
#[derive(Default)]
pub struct EventBridge {
    state: Mutex<EventBridgeState>,
}

impl EventBridge {
    pub fn webview_generation(&self) -> Result<u64, String> {
        let state = self
            .state
            .lock()
            .map_err(|_| "Desktop 事件桥锁不可用".to_string())?;
        Ok(state.webview_generation)
    }

    pub fn set_active_agent(
        &self,
        agent_id: &str,
        generation: u64,
        webview_generation: u64,
    ) -> Result<bool, String> {
        let mut state = self
            .state
            .lock()
            .map_err(|_| "Desktop 事件桥锁不可用".to_string())?;
        Ok(state.set_active_agent(agent_id, generation, webview_generation))
    }

    pub fn webview_loading(&self) {
        if let Ok(mut state) = self.state.lock() {
            state.set_webview_loading();
        }
    }

    pub fn stream_connected(&self, app: &AppHandle) {
        let Ok(mut state) = self.state.lock() else {
            return;
        };
        if let Some(batch) = state.set_stream_connected() {
            self.emit_batch(app, &mut state, batch);
        }
    }

    pub fn webview_ready(&self, app: &AppHandle) -> Result<(), String> {
        let mut state = self
            .state
            .lock()
            .map_err(|_| "Desktop 事件桥锁不可用".to_string())?;
        if let Some(batch) = state.set_webview_ready() {
            self.emit_batch(app, &mut state, batch);
        }
        Ok(())
    }

    pub fn forward(&self, app: &AppHandle, event: crate::nodeclient::StreamEvent) {
        let Ok(mut state) = self.state.lock() else {
            return;
        };
        if let Some(batch) = state.push(event) {
            self.emit_batch(app, &mut state, batch);
        }
    }

    fn emit_batch(&self, app: &AppHandle, state: &mut EventBridgeState, batch: ReadyBatch) {
        let Some(window) = app.get_webview_window("desktop_mvp") else {
            state.set_webview_loading();
            state.overflowed = true;
            return;
        };
        if batch.requires_resync {
            if let Err(error) = window.emit(
                "desktop-node-event",
                resync_required_event("desktop_listener_ready"),
            ) {
                crate::diagnostics::stderr_best_effort(format_args!(
                    "desktop event resync emit failed: {error}"
                ));
                state.webview_ready = false;
                state.overflowed = true;
                return;
            }
        }
        for event in batch.events {
            if let Err(error) = window.emit("desktop-node-event", event) {
                crate::diagnostics::stderr_best_effort(format_args!(
                    "desktop event emit failed: {error}"
                ));
                state.webview_ready = false;
                state.overflowed = true;
                return;
            }
        }
    }
}

fn resync_required_event(reason: &str) -> crate::nodeclient::StreamEvent {
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
        delivery: "replayable".into(),
        data,
    }
}

pub struct Subscriber {
    client: Arc<Client>,
    store: Arc<Store>,
    on_change: Arc<dyn Fn() + Send + Sync>,
    on_connected: Option<Arc<dyn Fn() + Send + Sync>>,
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
            on_connected: None,
            on_event: None,
            stop: Arc::new(AtomicBool::new(false)),
            started: Mutex::new(false),
        }
    }

    pub fn with_connection_handler(mut self, handler: impl Fn() + Send + Sync + 'static) -> Self {
        self.on_connected = Some(Arc::new(handler));
        self
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
            on_connected: self.on_connected.clone(),
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
    on_connected: Option<Arc<dyn Fn() + Send + Sync>>,
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
    let connected_handler = parts.on_connected.clone();
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
            if let Some(handler) = &connected_handler {
                handler();
            }
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

    fn stream_event(seq: i32) -> StreamEvent {
        StreamEvent {
            event_type: "test".into(),
            session_id: "agent".into(),
            agent_id: "agent".into(),
            seq,
            agent_seq: seq,
            event_version: 1,
            stream_epoch: "epoch".into(),
            delivery: "replayable".into(),
            data: HashMap::new(),
        }
    }

    fn conversation_delta(agent_id: &str, delivery: &str) -> StreamEvent {
        StreamEvent {
            event_type: "conversation_delta".into(),
            session_id: agent_id.into(),
            agent_id: agent_id.into(),
            seq: 0,
            agent_seq: 0,
            event_version: 1,
            stream_epoch: "epoch".into(),
            delivery: delivery.into(),
            data: HashMap::new(),
        }
    }

    #[test]
    fn background_ephemeral_deltas_are_filtered_but_active_and_reliable_events_are_kept() {
        let mut state = EventBridgeState::default();
        state.set_stream_connected();
        state.set_webview_ready();
        assert!(state.set_active_agent("active", 1, 0));

        assert!(state
            .push(conversation_delta("background", "ephemeral"))
            .is_none());
        let active_delta = state
            .push(conversation_delta("active", "ephemeral"))
            .expect("active delta should be forwarded");
        assert_eq!(active_delta.events.len(), 1);

        let mut reliable = stream_event(1);
        reliable.event_type = "conversation_patch".into();
        reliable.agent_id = "background".into();
        reliable.delivery = "replayable".into();
        let batch = state
            .push(reliable)
            .expect("reliable facts must be forwarded");
        assert_eq!(batch.events[0].event_type, "conversation_patch");

        let mut other_ephemeral = conversation_delta("background", "ephemeral");
        other_ephemeral.event_type = "notification_changed".into();
        let batch = state
            .push(other_ephemeral)
            .expect("non-delta ephemeral notifications must be forwarded");
        assert_eq!(batch.events[0].event_type, "notification_changed");
    }

    #[test]
    fn high_volume_background_deltas_do_not_fill_the_pre_ready_buffer() {
        let mut state = EventBridgeState::default();
        assert!(state.set_active_agent("active", 1, 0));

        for _ in 0..10_000 {
            assert!(state
                .push(conversation_delta("background", "ephemeral"))
                .is_none());
        }

        assert!(state.queued.is_empty());
        assert_eq!(state.queued_bytes, 0);
        assert!(!state.overflowed);

        let mut reliable = stream_event(1);
        reliable.agent_id = "background".into();
        reliable.event_type = "conversation_patch".into();
        let _ = state.push(reliable);
        assert_eq!(state.queued.len(), 1);
        assert!(!state.overflowed);
    }

    #[test]
    fn active_agent_generation_rejects_out_of_order_selection_and_resets_on_reload() {
        let mut state = EventBridgeState::default();
        assert!(state.set_active_agent("agent-b", 2, 0));
        assert!(!state.set_active_agent("agent-a", 1, 0));
        assert!(!state.set_active_agent("agent-a", 2, 0));
        assert_eq!(state.active_agent_id.as_deref(), Some("agent-b"));

        state.set_webview_loading();
        assert_eq!(state.active_agent_id, None);
        assert!(!state.set_active_agent("stale-agent", 3, 0));
        assert!(state.set_active_agent("agent-a", 1, 1));
        assert_eq!(state.active_agent_id.as_deref(), Some("agent-a"));
    }

    #[test]
    fn old_page_selection_cannot_override_a_reloaded_webview_scope() {
        let mut state = EventBridgeState::default();
        assert!(state.set_active_agent("agent-before-reload", 4, 0));

        state.set_webview_loading();
        let new_page_generation = state.webview_generation;

        assert!(!state.set_active_agent("late-old-selection", 5, 0));
        assert!(state.set_active_agent("agent-after-reload", 1, new_page_generation));
        assert_eq!(state.active_agent_id.as_deref(), Some("agent-after-reload"));
    }

    #[test]
    fn buffers_until_both_webview_listener_and_stream_are_ready() {
        let mut state = EventBridgeState::default();
        assert!(state.push(stream_event(1)).is_none());
        assert!(state.push(stream_event(2)).is_none());
        assert!(state.set_webview_ready().is_none());
        let batch = state.set_stream_connected().expect("ready batch");
        assert!(batch.requires_resync);
        assert_eq!(
            batch
                .events
                .iter()
                .map(|event| event.seq)
                .collect::<Vec<_>>(),
            [1, 2]
        );
    }

    #[test]
    fn overflow_drops_unbounded_backlog_and_requests_authoritative_resync() {
        let mut state = EventBridgeState::default();
        state.set_stream_connected();
        for seq in 1..=(MAX_PRE_READY_EVENTS as i32 + 1) {
            assert!(state.push(stream_event(seq)).is_none());
        }
        let batch = state.set_webview_ready().expect("ready batch");
        assert!(batch.requires_resync);
        assert!(batch.events.is_empty());
    }

    #[test]
    fn oversized_pre_ready_event_triggers_resync_without_retaining_its_payload() {
        let mut state = EventBridgeState::default();
        state.set_stream_connected();
        let mut event = stream_event(1);
        event.data.insert(
            "content".into(),
            Value::String("x".repeat(MAX_PRE_READY_BYTES + 1)),
        );
        assert!(state.push(event).is_none());
        assert!(state.queued.is_empty());
        assert!(state.overflowed);
        let batch = state.set_webview_ready().expect("ready batch");
        assert!(batch.requires_resync);
        assert!(batch.events.is_empty());
    }

    #[test]
    fn webview_reload_reestablishes_a_resync_boundary() {
        let mut state = EventBridgeState::default();
        state.set_stream_connected();
        let first = state.set_webview_ready().expect("first ready batch");
        assert!(first.requires_resync);
        assert!(first.events.is_empty());

        state.set_webview_loading();
        assert!(state.push(stream_event(3)).is_none());
        let reloaded = state.set_webview_ready().expect("reload ready batch");
        assert!(reloaded.requires_resync);
        assert_eq!(
            reloaded
                .events
                .iter()
                .map(|event| event.seq)
                .collect::<Vec<_>>(),
            [3]
        );
    }

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
