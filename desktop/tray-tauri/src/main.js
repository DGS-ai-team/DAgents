import { createApp } from "vue";
import { invoke } from "@tauri-apps/api/core";
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
    if (status != null) {
      error.status = status;
      error.response = { status };
    }
    error.data = payload?.data ?? payload?.error?.data;
    throw error;
  }
};

createApp(App).use(router).mount("#app");
