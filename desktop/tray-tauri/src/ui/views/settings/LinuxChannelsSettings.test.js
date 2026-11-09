/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import LinuxChannelsSettings from "./LinuxChannelsSettings.vue";
import * as api from "../../api/node.js";

const { route, routerReplace, confirmDesktopAction } = vi.hoisted(() => ({
  route: { query: {} },
  routerReplace: vi.fn(),
  confirmDesktopAction: vi.fn(),
}));
vi.mock("vue-router", () => ({ useRoute: () => route, useRouter: () => ({ replace: routerReplace }) }));
vi.mock("../../api/node.js", () => ({
  listLinuxChannels: vi.fn(),
  listLinuxCredentials: vi.fn(),
  createLinuxChannel: vi.fn(),
  createLinuxCredential: vi.fn(),
  testLinuxChannel: vi.fn(),
  patchLinuxChannel: vi.fn(),
}));
vi.mock("../../utils/desktopDialogs.js", () => ({
  confirmDesktopAction,
}));

const savedCredential = { credential_id: "credential-a", display_name: "Fixture key", auth_type: "private_key", secret_source: "direct", has_secret: true };
const savedChannel = { channel_id: "channel-a", display_name: "Test server", host: "127.0.0.1", port: 22, username: "fixture", credential_id: "credential-a", host_key_policy: "known_hosts", max_sessions: 1 };

beforeEach(() => {
  vi.clearAllMocks();
  route.query = {};
  api.listLinuxChannels.mockResolvedValue({ channels: [] });
  api.listLinuxCredentials.mockResolvedValue({ credentials: [savedCredential] });
  api.createLinuxChannel.mockResolvedValue({ channel_id: "channel-a" });
  api.createLinuxCredential.mockResolvedValue({ credential_id: "credential-b", secret_source: "direct" });
  api.testLinuxChannel.mockReset();
  api.patchLinuxChannel.mockReset();
  confirmDesktopAction.mockReset();
  confirmDesktopAction.mockResolvedValue(true);
});

const mountSettings = ({ stubSelect = true } = {}) => {
  const stubs = { SettingsPageHeader: true, UiIcon: true };
  if (stubSelect) {
    stubs.UiSelect = {
      props: ["modelValue", "options"],
      emits: ["update:modelValue", "change"],
      template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value); $emit(\'change\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
    };
  }
  return mount(LinuxChannelsSettings, { global: { stubs } });
};

async function chooseRealSelectOption(wrapper, label) {
  await wrapper.get(`[aria-label="认证方式"]`).trigger("click");
  await flushPromises();
  const option = [...document.body.querySelectorAll('[role="option"]')].find(item => item.textContent.trim() === label);
  expect(option, `real UiSelect option ${label}`).toBeTruthy();
  option.click();
  await flushPromises();
}

