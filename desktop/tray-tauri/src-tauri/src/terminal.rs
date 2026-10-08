//! Tauri-owned relay for the Node agent terminal WebSocket.
//! The WebView only sends typed terminal actions through commands; it never
//! receives a Node URL, token, or direct network capability.

use percent_encoding::{utf8_percent_encode, NON_ALPHANUMERIC};
use serde::Serialize;
use serde_json::{json, Value};
use std::net::TcpStream;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, Sender, TryRecvError};
use std::sync::Arc;
use std::thread;
use std::time::Duration;
use tauri::{AppHandle, Emitter};
use tungstenite::client::IntoClientRequest;
use tungstenite::http::header::{HeaderValue, AUTHORIZATION};
use tungstenite::stream::MaybeTlsStream;
use tungstenite::{connect, Error as WsError, Message, WebSocket};

#[derive(Clone, Debug)]
pub struct TerminalControl {
    pub message: Value,
}

#[derive(Clone, Debug)]
pub struct RelayHandle {
    pub agent_id: String,
    pub sender: Sender<TerminalControl>,
    pub alive: Arc<AtomicBool>,
}

#[derive(Clone, Serialize)]
struct RelayEvent<'a> {
    connection_id: &'a str,
    agent_id: &'a str,
    event: Value,
}

pub fn start(
    app: AppHandle,
    endpoint: String,
    token: Option<String>,
    connection_id: String,
    agent_id: String,
    first_command: Value,
) -> (Sender<TerminalControl>, Arc<AtomicBool>) {
    let (tx, rx) = mpsc::channel::<TerminalControl>();
    let alive = Arc::new(AtomicBool::new(true));
    let thread_alive = Arc::clone(&alive);
    let event_id = connection_id.clone();
    let event_agent = agent_id.clone();
    thread::spawn(move || {
        let result = run_relay(
            &app,
            &endpoint,
            token.as_deref(),
            &connection_id,
            &agent_id,
            first_command,
            rx,
        );
        thread_alive.store(false, Ordering::Release);
        if let Err(error) = result {
            emit_event(
                &app,
                &event_id,
                &event_agent,
                json!({"type":"error", "error":error}),
            );
        }
        emit_event(
            &app,
            &event_id,
            &event_agent,
            json!({"type":"relay_closed"}),
        );
    });
    (tx, alive)
}

fn run_relay(
    app: &AppHandle,
    endpoint: &str,
    token: Option<&str>,
    connection_id: &str,
    agent_id: &str,
    first_command: Value,
    rx: mpsc::Receiver<TerminalControl>,
) -> Result<(), String> {
    let ws_url = build_ws_url(endpoint, agent_id)?;
    let mut request = ws_url
        .into_client_request()
        .map_err(|e| format!("终端握手请求无效: {e}"))?;
    if let Some(token) = token.filter(|value| !value.is_empty()) {
        let value = HeaderValue::from_str(&format!("Bearer {token}"))
            .map_err(|e| format!("Node 客户端凭据无效: {e}"))?;
        request.headers_mut().insert(AUTHORIZATION, value);
    }
    let (mut socket, _) = connect(request).map_err(|e| format!("连接 Node 终端失败: {e}"))?;
    set_read_timeout(&mut socket, Duration::from_millis(30))?;
    send_json(&mut socket, &first_command)?;
    let mut detached = false;
    loop {
        match rx.try_recv() {
            Ok(control) => {
                if control.message.get("type").and_then(Value::as_str) == Some("detach") {
                    detached = true;
                    break;
                }
                send_json(&mut socket, &control.message)?;
            }
            Err(TryRecvError::Disconnected) => {
                detached = true;
                break;
            }
            Err(TryRecvError::Empty) => {}
        }

        match socket.read() {
            Ok(Message::Text(text)) => {
                let value: Value = serde_json::from_str(&text)
                    .map_err(|e| format!("Node 终端返回无效 JSON: {e}"))?;
                emit_event(app, connection_id, agent_id, value);
            }
            Ok(Message::Binary(bytes)) => {
                if let Ok(value) = serde_json::from_slice::<Value>(&bytes) {
                    emit_event(app, connection_id, agent_id, value);
                }
            }
            Ok(Message::Close(_)) => break,
            Ok(_) => {}
            Err(WsError::Io(error))
                if matches!(
                    error.kind(),
                    std::io::ErrorKind::WouldBlock
                        | std::io::ErrorKind::TimedOut
                        | std::io::ErrorKind::Interrupted
                ) => {}
            Err(error) => return Err(format!("Node 终端连接中断: {error}")),
        }
    }
    if detached {
        let _ = socket.close(None);
    }
    Ok(())
}

