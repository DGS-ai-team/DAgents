import { describe, expect, it } from "vitest";
import { isTemporaryAgentTool, parseTemporaryAgentToolResult } from "./temporaryAgentResults.js";

describe("isTemporaryAgentTool", () => {
  it("recognizes temporary agent tools", () => {
    expect(isTemporaryAgentTool("create_temporary_agent")).toBe(true);
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
        child_session_id: "child-session-id-1234567890",
        purpose: "summarize",
        max_turns: 3,
      }),
    );
    expect(out.summary).toContain("已创建临时 Agent");
    expect(out.summary).toContain("summarize");
    expect(out.summary).toContain("max_turns=3");
  });

  it("formats wait_temporary_agents batch result", () => {
    const out = parseTemporaryAgentToolResult(
      "wait_temporary_agents",
      JSON.stringify({
        results: [
          { child_session_id: "a", status: "completed", summary: "done" },
          { child_session_id: "b", status: "failed", error: "timeout" },
        ],
      }),
    );
    expect(out.summary).toContain("wait_temporary_agents");
    expect(out.summary).toContain("2/2");
    expect(out.detail).toContain("completed");
    expect(out.detail).toContain("timeout");
  });

  it("passes through ERROR prefix", () => {
    const out = parseTemporaryAgentToolResult("cancel_temporary_agent", "ERROR: not found");
    expect(out.summary).toBe("ERROR: not found");
  });

  it("formats cancel_temporary_agent", () => {
    const out = parseTemporaryAgentToolResult(
      "cancel_temporary_agent",
      JSON.stringify({ child_session_id: "abc", status: "cancelled" }),
    );
    expect(out.summary).toContain("已取消临时 Agent");
    expect(out.summary).toContain("cancelled");
  });
});
