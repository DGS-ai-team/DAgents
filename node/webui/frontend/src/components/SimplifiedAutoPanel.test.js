/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SimplifiedAutoPanel from "./SimplifiedAutoPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({ getAutoConfig: vi.fn(), getAutoExperience: vi.fn(), getAgentDreamingStatus: vi.fn(), putAutoConfig: vi.fn(), reconcileAutoConfig: vi.fn() }));

describe("SimplifiedAutoPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.getAutoConfig.mockResolvedValue({ revision: 4, responsibility: "整理资料", wake_interval_seconds: 1800, max_tool_rounds: 3, dreaming_enabled: false, timezone: "Asia/Shanghai", experience: { content: "只读经验" } });
    api.getAutoExperience.mockResolvedValue({ experience: { content: "只读经验" } });
    api.getAgentDreamingStatus.mockResolvedValue({ state: "waiting", next_at: null, last_success: null, last_error: "" });
    api.putAutoConfig.mockResolvedValue({ revision: 5, responsibility: "整理资料", wake_interval_seconds: 3600, max_tool_rounds: 4, dreaming_enabled: true, dreaming_time: "03:00", timezone: "UTC", experience: { content: "新经验" } });
    api.reconcileAutoConfig.mockResolvedValue({ profile: { revision: 6, responsibility: "整理资料", wake_interval_seconds: 3600, max_tool_rounds: 4, dreaming_enabled: false, dreaming_time: "03:00", timezone: "UTC" } });
  });

  it("loads and saves the simplified Auto fields with CAS", async () => {
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(wrapper.text()).toContain("职责");
    expect(wrapper.text()).toContain("只读经验");
    await wrapper.get('[aria-label="最大工具轮次"]').setValue(4);
    await wrapper.get(".btn--primary").trigger("click");
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
    await wrapper.get(".btn--primary").trigger("click");
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

  it("recognizes the real 503 saved_profile details and reconciles the trigger", async () => {
    api.putAutoConfig.mockRejectedValueOnce(Object.assign(new Error("trigger sync failed"), { status: 503, data: { error: { code: "trigger_sync_failed", details: { saved_profile: { revision: 5, responsibility: "已保存职责", wake_interval_seconds: 1800, max_tool_rounds: 3, dreaming_enabled: false, dreaming_time: "03:00", timezone: "UTC" }, trigger_sync: "pending" } } } }));
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    await wrapper.get('[aria-label="职责"]').setValue("用户继续编辑的职责");
    await wrapper.get(".btn--primary").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("配置已保存、唤醒未同步");
    expect(wrapper.get('[aria-label="职责"]').element.value).toBe("用户继续编辑的职责");
    const reconcileButton = wrapper.findAll("button").find((button) => button.text() === "同步唤醒");
    expect(reconcileButton).toBeTruthy();
    await reconcileButton.trigger("click");
    await flushPromises();
    expect(api.reconcileAutoConfig).toHaveBeenCalledWith("auto-1");
    expect(wrapper.get('[aria-label="职责"]').element.value).toBe("用户继续编辑的职责");
  });

  it("shows a configured dreaming value read-only while the feature is in development", async () => {
    api.getAutoConfig.mockResolvedValueOnce({ revision: 7, responsibility: "整理", wake_interval_seconds: 1800, max_tool_rounds: 4, dreaming_enabled: true, dreaming_time: "02:30", timezone: "Asia/Shanghai" });
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    const dreaming = wrapper.get('[aria-label="启用每日 dreaming"]');
    expect(dreaming.element.checked).toBe(true);
    expect(dreaming.element.disabled).toBe(false);
    expect(wrapper.text()).toContain("上次成功");
  });

  it("disables dreaming editing when its status cannot be loaded", async () => {
    api.getAgentDreamingStatus.mockRejectedValueOnce(new Error("状态服务不可用"));
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(wrapper.get('[aria-label="启用每日 dreaming"]').element.disabled).toBe(true);
    expect(wrapper.text()).toContain("状态服务不可用");
    expect(wrapper.text()).toContain("刷新状态");
  });

  it("releases a pending status refresh when switching agents", async () => {
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    let resolveRefresh;
    api.getAgentDreamingStatus.mockImplementationOnce(() => new Promise((resolve) => { resolveRefresh = resolve; }));
    const refreshButton = wrapper.findAll("button").find((button) => button.text() === "刷新状态");
    expect(refreshButton).toBeTruthy();
    await refreshButton.trigger("click");
    expect(wrapper.get('[aria-label="启用每日 dreaming"]').element.disabled).toBe(true);
    await wrapper.setProps({ agentId: "auto-2" });
    await flushPromises();
    resolveRefresh({ state: "running" });
    await flushPromises();
    expect(wrapper.get('[aria-label="启用每日 dreaming"]').element.disabled).toBe(false);
  });
});
