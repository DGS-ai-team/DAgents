/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import PolicyPanel from "./PolicyPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({
  getPolicy: vi.fn(), listAgentPolicyGrants: vi.fn(), createAgentPolicyGrant: vi.fn(), revokeAgentPolicyGrant: vi.fn(),
}));

describe("PolicyPanel temporary grants", () => {
  it("creates a bounded file grant from checkbox and settings fields", async () => {
    api.getPolicy.mockResolvedValue({ tools: [], shell: {}, platform: {} });
    api.listAgentPolicyGrants.mockResolvedValue({ grants: [] });
    api.createAgentPolicyGrant.mockResolvedValue({ id: "g1", tools: ["write_file"], workspace: "C:/work", expires_at: "2099-01-01T00:00:00Z" });
    const wrapper = mount(PolicyPanel, { props: { embedded: true, agentId: "agent-a" } });
    await flushPromises();
    await wrapper.find('input[aria-label="授权 write_file"]').setValue(true);
    await wrapper.find('input[aria-label="授权工作目录"]').setValue("C:/work");
    await wrapper.find('input[aria-label="授权有效期"]').setValue("2099-01-01T00:00");
    await wrapper.find("form.policy-panel__grant-form").trigger("submit");
    await flushPromises();
    expect(api.createAgentPolicyGrant).toHaveBeenCalledWith("agent-a", expect.objectContaining({ tools: ["write_file"], workspace: "C:/work" }));
    expect(wrapper.text()).toContain("有效");
  });
});
