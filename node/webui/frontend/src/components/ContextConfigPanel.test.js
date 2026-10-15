/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ContextConfigPanel from "./ContextConfigPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({
  getSetupConfig: vi.fn(),
  patchSetupConfig: vi.fn(),
  getHealth: vi.fn(),
  getAgentInfo: vi.fn(),
}));

const setupConfig = () => ({
  config_path: "C:/test/config.yaml",
  config_writable: true,
  compression: {
    silent_trigger_tokens: 64000,
    blocking_trigger_tokens: 96000,
    idle_auto_compress_seconds: 0,
    idle_auto_compress_poll_seconds: 0,
    idle_auto_compress_min_tokens: 0,
  },
  hooks: {
    inject_today_date_enabled: false,
    tool_result_enabled: false,
    tool_result_spill_threshold_tokens: 12000,
  },
});

beforeEach(() => {
  vi.clearAllMocks();
  api.getSetupConfig.mockResolvedValue(setupConfig());
  api.patchSetupConfig.mockResolvedValue(setupConfig());
  api.getHealth.mockResolvedValue({});
  api.getAgentInfo.mockResolvedValue({});
});

describe("ContextConfigPanel save errors", () => {
  it("keeps the loaded editable draft visible when the save fails", async () => {
    api.patchSetupConfig.mockRejectedValueOnce(new Error("服务暂时不可用"));
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();

    const silent = wrapper.get('input[type="number"]');
    await silent.setValue("81000");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    expect(wrapper.get('[role="alert"]').text()).toContain("服务暂时不可用");
    expect(wrapper.get('input[type="number"]').element.value).toBe("81000");
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(6);
    expect(wrapper.get('input[type="number"]').element.disabled).toBe(false);
    expect(api.patchSetupConfig).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("explains an invalid threshold pair locally and allows zero as disabled", async () => {
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();
    const fields = wrapper.findAll('input[type="number"]');
    await fields[0].setValue("81000");
    await fields[1].setValue("70000");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");

    expect(wrapper.get('[role="alert"]').text()).toBe("阻塞压缩阈值不能小于静默压缩阈值");
    expect(api.patchSetupConfig).not.toHaveBeenCalled();

    await fields[0].setValue("0");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("does not offer saving before the initial configuration load completes", async () => {
    let resolveLoad;
    api.getSetupConfig.mockImplementationOnce(() => new Promise((resolve) => { resolveLoad = resolve; }));
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();

    expect(wrapper.get(".setup-config-panel__actions button").element.disabled).toBe(true);
    expect(wrapper.text()).toContain("加载中…");
    resolveLoad(setupConfig());
    await flushPromises();
    expect(wrapper.get(".setup-config-panel__actions button").element.disabled).toBe(false);
    wrapper.unmount();
  });

  it("allows editing and saving database-backed settings when config_path is empty", async () => {
    api.getSetupConfig.mockResolvedValueOnce({ ...setupConfig(), config_path: "" });
    api.patchSetupConfig.mockResolvedValueOnce({ ...setupConfig(), config_path: "" });
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();

    const button = wrapper.get(".setup-config-panel__actions button");
    expect(button.element.disabled).toBe(false);
    const silent = wrapper.get('input[type="number"]');
    await silent.setValue("70000");
    expect(silent.element.value).toBe("70000");
    await button.trigger("click");
    await flushPromises();

    expect(api.patchSetupConfig).toHaveBeenCalledTimes(1);
    expect(wrapper.find('input[type="number"]').exists()).toBe(true);
    wrapper.unmount();
  });
});
