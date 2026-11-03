/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import PolicyPanel from "./PolicyPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({
  getPolicy: vi.fn(), updateToolPolicy: vi.fn(), updateShellPolicy: vi.fn(),
}));

describe("PolicyPanel", () => {
  it("loads and updates ordinary tool policy without grant APIs", async () => {
    api.getPolicy.mockResolvedValue({
      tools: [{ name: "write_file", mode: "rule" }],
      shell: {},
      platform: {},
    });
    api.updateToolPolicy.mockResolvedValue({ ok: true });
    const wrapper = mount(PolicyPanel, { props: { embedded: true, agentId: "agent-a" } });
    await flushPromises();

    expect(wrapper.find("h2").text()).toBe("工具审批");
    expect(wrapper.find(".policy-panel--embedded").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("临时岗位授权");
    expect(wrapper.find("tbody tr").text()).toContain("write_file");

    await wrapper.find(".policy-decision-btn--deny").trigger("click");
    await flushPromises();
    expect(api.updateToolPolicy).toHaveBeenCalledWith("agent-a", [{ name: "write_file", mode: "deny" }]);
  });
});
