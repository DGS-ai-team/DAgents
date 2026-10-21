import { createApp } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { confirm as confirmNative, message as showNativeMessage, open as openNativeDialog } from "@tauri-apps/plugin-dialog";
import App from "./App.vue";
import router from "./settings-router.js";
import "../../../node/webui/frontend/src/styles/tokens.css";
import "../../../node/webui/frontend/src/styles/layout.css";
import "../../../node/webui/frontend/src/styles/workbench.css";
import "../../../node/webui/frontend/src/styles/overrides.css";

// The Desktop workbench uses the light palette; the reused Node settings
// components read their semantic colors from this shared theme attribute.
document.documentElement.dataset.theme = "light";

globalThis.__DAGENTS_API_REQUEST__ = async ({ method, path, body }) => {
  try {
    return await invoke("desktop_node_request", { method, path, body: body ?? null });
  } catch (cause) {
    const payload = typeof cause === "string" ? (() => { try { return JSON.parse(cause); } catch { return null; } })() : cause;
    const error = new Error(payload?.message || payload?.error?.message || String(cause));
    const status = payload?.status ?? payload?.error?.status;
    const code = payload?.code ?? payload?.error?.code;
    const details = payload?.details ?? payload?.error?.details;
    if (status != null) {
      error.status = status;
      error.response = { status };
    }
    if (code != null) error.code = code;
    if (details != null) error.details = details;
    error.data = payload?.data ?? payload?.error?.data ?? (code || details ? { error: { code, message: error.message, details } } : undefined);
    throw error;
  }
};

globalThis.__DAGENTS_DESKTOP_REQUEST__ = async ({ method, path, body }) => {
  if (method === "GET" && path === "/v1/platform/capabilities") {
    return invoke("desktop_shell_request", { method, path, body: body ?? null });
  }
  if (method === "GET" && path === "/v1/platform/clipboard/files") {
    return invoke("desktop_shell_request", { method, path, body: body ?? null });
  }
  if (method === "POST" && (path === "/v1/platform/ui-focus" || path === "/v1/platform/update/apply")) {
    return invoke("desktop_shell_request", { method, path, body: body ?? null });
  }
  throw new Error(`Unsupported Desktop shell request: ${method} ${path}`);
};

globalThis.__DAGENTS_PICK_DIRECTORY__ = async () => {
  const path = await openNativeDialog({ directory: true, multiple: false, title: "选择工作目录" });
  return path
    ? { ok: true, cancelled: false, path }
    : { ok: true, cancelled: true };
};

globalThis.__DAGENTS_NATIVE_DIALOGS__ = {
  confirm: (message, options = {}) => confirmNative(message, {
    title: options.title || "DAgents",
    kind: options.kind || "warning",
    okLabel: options.okLabel || "确定",
    cancelLabel: options.cancelLabel || "取消",
  }),
  message: (message, options = {}) => showNativeMessage(message, {
    title: options.title || "DAgents",
    kind: options.kind || "info",
    buttons: { ok: options.okLabel || "确定" },
  }),
};

createApp(App).use(router).mount("#app");
