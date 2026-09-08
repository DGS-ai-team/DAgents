/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentAutonomyPanel from "./AgentAutonomyPanel.vue";
import * as api from "../api/node.js";

vi.mock("vue-router", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("../api/node.js", () => ({ getAgentAutonomy: vi.fn(), putAgentAutonomy: vi.fn(), getGoalRuns: vi.fn() }));

beforeEach(() => {
  vi.clearAllMocks();
  api.getAgentAutonomy.mockResolvedValue({ goal_id: "g1", status: "waiting", limits: { title: "目标", objective: "做事", acceptance: "完成", max_runs: 12, token_budget: 100, turn_token_budget: 20, min_wake_interval_seconds: 300, expires_at: "2026-09-08T12:00:00Z", session_id: "goal-session", status_reason: "approval_required", next_wake_at: "2026-09-08T13:00:00Z" }, progress: null });
  api.getGoalRuns.mockResolvedValue({ runs: [{ id: "r1", status: "running", reason: "schedule", started_at: "2026-09-08T12:00:00Z" }] });
  api.putAgentAutonomy.mockResolvedValue({ goal_id: "g1", status: "waiting", limits: {}, progress: null });
});

describe("AgentAutonomyPanel", () => {
  it("keeps waiting enabled, converts deadline to local input, and saves turn budget", async () => {
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(wrapper.get('input[type="checkbox"]').element.checked).toBe(true);
    expect(wrapper.text()).toContain("单轮 Token 预算");
    expect(wrapper.text()).toContain("等待审批");
    expect(wrapper.text()).toContain("处理审批");
    expect(wrapper.text()).toContain("定时唤醒");
    await wrapper.find('input[type="datetime-local"]').setValue("2026-09-08T20:30");
    await wrapper.get("button.btn--primary").trigger("click");
    await flushPromises();
    expect(api.putAgentAutonomy).toHaveBeenCalledWith("auto-1", expect.objectContaining({ enabled: true, turn_token_budget: 20, expires_at: new Date("2026-09-08T20:30").toISOString() }));
  });

  it("keeps the form and retry action visible after a load failure", async () => {
    api.getAgentAutonomy.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } });
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
    api.getAgentAutonomy.mockResolvedValueOnce({ goal_id: "g1", status: "completed", limits: { max_runs: 1, token_budget: 10, tokens_used: 10 }, progress: null });
    const wrapper = mount(AgentAutonomyPanel, { props: { agentId: "auto-1" } });
    await flushPromises();
    expect(wrapper.text()).toContain("不会再运行");
    expect(wrapper.text()).toContain("创建新的自主 Agent");
  });
});
