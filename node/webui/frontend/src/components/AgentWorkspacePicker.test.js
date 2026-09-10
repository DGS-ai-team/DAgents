/** @vitest-environment jsdom */
import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import AgentWorkspacePicker from "./AgentWorkspacePicker.vue";

function mountPicker(draft, fieldError = "") {
  return mount(AgentWorkspacePicker, {
    props: { draft, fieldError, "onClear-error": vi.fn() },
  });
}

describe("AgentWorkspacePicker", () => {
  it("accepts a Windows or Linux path directly in the browser", async () => {
    const draft = { workspaceMode: "custom", workspacePath: "" };
    const wrapper = mountPicker(draft);
    const pathInput = wrapper.get("#agent-workspace-path");

    expect(pathInput.attributes("placeholder")).toContain("C:\\Projects");
    expect(wrapper.find('button').exists()).toBe(false);

    await pathInput.setValue("D:\\workspace\\project");
    expect(draft.workspacePath).toBe("D:\\workspace\\project");

    await pathInput.setValue("/home/user/project");
    expect(draft.workspacePath).toBe("/home/user/project");
  });

  it("keeps the custom path requirement and clears it when switching to private", async () => {
    const draft = { workspaceMode: "custom", workspacePath: "/home/user/project" };
    const wrapper = mountPicker(draft, "输入一个本机目录路径");

    expect(wrapper.get("#agent-workspace-path").attributes("aria-invalid")).toBe("true");
    const privateOption = wrapper.get('input[type="radio"][value="private"]');
    await privateOption.setValue(true);

    expect(draft.workspaceMode).toBe("private");
    expect(draft.workspacePath).toBe("");
    expect(wrapper.find("#agent-workspace-path").exists()).toBe(false);
  });
});
