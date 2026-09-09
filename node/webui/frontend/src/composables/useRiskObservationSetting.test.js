/** @vitest-environment jsdom */
import { nextTick, ref, defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useRiskObservationSetting } from "./useRiskObservationSetting.js";
import * as api from "../api/node.js";
vi.mock("../api/node.js", () => ({ patchAgent: vi.fn(), reloadAgentRuntime: vi.fn(), getAgent: vi.fn() }));

describe("useRiskObservationSetting", () => {
  beforeEach(() => vi.clearAllMocks());
  it("patches hooks only and reloads the original agent", async () => {
    const agentId = ref("a1"), agentMeta = ref({ config_snapshot: { defaults: { hooks: { audit: true } } } }), draft = ref({ riskObservationEnabled: false });
    api.patchAgent.mockResolvedValue({ id: "a1" }); api.reloadAgentRuntime.mockResolvedValue({});
    const s = useRiskObservationSetting({ agentId, agentMeta, draft: draft.value });
    await s.save(true);
    expect(api.patchAgent).toHaveBeenCalledWith("a1", { defaults: { hooks: { audit: true, risk_observation_enabled: true } } });
    expect(api.reloadAgentRuntime).toHaveBeenCalledWith("a1"); expect(s.confirmed.value).toBe(true); expect(s.saving.value).toBe(false);
  });
  it("restores confirmed server value when PATCH and confirmation GET fail", async () => {
    const agentId = ref("a1"), agentMeta = ref({ config_snapshot: { defaults: { hooks: { risk_observation_enabled: false } } } }), draft = { riskObservationEnabled: false };
    api.patchAgent.mockRejectedValue(new Error("conflict")); api.getAgent.mockRejectedValue(new Error("offline"));
    const s = useRiskObservationSetting({ agentId, agentMeta, draft }); await s.save(true);
    expect(draft.riskObservationEnabled).toBe(false); expect(s.confirmed.value).toBe(false); expect(s.saving.value).toBe(false);
  });
  it("does not let a late confirmation failure alter the next Agent", async () => {
    const agentId = ref("a1"), agentMeta = ref({ config_snapshot: JSON.stringify({ defaults: { hooks: { risk_observation_enabled: true } } }) }), draft = { riskObservationEnabled: true };
    api.patchAgent.mockRejectedValue(new Error("conflict")); api.getAgent.mockResolvedValue({ config_snapshot: JSON.stringify({ defaults: { hooks: { risk_observation_enabled: true } } }) });
    let rejectGet; api.getAgent.mockReturnValue(new Promise((_, reject) => { rejectGet = reject; }));
    const s = useRiskObservationSetting({ agentId, agentMeta, draft }); const pending = s.save(false); await flushPromises(); agentId.value = "a2"; draft.riskObservationEnabled = true; await nextTick(); rejectGet(new Error("offline")); await pending;
    expect(draft.riskObservationEnabled).toBe(true); expect(s.saving.value).toBe(false);
  });
  it("restores true from a string snapshot when both save and confirmation fail", async () => {
    const agentId = ref("a1"), agentMeta = ref({ config_snapshot: JSON.stringify({ defaults: { hooks: { risk_observation_enabled: true } } }) }), draft = { riskObservationEnabled: true };
    api.patchAgent.mockRejectedValue(new Error("conflict")); api.getAgent.mockRejectedValue(new Error("offline"));
    const s = useRiskObservationSetting({ agentId, agentMeta, draft }); await s.save(false);
    expect(draft.riskObservationEnabled).toBe(true); expect(s.confirmed.value).toBe(false);
  });
  it("releases saving and ignores old completion after Agent switch", async () => {
    let finish; const agentId = ref("a1"), agentMeta = ref({ config_snapshot: {} }), draft = { riskObservationEnabled: false };
    api.patchAgent.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    let state; const Harness = defineComponent({ setup() { state = useRiskObservationSetting({ agentId, agentMeta, draft }); return () => h("div"); } });
    const wrapper = mount(Harness); const pending = state.save(true); agentId.value = "a2"; await nextTick(); finish({ id: "a1" }); await pending;
    expect(state.saving.value).toBe(false); expect(api.reloadAgentRuntime).not.toHaveBeenCalled(); wrapper.unmount();
  });
});
