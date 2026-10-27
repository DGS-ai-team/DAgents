# Dependency readiness check (2026-10-17)

## Desktop design prototypes

At the time of this check, the Vite pin was updated from 6.4.2 to 6.4.4 in both `docs/design/desktop-product/prototype` and `docs/design/desktop-interactive-prototype`. `npm audit fix` refreshed compatible transitive lockfile entries without a major-version change. These are prototype-only manifests and lockfiles; no production desktop dependency was changed. The current design-product README marks `docs/design/desktop-interactive-prototype/` as retired; do not continue routine dependency updates there.

Verification in each prototype directory:

- `npm audit --audit-level=moderate`: 0 vulnerabilities.
- `npm run build`: passed; generated client, server, and Sites hosting outputs.
- `npm run test:sites`: 4/4 tests passed.

## Rust `glib` advisory

`desktop/tray-tauri/src-tauri/Cargo.lock` resolves `glib 0.18.5`, flagged by [RustSec RUSTSEC-2024-0429](https://rustsec.org/advisories/RUSTSEC-2024-0429.html). The affected `VariantStrIter` iterator methods have an unsound pointer out-parameter; RustSec lists the fix in `glib >=0.20.0`.

Target-specific `cargo tree -i glib` inspection found no `glib` dependency for either `x86_64-pc-windows-msvc` or `i686-pc-windows-msvc`. The Linux target resolves the GLib/GTK3 stack through Tauri's Wry and tray support: `tauri -> tauri-runtime-wry/wry` and `tauri -> tray-icon/muda -> gtk/libappindicator -> glib 0.18.5`, with WebKitGTK also using the GTK 0.18 family. The currently supported packaged Desktop is Windows; this finding is therefore confined to the Linux dependency graph, which is not a release target for this shell.

There is no safe lockfile-only bump to GLib 0.20: the GTK3 Rust bindings in this graph require the 0.18 API family, and GLib is also used across GTK, GDK, Cairo, Pango, WebKitGTK, and app-indicator bindings. RustSec's fix came from the coordinated gtk-rs-core 0.20 update. A real Linux fix needs an upstream-compatible GTK/Tauri dependency-family migration (or a deliberately different tray/WebKit backend), followed by Linux build/runtime validation. That scope is intentionally deferred rather than forcing incompatible duplicate GLib types or upgrading the full GTK/Tauri stack without Linux release coverage.

The initial audit made no dependency changes. After it identified an unused production dependency, `zip` was removed from the Tauri manifest and lockfile as documented below.

## RustSec audit

Installed `cargo-audit 0.22.2` under the ignored `.runtime-test-release-closeout-20261017/audit-tools` directory (Cargo `1.97.1`, Rust `1.97.1`); no global tool installation was made. The first `cargo audit --file desktop/tray-tauri/src-tauri/Cargo.lock` fetched the RustSec advisory database (1,334 advisories) and scanned all 488 locked crate dependencies. It found three vulnerabilities and seven allowed warnings. Investigation found no `zip` API use in the Tauri Rust sources or `build.rs`; `zip` was an unused direct manifest dependency. It has been removed and Cargo regenerated the lockfile. The post-removal `cargo audit --file desktop/tray-tauri/src-tauri/Cargo.lock --no-fetch --stale` scanned 484 locked crate dependencies with the cached 1,334-advisory database: zero vulnerabilities and seven allowed warnings. A later advisory database refresh failed with a GitHub fetch error, so this local post-removal scan used the successfully fetched database from earlier in this audit session. CI now runs pinned `cargo-audit 0.22.2` against the lockfile without ignores and fails on vulnerabilities; warnings remain visible but non-blocking.

The initial Windows dependency graph included direct dependency `zip 7.2.0`, flagged by RUSTSEC-2026-0367 (high, CVSS 7.3), RUSTSEC-2026-0368 (severity not assigned), and RUSTSEC-2026-0369 (critical, CVSS 9.1). All recommend `zip >=9.0.0` and concern archive extraction/path or permission handling. Since `zip` was unused, it was removed instead of upgraded. `cargo tree --locked --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml -i zip --target x86_64-pc-windows-msvc` now reports no matching package. No advisory was ignored.

The full-lockfile scan also reports `glib 0.18.5` under RUSTSEC-2024-0429 as an unsoundness warning, along with six unmaintained-crate warnings (`proc-macro-error` and the `unic-*` crates). `cargo tree` confirms `glib` is absent from the Windows target graph and belongs to the Linux GTK/WebKitGTK tray graph described above. The RustSec scan covers all platforms represented in the lockfile; the Linux-only GLib warning is not a Windows release dependency. After removing `zip`, `cargo test --locked --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml --lib` passed (42 tests), and `cargo check --locked --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml --lib --target x86_64-pc-windows-msvc` passed. These checks do not include installer/runtime validation or the 32-bit Windows target.

The subsequent [CI run for `12738e4a`](https://github.com/DGS-ai-team/DAgents/actions/runs/39011353761) passed the new RustSec job using its normal online advisory refresh, without `--no-fetch`, `--stale`, or advisory ignores. Windows x64/x86 Desktop compilation and Linux Rust tests also passed in that run. This closes the local database-refresh limitation for the checked source; the Linux-only and unmaintained-crate warnings remain recorded above.