describe("Linux channel return flow", () => {
  it("creates a global SSH channel and returns to the originating agent resource section", async () => {
    route.query = { create: "1", returnAgentId: "agent-a", returnSection: "resources" };
    const wrapper = mountSettings();
    await flushPromises();

    expect(wrapper.get("#channel-form-title").text()).toBe("新增 SSH 通道");
    await wrapper.get('input[placeholder="例如：生产服务器"]').setValue("Test server");
    await wrapper.get('input[placeholder="例如：10.0.0.21"]').setValue("10.0.0.21");
    await wrapper.get('input[placeholder="例如：deploy"]').setValue("deploy");
    await wrapper.get(".linux-settings__channel-editor select").setValue("credential-a");
    await wrapper.get(".linux-settings__modal-actions .btn--primary").trigger("click");
    await flushPromises();

    expect(api.createLinuxChannel).toHaveBeenCalledWith(expect.objectContaining({
      host: "10.0.0.21",
      username: "deploy",
      credential_id: "credential-a",
      host_key_policy: "known_hosts",
    }));
    expect(routerReplace).toHaveBeenCalledWith({
      name: "settings-agent-detail",
      params: { agentId: "agent-a" },
      query: { section: "resources" },
    });
    wrapper.unmount();
  });

  it("preserves multiline private-key text in the direct credential payload", async () => {
    const wrapper = mountSettings();
    await flushPromises();
    await wrapper.get('button').trigger("click");
    await flushPromises();
    await wrapper.get('select[aria-label="认证方式"]').setValue("private_key");

    const privateKeyFixture = "-----BEGIN OPENSSH PRIVATE KEY-----\nfixture-line-one\nfixture-line-two\n-----END OPENSSH PRIVATE KEY-----\n";
    await wrapper.get("textarea").setValue(privateKeyFixture);
    await wrapper.get('.linux-settings__modal-actions .btn--primary').trigger("click");
    await flushPromises();

    expect(api.createLinuxCredential).toHaveBeenCalledWith(expect.objectContaining({
      auth_type: "private_key",
      secret_value: privateKeyFixture,
    }));
    expect(api.createLinuxCredential.mock.calls[0][0]).not.toHaveProperty("secret_ref");
    wrapper.unmount();
  });

  it("uses the selected password type through the real UiSelect and keeps direct mode on type changes", async () => {
    const wrapper = mountSettings({ stubSelect: false });
    await flushPromises();
    await wrapper.get('.settings-section--standalone .settings-section__actions button').trigger("click");
    await flushPromises();
    expect(wrapper.get('button[aria-label="认证方式"]').text()).toContain("密码");
    expect(wrapper.get('button[aria-label="凭据来源"]').text()).toContain("直接输入");
    await wrapper.get('input[type="password"]').setValue("clear-this-on-type-change");

    await chooseRealSelectOption(wrapper, "SSH 私钥");
    expect(wrapper.get('button[aria-label="认证方式"]').text()).toContain("SSH 私钥");
    expect(wrapper.get('button[aria-label="凭据来源"]').text()).toContain("直接输入");
    expect(wrapper.find("textarea[placeholder='粘贴 SSH 私钥内容']").exists()).toBe(true);
    expect(wrapper.get("textarea").element.value).toBe("");

    await chooseRealSelectOption(wrapper, "密码");
    expect(wrapper.get('button[aria-label="认证方式"]').text()).toContain("密码");
    expect(wrapper.get('button[aria-label="凭据来源"]').text()).toContain("直接输入");
    const password = wrapper.get('input[type="password"]');
    expect(password.element.value).toBe("");
    await password.setValue("fixture-only-password");
    await wrapper.get('.linux-settings__modal-actions .btn--primary').trigger("click");
    await flushPromises();

    expect(api.createLinuxCredential).toHaveBeenCalledWith(expect.objectContaining({
      auth_type: "password",
      secret_value: "fixture-only-password",
    }));
    expect(api.createLinuxCredential.mock.calls[0][0]).not.toHaveProperty("secret_ref");
    wrapper.unmount();
  });

});

