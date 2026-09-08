import { describe, expect, it } from "vitest";
import { conversationChanged, conversationTarget } from "./conversationSession.js";

describe("Goal conversation routing", () => {
  it("switches to and back from a dedicated session without changing Agent identity", () => {
    expect(conversationTarget("agent-a", "goal-session-9")).toBe("goal-session-9");
    expect(conversationChanged(
      { agentId: "agent-a", sessionId: "" },
      { agentId: "agent-a", sessionId: "goal-session-9" },
    )).toBe(true);
    expect(conversationTarget("agent-a", "")).toBe("agent-a");
    expect(conversationChanged(
      { agentId: "agent-a", sessionId: "goal-session-9" },
      { agentId: "agent-a", sessionId: "" },
    )).toBe(true);
  });
});
