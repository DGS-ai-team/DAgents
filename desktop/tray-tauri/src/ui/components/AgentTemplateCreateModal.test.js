/** @vitest-environment jsdom */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import AgentTemplateCreateModal from "./AgentTemplateCreateModal.vue";
import UiSelect from "./UiSelect.vue";

const { createAgentTemplate, getSetupConfig } = vi.hoisted(() => ({
  createAgentTemplate: vi.fn(),
  getSetupConfig: vi.fn(),
}));

vi.mock("../api/node.js", () => ({ createAgentTemplate, getSetupConfig }));

const AgentSettingsFormStub = {
  components: { UiSelect },
  template: '<UiSelect :options="[{ value: \'profile\', label: \'Profile\' }]" />',
};

let wrapper;
let opener;

function mountModal(open = false) {
  wrapper = mount(AgentTemplateCreateModal, {
    attachTo: document.body,
    props: { open },
    global: { stubs: { AgentSettingsForm: AgentSettingsFormStub } },
  });
  return wrapper;
}

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  opener?.remove();
  opener = null;
  document.body.innerHTML = "";
  vi.resetAllMocks();
});

beforeEach(() => {
  getSetupConfig.mockResolvedValue({ llm: { profiles: [] } });
});

describe("AgentTemplateCreateModal focus and keyboard behavior", () => {
  it("focuses the template name on open and restores the opener after close", async () => {
    opener = document.createElement("button");
    opener.textContent = "新建模板";
    document.body.append(opener);
    opener.focus();
    mountModal();

    await wrapper.setProps({ open: true });
    await flushPromises();
    expect(document.activeElement).toBe(document.querySelector(".agent-template-meta input"));
    expect(document.body.textContent).toContain("模板名称");

    document.body.querySelector(".agent-create-modal__actions .btn--ghost").click();
    expect(wrapper.emitted("close")).toHaveLength(1);
    await wrapper.setProps({ open: false });
    await flushPromises();
    expect(document.activeElement).toBe(opener);
  });

  it("traps Tab and Shift+Tab inside the modal", async () => {
    mountModal(true);
    await flushPromises();
    const dialog = document.body.querySelector('[role="dialog"]');
    const focusables = [...dialog.querySelectorAll(
      'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])',
    )].filter((element) => !element.closest("[inert], [aria-hidden='true']"));
    expect(focusables.length).toBeGreaterThan(1);

    focusables.at(-1).focus();
    dialog.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", bubbles: true }));
    expect(document.activeElement).toBe(focusables[0]);

    focusables[0].focus();
    dialog.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", shiftKey: true, bubbles: true }));
    expect(document.activeElement).toBe(focusables.at(-1));
  });

  it("lets an open model selector consume Escape before closing the modal", async () => {
    mountModal(true);
    await flushPromises();
    document.body.querySelector(".ui-select__trigger").click();
    await flushPromises();
    expect(document.body.querySelector(".ui-select__menu")).toBeTruthy();

    document.body.querySelector(".ui-select__menu").dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    await flushPromises();
    expect(document.body.querySelector(".ui-select__menu")).toBeNull();
    expect(document.body.querySelector('[role="dialog"]')).toBeTruthy();
    expect(wrapper.emitted("close")).toBeUndefined();

    document.body.querySelector('[role="dialog"]').dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(wrapper.emitted("close")).toHaveLength(1);
  });

  it("does not dismiss while the template is saving", async () => {
    let finishCreate;
    createAgentTemplate.mockImplementation(() => new Promise((resolve) => { finishCreate = resolve; }));
    mountModal(true);
    await flushPromises();
    const templateName = document.body.querySelector(".agent-template-meta input");
    templateName.value = "测试模板";
    templateName.dispatchEvent(new Event("input", { bubbles: true }));
    await flushPromises();
    document.body.querySelector(".agent-create-modal__actions .btn--primary").click();
    await flushPromises();
    expect(createAgentTemplate).toHaveBeenCalledOnce();

    document.body.querySelector('[role="dialog"]').dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    document.body.querySelector(".agent-create-overlay").dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(wrapper.emitted("close")).toBeUndefined();

    finishCreate({ id: "template-id" });
    await flushPromises();
    expect(wrapper.emitted("close")).toHaveLength(1);
  });
});
