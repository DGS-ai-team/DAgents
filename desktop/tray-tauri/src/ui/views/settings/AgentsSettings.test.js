/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentsSettings from "./AgentsSettings.vue";
import * as api from "../../api/node.js";

vi.mock("vue-router", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("../../api/node.js", () => ({ listAgents: vi.fn(), listAgentTemplates: vi.fn(), getUIBootstrap: vi.fn(), deleteAgentTemplate: vi.fn() }));

describe("AgentsSettings agent view", () => {
  beforeEach(() => {
    localStorage.clear();
    api.listAgents.mockResolvedValue({ agents: [{ agent_id: "auto-1", display_name: "巡检", agent_type: "auto" }] });
    api.listAgentTemplates.mockResolvedValue({ templates: [] });
    api.getUIBootstrap.mockResolvedValue({ info: { node_id: "node-settings" } });
  });

  it("shows an explicit empty result when search matches no Agent", async () => {
    const wrapper = mount(AgentsSettings, { global: { stubs: { RouterLink: { template: "<a><slot /></a>" }, AgentTemplateCreateModal: true } } });
    await flushPromises();
    await wrapper.get('input[aria-label="搜索智能体"]').setValue("不存在");
    expect(wrapper.text()).toContain("暂无符合条件的智能体");
    wrapper.unmount();
  });
});
