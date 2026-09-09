/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AutoTodoPanel from "./AutoTodoPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({
  listAgentTodos: vi.fn(), createAgentTodo: vi.fn(), updateAgentTodo: vi.fn(), deleteAgentTodo: vi.fn(),
}));

beforeEach(() => {
  vi.clearAllMocks();
  api.listAgentTodos.mockResolvedValue({ todos: [{ id: "t1", text: "原任务", status: "pending", revision: 1 }] });
  api.createAgentTodo.mockResolvedValue({ id: "t2", text: "新任务", status: "pending", revision: 1 });
  api.updateAgentTodo.mockResolvedValue({ id: "t1", text: "更新", status: "completed", revision: 2 });
  api.deleteAgentTodo.mockResolvedValue({ deleted: true });
});

describe("AutoTodoPanel", () => {
  it("loads and edits text/status while preserving the other field", async () => {
    const wrapper = mount(AutoTodoPanel, { props: { agentId: "a1" } });
    await flushPromises();
    await wrapper.get('input[aria-label="待办文本"]').setValue("更新");
    await wrapper.get('select[aria-label="待办状态"]').setValue("completed");
    await wrapper.get(".auto-todo-list .btn").trigger("click");
    await flushPromises();
    expect(api.updateAgentTodo).toHaveBeenCalledWith("a1", "t1", { expected_revision: 1, text: "更新", status: "completed" });
    expect(wrapper.get('input[aria-label="待办文本"]').element.value).toBe("更新");
  });

  it("shows CAS conflict and keeps the draft", async () => {
    api.updateAgentTodo.mockRejectedValueOnce(Object.assign(new Error("conflict"), { status: 409 }));
    const wrapper = mount(AutoTodoPanel, { props: { agentId: "a1" } });
    await flushPromises();
    await wrapper.get('input[aria-label="待办文本"]').setValue("我的草稿");
    await wrapper.get(".auto-todo-list .btn").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("更新");
    expect(wrapper.get('input[aria-label="待办文本"]').element.value).toBe("我的草稿");
    api.listAgentTodos.mockResolvedValueOnce({ todos: [{ id: "t1", text: "服务器版本", status: "pending", revision: 2 }] });
    await wrapper.get('[role="alert"] button').trigger("click");
    await flushPromises();
    expect(wrapper.get('input[aria-label="待办文本"]').element.value).toBe("我的草稿");
    expect(api.listAgentTodos).toHaveBeenCalledWith("a1");
  });

  it("ignores a late response from the previous agent", async () => {
    let resolveOld;
    api.listAgentTodos.mockReset();
    api.listAgentTodos.mockReturnValueOnce(new Promise((resolve) => { resolveOld = resolve; }))
      .mockResolvedValueOnce({ todos: [{ id: "new", text: "新 Agent", status: "pending", revision: 1 }] });
    const wrapper = mount(AutoTodoPanel, { props: { agentId: "old" } });
    await wrapper.setProps({ agentId: "new" });
    await flushPromises();
    resolveOld({ todos: [{ id: "old", text: "旧 Agent", status: "pending", revision: 1 }] });
    await flushPromises();
    expect(wrapper.get('input[aria-label="待办文本"]').element.value).toBe("新 Agent");
    expect(wrapper.get('input[aria-label="待办文本"]').element.value).not.toBe("旧 Agent");
  });

  it("clears the old draft and saving lock when switching agents during a save", async () => {
    let resolveSave;
    api.updateAgentTodo.mockReturnValueOnce(new Promise((resolve) => { resolveSave = resolve; }));
    api.listAgentTodos.mockResolvedValue({ todos: [{ id: "t1", text: "旧", status: "pending", revision: 1 }] });
    const wrapper = mount(AutoTodoPanel, { props: { agentId: "old" } });
    await flushPromises();
    await wrapper.get('input[aria-label="待办文本"]').setValue("旧草稿");
    await wrapper.get(".auto-todo-list .btn").trigger("click");
    await wrapper.setProps({ agentId: "new" });
    await flushPromises();
    expect(wrapper.get('input[aria-label="新待办"]').element.disabled).toBe(false);
    expect(wrapper.get('input[aria-label="待办文本"]').element.value).not.toBe("旧草稿");
    resolveSave({ id: "t1", text: "旧草稿", status: "pending", revision: 2 });
  });

  it("offers retry after load failure and blocks create while loading", async () => {
    api.listAgentTodos.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mount(AutoTodoPanel, { props: { agentId: "a1" } });
    expect(wrapper.get('button[type="submit"]').element.disabled).toBe(true);
    await flushPromises();
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain("offline");
    expect(wrapper.get('[role="alert"] button').text()).toContain("重试");
    api.listAgentTodos.mockResolvedValueOnce({ todos: [] });
    await wrapper.get('[role="alert"] button').trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("暂无待办事项");
  });

  it("keeps writes disabled after a failed load and exposes a deleted conflict draft for recovery", async () => {
    api.listAgentTodos.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mount(AutoTodoPanel, { props: { agentId: "a1" } });
    await flushPromises();
    await wrapper.get('input[aria-label="新待办"]').setValue("待恢复草稿");
    expect(wrapper.get('button[type="submit"]').element.disabled).toBe(true);

    api.listAgentTodos.mockResolvedValueOnce({ todos: [{ id: "t1", text: "原任务", status: "pending", revision: 1 }] });
    await wrapper.get('[role="alert"] button').trigger("click");
    await flushPromises();
    await wrapper.get('input[aria-label="待办文本"]').setValue("本地草稿");
    api.updateAgentTodo.mockRejectedValueOnce(Object.assign(new Error("conflict"), { status: 409 }));
    await wrapper.get(".auto-todo-list .btn").trigger("click");
    await flushPromises();
    api.listAgentTodos.mockResolvedValueOnce({ todos: [] });
    await wrapper.get('[role="alert"] button').trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("本地草稿");
    await wrapper.get('.auto-todo-panel__recovery button').trigger("click");
    expect(wrapper.get('input[aria-label="新待办"]').element.value).toBe("本地草稿");
  });
});
