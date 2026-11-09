// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import SetupConfigPanel from "./SetupConfigPanel.vue";

const { getSetupConfig, patchSetupConfig, probeLLMModels, getLLMCapabilities } = vi.hoisted(() => ({
  getSetupConfig: vi.fn(),
  patchSetupConfig: vi.fn(),
  probeLLMModels: vi.fn(),
  getLLMCapabilities: vi.fn(),
}));
const { notifyConfigurationChanged } = vi.hoisted(() => ({ notifyConfigurationChanged: vi.fn() }));

vi.mock("../api/node.js", () => ({
  getSetupConfig,
  patchSetupConfig,
  probeLLMModels,
  getLLMCapabilities,
}));
vi.mock("../utils/configurationEvents.js", () => ({ notifyConfigurationChanged }));

let wrapper;

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  document.body.innerHTML = "";
  vi.resetAllMocks();
});

describe("SetupConfigPanel manage payload ownership", () => {
  it("does not resubmit hidden Manage fields from the GET response", async () => {
    const config = {
      config_writable: true,
      llm: { active: "profile-a", profiles: [{ id: "profile-a", provider: "mock", model: "mock", mock: true }] },
      manage: {
        enabled: false,
        url: "",
        team: "hidden-team",
        registration_base_url: "https://internal.example.test",
        registration_interval_seconds: 12,
        registration_ttl_seconds: 34,
        workgroup_enabled: false,
        has_node_token: true,
      },
    };
    getSetupConfig.mockResolvedValue(config);
    patchSetupConfig.mockResolvedValue(config);
    getLLMCapabilities.mockResolvedValue({ supported: false });
    wrapper = mount(SetupConfigPanel, { attachTo: document.body });
    await flushPromises();

    await wrapper.get(".btn--primary").trigger("click");
    await flushPromises();

    const manage = patchSetupConfig.mock.calls[0][0].manage;
    expect(manage).toEqual({ enabled: false, url: "", workgroup_enabled: false, clear_node_token: false });
    expect(manage).not.toHaveProperty("team");
    expect(manage).not.toHaveProperty("registration_base_url");
    expect(manage).not.toHaveProperty("registration_interval_seconds");
    expect(manage).not.toHaveProperty("registration_ttl_seconds");
  });
});

describe("LLM reasoning profile settings", () => {
  it("opens only the requested profile after setup finishes loading", async () => {
    getSetupConfig.mockResolvedValue({
      config_writable: true,
      llm: {
        active: "first",
        profiles: [
          { id: "first", provider: "qwen", model: "qwen-plus" },
          { id: "second", provider: "openai", model: "o3" },
        ],
      },
      manage: { enabled: false, has_node_token: true },
    });
    getLLMCapabilities.mockResolvedValue({ supported: true, thinking_control: "effort", allowed_efforts: ["low", "medium", "high"], default_effort: "medium" });

    wrapper = mount(SetupConfigPanel, {
      props: { requestedProfileId: "second" },
      attachTo: document.body,
    });
    await flushPromises();

    expect(wrapper.text()).toContain("管理模型配置、默认模型和管理平台连接");
    expect(wrapper.text()).toContain("连接到管理平台后可注册本机服务");
    expect(wrapper.text()).toContain("管理平台连接密钥");
    expect(wrapper.text()).toContain("清除已保存的管理平台连接密钥");

    const modal = document.body.querySelector(".llm-profile-modal");
    expect(modal?.textContent).toContain("模型配置");
    expect(modal?.querySelector("input[type='text']")?.value).toBe("second");
    expect(getLLMCapabilities).toHaveBeenCalledWith("openai", "o3");
    expect(wrapper.emitted("profile-request-handled")).toHaveLength(1);
  });
});

