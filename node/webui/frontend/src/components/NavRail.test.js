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

describe("NavRail sections", () => {
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

  it("shows normal and autonomous agents as separate peer sections", async () => {
    const wrapper = mount(NavRail, { global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } } });
    await flushPromises();
    expect(wrapper.text()).toContain("自主智能体");
    expect(wrapper.text()).toContain("聊天");
    expect(wrapper.text()).toContain("巡检");
    expect(wrapper.findAll('input[type="search"]')).toHaveLength(0);
    expect(wrapper.findAll("select")).toHaveLength(0);
    const sections = wrapper.findAll(".nav-rail__section-title").map((node) => node.text());
    expect(sections.slice(0, 3)).toEqual(["智能体", "工作组", "自主智能体"]);
    const sectionNodes = wrapper.findAll(".nav-rail__section");
    expect(sectionNodes[0].find(".nav-rail__section-count").text()).toBe("1");
    expect(sectionNodes[2].find(".nav-rail__section-count").text()).toBe("1");
    expect(wrapper.findAll(".auto-badge")).toHaveLength(0);
    const autoRow = wrapper.findAll(".nav-rail__agent-item").at(-1);
    expect(autoRow.find('[title="重命名"]').exists()).toBe(true);
    expect(autoRow.find('[title="删除 Agent"]').exists()).toBe(true);
    await autoRow.trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("switch").at(-1)).toEqual(["auto-1"]);
    const switches = wrapper.emitted("switch").length;
    await autoRow.find('[title="智能体配置"]').trigger("click");
    expect(wrapper.emitted("switch")).toHaveLength(switches);
    await wrapper.findAll(".nav-rail__agent-item").at(-1).trigger("click");
    expect(wrapper.emitted("switch").at(-1)).toEqual(["auto-1"]);
    wrapper.unmount();
  });

  it("collapses each peer section with one accessible toggle", async () => {
    const wrapper = mount(NavRail, { global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } } });
    await flushPromises();
    const toggles = wrapper.findAll(".nav-rail__section-toggle");
    expect(toggles.length).toBeGreaterThanOrEqual(3);
    expect(wrapper.findAll(".nav-rail__section-collapse")).toHaveLength(0);
    await toggles[2].trigger("click");
    expect(toggles[2].attributes("aria-expanded")).toBe("false");
    expect(wrapper.text()).not.toContain("巡检");
    wrapper.unmount();
  });
});