describe("Linux channel diagnostics", () => {
  it("labels a direct private-key credential as direct input, not as a password", async () => {
    const wrapper = mountSettings();
    await flushPromises();

    const credentialRow = wrapper.findAll(".linux-settings__row").find(row => row.text().includes("Fixture key"));
    expect(credentialRow.text()).toContain("SSH 私钥 · 已配置直接输入");
    expect(credentialRow.text()).not.toContain("已配置直接密码");
    wrapper.unmount();
  });

  it("shows private-key authentication failures as actionable errors and clears stale success", async () => {
    api.listLinuxChannels.mockResolvedValue({ channels: [savedChannel] });
    api.testLinuxChannel
      .mockResolvedValueOnce({ available: true, message: "connected", stages: [] })
      .mockResolvedValueOnce({
        available: false,
        error_code: "private_key_invalid",
        message: "safe backend message",
        stages: [
          { name: "config", status: "passed", message: "ok" },
          { name: "credential", status: "passed", message: "ok" },
          { name: "authentication", status: "failed", message: "private key invalid" },
        ],
      });
    const wrapper = mountSettings();
    await flushPromises();
    await wrapper.get('[aria-label="测试 SSH 通道 Test server"]').trigger("click");
    await flushPromises();
    expect(wrapper.get(".linux-settings__ok").text()).toContain("连接测试通过");

    await wrapper.get('[aria-label="测试 SSH 通道 Test server"]').trigger("click");
    await flushPromises();

    expect(wrapper.find(".linux-settings__ok").exists()).toBe(false);
    expect(wrapper.get(".linux-settings__error").text()).toContain("SSH 私钥内容无效");
    expect(wrapper.get(".linux-settings__error").text()).toContain("如果您使用的是密码，请新建密码凭据并编辑通道切换到该凭据");
    expect(wrapper.get(".linux-settings__stage-name").text()).toBe("配置");
    expect(wrapper.findAll(".linux-settings__stage-name").map(node => node.text())).toEqual(["配置", "凭据", "凭据解析"]);
    expect(wrapper.findAll(".linux-settings__stage-mark")[0].attributes("title")).toBe("本步骤已通过");
    wrapper.unmount();
  });

  it("uses the success state only when the connection test is available", async () => {
    api.listLinuxChannels.mockResolvedValue({ channels: [savedChannel] });
    api.testLinuxChannel.mockResolvedValue({ available: true, message: "connected", stages: [] });
    const wrapper = mountSettings();
    await flushPromises();
    await wrapper.get('[aria-label="测试 SSH 通道 Test server"]').trigger("click");
    await flushPromises();

    expect(wrapper.get(".linux-settings__ok").text()).toContain("连接测试通过");
    expect(wrapper.find(".linux-settings__error").exists()).toBe(false);
    wrapper.unmount();
  });

  it("does not show a failed pinned-fingerprint re-test in the success state", async () => {
    api.listLinuxChannels.mockResolvedValue({ channels: [savedChannel] });
    api.testLinuxChannel
      .mockResolvedValueOnce({ available: false, error_code: "host_key_unknown", host_key_fingerprint: "SHA256:fixture", stages: [] })
      .mockResolvedValueOnce({ available: false, error_code: "private_key_invalid", message: "safe backend message", stages: [] });
    api.patchLinuxChannel.mockResolvedValue({});
    const wrapper = mountSettings();
    await flushPromises();
    await wrapper.get('[aria-label="测试 SSH 通道 Test server"]').trigger("click");
    await flushPromises();
    await wrapper.get(".linux-settings__diagnostics-head .btn--primary").trigger("click");
    await flushPromises();

    expect(api.patchLinuxChannel).toHaveBeenCalledWith("channel-a", expect.objectContaining({ host_key_policy: "pinned", host_key_ref: "SHA256:fixture" }));
    expect(wrapper.find(".linux-settings__ok").exists()).toBe(false);
    expect(wrapper.get(".linux-settings__error").text()).toContain("SSH 私钥内容无效");
    wrapper.unmount();
  });

  it("explains unknown host identity and does not pin it when confirmation is cancelled", async () => {
    api.listLinuxChannels.mockResolvedValue({ channels: [savedChannel] });
    api.testLinuxChannel.mockResolvedValue({
      available: false,
      error_code: "host_key_unknown",
      host_key_fingerprint: "SHA256:fixture",
      stages: [
        { name: "host_key", status: "passed", message: "host-key policy ready" },
        { name: "ssh_handshake", status: "failed", message: "unknown host key" },
      ],
    });
    confirmDesktopAction.mockResolvedValue(false);
    const wrapper = mountSettings();
    await flushPromises();
    await wrapper.get('[aria-label="测试 SSH 通道 Test server"]').trigger("click");
    await flushPromises();

    expect(wrapper.get(".linux-settings__error").text()).toContain("尚未确认服务器身份，请核对下方指纹，确认无误后保存并重试");
    expect(wrapper.findAll(".linux-settings__stage-name").map(node => node.text())).toEqual(["主机校验配置", "SSH 握手"]);
    await wrapper.get(".linux-settings__diagnostics-head .btn--primary").trigger("click");
    await flushPromises();

    expect(confirmDesktopAction).toHaveBeenCalledWith(expect.stringContaining("SHA256:fixture"));
    expect(api.patchLinuxChannel).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
