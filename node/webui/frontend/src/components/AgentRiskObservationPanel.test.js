/** @vitest-environment jsdom */
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AgentRiskObservationPanel from "./AgentRiskObservationPanel.vue";
import * as api from "../api/node.js";

vi.mock("../api/node.js", () => ({ getAgentRiskObservations: vi.fn() }));
const wrappers = [];
afterEach(() => { while (wrappers.length) wrappers.pop().unmount(); });
beforeEach(() => { vi.clearAllMocks(); });

describe("AgentRiskObservationPanel", () => {
  it("renders read-only observations and emits the Auto toggle", async () => {
    api.getAgentRiskObservations.mockResolvedValue({ observations: [{ request_id: "r1", tool_name: "write_file", level: "high", reason: "敏感路径", recommendation: "请复核", created_at: "2026-09-09T10:00:00Z" }] });
    const wrapper = mount(AgentRiskObservationPanel, { props: { agentId: "a1", enabled: false } }); wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.text()).toContain("只记录风险建议");
    expect(wrapper.text()).toContain("write_file");
    await wrapper.find('input[type="checkbox"]').setValue(true);
    expect(wrapper.emitted("update:enabled")[0]).toEqual([true]);
  });

  it("shows a concrete save error", () => {
    const wrapper = mount(AgentRiskObservationPanel, { props: { agentId: "a1", saveError: "保存冲突" } }); wrappers.push(wrapper);
    expect(wrapper.find('[role="alert"]').text()).toContain("保存冲突");
  });

  it("shows empty and legacy Node states", async () => {
    api.getAgentRiskObservations.mockResolvedValueOnce({ observations: [] });
    const wrapper = mount(AgentRiskObservationPanel, { props: { agentId: "a1" } }); wrappers.push(wrapper);
    await flushPromises();
    expect(wrapper.text()).toContain("暂无风险观察记录");
    const missing = new Error("HTTP 404"); missing.status = 404;
    api.getAgentRiskObservations.mockRejectedValueOnce(missing);
    await wrapper.setProps({ agentId: "a2" });
    await flushPromises();
    expect(wrapper.text()).toContain("当前 Node 版本尚不支持风险观察");
    expect(wrapper.find('input[type="checkbox"]').element.disabled).toBe(false);
    expect(wrapper.emitted("unsupported").at(-1)).toEqual([true]);
  });

  it("ignores a stale response after switching agents", async () => {
    let resolveFirst;
    api.getAgentRiskObservations.mockImplementation((id) => id === "a1"
      ? new Promise((resolve) => { resolveFirst = resolve; })
      : Promise.resolve({ observations: [{ request_id: "new", tool_name: "read_file" }] }));
    const wrapper = mount(AgentRiskObservationPanel, { props: { agentId: "a1" } }); wrappers.push(wrapper);
    await wrapper.setProps({ agentId: "a2" });
    await flushPromises();
    resolveFirst({ observations: [{ request_id: "old", tool_name: "exec" }] });
    await flushPromises();
    expect(wrapper.text()).toContain("read_file");
    expect(wrapper.text()).not.toContain("exec");
  });
});
