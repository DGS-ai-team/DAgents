# Desktop MVP UI

This Vue surface is bundled by `desktop/tray-tauri` and talks to the Node only through typed Tauri commands. The standalone Vite preview is layout-only and reports the host as unavailable; it does not use mock Node data.

`assets/icons/` is a small self-contained copy of accepted Desktop assets from `docs/design/desktop-product/icons/`, so the app build does not depend on the design docs. Agent/Auto and the three group symbols retain the user-provided filled geometry. `terminal.svg` is the existing DAgents visual-system asset; `message.svg` is user-provided; `send.svg` uses the inline Node Web UI `UiIcon.vue` geometry; `play.svg` and the two chevrons are Lucide under ISC; and the two panel controls are Desktop originals. The source icon manifest records each license and geometry.

The MVP uses real Node status, agent metadata, hydrate transcript, message enqueueing, SSE updates, execute-tool approval resume, and a local terminal WebSocket relayed through the Tauri Rust process. Browser and file resources and Manage-backed workgroups remain unavailable. Terminal sessions are Agent-scoped; switching to another Agent detaches the view while preserving the Node PTY. The configured Node endpoint must resolve to loopback; users can use their own secure SSH port forward to a loopback port.

See the [MVP acceptance record](../../docs/design/desktop-product/mvp-closeout.md) for scope, live-model verification, and remaining work. Run `npm run test:ui --prefix desktop/tray-tauri` for the draft, approval, route, and terminal-output regressions.
