/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SimplifiedAutoPanel from "./SimplifiedAutoPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({ getAutoConfig: vi.fn(), getAutoExperience: vi.fn(), putAutoConfig: vi.fn() }));

describe("SimplifiedAutoPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.getAutoConfig.mockResolvedValue({ revision: 4, responsibility: "整理资料", wake_interval_seconds: 1800, max_tool_rounds: 3, dreaming_enabled: false, timezone: "Asia/Shanghai", experience: { content: "只读经验" } });
    api.getAutoExperience.mockResolvedValue({ experience: { content: "只读经验" } });
    api.putAutoConfig.mockResolvedValue({ revision: 5, responsibility: "整理资料", wake_interval_seconds: 3600, max_tool_rounds: 4, dreaming_enabled: true, dreaming_time: "03:00", timezone: "UTC", experience: { content: "新经验" } });
  });

  it("loads and saves the simplified Auto fields with CAS", async () => {
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(wrapper.text()).toContain("职责");
    expect(wrapper.text()).toContain("只读经验");
    await wrapper.get('[aria-label="最大工具轮次"]').setValue(4);
    await wrapper.get(".btn").trigger("click");
    await flushPromises();
    expect(api.putAutoConfig).toHaveBeenCalledWith("auto-1", expect.objectContaining({ expected_revision: 4, max_tool_rounds: 4 }));
    expect(wrapper.text()).toContain("Auto 设置已保存");
  });

  it("does not let a late old-agent response overwrite the new agent", async () => {
    let resolveOld;
    api.getAutoConfig.mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }));
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "old" } });
    await wrapper.setProps({ agentId: "new" });
    await flushPromises();
    expect(api.getAutoConfig).toHaveBeenCalledWith("new");
    resolveOld({ revision: 1, responsibility: "旧", wake_interval_seconds: 0 });
    await flushPromises();
    expect(wrapper.get('[aria-label="职责"]').element.value).not.toBe("旧");
  });

  it("keeps edits made while save is pending and releases saving after an agent switch", async () => {
    let resolveSave;
    api.putAutoConfig.mockImplementationOnce(() => new Promise((resolve) => { resolveSave = resolve; }));
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    await wrapper.get('[aria-label="最大工具轮次"]').setValue(4);
    await wrapper.get(".btn").trigger("click");
    await wrapper.get('[aria-label="职责"]').setValue("用户刚改的职责");
    await wrapper.setProps({ agentId: "auto-2" });
    resolveSave({ revision: 5, responsibility: "旧响应", max_tool_rounds: 9 });
    await flushPromises();
    expect(wrapper.find(".btn").attributes("disabled")).toBeUndefined();
  });

  it("does not allow saving an unconfirmed agent after load failure", async () => {
    api.getAutoConfig.mockRejectedValueOnce(new Error("offline"));
    api.getAutoExperience.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "unavailable" } });
    await flushPromises();
    expect(wrapper.find(".btn").attributes("disabled")).toBeDefined();
    await wrapper.get(".btn").trigger("click");
    expect(api.putAutoConfig).not.toHaveBeenCalled();
  });
});
