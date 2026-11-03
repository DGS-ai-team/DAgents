// @vitest-environment jsdom
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import McpAgentPanel from "./McpAgentPanel.vue";
import * as api from "../api/node.js";

const { routerPush } = vi.hoisted(() => ({ routerPush: vi.fn() }));

vi.mock("vue-router", () => ({ useRouter: () => ({ push: routerPush }) }));

vi.mock("../api/node.js", () => ({
  listMcpServers: vi.fn(),
  getAgentMcp: vi.fn(),
  putAgentMcp: vi.fn(),
  refreshMcpServer: vi.fn(),
}));

const servers = [
  {
    id: "server-a",
    display_name: "Server A",
    enabled: true,
    status: "ready",
    enabled_tool_count: 1,
    tools: [
      { name: "read_file", enabled: true },
      { name: "write_file", enabled: true },
    ],
  },
];

const bindings = [{ server_id: "server-a", enabled: true, tool_allowlist: [] }];

function dispatchConfigurationChange(kind) {
  window.dispatchEvent(new CustomEvent("dagents:configuration-changed", { detail: { kind } }));
}

beforeEach(() => {
  vi.clearAllMocks();
  api.listMcpServers.mockResolvedValue({ servers });
  api.getAgentMcp.mockResolvedValue({ bindings });
  api.putAgentMcp.mockResolvedValue({});
  api.refreshMcpServer.mockResolvedValue(servers[0]);
});

describe("McpAgentPanel draft protection", () => {
  it("preserves unsaved bindings when configuration events refresh the panel", async () => {
    const wrapper = mount(McpAgentPanel, { props: { agentId: "agent-a" } });
    await flushPromises();

    expect(wrapper.get('.mcp-panel__server-toggle input[type="checkbox"]').element.checked).toBe(true);
    const readFileToggle = wrapper.get('.mcp-panel__tool input[type="checkbox"]');
    await readFileToggle.setValue(false);
    dispatchConfigurationChange("mcp");
    await flushPromises();

    expect(api.listMcpServers).toHaveBeenCalledTimes(2);
    expect(api.getAgentMcp).toHaveBeenCalledTimes(2);
    expect(wrapper.get('.mcp-panel__server-toggle input[type="checkbox"]').element.checked).toBe(true);
    const toolToggles = wrapper.findAll('.mcp-panel__tool input[type="checkbox"]');
    expect(toolToggles[0].element.checked).toBe(false);
    expect(toolToggles[1].element.checked).toBe(true);
    expect(wrapper.text()).toContain("未保存的绑定草稿已保留");

    await wrapper.get(".btn--primary").trigger("click");
    await flushPromises();
    expect(api.putAgentMcp).toHaveBeenCalledWith("agent-a", [
      { server_id: "server-a", enabled: true, tool_allowlist: ["write_file"] },
    ]);
    expect(wrapper.text()).toContain("MCP 已保存并应用");

    dispatchConfigurationChange("mcp-catalog");
    await flushPromises();
    expect(wrapper.get('.mcp-panel__server-toggle input[type="checkbox"]').element.checked).toBe(true);
    expect(wrapper.get('.mcp-panel__tool input[type="checkbox"]').element.checked).toBe(true);
    wrapper.unmount();
  });

  it("opens global MCP setup with a return target for this agent", async () => {
    api.listMcpServers.mockResolvedValueOnce({ servers: [] });
    api.getAgentMcp.mockResolvedValueOnce({ bindings: [] });
    const wrapper = mount(McpAgentPanel, { props: { agentId: "agent-a" } });
    await flushPromises();

    await wrapper.get(".mcp-panel__empty button").trigger("click");
    expect(routerPush).toHaveBeenCalledWith({
      path: "/settings/mcp",
      query: { returnAgentId: "agent-a", returnSection: "resources", create: "1" },
    });
    wrapper.unmount();
  });
});
