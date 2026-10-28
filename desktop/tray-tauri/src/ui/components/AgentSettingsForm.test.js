/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { reactive } from "vue";
import { describe, expect, it, vi } from "vitest";
import AgentSettingsForm from "./AgentSettingsForm.vue";
import ToolGroupIcon from "./ToolGroupIcon.vue";

vi.mock("../api/node.js", () => ({
  listNodeSkillsCatalog: vi.fn().mockResolvedValue({ available_skills: [] }),
}));

const IconStub = {
  props: ["name", "size"],
  template: '<i :data-icon="name" :data-size="size"></i>',
};

const toolGroupIconStub = {
  props: ["name"],
  template: '<i class="test-tool-group-icon" :data-name="name"></i>',
};

function mountForm({ mode, showAdvanced = false, draft, groups }) {
  return mount(AgentSettingsForm, {
    props: {
      draft,
      mode,
      showAdvanced,
      availableToolGroups: groups,
    },
    global: {
      stubs: {
        ToolGroupIcon: toolGroupIconStub,
        UiSelect: true,
      },
    },
  });
}

describe("AgentSettingsForm tool group tiles", () => {
  it("exposes the per-turn tool-call cap and updates the draft", async () => {
    const draft = reactive({
      agentType: "normal",
      displayName: "助手",
      description: "",
      llmProfileId: "default",
      maxToolCalls: 64,
      toolGroups: [],
      visibleSkills: null,
      promptSoulMd: "",
      promptCustomMd: "",
      promptMemoryScope: "agent",
    });
    const wrapper = mountForm({ mode: "full", draft, groups: [] });
    const input = wrapper.get('[aria-label="每轮最大工具调用数"]');

    expect(input.element.value).toBe("64");
    expect(wrapper.text()).toContain("Auto 自主激活与 dreaming 使用各自的轮次上限");
    await input.setValue("12");
    expect(draft.maxToolCalls).toBe(12);
    wrapper.unmount();
  });

  it("toggles selected tool-group data and aria state in the create flow", async () => {
    const draft = reactive({ toolGroups: [] });
    const wrapper = mountForm({
      mode: "create-capabilities",
      draft,
      groups: [{ name: "fs", label: "文件" }],
    });
    const tile = wrapper.get(".agent-settings-tile");

    expect(tile.attributes("aria-pressed")).toBe("false");
    await tile.trigger("click");
    expect(draft.toolGroups).toEqual(["fs"]);
    expect(tile.attributes("aria-pressed")).toBe("true");
    await tile.trigger("click");
    expect(draft.toolGroups).toEqual([]);
    expect(tile.attributes("aria-pressed")).toBe("false");
    wrapper.unmount();
  });

  it("keeps a fixed icon slot before centered text for long labels in both forms", () => {
    const label = "A long tool group label that can wrap naturally";
    for (const mode of ["create-capabilities", "full"]) {
      const wrapper = mountForm({
        mode,
        showAdvanced: true,
        draft: reactive({ toolGroups: [], visibleSkills: null }),
        groups: [{ name: "fs", label }],
      });
      const tile = wrapper.get(mode === "full" ? ".agent-settings-tile--compact" : ".agent-settings-tile");
      const children = tile.element.children;

      expect(children[0].classList.contains("agent-settings-tile__icon")).toBe(true);
      expect(children[1].classList.contains("agent-settings-tile__label")).toBe(true);
      expect(children[1].textContent).toContain(label);
      expect(tile.attributes("aria-pressed")).toBe("false");
      wrapper.unmount();
    }
  });
});

describe("ToolGroupIcon semantic aliases", () => {
  it.each([
    ["bash", "code-2"],
    ["shell", "code-2"],
    ["terminal", "terminal"],
    ["child_agents", "bot"],
    ["child", "bot"],
    ["mcp", "network"],
    ["memory", "brain"],
    ["wecom", "wecom"],
    ["hitl", "message-circle"],
  ])("maps %s to the shared %s icon", (name, expected) => {
    const wrapper = mount(ToolGroupIcon, {
      props: { name },
      global: { stubs: { UiIcon: IconStub } },
    });

    expect(wrapper.get("[data-icon]").attributes("data-icon")).toBe(expected);
    wrapper.unmount();
  });
});
