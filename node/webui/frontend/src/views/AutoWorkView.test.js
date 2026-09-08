/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AutoWorkView from "./AutoWorkView.vue";
import * as api from "../api/node.js";

const route = reactive({ params: { agentId: "a" } });
vi.mock("vue-router", () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn() }),
}));
vi.mock("../api/node.js", () => ({
  getAgent: vi.fn(),
  getAgentAutonomy: vi.fn(),
  getAgentAutonomyCycles: vi.fn(),
  getGoalRuns: vi.fn(),
}));

const wrappers = [];
const mountOptions = {
  global: {
    stubs: {
      NavRail: { template: "<div data-testid='nav-rail' />" },
      RouterLink: { template: "<a><slot /></a>" },
    },
  },
};
const auto = (id) => ({
  agent_id: id,
  display_name: id,
  agent_type: "auto",
  workspace: { mode: "private" },
});

beforeEach(() => {
  vi.resetAllMocks();
  route.params.agentId = "a";
  api.getAgent.mockResolvedValue(auto("a"));
  api.getAgentAutonomy.mockResolvedValue({ summary: { state: "standby" }, profile: {}, usage: {} });
  api.getAgentAutonomyCycles.mockResolvedValue({ items: [], total: 0 });
});

afterEach(() => {
  while (wrappers.length) wrappers.pop().unmount();
});

describe("AutoWorkView", () => {
  it("shows a normal-Agent explanation and does not call Auto APIs", async () => {
    api.getAgent.mockResolvedValue({ agent_id: "n", agent_type: "normal", display_name: "普通" });
    const wrapper = mount(AutoWorkView, mountOptions);
    wrappers.push(wrapper);
    await flushPromises();

    expect(wrapper.text()).toContain("普通 Agent");
    expect(wrapper.text()).toContain("打开聊天");
    expect(api.getAgentAutonomy).not.toHaveBeenCalled();
  });

  it("ignores a late response for Agent A after switching to Agent B", async () => {
    let resolveA;
    api.getAgent
      .mockReset()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveA = resolve; }))
      .mockImplementation((id) => Promise.resolve(auto(id)));
    const wrapper = mount(AutoWorkView, mountOptions);
    wrappers.push(wrapper);

    route.params.agentId = "b";
    await wrapper.vm.$nextTick();
    await flushPromises();
    expect(wrapper.text()).toContain("b");

    resolveA(auto("a"));
    await flushPromises();
    expect(wrapper.get("h1").text()).toContain("b");
    expect(wrapper.get("h1").text()).not.toContain("a");
  });

  it("keeps chat and settings links visible without a current cycle", async () => {
    const wrapper = mount(AutoWorkView, mountOptions);
    wrappers.push(wrapper);
    await flushPromises();

    expect(wrapper.text()).toContain("打开聊天");
    expect(wrapper.text()).toContain("设置");
  });

  it("retries a failed run request and renders the returned run", async () => {
    api.getAgentAutonomyCycles.mockResolvedValue({
      items: [{ id: "g", objective: "目标", status: "active" }],
      total: 1,
    });
    api.getGoalRuns
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ runs: [{ id: "run-2", status: "completed" }] });
    const wrapper = mount(AutoWorkView, mountOptions);
    wrappers.push(wrapper);
    await flushPromises();

    await wrapper.find("article button").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("offline");

    await wrapper.get('[role="alert"] button').trigger("click");
    await flushPromises();
    expect(api.getGoalRuns).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("已完成");
  });

  it("requests the next history page when pagination is clicked", async () => {
    api.getAgentAutonomyCycles.mockResolvedValue({
      items: [{ id: "g", objective: "目标", status: "active" }],
      total: 21,
    });
    const wrapper = mount(AutoWorkView, mountOptions);
    wrappers.push(wrapper);
    await flushPromises();

    const pageButtons = wrapper.findAll(".auto-work__pages button");
    await pageButtons[1].trigger("click");
    await flushPromises();
    expect(api.getAgentAutonomyCycles).toHaveBeenLastCalledWith("a", { page: 2, page_size: 20 });
  });
});
