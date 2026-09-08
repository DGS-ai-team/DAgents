/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AgentAutonomyPanel from "./AgentAutonomyPanel.vue";
import * as api from "../api/node.js";

vi.mock("vue-router", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("../api/node.js", () => ({ getAgentAutonomy: vi.fn(), putAgentAutonomy: vi.fn(), getGoalRuns: vi.fn(), createAgentAutonomyCycle: vi.fn(), agentAutonomyAction: vi.fn() }));

const wrappers = [];
afterEach(() => { while (wrappers.length) wrappers.pop().unmount(); });

beforeEach(() => {
  vi.clearAllMocks();
  api.getAgentAutonomy.mockResolvedValue({ goal_id: "g1", status: "waiting", profile: { revision: 7, enabled: true }, limits: { revision: 4, title: "目标", objective: "做事", acceptance: "完成", max_runs: 12, token_budget: 100, turn_token_budget: 20, min_wake_interval_seconds: 300, expires_at: "2026-09-08T12:00:00Z", session_id: "goal-session", status_reason: "approval_required", next_wake_at: "2026-09-08T13:00:00Z" }, progress: null });
  api.getGoalRuns.mockResolvedValue({ runs: [{ id: "r1", status: "running", reason: "schedule", started_at: "2026-09-08T12:00:00Z" }] });
  api.putAgentAutonomy.mockResolvedValue({ goal_id: "g1", status: "waiting", limits: {}, progress: null });
});

describe("AgentAutonomyPanel", () => {
  it("keeps waiting enabled, converts deadline to local input, and saves turn budget", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.text()).toContain("Auto 状态");
    expect(wrapper.text()).toContain("单轮 Token 预算");
    expect(wrapper.text()).toContain("等待审批");
    expect(wrapper.text()).toContain("处理审批");
    expect(wrapper.text()).toContain("定时唤醒");
    await wrapper.find('input[type="datetime-local"]').setValue("2026-09-08T20:30");
    await wrapper.findAll("button").find((button) => button.text().includes("保存周期设置")).trigger("click");
    await flushPromises();
    expect(api.putAgentAutonomy).toHaveBeenCalledWith("auto-1", expect.objectContaining({ expected_revision: 7, expected_goal_revision: 4, turn_token_budget: 20, expires_at: new Date("2026-09-08T20:30").toISOString() }));
  });

  it("keeps the form and retry action visible after a load failure", async () => {
    api.getAgentAutonomy.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.find('[role="alert"]').text()).toContain("offline");
    expect(wrapper.findAll('button').some((button) => button.text().includes("刷新状态"))).toBe(true);
    expect(wrapper.find('input[type="number"]').exists()).toBe(true);
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "active", limits: {}, progress: null });
    await wrapper.findAll('button').find((button) => button.text().includes("刷新状态")).trigger("click");
    await flushPromises();
    expect(wrapper.text()).not.toContain("offline");
  });

  it("shows that a completed task will not wake again", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "completed", profile: { revision: 7, enabled: true }, limits: { max_runs: 1, token_budget: 10, tokens_used: 10 }, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.text()).toContain("不会再运行");
    expect(wrapper.text()).toContain("创建新的自主任务周期");
  });

  it("creates a cycle with the complete form and reads autonomy back", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "completed", profile: { revision: 7, enabled: true }, limits: { revision: 4, title: "目标", objective: "做事", acceptance: "完成" }, progress: null });
    api.createAgentAutonomyCycle.mockResolvedValue({ id: "g2", status: "waiting" });
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g2", status: "waiting", profile: { revision: 8, enabled: true }, limits: { revision: 1, title: "目标", objective: "做事", acceptance: "完成" }, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.findAll("button").find((button) => button.text().includes("创建新的自主任务周期")).trigger("click");
    await flushPromises();
    expect(api.createAgentAutonomyCycle).toHaveBeenCalledWith("auto-1", expect.objectContaining({ expected_profile_revision: 7, max_runs: 12, token_budget: 100000, turn_token_budget: 10000, min_wake_interval_seconds: 300, idempotency_key: expect.any(String) }));
    expect(api.getAgentAutonomy).toHaveBeenCalledTimes(2);
  });

  it("retries a failed cycle with the same idempotency key", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "completed", profile: { revision: 7, enabled: true }, limits: {}, progress: null });
    api.createAgentAutonomyCycle.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({ id: "g2" });
    api.getAgentAutonomy.mockResolvedValue({ goal_id: "g2", status: "waiting", profile: { revision: 8, enabled: true }, limits: {}, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    const create = () => wrapper.findAll("button").find((button) => button.text().includes("创建新的自主任务周期")).trigger("click");
    await create(); await flushPromises();
    await create(); await flushPromises();
    const first = api.createAgentAutonomyCycle.mock.calls[0][1].idempotency_key;
    const second = api.createAgentAutonomyCycle.mock.calls[1][1].idempotency_key;
    expect(second).toBe(first);
  });

  it("allows disabling the Auto profile after its current cycle is terminal", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "completed", profile: { revision: 7, enabled: true }, limits: { revision: 4 }, progress: null });
    api.agentAutonomyAction.mockResolvedValue({ goal_id: "g1", status: "completed", profile: { revision: 8, enabled: false }, limits: { revision: 4 }, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.findAll("button").find((button) => button.text().includes("停用 Auto")).trigger("click");
    await flushPromises();
    expect(api.agentAutonomyAction).toHaveBeenCalledWith("auto-1", expect.objectContaining({ action: "disable_auto", expected_profile_revision: 7 }));
  });

  it("builds recurring schedule and cycle duration from structured controls", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.findAll("select")[1].setValue("weekly");
    await wrapper.find('input[type="time"]').setValue("08:30");
    await wrapper.find('input[type="checkbox"][value="mon"]').setValue(true);
    await wrapper.find('input[type="checkbox"][value="fri"]').setValue(true);
    await wrapper.findAll('input[type="number"]')[0].setValue("2");
    await wrapper.findAll("select")[2].setValue("day");
    await wrapper.findAll("button").find((button) => button.text().includes("保存岗位设置")).trigger("click");
    await flushPromises();
    const [, payload] = api.putAgentAutonomy.mock.calls.at(-1);
    expect(payload.profile.work_schedule).toBe("weekly mon,fri 08:30");
    expect(payload.profile.cycle_duration_seconds).toBe(172800);
  });

  it("uses edited structured schedule instead of preserving legacy text", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 7, enabled: true, work_schedule: "工作日 09:00" }, limits: {}, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.find('select[aria-label="工作安排频率"]').setValue("daily");
    await wrapper.find('input[aria-label="工作安排时间"]').setValue("11:15");
    await wrapper.find('input[aria-label="工作安排时间"]').trigger("change");
    await wrapper.findAll("button").find((button) => button.text().includes("保存岗位设置")).trigger("click");
    await flushPromises();
    expect(api.putAgentAutonomy.mock.calls.at(-1)[1].profile.work_schedule).toBe("daily 11:15");
  });

  it("sends profile-only fields and preserves an unsaved cycle draft", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 7, enabled: true, work_schedule: "工作日 09:00" }, limits: { revision: 4, title: "目标", objective: "做事", acceptance: "完成" }, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.find('input[placeholder="例如：工作日 09:00"]').setValue("工作日 10:00");
    await wrapper.findAll("input").find((input) => input.element.type === "text" && input.element.value === "目标").setValue("未保存目标");
    api.putAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 8, enabled: true }, limits: { revision: 4, title: "服务端目标" }, progress: null });
    await wrapper.findAll("button").find((button) => button.text().includes("保存岗位设置")).trigger("click");
    await flushPromises();
    const [, payload] = api.putAgentAutonomy.mock.calls.at(-1);
    expect(payload).toEqual({ expected_revision: 7, profile: { agent_id: "auto-1", role_objective: "", role_boundaries: "", plan_mode: "recurring", timezone: "Asia/Shanghai", work_schedule: "工作日 10:00", cycle_duration_seconds: 86400 } });
    expect(wrapper.find('input[placeholder="例如：工作日 09:00"]').element.value).toBe("工作日 10:00");
  });

  it("only renders the legacy schedule editor for an unparseable value", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.find('input[aria-label="旧工作安排文本"]').exists()).toBe(false);
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 7, enabled: true, work_schedule: "工作日 09:00" }, limits: {}, progress: null });
    await wrapper.findAll("button").find((button) => button.text().includes("刷新状态")).trigger("click");
    await flushPromises();
    expect(wrapper.find('input[aria-label="旧工作安排文本"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("转为结构化设置");
  });

  it("treats invalid times and unknown weekly days as legacy schedules", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 7, enabled: true, work_schedule: "daily 99:99" }, limits: {}, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.find('input[aria-label="旧工作安排文本"]').exists()).toBe(true);
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 7, enabled: true, work_schedule: "weekly banana 09:00" }, limits: {}, progress: null });
    await wrapper.findAll("button").find((button) => button.text().includes("刷新状态")).trigger("click");
    await flushPromises();
    expect(wrapper.find('input[aria-label="旧工作安排文本"]').exists()).toBe(true);
  });

  it("rejects an invalid time instead of replacing it with 09:00", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    const time = wrapper.find('input[aria-label="工作安排时间"]');
    await time.setValue("25:61");
    await time.trigger("change");
    await wrapper.findAll("button").find((button) => button.text().includes("保存岗位设置")).trigger("click");
    await flushPromises();
    expect(api.putAgentAutonomy).not.toHaveBeenCalled();
    expect(wrapper.find('[role="alert"]').text()).toContain("工作安排时间无效");
  });

  it("rejects an empty weekly schedule and an overlong cycle", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.find('select[aria-label="工作安排频率"]').setValue("weekly");
    await wrapper.findAll("button").find((button) => button.text().includes("保存岗位设置")).trigger("click");
    await flushPromises();
    expect(api.putAgentAutonomy).not.toHaveBeenCalled();
    expect(wrapper.find('[role="alert"]').text()).toContain("至少选择一天");
    await wrapper.find('input[aria-label="周一"]').setValue(true);
    await wrapper.find('input[aria-label="每周期时长数值"]').setValue("32");
    await wrapper.findAll("select").find((select) => select.attributes("aria-label") === "每周期时长单位").setValue("day");
    await wrapper.findAll("button").find((button) => button.text().includes("保存岗位设置")).trigger("click");
    await flushPromises();
    expect(api.putAgentAutonomy).not.toHaveBeenCalled();
    expect(wrapper.find('[role="alert"]').text()).toContain("每周期时长");
  });

  it("saves cycle fields while an unsaved role schedule is invalid", async () => {
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 7, enabled: true, work_schedule: "工作日 09:00" }, limits: { title: "目标" }, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.find('input[aria-label="工作安排时间"]').setValue("25:61");
    await wrapper.find('input[aria-label="工作安排时间"]').trigger("change");
    await wrapper.find('input[placeholder="例如：工作日 09:00"]').setValue("daily 25:61");
    await wrapper.findAll('input').find((input) => input.element.value === "目标").setValue("独立周期");
    await wrapper.findAll("button").find((button) => button.text().includes("保存周期设置")).trigger("click");
    await flushPromises();
    expect(api.putAgentAutonomy).toHaveBeenCalledWith("auto-1", expect.objectContaining({ title: "独立周期" }));
  });

  it("preserves the complete role draft after saving a cycle", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.find('textarea').setValue("长期职责草稿");
    await wrapper.findAll('textarea')[1].setValue("边界草稿");
    await wrapper.find('input[aria-label="工作安排时间"]').setValue("08:15");
    await wrapper.find('input[aria-label="工作安排时间"]').trigger("change");
    api.putAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 8, enabled: true }, limits: {}, progress: null });
    await wrapper.findAll("button").find((button) => button.text().includes("保存周期设置")).trigger("click");
    await flushPromises();
    expect(wrapper.findAll('textarea')[0].element.value).toBe("长期职责草稿");
    expect(wrapper.findAll('textarea')[1].element.value).toBe("边界草稿");
    expect(wrapper.find('input[aria-label="工作安排时间"]').element.value).toBe("08:15");
  });

  it("reloads its data when the agent prop changes", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.find('select[aria-label="工作安排频率"]').setValue("weekly");
    await wrapper.find('input[aria-label="周一"]').setValue(true);
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g2", status: "paused", profile: { revision: 11, enabled: false }, limits: { title: "第二个 Agent" }, progress: null });
    await wrapper.setProps({ agentId: "auto-2" });
    await flushPromises();
    expect(api.getAgentAutonomy).toHaveBeenLastCalledWith("auto-2");
    expect(wrapper.findAll("input").find((input) => input.element.value === "第二个 Agent")).toBeTruthy();
    expect(wrapper.find('select[aria-label="工作安排频率"]').element.value).toBe("daily");
    expect(wrapper.find('input[aria-label="工作安排时间"]').element.value).toBe("09:00");
    expect(wrapper.find('input[aria-label="周一"]').exists()).toBe(false);
    expect(wrapper.find('input[aria-label="旧工作安排文本"]').exists()).toBe(false);
  });
});
