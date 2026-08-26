//! Localhost desktop helper API for Web UI integration.

use crate::clipboard;
use crate::config::ShellConfig;
use crate::layout::Layout;
use crate::nodeclient::Client;
use crate::uifocus::{Store as UIFocusStore, DEFAULT_TTL};
use crate::update::{self, Applier, ApplyOptions, Checker, Status, EXIT_UP_TO_DATE};
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;
use tiny_http::{Header, Method, Request, Response, Server as TinyServer, StatusCode};

pub const DEFAULT_LISTEN_ADDR: &str = "127.0.0.1:18767";

pub struct Server {
    updates: Arc<Checker>,
    applier: Arc<Applier>,
    ui_focus: Arc<UIFocusStore>,
}

impl Server {
    pub fn new(updates: Arc<Checker>, applier: Arc<Applier>, ui_focus: Arc<UIFocusStore>) -> Self {
        Self {
            updates,
            applier,
            ui_focus,
        }
    }

    pub fn start(self: Arc<Self>, stop: Arc<AtomicBool>) {
        thread::spawn(move || {
            let server = match TinyServer::http(DEFAULT_LISTEN_ADDR) {
                Ok(server) => server,
                Err(e) => {
                    eprintln!("desktop API listen: {e}");
                    return;
                }
            };
            eprintln!("desktop API listening on http://{DEFAULT_LISTEN_ADDR}");
            while !stop.load(Ordering::SeqCst) {
                match server.recv_timeout(Duration::from_millis(500)) {
                    Ok(Some(req)) => self.handle(req),
                    Ok(None) => {}
                    Err(e) => eprintln!("desktop API: {e}"),
                }
            }
        });
    }

    fn handle(&self, mut req: Request) {
        if req.method() == &Method::Options {
            let response = with_cors(&req, empty_response(StatusCode(204)));
            let _ = req.respond(response);
            return;
        }
        let path = req.url().split('?').next().unwrap_or(req.url()).to_string();
        let method = req.method().clone();
        let response = match (method, path.as_str()) {
            (Method::Get, "/health") => json_response(StatusCode(200), &json!({ "ok": true })),
            (Method::Get, "/v1/desktop/update") => {
                json_response(StatusCode(200), &self.updates.snapshot())
            }
            (Method::Post, "/v1/desktop/update/apply") => self.handle_update_apply(&mut req),
            (Method::Get, "/v1/desktop/clipboard/files") => match clipboard::file_paths() {
                Ok(paths) => json_response(StatusCode(200), &json!({ "paths": paths })),
                Err(err) => json_response(
                    StatusCode(500),
                    &json!({ "paths": Vec::<String>::new(), "message": err }),
                ),
            },
            (Method::Post, "/v1/desktop/ui/focus") => self.handle_ui_focus(&mut req),
            _ => json_response(StatusCode(404), &json!({ "message": "not found" })),
        };
        let response = with_cors(&req, response);
        let _ = req.respond(response);
    }

    fn handle_update_apply(&self, req: &mut Request) -> Response<std::io::Cursor<Vec<u8>>> {
        let body = read_body(req);
        let parsed = if body.trim().is_empty() {
            ApplyRequest::default()
        } else {
            match serde_json::from_str::<ApplyRequest>(&body) {
                Ok(parsed) => parsed,
                Err(err) => {
                    return json_response(
                        StatusCode(400),
                        &ApplyResponse {
                            ok: false,
                            message: format!("invalid json: {err}"),
                            code: 0,
                            status: Status::default(),
                        },
                    );
                }
            }
        };
        let (result, code) = self.applier.run(ApplyOptions {
            force: parsed.force,
            skip_confirm: true,
            ..ApplyOptions::default()
        });
        json_response(
            StatusCode(200),
            &ApplyResponse {
                ok: code == 0 || code == EXIT_UP_TO_DATE,
                message: result.message,
                code,
                status: result.status,
            },
        )
    }

    fn handle_ui_focus(&self, req: &mut Request) -> Response<std::io::Cursor<Vec<u8>>> {
        let body = read_body(req);
        let parsed = if body.trim().is_empty() {
            UIFocusRequest::default()
        } else {
            match serde_json::from_str::<UIFocusRequest>(&body) {
                Ok(parsed) => parsed,
                Err(err) => {
                    return json_response(
                        StatusCode(400),
                        &json!({ "ok": false, "message": format!("invalid json: {err}") }),
                    );
                }
            }
        };
        let ttl = if parsed.ttl_seconds > 0 {
            Duration::from_secs(parsed.ttl_seconds as u64)
        } else {
            DEFAULT_TTL
        };
        self.ui_focus
            .report(&parsed.source_id, &parsed.agent_id, ttl);
        json_response(
            StatusCode(200),
            &json!({
                "ok": true,
                "agent_id": parsed.agent_id,
                "source_id": parsed.source_id,
            }),
        )
    }
}

pub fn base_url() -> String {
    format!("http://{DEFAULT_LISTEN_ADDR}")
}

pub fn run_update_command(args: &[String], layout: Layout, cfg: Arc<ShellConfig>) -> i32 {
    let (check_only, force) = parse_update_args(args);
    if desktop_api_health().is_ok() {
        if check_only {
            let (status, code) = get_update_status();
            update::print_status(&status);
            return code;
        }
        return post_update_apply(force);
    }
    let client = Arc::new(Client::new(&cfg.endpoint));
    let checker = Arc::new(Checker::new(Arc::clone(&cfg), layout.home.clone()));
    let applier = Applier::new(cfg, layout, checker, client);
    let (result, code) = applier.run(ApplyOptions {
        check_only,
        force,
        skip_confirm: false,
    });
    if !result.message.trim().is_empty() && code != 0 && code != EXIT_UP_TO_DATE {
        eprintln!("{}", result.message);
    }
    code
}

