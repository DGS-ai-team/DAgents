/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentDetailSettings from "./AgentDetailSettings.vue";
import * as api from "../../api/node.js";

vi.mock("vue-router", () => ({
  useRoute: () => globalThis.__agentDetailSettingsRoute,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));
vi.mock("../../api/node.js", () => ({
  getAgent: vi.fn(),
  getSetupConfig: vi.fn(),
  getAgentPromptContext: vi.fn(),
  patchAgent: vi.fn(),
  putAgentPromptContext: vi.fn(),
  reloadAgentRuntime: vi.fn(),
}));

const agent = (id) => ({
  agent_id: id,
  display_name: `Writer ${id}`,
  agent_type: "normal",
  config_snapshot: { defaults: { llm: { active: "profile-a" }, tools: { enabled_groups: [] } } },
});

function mountPage() {
  return mount(AgentDetailSettings, {
    global: {
      stubs: {
        UiIcon: true,
        AgentSettingsForm: {
          props: ["draft"],
          template: '<textarea aria-label="提示词草稿" v-model="draft.promptCustomMd"></textarea><output aria-label="工具组">{{ draft.toolGroups.join(",") }}</output>',
        },
        PolicyPanel: true,
        McpAgentPanel: true,
        LinuxAgentPanel: true,
        MemoryPanel: true,
        SimplifiedAutoPanel: true,
        AgentHandbookPanel: true,
      },
    },
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  globalThis.__agentDetailSettingsRoute = reactive({ params: { agentId: "agent-a" }, query: {} });
  api.getAgent.mockImplementation(async (id) => agent(id));
  api.getSetupConfig.mockResolvedValue({ llm: { profiles: [{ id: "profile-a" }] }, features: {} });
  api.getAgentPromptContext.mockResolvedValue({ soul_md: "existing soul", custom_md: "existing prompt", memory_scope: "agent" });
  api.patchAgent.mockImplementation(async (id) => agent(id));
  api.putAgentPromptContext.mockResolvedValue({});
  api.reloadAgentRuntime.mockResolvedValue({});
});

describe("AgentDetailSettings staged persistence", () => {
  it("keeps prompt edits and explains confirmed base save when prompt save fails", async () => {
    api.putAgentPromptContext.mockRejectedValueOnce(new Error("prompt write failed"));
    const wrapper = mountPage();
    await flushPromises();
    const prompt = wrapper.get('textarea[aria-label="提示词草稿"]');
    await prompt.setValue("unsaved prompt draft");
    await wrapper.get(".agent-detail__actions .btn--primary").trigger("click");
    await flushPromises();

    expect(api.patchAgent).toHaveBeenCalledWith("agent-a", expect.any(Object));
    expect(api.putAgentPromptContext).toHaveBeenCalledWith("agent-a", expect.objectContaining({ custom_md: "unsaved prompt draft" }));
    expect(api.reloadAgentRuntime).not.toHaveBeenCalled();
    expect(wrapper.get('textarea[aria-label="提示词草稿"]').element.value).toBe("unsaved prompt draft");
    expect(wrapper.get('[role="alert"]').text()).toContain("智能体配置已保存，但提示词保存结果未确认");
    wrapper.unmount();
  });

  it("blocks all saves when prompt context could not be loaded", async () => {
    api.getAgentPromptContext.mockRejectedValueOnce(new Error("context unavailable"));
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("已阻止保存以免覆盖现有内容");
    expect(wrapper.get(".agent-detail__actions .btn--primary").element.disabled).toBe(true);
    expect(api.patchAgent).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("preserves existing Agent tool groups and blocks save when setup catalog loading fails", async () => {
    api.getAgent.mockResolvedValueOnce({
      ...agent("agent-a"),
      config_snapshot: { defaults: { llm: { active: "profile-a" }, tools: { enabled_groups: ["fs"] } } },
    });
    api.getSetupConfig.mockRejectedValueOnce(new Error("setup unavailable"));
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.get('[aria-label="工具组"]').text()).toBe("fs");
    expect(wrapper.text()).toContain("已保留 Agent 现有工具组并阻止保存");
    expect(wrapper.get(".agent-detail__actions .btn--primary").element.disabled).toBe(true);
    expect(api.patchAgent).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps all asynchronous save stages on the Agent selected when save began", async () => {
    let releasePrompt;
    api.putAgentPromptContext.mockImplementationOnce(() => new Promise((resolve) => { releasePrompt = resolve; }));
    const wrapper = mountPage();
    await flushPromises();
    const pending = wrapper.get(".agent-detail__actions .btn--primary").trigger("click");
    await flushPromises();
    globalThis.__agentDetailSettingsRoute.params.agentId = "agent-b";
    await flushPromises();
    releasePrompt({});
    await pending;
    await flushPromises();

    expect(api.patchAgent).toHaveBeenCalledWith("agent-a", expect.any(Object));
    expect(api.putAgentPromptContext).toHaveBeenCalledWith("agent-a", expect.any(Object));
    expect(api.reloadAgentRuntime).toHaveBeenCalledWith("agent-a");
    expect(wrapper.text()).toContain("Writer agent-b");
    wrapper.unmount();
  });
});