fn build_ws_url(endpoint: &str, agent_id: &str) -> Result<String, String> {
    let base = url::Url::parse(endpoint).map_err(|e| format!("Node 地址无效: {e}"))?;
    if base.scheme() != "http"
        || !matches!(
            base.host_str(),
            Some("127.0.0.1") | Some("localhost") | Some("::1")
        )
        || !base.username().is_empty()
        || base.password().is_some()
        || !matches!(base.path(), "" | "/")
        || base.query().is_some()
        || base.fragment().is_some()
    {
        return Err("终端仅允许连接本机 HTTP Node；远程请使用 loopback SSH 隧道".into());
    }
    let host = base.host_str().ok_or("Node 地址缺少主机名")?;
    let port = base.port().unwrap_or(80);
    let ws_host = if host.contains(':') {
        format!("[{host}]")
    } else {
        host.to_string()
    };
    let encoded_agent = utf8_percent_encode(agent_id, NON_ALPHANUMERIC).to_string();
    Ok(format!(
        "ws://{ws_host}:{port}/v1/agents/{encoded_agent}/terminals/ws"
    ))
}

fn send_json(
    socket: &mut WebSocket<MaybeTlsStream<TcpStream>>,
    value: &Value,
) -> Result<(), String> {
    socket
        .send(Message::Text(value.to_string()))
        .map_err(|e| format!("发送终端命令失败: {e}"))
}

fn set_read_timeout(
    socket: &mut WebSocket<MaybeTlsStream<TcpStream>>,
    timeout: Duration,
) -> Result<(), String> {
    match socket.get_mut() {
        MaybeTlsStream::Plain(stream) => stream
            .set_read_timeout(Some(timeout))
            .map_err(|e| format!("设置终端读取超时失败: {e}")),
        _ => Err("Desktop 本地 Node 终端只支持 loopback HTTP".into()),
    }
}

