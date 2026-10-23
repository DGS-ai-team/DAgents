/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import McpSettings from "./McpSettings.vue";
import * as api from "../../api/node.js";

vi.mock("../../api/node.js", () => ({
  getMcpConfig: vi.fn(),
  saveMcpConfig: vi.fn(),
  patchMcpServer: vi.fn(),
  refreshMcpServer: vi.fn(),
}));

const config = {
  mcpServers: {
    "server-a": {
      type: "streamable-http",
      url: "http://127.0.0.1:9001/mcp",
      headers: { Authorization: "__DAGENTS_SECRET_REDACTED__" },
      enabled: true,
      enabled_tools: ["echo"],
    },
  },
};

const server = (enabled = true) => ({
  id: "server-a",
  display_name: "Local fixture",
  transport: "streamable-http",
  status: "ready",
  enabled_tool_count: enabled ? 1 : 0,
  tool_count: 1,
  tools: [{ name: "echo", enabled }],
});

function result(configText = JSON.stringify(config), configuredServer = server()) {
  return { config_text: configText, servers: [configuredServer] };
}

function mountPage() {
  return mount(McpSettings, { global: { stubs: { SettingsPageHeader: true, UiIcon: true } } });
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.getMcpConfig.mockResolvedValue(result());
  api.saveMcpConfig.mockImplementation(async (text) => result(text));
  api.patchMcpServer.mockResolvedValue(server(false));
  api.refreshMcpServer.mockResolvedValue(server());
});

describe("MCP settings draft consistency", () => {
  it("does not offer or send the initial example when the first read fails", async () => {
    api.getMcpConfig.mockRejectedValueOnce(new Error("initial read failed"));
    const wrapper = mountPage();
    await flushPromises();

    const editor = wrapper.get('textarea[aria-label="MCP 原始配置"]');
    const saveButton = wrapper.find(".mcp-settings__editor .btn--primary");
    expect(editor.element.disabled).toBe(true);
    expect(editor.element.value).toBe("");
    expect(saveButton.element.disabled).toBe(true);
    await saveButton.trigger("click");
    expect(api.saveMcpConfig).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("initial read failed");
    wrapper.unmount();
  });

  it("blocks edits until the first read and merges tool patches without losing draft services or secrets", async () => {
    const initialRead = deferred();
    api.getMcpConfig.mockReturnValueOnce(initialRead.promise);
    const wrapper = mountPage();
    const editor = wrapper.get('textarea[aria-label="MCP 原始配置"]');
    const saveButton = wrapper.find(".mcp-settings__editor .btn--primary");
    expect(editor.element.disabled).toBe(true);
    expect(saveButton.element.disabled).toBe(true);
    await saveButton.trigger("click");
    expect(api.saveMcpConfig).not.toHaveBeenCalled();

    initialRead.resolve(result());
    await flushPromises();
    expect(editor.element.disabled).toBe(false);
    await wrapper.get(".mcp-settings__server-card").trigger("click");

    const draft = {
      mcpServers: {
        ...config.mcpServers,
        "server-new": {
          type: "stdio",
          command: "fixture-command",
          env: { FIXTURE_KEY: "synthetic-user-entered-secret" },
        },
      },
    };
    await editor.setValue(JSON.stringify(draft, null, 2));

    const patch = deferred();
    api.patchMcpServer.mockReturnValueOnce(patch.promise);
    await wrapper.get(".mcp-settings__tool-row input").setValue(false);
    expect(editor.element.disabled).toBe(true);
    expect(saveButton.element.disabled).toBe(true);
    await saveButton.trigger("click");
    expect(api.saveMcpConfig).not.toHaveBeenCalled();

    patch.resolve(server(false));
    await flushPromises();
    expect(editor.element.disabled).toBe(false);
    const merged = JSON.parse(editor.element.value);
    expect(merged.mcpServers["server-a"].enabled_tools).toEqual([]);
    expect(merged.mcpServers["server-new"].env.FIXTURE_KEY).toBe("synthetic-user-entered-secret");

    await saveButton.trigger("click");
    await flushPromises();
    const saved = JSON.parse(api.saveMcpConfig.mock.calls[0][0]);
    expect(saved.mcpServers["server-a"].enabled_tools).toEqual([]);
    expect(saved.mcpServers["server-new"].env.FIXTURE_KEY).toBe("synthetic-user-entered-secret");
    wrapper.unmount();
  });

  it("does not patch tools when the edited JSON is invalid or no longer contains that server", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get(".mcp-settings__server-card").trigger("click");
    const editor = wrapper.get('textarea[aria-label="MCP 原始配置"]');
    const toggle = wrapper.get(".mcp-settings__tool-row input");

    await editor.setValue('{"mcpServers":');
    expect(toggle.element.disabled).toBe(true);
    await toggle.setValue(false);
    expect(api.patchMcpServer).not.toHaveBeenCalled();

    await editor.setValue(JSON.stringify({ mcpServers: { custom: { type: "stdio", command: "local" } } }));
    expect(toggle.element.disabled).toBe(true);
    await toggle.setValue(false);
    expect(api.patchMcpServer).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps the edited config when an independent tool patch fails", async () => {
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.get(".mcp-settings__server-card").trigger("click");
    const editor = wrapper.get('textarea[aria-label="MCP 原始配置"]');
    const edited = JSON.stringify({ mcpServers: { ...config.mcpServers, custom: { type: "stdio", command: "local" } } }, null, 2);
    await editor.setValue(edited);
    api.patchMcpServer.mockRejectedValueOnce(new Error("synthetic patch failure"));

    await wrapper.get(".mcp-settings__tool-row input").setValue(false);
    await flushPromises();

    expect(editor.element.value).toBe(edited);
    expect(wrapper.get(".mcp-settings__tool-row input").element.checked).toBe(true);
    expect(api.getMcpConfig).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("synthetic patch failure");
    wrapper.unmount();
  });
});
