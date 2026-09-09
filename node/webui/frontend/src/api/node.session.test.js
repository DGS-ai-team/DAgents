/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cancelAgentTurn, clearContext, compressContext, createAgentTodo, deleteAgentTodo, getAgentContext, getAgentHydrate, putAutoConfig, submitMessage, submitResume, updateAgentTodo } from "./node.js";

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

describe("simplified Auto todo API", () => {
  it("uses the correct verbs and CAS request bodies", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => ({ ok: true, json: async () => ({}) }));
    await putAutoConfig("a/1", { expected_revision: 2, responsibility: "owner" });
    await createAgentTodo("a/1", { text: "x" });
    await updateAgentTodo("a/1", "t/2", { expected_revision: 3, status: "completed" });
    await deleteAgentTodo("a/1", "t/2", 4);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ expected_revision: 2, responsibility: "owner" });
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ method: "POST" });
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ text: "x" });
    expect(fetchMock.mock.calls[2][1]).toMatchObject({ method: "PATCH" });
    expect(JSON.parse(fetchMock.mock.calls[2][1].body).expected_revision).toBe(3);
    expect(fetchMock.mock.calls[3][1]).toMatchObject({ method: "DELETE" });
    expect(JSON.parse(fetchMock.mock.calls[3][1].body)).toEqual({ expected_revision: 4 });
    expect(String(fetchMock.mock.calls[2][0])).toContain("t%2F2");
  });
});
