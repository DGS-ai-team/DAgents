//! Standalone `dagents update` command; Desktop UI uses typed Tauri IPC.

use crate::config::ShellConfig;
use crate::layout::Layout;
use crate::nodeclient::Client;
use crate::nodectl;
use crate::update::{self, Applier, ApplyOptions, Checker};
use std::sync::Arc;
use std::time::Duration;

pub fn run_update_command(args: &[String], layout: Layout, cfg: Arc<ShellConfig>) -> i32 {
    let (check_only, force) = parse_update_args(args);
    if let Err(error) = nodectl::start(&layout, &cfg, Duration::from_secs(45)) {
        crate::diagnostics::stderr_best_effort(format_args!("启动 Node 失败: {error}"));
        return 1;
    }
    let client = Arc::new(Client::new(crate::config::NODE_ENDPOINT));
    let checker = Arc::new(Checker::new(
        Arc::clone(&cfg),
        layout.home.clone(),
        Arc::clone(&client),
    ));
    let applier = Applier::new(layout, checker, client);
    let (result, code) = applier.run(ApplyOptions {
        check_only,
        force,
        ..Default::default()
    });
    if !result.message.trim().is_empty() && code != 0 && code != update::EXIT_UP_TO_DATE {
        crate::diagnostics::stderr_best_effort(format_args!("{}", result.message));
    }
    code
}

fn parse_update_args(args: &[String]) -> (bool, bool) {
    let mut check_only = false;
    let mut force = false;
    for arg in args {
        match arg.trim().to_ascii_lowercase().as_str() {
            "--check" | "-check" => check_only = true,
            "--force" | "-force" => force = true,
            _ => {}
        }
    }
    (check_only, force)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_update_flags() {
        assert_eq!(
            parse_update_args(&["--check".into(), "--force".into()]),
            (true, true)
        );
        assert_eq!(parse_update_args(&[]), (false, false));
    }
}
