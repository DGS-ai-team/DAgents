// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import SetupConfigPanel from "./SetupConfigPanel.vue";

const { getSetupConfig, patchSetupConfig, probeLLMModels } = vi.hoisted(() => ({
  getSetupConfig: vi.fn(),
  patchSetupConfig: vi.fn(),
  probeLLMModels: vi.fn(),
}));

vi.mock("../api/node.js", () => ({
  getSetupConfig,
  patchSetupConfig,
  probeLLMModels,
}));

let wrapper;

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  document.body.innerHTML = "";
  vi.resetAllMocks();
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

    expect(patchSetupConfig).toHaveBeenCalledTimes(2);
    const retriedPatch = patchSetupConfig.mock.calls[1][0];
    expect(retriedPatch.llm.active).toBe("final-profile");
    expect(retriedPatch.llm.profiles[0]).toMatchObject({
      id: "final-profile",
      original_id: "custom-profile",
      model: "new-model",
    });
  });
});
