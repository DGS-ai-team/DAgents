/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import GoalsView from "./GoalsView.vue";
import * as api from "../api/node.js";
import { agentStore } from "../stores/agent.js";

vi.mock("vue-router", () => ({ useRoute: () => ({ query: {} }) }));
vi.mock("../api/node.js", () => ({
  listGoals: vi.fn(), createGoal: vi.fn(), getGoal: vi.fn(), getGoalRuns: vi.fn(), goalAction: vi.fn(),
}));

const ownGoal = { id: "g-own", agent_id: "agent-a", title: "自己的任务", objective: "做事", acceptance: "完成", status: "active", runs: 1, max_runs: 12, token_budget: 100000, tokens_used: 100 };

beforeEach(() => {
  vi.clearAllMocks();
  agentStore.agentId = "agent-a";
  api.listGoals.mockResolvedValue({ goals: [ownGoal, { ...ownGoal, id: "g-other", agent_id: "agent-b", title: "别的 Agent" }] });
  api.getGoalRuns.mockResolvedValue({ runs: [] });
  api.getGoal.mockResolvedValue(ownGoal);
  api.createGoal.mockResolvedValue(ownGoal);
  api.goalAction.mockResolvedValue({ ...ownGoal, status: "active" });
});

describe("GoalsView current Agent behavior", () => {
  it("filters history and directs new work to Auto Agent settings", async () => {
    const wrapper = mount(GoalsView, { global: { stubs: { RouterLink: true } } });
    await flushPromises();
    expect(wrapper.text()).toContain("自己的任务");
    expect(wrapper.text()).not.toContain("别的 Agent");
    expect(wrapper.text()).toContain("新建长期任务请创建 Auto Agent");
    expect(wrapper.find(".goal-create-link").exists()).toBe(true);
    expect(api.createGoal).not.toHaveBeenCalled();
  });

  it("shows wake acknowledgement as a status notice", async () => {
    const wrapper = mount(GoalsView, { global: { stubs: { RouterLink: true } } });
    await flushPromises();
    await wrapper.find(".goal-row").trigger("click");
    await flushPromises();
    await wrapper.get(".goal-actions button:nth-child(2)").trigger("click");
    await flushPromises();
    expect(wrapper.find('[role="status"]').text()).toContain("已提交下一次运行");
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  });
});
