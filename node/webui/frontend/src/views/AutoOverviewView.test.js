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
      items: [{ agent_id: "auto-1", display_name: "研究员", agent_type: "auto", state: "needs_attention", state_reason: "需要处理", next_at: "2026-09-09T12:00:00Z", todo_summary: ["核对资料", "更新手册"] }], counts: { total: 1, working: 0, needs_attention: 1 }, total: 1, page: 1, page_size: 20,
    });
  });

  it("loads the overview endpoint and renders status, next check and todos", async () => {
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await flushPromises();
    expect(api.getAutoOverview).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }));
    expect(wrapper.text()).toContain("需处理");
    expect(wrapper.text()).toContain("核对资料；更新手册");
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
    expect(wrapper.text()).toContain("暂无 Auto Agent");
  });

  it("opens the normal chat or settings", async () => {
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await flushPromises();
    const actions = wrapper.findAll(".auto-overview__actions button");

    await actions[0].trigger("click");
    expect(routerMock.push).toHaveBeenLastCalledWith({ name: "agents", params: { agentId: "auto-1" } });
    await actions[1].trigger("click");
    expect(routerMock.push).toHaveBeenLastCalledWith({ name: "settings-agent-detail", params: { agentId: "auto-1" }, query: { section: "autonomy" } });
  });

  it("keeps search, status filters and pagination", async () => {
    api.getAutoOverview.mockResolvedValueOnce({ items: [{ agent_id: "a1", state: "working" }], counts: { total: 21, working: 21 }, total: 21, page: 1, page_size: 20 });
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await flushPromises();
    api.getAutoOverview.mockResolvedValueOnce({ items: [{ agent_id: "a1", state: "working" }], counts: { total: 21, working: 21 }, total: 21, page: 1, page_size: 20 });
    api.getAutoOverview.mockResolvedValueOnce({ items: [{ agent_id: "a1", state: "working" }], counts: { total: 21, working: 21 }, total: 21, page: 1, page_size: 20 });
    await wrapper.get('[aria-label="搜索 Auto Agent"]').setValue("研究");
    await wrapper.get('[aria-label="状态筛选"]').setValue("working");
    await wrapper.get(".auto-overview__filter-button").trigger("click");
    await flushPromises();
    expect(api.getAutoOverview).toHaveBeenLastCalledWith(expect.objectContaining({ search: "研究", status: "working", page: 1 }));
    api.getAutoOverview.mockResolvedValueOnce({ items: [{ agent_id: "a2", state: "standby" }], total: 21, page: 2, page_size: 20 });
    await wrapper.get(".auto-overview__pagination button:last-child").trigger("click");
    await flushPromises();
    expect(api.getAutoOverview).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 20 }));
  });

  it("ignores a stale failed request after a newer request succeeds", async () => {
    let rejectOld;
    const old = new Promise((_, reject) => { rejectOld = reject; });
    api.getAutoOverview.mockReset();
    api.getAutoOverview.mockReturnValueOnce(old).mockResolvedValueOnce({ items: [{ agent_id: "new", display_name: "最新", agent_type: "auto", state: "standby" }] });
    const wrapper = mount(AutoOverviewView);
    wrappers.push(wrapper);
    await wrapper.get(".auto-overview__refresh").trigger("click");
    await flushPromises();
    rejectOld(new Error("stale"));
    await flushPromises();
    expect(wrapper.text()).toContain("最新");
    expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  });
});
