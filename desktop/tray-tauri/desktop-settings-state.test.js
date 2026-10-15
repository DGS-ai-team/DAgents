import test from "node:test";
import assert from "node:assert/strict";
import { createDesktopSettingsState, desktopSettingsDirty, loadDesktopSettings, saveDesktopSettings } from "./src/desktop-settings-state.js";

test("a failed settings read keeps controls unsavable", async () => {
  const state = createDesktopSettingsState();
  let writes = 0;
  await loadDesktopSettings(state, async () => { throw new Error("read failed"); });
  state.settings.notification_enabled = false;
  assert.equal(await saveDesktopSettings(state, async () => { writes += 1; }), false);
  assert.equal(state.loaded, false);
  assert.equal(writes, 0);
});

test("tunnel mode is stored and restored with Desktop-local preferences", async () => {
  const state = createDesktopSettingsState();
  const writes = [];
  const invoke = async (command, args) => {
    if (command === "desktop_settings_get") {
      return {
        settings: { ...createDesktopSettingsState().settings, connection_mode: "tunnel", node_endpoint: "http://127.0.0.1:18766" },
        runtime_node_endpoint: "http://127.0.0.1:18766",
        runtime_connection_mode: "tunnel",
      };
    }
    if (command === "desktop_settings_save") {
      writes.push(args.settings);
      return undefined;
    }
    throw new Error(`unexpected command ${command}`);
  };
  await loadDesktopSettings(state, invoke);
  assert.equal(state.settings.connection_mode, "tunnel");
  assert.equal(state.runtimeConnectionMode, "tunnel");
  state.settings.connection_mode = "local";
  assert.equal(await saveDesktopSettings(state, invoke), true);
  assert.equal(writes[0].connection_mode, "local");

  const reopened = createDesktopSettingsState();
  const persistedInvoke = async (command) => command === "desktop_settings_get"
    ? { settings: writes[0], runtime_node_endpoint: "http://127.0.0.1:18766", runtime_connection_mode: "tunnel" }
    : undefined;
  await loadDesktopSettings(reopened, persistedInvoke);
  assert.equal(reopened.settings.connection_mode, "local");
  assert.equal(reopened.runtimeConnectionMode, "tunnel");
});

test("a failed read can be retried without writing defaults over saved settings", async () => {
  const state = createDesktopSettingsState();
  const persisted = {
    node_endpoint: "http://127.0.0.1:19000",
    notification_enabled: false,
    update_check_enabled: true,
    update_check_interval_seconds: 3600,
  };
  let reads = 0;
  let writes = 0;
  const invoke = async (command) => {
    if (command === "desktop_settings_get") {
      reads += 1;
      if (reads === 1) throw new Error("temporary read failure");
      return { settings: persisted, runtime_node_endpoint: "http://127.0.0.1:18765" };
    }
    if (command === "desktop_settings_save") writes += 1;
    throw new Error(`unexpected command ${command}`);
  };

  await loadDesktopSettings(state, invoke);
  assert.equal(state.loaded, false);
  assert.equal(await saveDesktopSettings(state, invoke), false);
  assert.equal(writes, 0);

  await loadDesktopSettings(state, invoke);
  assert.equal(state.loaded, true);
  assert.equal(state.settings.node_endpoint, persisted.node_endpoint);
  assert.equal(state.settings.notification_enabled, false);
  assert.equal(desktopSettingsDirty(state), false);
  assert.equal(writes, 0);
});

test("a failed re-read revokes the loaded flag and disables later saves", async () => {
  const state = createDesktopSettingsState();
  let reads = 0;
  let writes = 0;
  const invoke = async (command) => {
    if (command === "desktop_settings_get") {
      reads += 1;
      if (reads === 1) return { settings: { ...createDesktopSettingsState().settings }, runtime_node_endpoint: "http://127.0.0.1:18765" };
      throw new Error("read failed");
    }
    if (command === "desktop_settings_save") writes += 1;
    throw new Error(`unexpected command ${command}`);
  };

  await loadDesktopSettings(state, invoke);
  await loadDesktopSettings(state, invoke);
  assert.equal(state.loaded, false);
  assert.equal(await saveDesktopSettings(state, invoke), false);
  assert.equal(writes, 0);
});

test("a save failure preserves edits and a later view reloads persisted settings", async () => {
  const persisted = {
    node_endpoint: "http://127.0.0.1:18765",
    notification_enabled: true,
    update_check_enabled: true,
    update_check_interval_seconds: 21600,
  };
  const invoke = async (command, args) => {
    if (command === "desktop_settings_get") return { settings: persisted, runtime_node_endpoint: "http://127.0.0.1:18765" };
    if (command === "desktop_settings_save") throw new Error("disk full");
    throw new Error(`unexpected command ${command}`);
  };
  const state = createDesktopSettingsState();
  await loadDesktopSettings(state, invoke);
  state.settings.node_endpoint = "http://localhost:19000";
  assert.equal(await saveDesktopSettings(state, invoke), false);
  assert.equal(state.settings.node_endpoint, "http://localhost:19000");
  assert.match(state.error, /disk full/);
  assert.equal(desktopSettingsDirty(state), true);

  const reopened = createDesktopSettingsState();
  await loadDesktopSettings(reopened, invoke);
  assert.equal(reopened.settings.node_endpoint, "http://127.0.0.1:18765");
  assert.equal(desktopSettingsDirty(reopened), false);
});