fn desktop_api_health() -> Result<(), String> {
    let resp = ureq::get(&format!("{}/health", base_url()))
        .timeout(Duration::from_secs(3))
        .call()
        .map_err(|e| e.to_string())?;
    if resp.status() == 200 {
        Ok(())
    } else {
        Err(format!("desktop API health: status {}", resp.status()))
    }
}

fn get_update_status() -> (Status, i32) {
    let resp = match ureq::get(&format!("{}/v1/desktop/update", base_url()))
        .timeout(Duration::from_secs(30))
        .call()
    {
        Ok(resp) => resp,
        Err(e) => {
            eprintln!("update check: {e}");
            return (Status::default(), 1);
        }
    };
    let status: Status = match resp.into_json() {
        Ok(status) => status,
        Err(e) => {
            eprintln!("update check decode: {e}");
            return (Status::default(), 1);
        }
    };
    if !status.manage_reachable {
        (status, 1)
    } else if !status.upgrade_available {
        (status, EXIT_UP_TO_DATE)
    } else {
        (status, 0)
    }
}

fn post_update_apply(force: bool) -> i32 {
    let resp = match ureq::post(&format!("{}/v1/desktop/update/apply", base_url()))
        .set("Content-Type", "application/json")
        .timeout(Duration::from_secs(20 * 60))
        .send_json(json!({ "force": force }))
    {
        Ok(resp) => resp,
        Err(e) => {
            eprintln!("update apply: {e}");
            return 1;
        }
    };
    let status = resp.status();
    let out: ApplyResponse = match resp.into_json() {
        Ok(out) => out,
        Err(e) => {
            eprintln!("update apply decode: {e}");
            return 1;
        }
    };
    if !out.message.trim().is_empty() {
        if out.ok {
            println!("{}", out.message);
        } else {
            eprintln!("{}", out.message);
        }
    }
    if out.code != 0 {
        return out.code;
    }
    if (200..300).contains(&status) {
        0
    } else {
        1
    }
}

fn parse_update_args(args: &[String]) -> (bool, bool) {
    let mut check_only = false;
    let mut force = false;
    for arg in args {
        match arg.trim().to_ascii_lowercase().as_str() {
            "--check" => check_only = true,
            "--force" => force = true,
            _ => {}
        }
    }
    (check_only, force)
}

fn read_body(req: &mut Request) -> String {
    let mut body = String::new();
    let _ = req.as_reader().read_to_string(&mut body);
    body
}

fn empty_response(status: StatusCode) -> Response<std::io::Cursor<Vec<u8>>> {
    Response::from_data(Vec::new()).with_status_code(status)
}

fn json_response<T: Serialize>(
    status: StatusCode,
    value: &T,
) -> Response<std::io::Cursor<Vec<u8>>> {
    let body = serde_json::to_vec(value).unwrap_or_else(|_| b"{}".to_vec());
    add_header(
        Response::from_data(body).with_status_code(status),
        "Content-Type",
        "application/json; charset=utf-8",
    )
}

fn with_cors(
    req: &Request,
    mut response: Response<std::io::Cursor<Vec<u8>>>,
) -> Response<std::io::Cursor<Vec<u8>>> {
    let origin = req
        .headers()
        .iter()
        .find(|h| h.field.equiv("Origin"))
        .map(|h| h.value.as_str().trim().to_string())
        .unwrap_or_default();
    if is_localhost_origin(&origin) {
        response = add_header(response, "Access-Control-Allow-Origin", &origin);
        response = add_header(response, "Vary", "Origin");
        response = add_header(
            response,
            "Access-Control-Allow-Methods",
            "GET, POST, OPTIONS",
        );
        response = add_header(
            response,
            "Access-Control-Allow-Headers",
            "Content-Type, Accept",
        );
    }
    response
}

fn add_header(
    response: Response<std::io::Cursor<Vec<u8>>>,
    name: &str,
    value: &str,
) -> Response<std::io::Cursor<Vec<u8>>> {
    match Header::from_bytes(name.as_bytes(), value.as_bytes()) {
        Ok(header) => response.with_header(header),
        Err(_) => response,
    }
}

fn is_localhost_origin(origin: &str) -> bool {
    let lower = origin.trim().to_ascii_lowercase();
    lower.starts_with("http://127.0.0.1:")
        || lower.starts_with("http://localhost:")
        || lower.starts_with("http://[::1]:")
}

#[derive(Debug, Deserialize, Default)]
struct ApplyRequest {
    #[serde(default)]
    force: bool,
}

#[derive(Debug, Serialize, Deserialize, Default)]
struct ApplyResponse {
    ok: bool,
    #[serde(default)]
    message: String,
    #[serde(default)]
    code: i32,
    #[serde(default)]
    status: Status,
}

#[derive(Debug, Deserialize, Default)]
struct UIFocusRequest {
    #[serde(default)]
    agent_id: String,
    #[serde(default)]
    source_id: String,
    #[serde(default)]
    ttl_seconds: i32,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn allows_localhost_cors_origins() {
        assert!(is_localhost_origin("http://127.0.0.1:18765"));
        assert!(is_localhost_origin("http://localhost:1422"));
        assert!(is_localhost_origin("http://[::1]:18765"));
        assert!(!is_localhost_origin("https://example.com"));
    }

    #[test]
    fn parses_update_args() {
        let (check, force) = parse_update_args(&["--check".into(), "--force".into()]);
        assert!(check);
        assert!(force);
    }
}
