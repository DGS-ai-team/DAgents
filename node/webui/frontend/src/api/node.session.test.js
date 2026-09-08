/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cancelAgentTurn, clearContext, compressContext, getAgentContext, getAgentHydrate, submitMessage, submitResume } from "./node.js";

afterEach(() => vi.restoreAllMocks());

describe("dedicated Goal session API routing", () => {
  it("uses the supplied session id for every conversation operation", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({}),
    });
    const session = "goal-session-9d/1";
    await getAgentHydrate(session);
    await getAgentContext(session);
    await submitMessage(session, "hello");
    await submitResume(session, { type: "approval", approved: ["call-1"] });
    await cancelAgentTurn(session);
    await clearContext(session);
    await compressContext(session);

    const urls = fetchMock.mock.calls.map(([url]) => String(url));
    expect(urls[0]).toContain("/v1/agents/goal-session-9d%2F1/hydrate");
    expect(urls[1]).toContain("/v1/agents/goal-session-9d%2F1/context");
    expect(fetchMock.mock.calls[2][1].body).toContain('"agent_id":"goal-session-9d/1"');
    expect(fetchMock.mock.calls[3][1].body).toContain('"agent_id":"goal-session-9d/1"');
    expect(urls[4]).toContain("/v1/agents/goal-session-9d%2F1/cancel");
    expect(urls[5]).toContain("/v1/agents/goal-session-9d%2F1/clear-context");
    expect(urls[6]).toContain("/v1/agents/goal-session-9d%2F1/compress");
  });
});
