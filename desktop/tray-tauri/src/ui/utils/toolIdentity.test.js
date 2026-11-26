import { describe, expect, it } from "vitest";
import { toolCallIdFromEntry, toolNameFromEntry } from "./toolIdentity.js";

describe("tool identity", () => {
  it("reads stable tool identity and name from entries", () => {
    expect(toolCallIdFromEntry({ blockId: "b1", data: { tool_call_id: "c1" } })).toBe("c1");
    expect(toolCallIdFromEntry({ blockId: "b1" })).toBe("b1");
    expect(toolNameFromEntry({ data: { tool_name: "bash_run" } })).toBe("bash_run");
  });
});
