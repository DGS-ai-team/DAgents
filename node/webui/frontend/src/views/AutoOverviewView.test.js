/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AutoOverviewView from "./AutoOverviewView.vue";
import * as api from "../api/node.js";

const routerMock = vi.hoisted(() => ({ push: vi.fn() }));
const wrappers = [];
vi.mock("vue-router", () => ({ useRouter: () => routerMock }));
vi.mock("../components/NavRail.vue", () => ({ default: { template: "<nav />" } }));
vi.mock("../api/node.js", () => ({ getAutoOverview: vi.fn() }));

afterEach(() => {
  while (wrappers.length) wrappers.pop().unmount();
});

describe("AutoOverviewView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    routerMock.push.mockClear();
    api.getAutoOverview.mockResolvedValue({
      items: [{ agent_id: "auto-1", display_name: "研究员", agent_type: "auto", state: "needs_attention", state_reason: "approval_required", role_objective: "研究资料", last_summary: "已找到证据", tokens_used: 12, token_budget: 100 }],
      counts: { total: 1, running: 0, needs_attention: 1 }, total: 1, page: 1, page_size: 20,
    });
  });

  it("loads the aggregate endpoint and renders status, role, progress and usage", async () => {
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await flushPromises();
    expect(api.getAutoOverview).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }));
    expect(wrapper.text()).toContain("需处理");
    expect(wrapper.text()).toContain("研究资料");
    expect(wrapper.text()).toContain("已找到证据");
    expect(wrapper.text()).toContain("12 / 100 tokens");
  });

  it("preserves the page and offers retry after a failed load", async () => {
    api.getAutoOverview.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain("offline");
    api.getAutoOverview.mockResolvedValueOnce({ items: [], counts: {}, total: 0, page: 1, page_size: 20 });
    await wrapper.get('[role="alert"] button').trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("暂无符合条件");
  });

  it("applies a status filter when a count is clicked", async () => {
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await flushPromises();
    await wrapper.findAll(".auto-overview__count")[1].trigger("click");
    await flushPromises();
    expect(api.getAutoOverview).toHaveBeenLastCalledWith(expect.objectContaining({ status: "running" }));
    expect(wrapper.findAll(".auto-overview__count")[0].text()).toContain("1");
  });

  it("separates the work page action from the normal chat action", async () => {
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await flushPromises();
    const actions = wrapper.findAll(".auto-overview__actions button");

    await actions[0].trigger("click");
    expect(routerMock.push).toHaveBeenLastCalledWith({ name: "auto-work", params: { agentId: "auto-1" } });
    await actions[1].trigger("click");
    expect(routerMock.push).toHaveBeenLastCalledWith({ name: "agents", params: { agentId: "auto-1" } });
  });

  it("ignores a stale failed request after a newer request succeeds", async () => {
    let rejectOld;
    const old = new Promise((_, reject) => { rejectOld = reject; });
    api.getAutoOverview.mockReset();
    api.getAutoOverview.mockReturnValueOnce(old).mockResolvedValueOnce({ items: [{ agent_id: "new", display_name: "最新", agent_type: "auto", state: "standby" }], counts: {}, total: 1, page: 1, page_size: 20 });
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await wrapper.find(".auto-overview__filter-button").trigger("click");
    await flushPromises();
    rejectOld(new Error("stale"));
    await flushPromises();
    expect(wrapper.text()).toContain("最新");
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  });
});
