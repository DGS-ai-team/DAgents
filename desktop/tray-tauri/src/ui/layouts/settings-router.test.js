// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import router from "../../settings-router.js";

describe("desktop settings router", () => {
  it("resolves the chat and agent settings navigation targets", () => {
    const routeNames = new Set(router.getRoutes().map((route) => route.name).filter(Boolean));

    expect(routeNames).toContain("chat");
    expect(router.resolve({ name: "chat" }).path).toBe("/chat");
    expect(routeNames).toContain("settings-agents");
    expect(router.resolve({ name: "settings-agent-detail", params: { agentId: "agent-a" } }).path)
      .toBe("/settings/agents/agent-a");
  });
});