describe("SetupConfigPanel LLM profile editing", () => {
  it("keeps the profile ID through model probing and sends original_id when manually renamed", async () => {
    const initialView = {
      config_writable: true,
      config_path: "node_settings.db",
      llm: {
        active: "custom-profile",
        profiles: [{
          id: "custom-profile",
          provider: "openai",
          base_url: "https://api.example.test/v1",
          model: "old-model",
          has_api_key: true,
          mock: false,
          multimodal_enabled: false,
        }],
      },
      manage: { enabled: false },
    };
    getSetupConfig.mockResolvedValue(initialView);
    probeLLMModels.mockResolvedValue({ models: [{ id: "new-model" }] });
    patchSetupConfig.mockRejectedValueOnce(new Error("temporary save failure"));
    patchSetupConfig.mockImplementation(async (patch) => ({
      ...initialView,
      llm: {
        active: patch.llm.active,
        profiles: patch.llm.profiles.map((profile) => ({
          ...profile,
          has_api_key: true,
          api_key: undefined,
          clear_api_key: undefined,
          original_id: undefined,
        })),
      },
    }));

    wrapper = mount(SetupConfigPanel, { attachTo: document.body });
    await flushPromises();

    await wrapper.get(".llm-config-card__hit").trigger("click");
    await flushPromises();
    const modal = document.body.querySelector(".llm-profile-modal");
    expect(modal).toBeTruthy();

    const probeButton = [...modal.querySelectorAll("button")].find((button) => button.textContent.includes("测试并拉取模型"));
    probeButton.click();
    await flushPromises();

    const fields = [...modal.querySelectorAll("input")];
    const idInput = fields.find((input) => input.placeholder.includes("DeepSeek"));
    expect(idInput.value).toBe("custom-profile");
    expect(modal.textContent).toContain("new-model");
    idInput.value = "renamed-profile";
    idInput.dispatchEvent(new Event("input", { bubbles: true }));

    const confirmButton = [...modal.querySelectorAll("button")].find((button) => button.textContent.trim() === "确定");
    confirmButton.click();
    await flushPromises();
    expect(notifyConfigurationChanged).not.toHaveBeenCalled();

    expect(patchSetupConfig).toHaveBeenCalledTimes(1);
    const patch = patchSetupConfig.mock.calls[0][0];
    expect(patch.llm.active).toBe("renamed-profile");
    expect(patch.llm.profiles).toHaveLength(1);
    expect(patch.llm.profiles[0]).toMatchObject({
      id: "renamed-profile",
      original_id: "custom-profile",
      model: "new-model",
      clear_api_key: false,
    });
    expect(patch.llm.profiles[0].api_key).toBeUndefined();

    await wrapper.get(".llm-config-card__hit").trigger("click");
    await flushPromises();
    const retryModal = document.body.querySelector(".llm-profile-modal");
    const retryProbe = [...retryModal.querySelectorAll("button")].find((button) => button.textContent.includes("测试并拉取模型"));
    retryProbe.click();
    await flushPromises();
    expect(probeLLMModels.mock.calls[1][0].profile_id).toBe("custom-profile");
    const retryIdInput = [...retryModal.querySelectorAll("input")].find((input) => input.placeholder.includes("DeepSeek"));
    retryIdInput.value = "final-profile";
    retryIdInput.dispatchEvent(new Event("input", { bubbles: true }));
    [...retryModal.querySelectorAll("button")].find((button) => button.textContent.trim() === "确定").click();
    await flushPromises();
    expect(notifyConfigurationChanged).toHaveBeenCalledTimes(1);
    expect(notifyConfigurationChanged).toHaveBeenCalledWith("llm");

    expect(patchSetupConfig).toHaveBeenCalledTimes(2);
    const retriedPatch = patchSetupConfig.mock.calls[1][0];
    expect(retriedPatch.llm.active).toBe("final-profile");
    expect(retriedPatch.llm.profiles[0]).toMatchObject({
      id: "final-profile",
      original_id: "custom-profile",
      model: "new-model",
    });
  });

  it("shows an actionable server error when saving a profile change blocked by Agent references", async () => {
    getSetupConfig.mockResolvedValue({
      config_writable: true,
      config_path: "node_settings.db",
      llm: {
        active: "profile-bound",
        profiles: [
          { id: "profile-bound", provider: "openai", model: "model-a", has_api_key: true },
          { id: "profile-spare", provider: "qwen", model: "model-b", has_api_key: false },
        ],
      },
      manage: { enabled: false },
    });
    patchSetupConfig.mockRejectedValue(new Error(
      '无法删除或改名模型配置「profile-bound」：仍被 Agent「agent-one」使用。请先修改这些 Agent 的模型配置，或删除这些 Agent 后重试。',
    ));

    wrapper = mount(SetupConfigPanel, { attachTo: document.body });
    await flushPromises();
    await wrapper.get(".btn--primary").trigger("click");
    await flushPromises();

    expect(patchSetupConfig).toHaveBeenCalledTimes(1);
    expect(wrapper.get('[role="alert"]').text()).toContain("agent-one");
    expect(wrapper.get('[role="alert"]').text()).toContain("请先修改这些 Agent 的模型配置");
    expect(wrapper.text()).not.toContain("已保存。");
    expect(notifyConfigurationChanged).not.toHaveBeenCalled();
  });
});
