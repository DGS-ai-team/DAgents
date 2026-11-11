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
    idle_new_conversation_seconds: 0,
  },
  hooks: {
    inject_today_date_enabled: false,
    tool_result_enabled: false,
    tool_result_spill_threshold_tokens: 12000,
  },
});

function numberField(wrapper, label) {
  return wrapper.findAll("label.settings-field").find((field) => field.text().includes(label)).get('input[type="number"]');
}

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

    const silent = numberField(wrapper, "静默压缩阈值");
    await silent.setValue("81000");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    expect(wrapper.get('[role="alert"]').text()).toContain("服务暂时不可用");
    expect(silent.element.value).toBe("81000");
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(4);
    expect(wrapper.get('input[type="number"]').element.disabled).toBe(false);
    expect(api.patchSetupConfig).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("explains an invalid threshold pair locally and allows zero as disabled", async () => {
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();
    const silent = numberField(wrapper, "静默压缩阈值");
    const blocking = numberField(wrapper, "阻塞压缩阈值");
    await silent.setValue("81000");
    await blocking.setValue("70000");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");

    expect(wrapper.get('[role="alert"]').text()).toBe("阻塞压缩阈值不能小于静默压缩阈值");
    expect(api.patchSetupConfig).not.toHaveBeenCalled();

    await silent.setValue("0");
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
    const silent = numberField(wrapper, "静默压缩阈值");
    await silent.setValue("70000");
    expect(silent.element.value).toBe("70000");
    await button.trigger("click");
    await flushPromises();

    expect(api.patchSetupConfig).toHaveBeenCalledTimes(1);
    expect(wrapper.find('input[type="number"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("saves the idle new-conversation duration and explains its trigger", async () => {
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();
    expect(wrapper.text()).not.toContain("自动压缩：启用");
    expect(wrapper.text()).toContain("自动压缩");
    expect(wrapper.text()).toContain("提示内容");
    expect(wrapper.text()).toContain("普通 Agent 达到此时长后，下条消息");
    expect(wrapper.text()).toContain("开启新对话并归档旧上下文");
    expect(wrapper.text()).toContain("阈值设为 0 可关闭");
    expect(wrapper.text()).toContain("至少为 1");
    const idle = numberField(wrapper, "空闲时长");
    await idle.setValue("3600");
    expect(wrapper.text()).not.toContain("空闲归档：3600 秒后启用");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();

    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      compression: expect.objectContaining({ idle_new_conversation_seconds: 3600 }),
    }));
    expect(wrapper.text()).not.toContain("扫描间隔");
    expect(wrapper.text()).not.toContain("最低 tokens");
    wrapper.unmount();
  });

  it("clears saved status when the user edits again", async () => {
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();
    const field = wrapper.get('input[type="number"]');
    await field.setValue("70000");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toBe("已保存。");

    await field.setValue("71000");
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toBe("有未保存的更改");
    wrapper.unmount();
  });

  it("disables the editable form while saving and explains restart requirements", async () => {
    let resolveSave;
    api.patchSetupConfig.mockImplementationOnce(() => new Promise((resolve) => { resolveSave = resolve; }));
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();
    await wrapper.get('input[type="number"]').setValue("70000");
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    expect(wrapper.get(".config-panel-shell__fields").element.disabled).toBe(true);
    expect(wrapper.get(".setup-config-panel__actions button").element.disabled).toBe(true);
    resolveSave({ ...setupConfig(), restart_required: true });
    await flushPromises();
    expect(wrapper.get('[role="status"]').text()).toContain("部分设置需重启后生效");
    wrapper.unmount();
  });

  it("groups related context controls and preserves their loaded values", async () => {
    const wrapper = mount(ContextConfigPanel);
    await flushPromises();
    expect(wrapper.findAll(".settings-group")).toHaveLength(3);
    expect(wrapper.text()).toContain("自动摘要");
    expect(numberField(wrapper, "空闲时长").element.value).toBe("0");
    expect(numberField(wrapper, "静默压缩阈值").element.value).toBe("64000");
    const dateToggle = wrapper.get('input[aria-label="当天日期提示"]');
    expect(dateToggle.element.checked).toBe(false);
    await dateToggle.setValue(true);
    await wrapper.get(".setup-config-panel__actions button").trigger("click");
    await flushPromises();
    expect(api.patchSetupConfig).toHaveBeenCalledWith(expect.objectContaining({
      compression: expect.objectContaining({ silent_trigger_tokens: 64000, idle_new_conversation_seconds: 0 }),
      hooks: expect.objectContaining({ inject_today_date_enabled: true }),
    }));
    wrapper.unmount();
  });
});
