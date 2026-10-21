# Shared Desktop UI source

This directory contains Vue components and settings pages reused by DAgents Desktop. It is not a standalone web application: Agent Node exposes APIs only, there is no `/ui/` route, and this package has no browser `dev`, `build`, or `preview` command. The supported product interface is the Tauri Desktop application.

## UI source tests and lint

Run `npm ci --prefix node/webui/frontend`, `npm test --prefix node/webui/frontend`, and `npm run lint --prefix node/webui/frontend` to validate the shared Vue source. To run the interface, build and launch DAgents Desktop from `desktop/tray-tauri`; this source package does not provide a browser entry point.

The API adapter is `frontend/src/api/node.js`. Node-native directory selection goes through `/v1/platform/directory-picker`. Clipboard file paths, window focus, and update apply use the Desktop-only `__DAGENTS_DESKTOP_REQUEST__` bridge; they are not Node HTTP endpoints.

The native directory picker runs on the Node host: Windows uses PowerShell STA `FolderBrowserDialog`, macOS uses `osascript`, and Linux tries `zenity`, `kdialog`, then `yad`. Cancellation returns `200` with `{"cancelled":true}`; when no picker is available the UI can still accept a manually entered absolute path.