fn emit_event(app: &AppHandle, connection_id: &str, agent_id: &str, event: Value) {
    let _ = app.emit_to(
        "desktop_mvp",
        "desktop-terminal-event",
        RelayEvent {
            connection_id,
            agent_id,
            event,
        },
    );
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::Engine;
    use std::time::{Duration, Instant};
    use tungstenite::http::HeaderValue;

    #[test]
    fn websocket_url_is_limited_to_root_loopback_endpoint() {
        assert_eq!(
            build_ws_url("http://127.0.0.1:18765/", "agent-a").unwrap(),
            "ws://127.0.0.1:18765/v1/agents/agent%2Da/terminals/ws"
        );
        for endpoint in [
            "https://127.0.0.1:18765",
            "http://example.com:18765",
            "http://user:pass@127.0.0.1:18765",
            "http://127.0.0.1:18765/prefix",
            "http://127.0.0.1:18765/?target=elsewhere",
            "http://127.0.0.1:18765/#fragment",
        ] {
            assert!(
                build_ws_url(endpoint, "agent-a").is_err(),
                "accepted unsafe endpoint: {endpoint}"
            );
        }
    }

    fn connect_test_terminal(
        endpoint: &str,
        agent_id: &str,
    ) -> WebSocket<MaybeTlsStream<TcpStream>> {
        let base = url::Url::parse(endpoint).unwrap();
        let host = base.host_str().unwrap();
        let host = if host.contains(':') {
            format!("[{host}]")
        } else {
            host.to_string()
        };
        let port = base.port().unwrap_or(80);
        let agent = utf8_percent_encode(agent_id, NON_ALPHANUMERIC).to_string();
        let mut request = format!("ws://{host}:{port}/v1/agents/{agent}/terminals/ws")
            .into_client_request()
            .unwrap();
        if let Ok(token) = std::env::var("DAGENTS_CLIENT_TOKEN") {
            if !token.trim().is_empty() {
                request.headers_mut().insert(
                    AUTHORIZATION,
                    HeaderValue::from_str(&format!("Bearer {}", token.trim())).unwrap(),
                );
            }
        }
        let (mut socket, _) = connect(request).expect("connect terminal websocket");
        set_read_timeout(&mut socket, Duration::from_secs(2)).unwrap();
        socket
    }

    fn read_event(
        socket: &mut WebSocket<MaybeTlsStream<TcpStream>>,
        timeout: Duration,
        predicate: impl Fn(&Value) -> bool,
    ) -> Value {
        let until = Instant::now() + timeout;
        while Instant::now() < until {
            match socket.read() {
                Ok(Message::Text(text)) => {
                    let event: Value =
                        serde_json::from_str(&text).expect("Node terminal event JSON");
                    if predicate(&event) {
                        return event;
                    }
                }
                Ok(_) => {}
                Err(WsError::Io(error))
                    if matches!(
                        error.kind(),
                        std::io::ErrorKind::WouldBlock
                            | std::io::ErrorKind::TimedOut
                            | std::io::ErrorKind::Interrupted
                    ) => {}
                Err(error) => panic!("Node terminal websocket read failed: {error}"),
            }
        }
        panic!("timed out waiting for Node terminal event")
    }

    fn wait_for_session(endpoint: &str, agent_id: &str, session_id: &str, should_exist: bool) {
        let client = crate::nodeclient::Client::new(endpoint);
        let until = Instant::now() + Duration::from_secs(8);
        loop {
            let sessions = client
                .list_terminals(agent_id)
                .expect("list Node terminal sessions");
            let found = sessions.iter().any(|item| item.terminal_id == session_id);
            if found == should_exist {
                return;
            }
            assert!(
                Instant::now() < until,
                "terminal session listing did not converge"
            );
            thread::sleep(Duration::from_millis(150));
        }
    }

    #[test]
    fn live_local_node_terminal_open_input_detach_resume_close_smoke() {
        if std::env::var("DAGENTS_TERMINAL_SMOKE").as_deref() != Ok("1") {
            return;
        }
        let endpoint = std::env::var("DAGENTS_NODE_ENDPOINT")
            .unwrap_or_else(|_| "http://127.0.0.1:18765".into());
        let agent_id =
            std::env::var("DAGENTS_MVP_AGENT_ID").expect("DAGENTS_MVP_AGENT_ID required");
        let marker = format!("DAGENTS_TERMINAL_SMOKE_{}", std::process::id());
        let mut socket = connect_test_terminal(&endpoint, &agent_id);
        let shell = if cfg!(windows) { "powershell" } else { "bash" };
        socket
            .send(Message::Text(
                json!({"type":"open","target_kind":"local","shell":shell,"rows":24,"cols":80})
                    .to_string(),
            ))
            .unwrap();
        let started = read_event(&mut socket, Duration::from_secs(12), |event| {
            event["type"] == "started"
        });
        let session_id = started["session_id"]
            .as_str()
            .expect("started session_id")
            .to_string();
        let command = if cfg!(windows) {
            format!("Write-Output '{marker}'\r\n")
        } else {
            format!("printf '{marker}\\n'\n")
        };
        socket.send(Message::Text(json!({"type":"input","data":base64::engine::general_purpose::STANDARD.encode(command.as_bytes())}).to_string())).unwrap();
        read_event(&mut socket, Duration::from_secs(20), |event| {
            event["type"] == "output"
                && base64::engine::general_purpose::STANDARD
                    .decode(event["data"].as_str().unwrap_or_default())
                    .map(|bytes| String::from_utf8_lossy(&bytes).contains(&marker))
                    .unwrap_or(false)
        });

        socket.close(None).expect("detach terminal websocket");
        drop(socket);
        wait_for_session(&endpoint, &agent_id, &session_id, true);

        let mut resumed = connect_test_terminal(&endpoint, &agent_id);
        resumed
            .send(Message::Text(
                json!({"type":"resume","session_id":session_id}).to_string(),
            ))
            .unwrap();
        read_event(&mut resumed, Duration::from_secs(12), |event| {
            event["type"] == "started"
        });
        read_event(&mut resumed, Duration::from_secs(12), |event| {
            event["type"] == "output"
                && event["replay"] == true
                && base64::engine::general_purpose::STANDARD
                    .decode(event["data"].as_str().unwrap_or_default())
                    .map(|bytes| String::from_utf8_lossy(&bytes).contains(&marker))
                    .unwrap_or(false)
        });
        resumed
            .send(Message::Text(json!({"type":"close"}).to_string()))
            .unwrap();
        read_event(&mut resumed, Duration::from_secs(8), |event| {
            event["type"] == "closed"
        });
        wait_for_session(&endpoint, &agent_id, &session_id, false);
    }
}
