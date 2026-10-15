export const defaultDesktopSettings = () => ({
  connection_mode: "local",
  node_endpoint: "http://127.0.0.1:18765",
  notification_enabled: true,
  update_check_enabled: true,
  update_check_interval_seconds: 21600,
});

export function createDesktopSettingsState() {
  return {
    settings: defaultDesktopSettings(),
    runtimeEndpoint: "",
    runtimeConnectionMode: "local",
    intervalMinutes: 360,
    persisted: "",
    loaded: false,
    loading: false,
    saving: false,
    error: "",
    saved: false,
  };
}

export async function loadDesktopSettings(state, invoke) {
  state.loading = true;
  state.loaded = false;
  state.error = "";
  try {
    const stored = await invoke("desktop_settings_get");
    if (!stored?.settings) throw new Error("Desktop 设置响应不完整");
    Object.assign(state.settings, defaultDesktopSettings(), stored.settings);
    state.runtimeEndpoint = stored.runtime_node_endpoint || "";
    state.runtimeConnectionMode = stored.runtime_connection_mode || "local";
    state.intervalMinutes = Math.round(state.settings.update_check_interval_seconds / 60);
    state.persisted = JSON.stringify({ ...state.settings, intervalMinutes: state.intervalMinutes });
    state.loaded = true;
    state.saved = false;
    return true;
  } catch (cause) {
    state.error = String(cause?.message || cause || "无法读取 Desktop 设置");
    state.loaded = false;
    return false;
  } finally {
    state.loading = false;
  }
}

export async function saveDesktopSettings(state, invoke) {
  if (!state.loaded || state.loading || state.saving) return false;
  state.error = "";
  state.saved = false;
  state.saving = true;
  try {
    state.settings.update_check_interval_seconds = Number(state.intervalMinutes) * 60;
    await invoke("desktop_settings_save", { settings: { ...state.settings } });
    state.persisted = JSON.stringify({ ...state.settings, intervalMinutes: Number(state.intervalMinutes) });
    state.saved = true;
    return true;
  } catch (cause) {
    state.error = String(cause?.message || cause || "保存 Desktop 设置失败");
    return false;
  } finally {
    state.saving = false;
  }
}

export function updateIntervalMinutes(state, minutes) {
  state.intervalMinutes = minutes;
}

export function desktopSettingsDirty(state) {
  return JSON.stringify({ ...state.settings, intervalMinutes: Number(state.intervalMinutes) }) !== state.persisted;
}
