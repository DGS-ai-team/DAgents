# Dependency readiness check (2026-10-17)

## Desktop design prototypes

Updated the Vite pin from 6.4.2 to 6.4.4 in both `docs/design/desktop-product/prototype` and `docs/design/desktop-interactive-prototype`. `npm audit fix` refreshed compatible transitive lockfile entries without a major-version change. These are prototype-only manifests and lockfiles; no production desktop dependency was changed.

Verification in each prototype directory:

- `npm audit --audit-level=moderate`: 0 vulnerabilities.
- `npm run build`: passed; generated client, server, and Sites hosting outputs.
- `npm run test:sites`: 4/4 tests passed.

## Rust `glib` advisory

`desktop/tray-tauri/src-tauri/Cargo.lock` resolves `glib 0.18.5`, flagged by [RustSec RUSTSEC-2024-0429](https://rustsec.org/advisories/RUSTSEC-2024-0429.html). The affected `VariantStrIter` iterator methods have an unsound pointer out-parameter; RustSec lists the fix in `glib >=0.20.0`.

Target-specific `cargo tree -i glib` inspection found no `glib` dependency for either `x86_64-pc-windows-msvc` or `i686-pc-windows-msvc`. The Linux target resolves the GLib/GTK3 stack through Tauri's Wry and tray support: `tauri -> tauri-runtime-wry/wry` and `tauri -> tray-icon/muda -> gtk/libappindicator -> glib 0.18.5`, with WebKitGTK also using the GTK 0.18 family. The currently supported packaged Desktop is Windows; this finding is therefore confined to the Linux dependency graph, which is not a release target for this shell.

There is no safe lockfile-only bump to GLib 0.20: the GTK3 Rust bindings in this graph require the 0.18 API family, and GLib is also used across GTK, GDK, Cairo, Pango, WebKitGTK, and app-indicator bindings. RustSec's fix came from the coordinated gtk-rs-core 0.20 update. A real Linux fix needs an upstream-compatible GTK/Tauri dependency-family migration (or a deliberately different tray/WebKit backend), followed by Linux build/runtime validation. That scope is intentionally deferred rather than forcing incompatible duplicate GLib types or upgrading the full GTK/Tauri stack without Linux release coverage.

`Cargo.lock` and Tauri manifests were not changed during this check.
