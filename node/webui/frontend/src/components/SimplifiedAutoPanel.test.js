/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SimplifiedAutoPanel from "./SimplifiedAutoPanel.vue";
import * as api from "../api/node.js";
import router from "../router/index.js";

vi.mock("../api/node.js", () => ({ getAutoConfig: vi.fn(), getAutoExperience: vi.fn(), getAgentDreamingStatus: vi.fn(), getTrigger: vi.fn(), putAutoConfig: vi.fn(), reconcileAutoConfig: vi.fn() }));

describe("SimplifiedAutoPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.getAutoConfig.mockResolvedValue({ revision: 4, responsibility: "整理资料", wake_interval_seconds: 1800, max_tool_rounds: 3, dreaming_enabled: false, timezone: "Asia/Shanghai", experience: { content: "只读经验" } });
    api.getAutoExperience.mockResolvedValue({ experience: { content: "只读经验" } });
    api.getAgentDreamingStatus.mockResolvedValue({ state: "waiting", next_at: null, last_success: null, last_error: "" });
    api.getTrigger.mockResolvedValue({ recovery_required: false, enabled: true, condition: { interval_seconds: 1800 } });
    api.putAutoConfig.mockResolvedValue({ revision: 5, responsibility: "整理资料", wake_interval_seconds: 3600, max_tool_rounds: 4, dreaming_enabled: true, dreaming_time: "03:00", timezone: "UTC", experience: { content: "新经验" } });
    api.reconcileAutoConfig.mockResolvedValue({ profile: { revision: 6, responsibility: "整理资料", wake_interval_seconds: 1800, max_tool_rounds: 4, dreaming_enabled: false, dreaming_time: "03:00", timezone: "UTC" } });
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

  it("reads the default trigger recovery state and links to the mounted triggers route", async () => {
    api.getTrigger.mockResolvedValueOnce({ trigger_id: "auto-default:auto-1", recovery_required: true, pending_delivery_id: "delivery-1", revision: 7 });
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(api.getTrigger).toHaveBeenCalledWith("auto-default:auto-1");
    expect(wrapper.text()).toContain("默认唤醒");
    expect(wrapper.get('a[href="/ui/settings/triggers"]').text()).toContain("定时任务");
    expect(router.resolve("/settings/triggers").name).toBe("settings-triggers");
    expect(router.resolve("/settings/triggers").href).toBe("/ui/settings/triggers");
  });

  it("does not treat dreaming recovery as a trigger delivery recovery", async () => {
    api.getAgentDreamingStatus.mockResolvedValueOnce({ state: "recovery_pending" });
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(api.getTrigger).toHaveBeenCalledWith("auto-default:auto-1");
    expect(wrapper.text()).not.toContain("默认唤醒");
  });

  it("refreshes trigger recovery without replacing the unsaved Auto draft", async () => {
    api.getTrigger.mockResolvedValueOnce({ recovery_required: true, pending_delivery_id: "delivery-1", revision: 7 });
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    await wrapper.get('[aria-label="职责"]').setValue("尚未保存的职责");
    api.getTrigger.mockResolvedValueOnce({ recovery_required: true, pending_delivery_id: "delivery-2", revision: 8 });
    const triggerRow = wrapper.findAll(".simplified-auto-panel__row").find((row) => row.text().includes("默认唤醒"));
    expect(triggerRow).toBeTruthy();
    await triggerRow.find("button").trigger("click");
    await flushPromises();
    expect(wrapper.get('[aria-label="职责"]').element.value).toBe("尚未保存的职责");
    expect(wrapper.get('a[href="/ui/settings/triggers"]').attributes("target")).toBe("_blank");
  });

  it("shows a friendly status when the default trigger has not been synchronized", async () => {
    api.getTrigger.mockRejectedValueOnce(Object.assign(new Error("missing"), { status: 404 }));
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(wrapper.text()).toContain("默认唤醒尚未同步");
    expect(wrapper.text()).toContain("刷新状态");
  });

  it("keeps reconcile available for a stale interval and can repair a missing trigger", async () => {
    api.getAutoConfig.mockResolvedValueOnce({ revision: 4, responsibility: "整理资料", wake_interval_seconds: 3600, max_tool_rounds: 3, dreaming_enabled: false });
    api.getTrigger.mockResolvedValueOnce({ recovery_required: false, enabled: true, condition: { interval_seconds: 1800 } });
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(wrapper.findAll("button").some((button) => button.text() === "同步唤醒")).toBe(true);

    api.getTrigger.mockRejectedValueOnce(Object.assign(new Error("missing"), { status: 404 }));
    const triggerRow = wrapper.findAll(".simplified-auto-panel__row").find((row) => row.text().includes("默认唤醒"));
    await triggerRow.find("button").trigger("click");
    await flushPromises();
    expect(wrapper.findAll("button").some((button) => button.text() === "同步唤醒")).toBe(true);
    await wrapper.findAll("button").find((button) => button.text() === "同步唤醒").trigger("click");
    await flushPromises();
    expect(api.reconcileAutoConfig).toHaveBeenCalledWith("auto-1");
  });

  it("keeps reconcile visible when a failed close leaves the default trigger enabled", async () => {
    api.putAutoConfig.mockRejectedValueOnce(Object.assign(new Error("trigger sync failed"), { status: 503, data: { error: { details: { saved_profile: { revision: 5, responsibility: "关闭", wake_interval_seconds: 0, max_tool_rounds: 3, dreaming_enabled: false, dreaming_time: "03:00", timezone: "UTC" } } } } }));
    const wrapper = mount(SimplifiedAutoPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    await wrapper.get('[aria-label="自主激活频率"]').setValue(0);
    await wrapper.get(".btn--primary").trigger("click");
    await flushPromises();
    api.getTrigger.mockResolvedValueOnce({ recovery_required: false, enabled: true, condition: { interval_seconds: 1800 } });
    const triggerRow = wrapper.findAll(".simplified-auto-panel__row").find((row) => row.text().includes("默认唤醒"));
    await triggerRow.find("button").trigger("click");
    await flushPromises();
    expect(wrapper.findAll("button").some((button) => button.text() === "同步唤醒")).toBe(true);
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
    expect(wrapper.findAll("button").some((button) => button.text() === "同步唤醒")).toBe(false);
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
