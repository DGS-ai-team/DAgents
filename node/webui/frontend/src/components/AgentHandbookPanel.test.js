// @vitest-environment jsdom
import { mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import AgentHandbookPanel from "./AgentHandbookPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({
  getAgentHandbook: vi.fn(),
  getAgentHandbookHistory: vi.fn(),
  restoreAgentHandbook: vi.fn(),
  patchAgent: vi.fn(),
}));

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const root = {
  is_dir: true,
  directory: "C:/resolved/handbook",
  items: [{ name: "README.md", path: "README.md", is_dir: false }],
};
const file = {
  is_dir: false,
  content: "手册正文",
  digest: "digest-2",
  directory: "C:/resolved/handbook",
};

afterEach(() => vi.clearAllMocks());

describe("AgentHandbookPanel", () => {
  it("saves the directory and browses a non-empty README before requesting history restore", async () => {
    api.getAgentHandbook.mockImplementation((_id, path) =>
      Promise.resolve(path ? file : root),
    );
    api.getAgentHandbookHistory.mockResolvedValue({
      history: [{ revision: 4, before_exists: true }],
    });
    api.patchAgent.mockResolvedValue({});
    api.restoreAgentHandbook.mockResolvedValue({});
    const wrapper = mount(AgentHandbookPanel, { props: { agentId: "a" } });
    await tick();
    expect(wrapper.find("input[aria-label='手册目录']").element.value).toBe(
      "C:/resolved/handbook",
    );
    await wrapper.find(".agent-handbook-panel__item").trigger("click");
    await tick();
    expect(wrapper.text()).toContain("手册正文");
    await wrapper
      .find(".agent-handbook-panel__history-row button")
      .trigger("click");
    await tick();
    expect(api.restoreAgentHandbook).toHaveBeenCalledWith("a", {
      path: "README.md",
      revision: 4,
      expected_digest: "digest-2",
    });
  });

  it("saves an empty directory and releases busy state", async () => {
    api.getAgentHandbook.mockResolvedValue(root);
    api.patchAgent.mockResolvedValue({});
    const wrapper = mount(AgentHandbookPanel, { props: { agentId: "a" } });
    await tick();
    await wrapper.find("input[aria-label='手册目录']").setValue("");
    await wrapper.find("form").trigger("submit");
    await tick();
    expect(api.patchAgent).toHaveBeenCalledWith("a", {
      handbook: { directory: "" },
    });
    expect(wrapper.find("button[type='submit']").text()).toContain("保存目录");
  });

  it("does not let an old agent response overwrite the new agent", async () => {
    let resolveA;
    api.getAgentHandbook.mockImplementation((id) =>
      id === "a"
        ? new Promise((resolve) => {
            resolveA = resolve;
          })
        : Promise.resolve({ ...root, directory: "B:/handbook" }),
    );
    const wrapper = mount(AgentHandbookPanel, { props: { agentId: "a" } });
    await wrapper.setProps({ agentId: "b" });
    await tick();
    resolveA({ ...root, directory: "A:/handbook" });
    await tick();
    expect(wrapper.find("input[aria-label='手册目录']").element.value).toBe(
      "B:/handbook",
    );
  });

  it("deletes a newly-created file and returns to its parent directory", async () => {
    api.getAgentHandbook.mockImplementation((_id, path) =>
      Promise.resolve(path ? file : root),
    );
    api.getAgentHandbookHistory.mockResolvedValue({
      history: [{ revision: 1, before_exists: false }],
    });
    api.restoreAgentHandbook.mockResolvedValue({});
    const wrapper = mount(AgentHandbookPanel, { props: { agentId: "a" } });
    await tick();
    await wrapper.find(".agent-handbook-panel__item").trigger("click");
    await tick();
    await wrapper
      .find(".agent-handbook-panel__history-row button")
      .trigger("click");
    await tick();
    expect(api.restoreAgentHandbook).toHaveBeenCalledWith("a", {
      path: "README.md",
      revision: 1,
      expected_digest: "digest-2",
    });
  });

  it("keeps the page and reports a CAS conflict", async () => {
    api.getAgentHandbook.mockResolvedValue(file);
    api.getAgentHandbookHistory.mockResolvedValue({
      history: [{ revision: 1, before_exists: false }],
    });
    api.restoreAgentHandbook.mockRejectedValue(
      Object.assign(new Error("conflict"), { status: 409 }),
    );
    const wrapper = mount(AgentHandbookPanel, { props: { agentId: "a" } });
    await tick();
    await wrapper
      .find(".agent-handbook-panel__history-row button")
      .trigger("click");
    await tick();
    expect(wrapper.text()).toContain("文件已被修改");
    expect(wrapper.text()).toContain("手册正文");
  });

  it("explains when the Node version lacks the handbook", async () => {
    api.getAgentHandbook.mockRejectedValue(
      Object.assign(new Error("404 page not found"), { status: 404 }),
    );
    const wrapper = mount(AgentHandbookPanel, { props: { agentId: "a" } });
    await tick();
    expect(wrapper.text()).toContain(
      "当前 Node 版本尚不支持此功能，请更新 Node 后重试",
    );
    expect(wrapper.text()).not.toContain("404 page not found");
    await wrapper.find("button").trigger("click");
    expect(api.getAgentHandbook).toHaveBeenCalledTimes(2);
  });
});
