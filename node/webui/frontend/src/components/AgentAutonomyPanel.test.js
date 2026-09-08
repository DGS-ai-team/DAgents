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

  it("sends profile-only fields and preserves an unsaved cycle draft", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    await wrapper.find('input[placeholder="例如：工作日 09:00"]').setValue("工作日 10:00");
    await wrapper.findAll("input").find((input) => input.element.type === "text" && input.element.value === "目标").setValue("未保存目标");
    api.putAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "waiting", profile: { revision: 8, enabled: true }, limits: { revision: 4, title: "服务端目标" }, progress: null });
    await wrapper.findAll("button").find((button) => button.text().includes("保存岗位设置")).trigger("click");
    await flushPromises();
    const [, payload] = api.putAgentAutonomy.mock.calls.at(-1);
    expect(payload).toEqual({ expected_revision: 7, profile: { agent_id: "auto-1", role_objective: "", role_boundaries: "", plan_mode: "recurring", timezone: "Asia/Shanghai", work_schedule: "工作日 10:00" } });
    expect(wrapper.find('input[placeholder="例如：工作日 09:00"]').element.value).toBe("工作日 10:00");
  });

  it("reloads its data when the agent prop changes", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } }); wrappers.push(wrapper);
    await flushPromises();
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g2", status: "paused", profile: { revision: 11, enabled: false }, limits: { title: "第二个 Agent" }, progress: null });
    await wrapper.setProps({ agentId: "auto-2" });
    await flushPromises();
    expect(api.getAgentAutonomy).toHaveBeenLastCalledWith("auto-2");
    expect(wrapper.findAll("input").find((input) => input.element.value === "第二个 Agent")).toBeTruthy();
  });
});
