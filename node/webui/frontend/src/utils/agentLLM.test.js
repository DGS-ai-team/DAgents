import { describe, expect, it } from "vitest";
import { agentActiveProfile } from "./agentLLM.js";

describe("agentActiveProfile", () => {
  it("reads the per-Agent profile from the snapshot", () => {
    expect(agentActiveProfile({ config_snapshot: { defaults: { llm: { active: "mimo-v2.5" } } } })).toBe("mimo-v2.5");
  });

  it("accepts alternate API casing and missing snapshots", () => {
    expect(agentActiveProfile({ ConfigSnapshot: { Defaults: { LLM: { activeProfile: "deepseek" } } } })).toBe("deepseek");
    expect(agentActiveProfile(null)).toBe("");
  });
});
