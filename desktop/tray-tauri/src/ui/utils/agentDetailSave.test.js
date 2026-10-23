import { describe, expect, it, vi } from "vitest";
import { saveAgentDetailStages } from "./agentDetailSave.js";

function operations(overrides = {}) {
  return {
    patchAgent: vi.fn().mockResolvedValue({ agent_id: "agent-a", display_name: "Writer" }),
    putPromptContext: vi.fn().mockResolvedValue({}),
    reloadAgentRuntime: vi.fn().mockResolvedValue({}),
    ...overrides,
  };
}

const input = (ops) => ({
  agentId: "agent-a",
  agentPayload: { display_name: "Writer" },
  promptPayload: { custom_md: "draft" },
  ...ops,
});

describe("agent detail staged save", () => {
  it("reports base configuration as saved when prompt context fails and does not reload", async () => {
    const ops = operations({ putPromptContext: vi.fn().mockRejectedValue(new Error("prompt unavailable")) });
    const result = await saveAgentDetailStages(input(ops));

    expect(result).toMatchObject({ ok: false, stage: "prompt", promptSaved: false });
    expect(result.updatedAgent).toMatchObject({ agent_id: "agent-a" });
    expect(ops.patchAgent).toHaveBeenCalledOnce();
    expect(ops.putPromptContext).toHaveBeenCalledWith("agent-a", { custom_md: "draft" });
    expect(ops.reloadAgentRuntime).not.toHaveBeenCalled();
  });

  it("reports saved data when runtime reload fails", async () => {
    const ops = operations({ reloadAgentRuntime: vi.fn().mockRejectedValue(new Error("reload unavailable")) });
    const result = await saveAgentDetailStages(input(ops));

    expect(result).toMatchObject({ ok: false, stage: "reload", promptSaved: true });
    expect(result.updatedAgent).toMatchObject({ agent_id: "agent-a" });
    expect(ops.patchAgent).toHaveBeenCalledOnce();
    expect(ops.putPromptContext).toHaveBeenCalledOnce();
    expect(ops.reloadAgentRuntime).toHaveBeenCalledWith("agent-a");
  });

  it("treats an unconfirmed base-save response as a distinct failure stage", async () => {
    const ops = operations({ patchAgent: vi.fn().mockRejectedValue(new Error("connection lost")) });
    const result = await saveAgentDetailStages(input(ops));

    expect(result).toMatchObject({ ok: false, stage: "agent", promptSaved: false });
    expect(ops.putPromptContext).not.toHaveBeenCalled();
    expect(ops.reloadAgentRuntime).not.toHaveBeenCalled();
  });

  it("runs all stages in order on success", async () => {
    const order = [];
    const ops = operations({
      patchAgent: vi.fn(async () => { order.push("agent"); return { agent_id: "agent-a" }; }),
      putPromptContext: vi.fn(async () => { order.push("prompt"); }),
      reloadAgentRuntime: vi.fn(async () => { order.push("reload"); }),
    });
    const result = await saveAgentDetailStages(input(ops));

    expect(result).toMatchObject({ ok: true, promptSaved: true });
    expect(order).toEqual(["agent", "prompt", "reload"]);
  });
});
