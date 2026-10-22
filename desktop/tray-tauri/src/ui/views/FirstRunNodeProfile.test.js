// @vitest-environment jsdom
import { mount, flushPromises } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  setup: null,
  patchSetupConfig: vi.fn(),
  getSetupConfig: vi.fn(),
}));

vi.mock("../api/node.js", () => ({
  getUIBootstrap: vi.fn(async () => ({ user: {}, agent: { name: "Test Node" } })),
  getSetupConfig: (...args) => mocks.getSetupConfig(...args),
  patchSetupConfig: (...args) => mocks.patchSetupConfig(...args),
  probeLLMModels: vi.fn(),
}));

import FirstRunNodeProfile from "./FirstRunNodeProfile.vue";

describe("FirstRunNodeProfile", () => {
  beforeEach(() => {
    mocks.setup = {
      user: { preferred_name: "" },
      agent: { name: "Test Node", description: "" },
      llm: {
        active: "profile-current",
        profiles: [
          { id: "profile-other", provider: "qwen", base_url: "https://qwen.test/v1", model: "qwen-model", mock: false, multimodal_enabled: false },
          { id: "profile-current", provider: "deepseek", base_url: "https://deepseek.test/v1", model: "deepseek-chat", mock: false, multimodal_enabled: false },
        ],
      },
      onboarding: { node_profile_completed: false },
    };
    mocks.getSetupConfig.mockReset().mockImplementation(async () => structuredClone(mocks.setup));
    mocks.patchSetupConfig.mockReset().mockImplementation(async (patch) => {
      mocks.setup = {
        ...mocks.setup,
        ...patch,
        llm: patch.llm,
        onboarding: patch.onboarding,
      };
    });
  });

  it("keeps the visible and accessible progress aligned with its two steps", async () => {
    const wrapper = mount(FirstRunNodeProfile);
    await flushPromises();

    const progress = wrapper.find('[role="progressbar"]');
    expect(wrapper.find(".first-run__progress-label").text()).toBe("1 / 2");
    expect(progress.attributes("aria-valuemin")).toBe("1");
    expect(progress.attributes("aria-valuemax")).toBe("2");
    expect(progress.attributes("aria-valuenow")).toBe("1");

    await wrapper.find(".first-run__footer-primary").trigger("click");
    await flushPromises();

    expect(wrapper.find(".first-run__progress-label").text()).toBe("2 / 2");
    expect(progress.attributes("aria-valuenow")).toBe("2");
  });

  it("omits theme choice and preserves other profiles while confirming the saved active profile", async () => {
    const wrapper = mount(FirstRunNodeProfile);
    await flushPromises();

    expect(wrapper.text()).toContain("当前 Node 配置");
    expect(wrapper.text()).not.toContain("你喜欢哪种外观");
    const identityInput = wrapper.find('input[placeholder="可留空，例如：小明"]');
    expect(identityInput.exists()).toBe(true);
    await wrapper.find(".first-run__footer-primary").trigger("click");
    await flushPromises();
    await wrapper.find("#first-run-llm").trigger("submit");
    await flushPromises();

    expect(mocks.patchSetupConfig).toHaveBeenCalledOnce();
    const patch = mocks.patchSetupConfig.mock.calls[0][0];
    expect(patch.user.preferred_name).toBe("");
    expect(patch.llm.active).toBe("profile-current");
    expect(patch.llm.profiles).toHaveLength(2);
    expect(patch.llm.profiles[0]).toMatchObject({ id: "profile-other", model: "qwen-model" });
    expect(patch.llm.profiles[1]).toMatchObject({ id: "profile-current", model: "deepseek-chat" });
    expect(wrapper.emitted("completed")).toHaveLength(1);
  });

  it("does not submit or replace profiles when initial Node settings cannot be read", async () => {
    mocks.getSetupConfig.mockRejectedValueOnce(new Error("Node settings unavailable"));
    const wrapper = mount(FirstRunNodeProfile);
    await flushPromises();

    expect(wrapper.text()).toContain("Node settings unavailable");
    expect(wrapper.find(".first-run__footer-primary").attributes("disabled")).toBeDefined();
    expect(mocks.patchSetupConfig).not.toHaveBeenCalled();
  });
});
