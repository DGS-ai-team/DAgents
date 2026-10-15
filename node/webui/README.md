# Node Web UI source

This directory contains the reusable Vue 3 + Vite frontend source. Agent Node is an API/runtime process and does not serve `/ui/` or embed frontend assets. Desktop WebView reuses these sources through its own build pipeline.

## Development

```bash
cd node/webui/frontend
npm run dev
```

Vite proxies `/v1` and `/health` to `http://127.0.0.1:18765` by default. Set `DAGENTS_NODE_URL` to use another local Node endpoint. The dev server opens at `http://localhost:5173/ui/`; that Vite path does not mean Node serves `/ui/`.

To produce a standalone frontend build for inspection, run `bash node/webui/build.sh`; output goes to `node/webui/frontend/dist/`. Node Go builds and tests do not require this output. Frontend tests use `npm test --prefix node/webui/frontend`.

The API adapter is `frontend/src/api/node.js`. Node-native directory selection goes through `/v1/platform/directory-picker`. Clipboard file paths, window focus, and update apply use the Desktop-only `__DAGENTS_DESKTOP_REQUEST__` bridge when running inside Desktop; they are not Node HTTP endpoints. Browser-only UI reports Shell features unavailable.

The native directory picker runs on the Node host: Windows uses PowerShell STA `FolderBrowserDialog`, macOS uses `osascript`, and Linux tries `zenity`, `kdialog`, then `yad`. Cancellation returns `200` with `{"cancelled":true}`; when no picker is available the UI can still accept a manually entered absolute path.
