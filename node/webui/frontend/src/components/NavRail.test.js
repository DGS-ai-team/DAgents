/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import NavRail from "./NavRail.vue";
import * as api from "../api/node.js";

vi.mock("vue-router", () => ({ useRoute: () => ({ name: "agents", params: {} }), useRouter: () => ({ push: vi.fn() }) }));
vi.mock("../api/node.js", () => ({
  listAgents: vi.fn(), listWorkgroups: vi.fn(), getUIBootstrap: vi.fn(), getWorkgroupTimeline: vi.fn(), listWorkgroupMembers: vi.fn(),
  patchAgent: vi.fn(), createWorkgroup: vi.fn(), unsubscribeWorkgroup: vi.fn(), archiveWorkgroupMember: vi.fn(),
}));
vi.mock("../stores/agent.js", () => ({ agentStore: { agentId: "normal-1" }, persistAgentId: vi.fn() }));
vi.mock("../stores/chrome.js", () => ({ chromeStore: { sseStatus: "connected" } }));
vi.mock("../stores/theme.js", () => ({ themeStore: { mode: "dark", resolved: "dark" }, cycleTheme: vi.fn() }));
vi.mock("../stores/unread.js", () => ({ hasWorkgroupUnread: () => false, noteWorkgroupTimeline: vi.fn() }));

describe("NavRail agent grouping controls", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    api.getUIBootstrap.mockResolvedValue({ info: { node_id: "node-test" } });
    api.listAgents.mockResolvedValue({ agents: [
      { agent_id: "auto-1", display_name: "巡检", agent_type: "auto", workspace: { mode: "private" } },
      { agent_id: "normal-1", display_name: "聊天", agent_type: "normal", workspace: { mode: "custom", path: "D:/work" } },
    ] });
    api.listWorkgroups.mockResolvedValue({ workgroups: [] });
  });

  it("filters, collapses a group, and selects the original Agent ID", async () => {
    const wrapper = mount(NavRail, { global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } } });
    await flushPromises();
    expect(wrapper.text()).toContain("自主智能体");
    await wrapper.get('select[aria-label="智能体类型筛选"]').setValue("auto");
    expect(wrapper.text()).toContain("巡检");
    expect(wrapper.text()).not.toContain("聊天");
    await wrapper.get(".nav-rail__agent-group-toggle").trigger("click");
    expect(wrapper.text()).not.toContain("巡检");
    await wrapper.get('select[aria-label="智能体类型筛选"]').setValue("all");
    await wrapper.get(".nav-rail__agent-group-toggle").trigger("click");
    await wrapper.findAll(".nav-rail__agent-item")[0].trigger("click");
    expect(wrapper.emitted("switch").at(-1)).toEqual(["auto-1"]);
    wrapper.unmount();
  });

  it("does not carry Node A preferences into Node B and restores A on remount", async () => {
    api.getUIBootstrap.mockResolvedValueOnce({ info: { node_id: "node-a" } });
    const first = mount(NavRail, { global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } } });
    await flushPromises();
    await first.get('select[aria-label="智能体类型筛选"]').setValue("auto");
    first.unmount();
    api.getUIBootstrap.mockResolvedValueOnce({ info: { node_id: "node-b" } });
    const second = mount(NavRail, { global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } } });
    await flushPromises();
    expect(second.get('select[aria-label="智能体类型筛选"]').element.value).toBe("all");
    second.unmount();
    api.getUIBootstrap.mockResolvedValueOnce({ info: { node_id: "node-a" } });
    const restored = mount(NavRail, { global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } } });
    await flushPromises();
    expect(restored.get('select[aria-label="智能体类型筛选"]').element.value).toBe("auto");
    restored.unmount();
  });
});
