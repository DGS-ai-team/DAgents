// @vitest-environment jsdom
import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SettingsLayout from "./SettingsLayout.vue";
import AgentDetailSettings from "../views/settings/AgentDetailSettings.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({
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

let memoryMountCount = 0;
const MemoryPanelStub = defineComponent({
  setup() {
    memoryMountCount += 1;
    const memoryDraft = ref("initial memory draft");
    return { memoryDraft };
  },
  template: '<div class="memory-test"><textarea aria-label="记忆草稿" v-model="memoryDraft"></textarea></div>',
});

function createSettingsRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/settings",
        component: SettingsLayout,
        children: [{ path: "agents/:agentId", name: "settings-agent-detail", component: AgentDetailSettings }],
      },
    ],
  });
}

function installLayoutBrowserAPIs() {
  vi.stubGlobal("matchMedia", () => ({ matches: false, addListener() {}, removeListener() {} }));
  vi.stubGlobal("requestAnimationFrame", (callback) => window.setTimeout(callback, 0));
  vi.stubGlobal("cancelAnimationFrame", (id) => window.clearTimeout(id));
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback) {
      this.callback = callback;
    }

    observe() {}
    disconnect() {}
  });
}

function mountSettings(router) {
  return mount(SettingsLayout, {
    global: {
      plugins: [router],
      stubs: {
        UiIcon: true,
        RouterLink: { props: ["to"], template: "<a><slot /></a>" },
        AgentSettingsForm: {
          props: ["draft"],
          template: '<textarea aria-label="提示词草稿" v-model="draft.promptCustomMd"></textarea>',
        },
        PolicyPanel: true,
        McpAgentPanel: true,
        LinuxAgentPanel: true,
        MemoryPanel: MemoryPanelStub,
        SimplifiedAutoPanel: true,
        AgentHandbookPanel: true,
      },
    },
  });
}

beforeEach(() => {
  installLayoutBrowserAPIs();
  vi.clearAllMocks();
  memoryMountCount = 0;
  api.getAgent.mockImplementation(async (id) => agent(id));
  api.getSetupConfig.mockResolvedValue({ llm: { profiles: [{ id: "profile-a" }] }, features: {} });
  api.getAgentPromptContext.mockResolvedValue({ soul_md: "existing soul", custom_md: "existing prompt", memory_scope: "agent" });
  api.patchAgent.mockImplementation(async (id) => agent(id));
  api.putAgentPromptContext.mockResolvedValue({});
  api.reloadAgentRuntime.mockResolvedValue({});
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("settings layout Agent detail routing", () => {
  it("keeps the Agent page mounted across sections and saves its draft once", async () => {
    const router = createSettingsRouter();
    await router.push({ name: "settings-agent-detail", params: { agentId: "agent-a" }, query: { section: "behavior" } });
    await router.isReady();
    const wrapper = mountSettings(router);
    await flushPromises();

    const header = wrapper.get(".settings-page__header").element;
    const prompt = wrapper.get('textarea[aria-label="提示词草稿"]');
    await prompt.setValue("draft across section route");
    expect(memoryMountCount).toBe(0);
    const sectionButton = (label) => wrapper.findAll(".agent-detail__subnav-item").find((button) => button.text() === label);
    await sectionButton("记忆").trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.query.section).toBe("memory");
    expect(wrapper.get(".settings-page__header").element).toBe(header);
    expect(wrapper.find(".memory-test").exists()).toBe(true);
    expect(memoryMountCount).toBe(1);
    await wrapper.get('textarea[aria-label="记忆草稿"]').setValue("memory edit kept in place");

    await router.push({ query: { section: "resources" } });
    await flushPromises();
    expect(wrapper.get(".settings-page__header").element).toBe(header);
    const visibleSection = () => wrapper.findAll(".agent-detail__section").find((section) => section.isVisible());
    expect(visibleSection().get(".settings-section-header__title").text()).toBe("连接与资源");
    await router.push({ query: { section: "policy" } });
    await flushPromises();
    expect(wrapper.get(".settings-page__header").element).toBe(header);
    await router.push({ query: { section: "behavior" } });
    await flushPromises();
    expect(wrapper.get(".settings-page__header").element).toBe(header);
    expect(api.getAgent).toHaveBeenCalledTimes(1);
    expect(api.getSetupConfig).toHaveBeenCalledTimes(1);
    expect(api.getAgentPromptContext).toHaveBeenCalledTimes(1);

    expect(memoryMountCount).toBe(1);
    expect(wrapper.get('textarea[aria-label="提示词草稿"]').element.value).toBe("draft across section route");
    await wrapper.get(".settings-savebar .btn--primary").trigger("click");
    await flushPromises();
    expect(api.putAgentPromptContext).toHaveBeenCalledWith(
      "agent-a",
      expect.objectContaining({ custom_md: "draft across section route" }),
    );
    expect(api.getAgent).toHaveBeenCalledTimes(1);

    await router.back();
    await router.back();
    await router.back();
    await flushPromises();
    expect(router.currentRoute.value.query.section).toBe("memory");
    expect(wrapper.find(".memory-test").exists()).toBe(true);
    expect(wrapper.get('textarea[aria-label="记忆草稿"]').element.value).toBe("memory edit kept in place");
    expect(memoryMountCount).toBe(1);
    wrapper.unmount();
  });

  it("uses a direct section query and creates a fresh editor when the Agent changes", async () => {
    const router = createSettingsRouter();
    await router.push({ name: "settings-agent-detail", params: { agentId: "agent-a" }, query: { section: "memory" } });
    await router.isReady();
    const wrapper = mountSettings(router);
    await flushPromises();
    expect(wrapper.find(".memory-test").exists()).toBe(true);
    await wrapper.get('textarea[aria-label="提示词草稿"]').setValue("agent-a unsaved draft");

    await router.push({ name: "settings-agent-detail", params: { agentId: "agent-b" }, query: { section: "behavior" } });
    await flushPromises();
    expect(api.getAgent).toHaveBeenNthCalledWith(2, "agent-b");
    expect(api.getAgent).toHaveBeenCalledTimes(2);
    expect(api.getSetupConfig).toHaveBeenCalledTimes(2);
    expect(api.getAgentPromptContext).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("Writer agent-b");
    expect(wrapper.get('textarea[aria-label="提示词草稿"]').element.value).toBe("existing prompt");
    expect(api.getSetupConfig).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("normalizes an unsupported autonomy section for a regular Agent", async () => {
    const router = createSettingsRouter();
    await router.push({ name: "settings-agent-detail", params: { agentId: "agent-a" }, query: { section: "autonomy" } });
    await router.isReady();
    const wrapper = mountSettings(router);
    await flushPromises();

    expect(router.currentRoute.value.query.section).toBe("behavior");
    expect(wrapper.get(".settings-section-header__title").text()).toBe("基本设置");
    expect(wrapper.findAll(".agent-detail__subnav-item").map((button) => button.text())).not.toContain("Auto 设置");
    wrapper.unmount();
  });
});
