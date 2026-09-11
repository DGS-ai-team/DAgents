import { describe, expect, it } from "vitest";
import { isTemporaryAgentTool, parseTemporaryAgentToolResult } from "./temporaryAgentResults.js";

describe("isTemporaryAgentTool", () => {
  it("recognizes temporary agent tools", () => {
    expect(isTemporaryAgentTool("create_temporary_agent")).toBe(true);
    expect(isTemporaryAgentTool("cancel_temporary_agent")).toBe(true);
    expect(isTemporaryAgentTool("bash_run")).toBe(false);
  });
});

describe("parseTemporaryAgentToolResult", () => {
  it("returns null for non-temporary tools", () => {
    expect(parseTemporaryAgentToolResult("bash_run", "{}")).toBeNull();
  });

  it("formats create_temporary_agent creation payload", () => {
    const out = parseTemporaryAgentToolResult(
      "create_temporary_agent",
      JSON.stringify({
        kind: "result",
        child_agent_id: "child-session-id-1234567890",
        status: "completed",
        summary: "summarize",
        turn_count: 3,
      }),
    );
    expect(out.summary).toContain("临时 Agent 完成");
    expect(out.detail).toContain("summarize");
    expect(out.detail).toContain("turn_count=3");
  });

  it("passes through ERROR prefix", () => {
    const out = parseTemporaryAgentToolResult("cancel_temporary_agent", "ERROR: not found");
    expect(out.summary).toBe("ERROR: not found");
  });

  it("formats cancel_temporary_agent", () => {
    const out = parseTemporaryAgentToolResult(
      "cancel_temporary_agent",
      JSON.stringify({ child_agent_id: "abc", status: "cancelled" }),
    );
    expect(out.summary).toContain("已取消临时 Agent");
    expect(out.summary).toContain("cancelled");
  });
});
