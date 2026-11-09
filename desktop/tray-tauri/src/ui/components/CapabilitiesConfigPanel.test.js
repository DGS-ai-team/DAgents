/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import CapabilitiesConfigPanel from "./CapabilitiesConfigPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({ getSetupConfig: vi.fn(), patchSetupConfig: vi.fn() }));

const setupConfig = () => ({
  config_path: "node_settings.db",
  config_writable: true,
  features: {
    browser_enabled: true,
    wecom_enabled: false,
    raw_message_history_enabled: false,
    raw_message_history_source: "settings",
    skills_enabled: true,
    triggers_enabled: true,
    child_agents_enabled: true,
    skills_max_in_prompt: 20,
    triggers_poll_seconds: 30,
  },
  browser: {
    default_timeout_ms: 10000,
    max_sessions: 3,
    output_dir: "browser",
    chrome_path: "",
    headed: false,
    ignore_https_errors: false,
  },
  wecom: { webhook_url: "", api_base: "", has_webhook_key: false },
  child_agents: {
    default_ttl_seconds: 900,
    max_ttl_seconds: 3600,
    max_active_per_parent: 3,
    default_max_turns: 10,
    max_max_turns: 20,
  },
});

beforeEach(() => {
  vi.clearAllMocks();
  api.getSetupConfig.mockResolvedValue(setupConfig());
  api.patchSetupConfig.mockResolvedValue(setupConfig());
});

describe("CapabilitiesConfigPanel advanced settings", () => {
  it("keeps browser and integration availability visible while retaining advanced values", async () => {
    const wrapper = mount(CapabilitiesConfigPanel);
    await flushPromises();

    expect(wrapper.text()).toContain("共享服务与运行限制");
    expect(wrapper.text()).toContain("浏览器服务");
    const browserAdvanced = wrapper.get(".browser-settings__advanced");
    expect(browserAdvanced.element.open).toBe(false);
    expect(browserAdvanced.text()).toContain("证书校验例外：关闭");

    await browserAdvanced.get("summary").trigger("click");
    const ignoreTls = browserAdvanced.findAll('input[type="checkbox"]')[1];
    await ignoreTls.setValue(true);
    expect(wrapper.get('[role="status"]').text()).toContain("已启用证书校验例外");

    const rawHistory = wrapper.get('details:not(.browser-settings__advanced):not(.wecom-settings__advanced)');
    expect(rawHistory.element.open).toBe(false);
    await rawHistory.get("summary").trigger("click");
    await rawHistory.get('input[type="checkbox"]').setValue(true);

    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      browser: expect.objectContaining({ default_timeout_ms: 10000, ignore_https_errors: true }),
      features: expect.objectContaining({ raw_message_history_enabled: true }),
      child_agents: {
        default_ttl_seconds: 900,
        max_ttl_seconds: 3600,
        max_active_per_parent: 3,
      },
    }));
    expect(api.patchSetupConfig.mock.calls[0][0].features).not.toHaveProperty("skills_enabled");
    expect(api.patchSetupConfig.mock.calls[0][0].features).not.toHaveProperty("triggers_enabled");
    expect(api.patchSetupConfig.mock.calls[0][0].features).not.toHaveProperty("child_agents_enabled");
    expect(api.patchSetupConfig.mock.calls[0][0].child_agents).not.toHaveProperty("default_max_turns");
    expect(api.patchSetupConfig.mock.calls[0][0].child_agents).not.toHaveProperty("max_max_turns");
    wrapper.unmount();
  });

  it("shows effective raw history as read-only when environment controls it", async () => {
    api.getSetupConfig.mockResolvedValue({
      ...setupConfig(),
      features: { ...setupConfig().features, raw_message_history_enabled: true, raw_message_history_source: "environment" },
    });
    const wrapper = mount(CapabilitiesConfigPanel);
    await flushPromises();
    expect(wrapper.text()).toContain("当前由环境变量 AGENT_RAW_MESSAGE_HISTORY_ENABLED 控制");
    const checkbox = wrapper.get('input[type="checkbox"][disabled]');
    expect(checkbox.element.checked).toBe(true);
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig.mock.calls[0][0].features).not.toHaveProperty("raw_message_history_enabled");
    wrapper.unmount();
  });

  it("does not submit environment-controlled raw history when its effective value is false", async () => {
    api.getSetupConfig.mockResolvedValue({
      ...setupConfig(),
      features: { ...setupConfig().features, raw_message_history_enabled: false, raw_message_history_source: "environment" },
    });
    const wrapper = mount(CapabilitiesConfigPanel);
    await flushPromises();
    expect(wrapper.text()).toContain("当前由环境变量 AGENT_RAW_MESSAGE_HISTORY_ENABLED 控制");
    const checkbox = wrapper.get('input[type="checkbox"][disabled]');
    expect(checkbox.element.checked).toBe(false);
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig.mock.calls[0][0].features).not.toHaveProperty("raw_message_history_enabled");
    wrapper.unmount();
  });
});
