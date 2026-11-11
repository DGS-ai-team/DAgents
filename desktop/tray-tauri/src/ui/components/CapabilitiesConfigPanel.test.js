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
  it("keeps service switches adjacent to settings and preserves drafts across collapses", async () => {
    const wrapper = mount(CapabilitiesConfigPanel);
    await flushPromises();

    expect(wrapper.text()).toContain("共享服务与运行限制");
    expect(wrapper.text()).toContain("浏览器服务");
    const browserAdvanced = wrapper.get(".capabilities-config__service-disclosure");
    expect(browserAdvanced.get(".settings-disclosure__trigger").attributes("aria-expanded")).toBe("false");
    expect(browserAdvanced.text()).toContain("浏览器超时、并发数、路径及证书处理选项");

    await browserAdvanced.get(".settings-disclosure__trigger").trigger("click");
    const ignoreTls = browserAdvanced.findAll('input[type="checkbox"]')[1];
    await ignoreTls.setValue(true);
    expect(wrapper.get('[role="status"]').text()).toContain("已启用证书校验例外");
    await browserAdvanced.get(".settings-disclosure__trigger").trigger("click");
    expect(browserAdvanced.get(".settings-disclosure__trigger").attributes("aria-expanded")).toBe("false");
    expect(browserAdvanced.get(".settings-disclosure__content").attributes("aria-hidden")).toBe("true");
    expect(browserAdvanced.get(".settings-disclosure__content").attributes("inert")).toBeDefined();
    await browserAdvanced.get(".settings-disclosure__trigger").trigger("click");
    expect(browserAdvanced.findAll('input[type="checkbox"]')[1].element.checked).toBe(true);

    const runtime = wrapper.findAll(".capabilities-config__group")[0];
    await runtime.get(".settings-disclosure__trigger").trigger("click");
    expect(runtime.get(".settings-disclosure__trigger").attributes("aria-expanded")).toBe("true");
    const skillsLimit = runtime.get('[data-validation-field="features.skills_max_in_prompt"]');
    await skillsLimit.setValue("21");
    await runtime.get(".settings-disclosure__trigger").trigger("click");
    await runtime.get(".settings-disclosure__trigger").trigger("click");
    expect(runtime.get('[data-validation-field="features.skills_max_in_prompt"]').element.value).toBe("21");

    const rawHistory = wrapper.findAll(".capabilities-config__group")[1];
    expect(rawHistory.get(".settings-disclosure__trigger").text()).toBe("诊断与日志");
    expect(rawHistory.text()).toContain("原始消息记录用于排查问题");
    await rawHistory.get(".settings-disclosure__trigger").trigger("click");
    await rawHistory.get('input[type="checkbox"]').setValue(true);

    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      browser: expect.objectContaining({ default_timeout_ms: 10000, ignore_https_errors: true }),
      features: expect.objectContaining({ raw_message_history_enabled: true, skills_max_in_prompt: 21 }),
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
    const diagnostics = wrapper.findAll(".capabilities-config__group")[1];
    expect(diagnostics.get(".settings-disclosure__trigger").text()).toBe("诊断与日志");
    expect(diagnostics.text()).toContain("当前由环境变量 AGENT_RAW_MESSAGE_HISTORY_ENABLED 控制");
    await diagnostics.get(".settings-disclosure__trigger").trigger("click");
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
    const diagnostics = wrapper.findAll(".capabilities-config__group")[1];
    expect(diagnostics.get(".settings-disclosure__trigger").text()).toBe("诊断与日志");
    await diagnostics.get(".settings-disclosure__trigger").trigger("click");
    const checkbox = wrapper.get('input[type="checkbox"][disabled]');
    expect(checkbox.element.checked).toBe(false);
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig.mock.calls[0][0].features).not.toHaveProperty("raw_message_history_enabled");
    wrapper.unmount();
  });

  it("opens and focuses invalid runtime fields instead of sending rejected values", async () => {
    const wrapper = mount(CapabilitiesConfigPanel, { attachTo: document.body });
    await flushPromises();
    const runtime = wrapper.findAll(".capabilities-config__group")[0];
    expect(runtime.get(".settings-disclosure__trigger").attributes("aria-expanded")).toBe("false");
    const invalidField = runtime.get('[data-validation-field="features.skills_max_in_prompt"]');
    invalidField.element.scrollIntoView = vi.fn();
    await invalidField.setValue("0");

    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    expect(runtime.get(".settings-disclosure__trigger").attributes("aria-expanded")).toBe("true");
    const input = runtime.get('[data-validation-field="features.skills_max_in_prompt"]');
    expect(input.attributes("aria-invalid")).toBe("true");
    expect(input.element.ownerDocument.activeElement).toBe(input.element);
    expect(input.element.scrollIntoView).toHaveBeenCalledWith({ block: "center", behavior: "auto" });
    expect(runtime.text()).toContain("请输入大于 0 的整数。");
    expect(api.patchSetupConfig).not.toHaveBeenCalled();
    await input.setValue("5");
    expect(input.attributes("aria-invalid")).toBeUndefined();
    wrapper.unmount();
  });

  it("reveals invalid browser settings without changing a disabled service switch", async () => {
    const config = setupConfig();
    config.features.browser_enabled = false;
    config.browser.default_timeout_ms = 0;
    api.getSetupConfig.mockResolvedValue(config);
    const wrapper = mount(CapabilitiesConfigPanel, { attachTo: document.body });
    await flushPromises();
    const browserSwitch = wrapper.get('.capabilities-config__service input[type="checkbox"]');
    expect(browserSwitch.element.checked).toBe(false);
    expect(wrapper.find(".capabilities-config__service-disclosure").exists()).toBe(false);

    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    expect(browserSwitch.element.checked).toBe(false);
    const browserDisclosure = wrapper.get(".capabilities-config__service-disclosure");
    expect(browserDisclosure.get(".settings-disclosure__trigger").attributes("aria-expanded")).toBe("true");
    const timeout = browserDisclosure.get('[data-validation-field="browser.default_timeout_ms"]');
    expect(timeout.attributes("aria-invalid")).toBe("true");
    expect(timeout.element.ownerDocument.activeElement).toBe(timeout.element);
    expect(api.patchSetupConfig).not.toHaveBeenCalled();
    await timeout.setValue("1000");
    expect(browserSwitch.element.checked).toBe(false);
    api.patchSetupConfig.mockResolvedValue({ ...config, browser: { ...config.browser, default_timeout_ms: 1000 } });
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      features: expect.objectContaining({ browser_enabled: false }),
      browser: expect.objectContaining({ default_timeout_ms: 1000 }),
    }));
    expect(browserSwitch.element.checked).toBe(false);
    wrapper.unmount();
  });

  it("requires a usable Webhook key before enabling WeCom, then accepts a raw key", async () => {
    const wrapper = mount(CapabilitiesConfigPanel, { attachTo: document.body });
    await flushPromises();
    const serviceToggles = wrapper.findAll(".capabilities-config__service > .settings-row input[type=checkbox]");
    await serviceToggles[1].setValue(true);
    await flushPromises();

    const wecomService = wrapper.findAll(".capabilities-config__service")[1];
    const webhook = wecomService.get(".settings-disclosure__trigger");
    expect(wecomService.text()).toContain("配置用于消息推送的 Webhook 地址及 Key");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    const url = wecomService.get('[data-validation-field="wecom.webhook_url"]');
    expect(webhook.attributes("aria-expanded")).toBe("true");
    expect(url.attributes("aria-invalid")).toBe("true");
    expect(url.element.ownerDocument.activeElement).toBe(url.element);
    expect(wrapper.text()).toContain("请输入包含 key 参数的 Webhook URL，或直接填写 Key。");
    expect(api.patchSetupConfig).not.toHaveBeenCalled();

    await url.setValue("fixture-wecom-key");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      features: expect.objectContaining({ wecom_enabled: true }),
      wecom: expect.objectContaining({ webhook_url: "fixture-wecom-key", clear_webhook_key: false }),
    }));
    wrapper.unmount();
  });

  it("accepts a keyed Webhook URL or an existing key and validates API bases only when enabled", async () => {
    const config = setupConfig();
    config.features.wecom_enabled = true;
    config.wecom.webhook_url = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=fixture-url-key";
    config.wecom.api_base = "ftp://qyapi.weixin.qq.com";
    api.getSetupConfig.mockResolvedValue(config);
    const wrapper = mount(CapabilitiesConfigPanel, { attachTo: document.body });
    await flushPromises();

    const disclosure = wrapper.findAll(".capabilities-config__service-disclosure")[1];
    expect(disclosure.get(".settings-disclosure__trigger").text()).toBe("Webhook 配置");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    const apiBase = disclosure.get('[data-validation-field="wecom.api_base"]');
    expect(apiBase.attributes("aria-invalid")).toBe("true");
    expect(apiBase.element.ownerDocument.activeElement).toBe(apiBase.element);
    expect(api.patchSetupConfig).not.toHaveBeenCalled();

    await apiBase.setValue("https://qyapi.weixin.qq.com");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      features: expect.objectContaining({ wecom_enabled: true }),
      wecom: expect.objectContaining({ webhook_url: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=fixture-url-key" }),
    }));
    wrapper.unmount();

    const existing = setupConfig();
    existing.features.wecom_enabled = true;
    existing.wecom.has_webhook_key = true;
    api.getSetupConfig.mockResolvedValue(existing);
    api.patchSetupConfig.mockClear();
    const existingWrapper = mount(CapabilitiesConfigPanel);
    await flushPromises();
    expect(existingWrapper.findAll(".capabilities-config__service-disclosure")[1].get(".settings-disclosure__trigger").text()).toBe("Webhook 配置");
    await existingWrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      wecom: expect.objectContaining({ clear_webhook_key: false }),
    }));
    expect(api.patchSetupConfig.mock.calls[0][0].wecom).not.toHaveProperty("webhook_key");
    existingWrapper.unmount();

    const newKeyConfig = setupConfig();
    newKeyConfig.features.wecom_enabled = true;
    api.getSetupConfig.mockResolvedValue(newKeyConfig);
    api.patchSetupConfig.mockClear();
    const newKeyWrapper = mount(CapabilitiesConfigPanel, { attachTo: document.body });
    await flushPromises();
    const newKeyInput = newKeyWrapper.get('[data-validation-field="wecom.webhook_key"]');
    await newKeyInput.setValue("fixture-new-key");
    await newKeyWrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      wecom: expect.objectContaining({ webhook_key: "fixture-new-key" }),
    }));
    newKeyWrapper.unmount();
  });

  it("keeps the clear-key control visible after disabling WeCom and blocks clearing its only key while enabled", async () => {
    const config = setupConfig();
    config.features.wecom_enabled = true;
    config.wecom.has_webhook_key = true;
    api.getSetupConfig.mockResolvedValue(config);
    const wrapper = mount(CapabilitiesConfigPanel, { attachTo: document.body });
    await flushPromises();
    const wecomService = wrapper.findAll(".capabilities-config__service")[1];
    const disclosure = wrapper.findAll(".capabilities-config__service-disclosure")[1];
    await disclosure.get(".settings-disclosure__trigger").trigger("click");
    await disclosure.get('input[type="checkbox"]').setValue(true);
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    const key = disclosure.get('[data-validation-field="wecom.webhook_key"]');
    expect(key.attributes("aria-invalid")).toBe("true");
    expect(wrapper.text()).toContain("如需清除，请先关闭企业微信。");
    expect(api.patchSetupConfig).not.toHaveBeenCalled();

    const serviceToggle = wecomService.get(':scope > .settings-row input[type="checkbox"]');
    await serviceToggle.setValue(false);
    await flushPromises();
    expect(wecomService.find(".capabilities-config__service-disclosure").exists()).toBe(true);
    expect(wecomService.find('[data-validation-field="wecom.webhook_key"]').exists()).toBe(true);

    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      features: expect.objectContaining({ wecom_enabled: false }),
      wecom: expect.objectContaining({ clear_webhook_key: true }),
    }));
    wrapper.unmount();
  });

  it("allows clearing a saved key while disabled and replacing it in the same enabled save", async () => {
    const disabledConfig = setupConfig();
    disabledConfig.wecom.has_webhook_key = true;
    api.getSetupConfig.mockResolvedValue(disabledConfig);
    const disabledWrapper = mount(CapabilitiesConfigPanel);
    await flushPromises();
    const disabledWecom = disabledWrapper.findAll(".capabilities-config__service")[1];
    const disabledDisclosure = disabledWecom.get(".capabilities-config__service-disclosure");
    expect(disabledDisclosure.get(".settings-disclosure__trigger").text()).toBe("Webhook 配置");
    await disabledDisclosure.get(".settings-disclosure__trigger").trigger("click");
    await disabledDisclosure.get('input[type="checkbox"]').setValue(true);
    await disabledWrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      features: expect.objectContaining({ wecom_enabled: false }),
      wecom: expect.objectContaining({ clear_webhook_key: true }),
    }));
    disabledWrapper.unmount();

    const enabledConfig = setupConfig();
    enabledConfig.features.wecom_enabled = true;
    enabledConfig.wecom.has_webhook_key = true;
    api.getSetupConfig.mockResolvedValue(enabledConfig);
    api.patchSetupConfig.mockClear();
    const enabledWrapper = mount(CapabilitiesConfigPanel);
    await flushPromises();
    const enabledDisclosure = enabledWrapper.findAll(".capabilities-config__service-disclosure")[1];
    await enabledDisclosure.get(".settings-disclosure__trigger").trigger("click");
    await enabledDisclosure.get('input[type="checkbox"]').setValue(true);
    await enabledDisclosure.get('[data-validation-field="wecom.webhook_key"]').setValue("fixture-replacement-key");
    await enabledWrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      features: expect.objectContaining({ wecom_enabled: true }),
      wecom: expect.objectContaining({ clear_webhook_key: true, webhook_key: "fixture-replacement-key" }),
    }));
    enabledWrapper.unmount();
  });
});
