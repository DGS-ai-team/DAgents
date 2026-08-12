import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import * as api from "../api/node.js";
import { transcriptStore, loadTranscriptFromHydrate } from "./transcript.js";
import {
  consumeStartupURL,
  hydrateAgent,
  invalidateHydration,
  syncStatusAfterHydrate,
} from "./hydrate.js";
import { agentStore, persistAgentId } from "./agent.js";
import { clearHitl, hitlStore } from "./hitl.js";
import { hasStatus, resetStatusLines, startStatus, statusStore } from "./statusLines.js";

vi.mock("../api/node.js", () => ({
  ensureAgentRuntime: vi.fn(() => Promise.resolve({ ok: true })),
  getAgentHydrate: vi.fn(),
  postAgentAck: vi.fn(() => Promise.resolve({ ok: true })),
}));

describe("consumeStartupURL", () => {
  afterEach(() => {
    persistAgentId("");
    vi.unstubAllGlobals();
  });

  it("reads ?agent=", () => {
    vi.stubGlobal("window", {
      location: { search: "?agent=agt-1" },
    });
    consumeStartupURL();
    expect(agentStore.agentId).toBe("agt-1");
  });

  it("accepts legacy ?session= as agent id", () => {
    vi.stubGlobal("window", {
      location: { search: "?session=sess-legacy" },
    });
    consumeStartupURL();
    expect(agentStore.agentId).toBe("sess-legacy");
  });

  it("prefers ?agent= over ?session=", () => {
    vi.stubGlobal("window", {
      location: { search: "?agent=agt-new&session=sess-old" },
    });
    consumeStartupURL();
    expect(agentStore.agentId).toBe("agt-new");
  });
});

describe("syncStatusAfterHydrate", () => {
  beforeEach(() => {
    resetStatusLines();
    agentStore.awaitingTurn = false;
  });

  afterEach(() => {
    resetStatusLines();
    agentStore.awaitingTurn = false;
  });

  it("clears leftover prefilling/thinking when turn is idle", () => {
    startStatus("prefilling");
    startStatus("thinking");
    agentStore.awaitingTurn = false;
    syncStatusAfterHydrate("idle");
    expect(hasStatus("prefilling")).toBe(false);
    expect(hasStatus("thinking")).toBe(false);
    expect(Object.keys(statusStore.phases)).toHaveLength(0);
  });

  it("restores thinking while model is streaming", () => {
    startStatus("prefilling");
    agentStore.awaitingTurn = true;
    syncStatusAfterHydrate("model_streaming");
    expect(hasStatus("prefilling")).toBe(false);
    expect(hasStatus("thinking")).toBe(true);
  });

  it("does not show thinking during tool execution", () => {
    startStatus("thinking");
    agentStore.awaitingTurn = true;
    syncStatusAfterHydrate("awaiting_tool_execution");
    expect(hasStatus("thinking")).toBe(false);
  });
});

describe("hydrateAgent lifecycle", () => {
  beforeEach(() => {
    persistAgentId("agt-1");
    clearHitl();
    transcriptStore.entries = [];
    transcriptStore.lastSeq = 0;
    agentStore.awaitingTurn = false;
    vi.mocked(api.getAgentHydrate).mockReset();
    vi.mocked(api.postAgentAck).mockClear();
    invalidateHydration();
  });

  afterEach(() => {
    clearHitl();
    persistAgentId("");
    vi.mocked(api.getAgentHydrate).mockReset();
  });

  it("ignores a hydrate response invalidated while the chat is deactivated", async () => {
    let resolveHydrate;
    vi.mocked(api.getAgentHydrate).mockReturnValueOnce(
      new Promise((resolve) => {
        resolveHydrate = resolve;
      }),
    );

    const pending = hydrateAgent();
    await vi.waitFor(() => expect(api.getAgentHydrate).toHaveBeenCalledWith("agt-1"));

    // Simulates leaving the KeepAlive chat view for Agent settings.
    invalidateHydration();
    resolveHydrate({
      transcript: [{ kind: "assistant", text: "stale" }],
      pending_hitl: {
        hitl_id: "hitl-1",
        items: [{ hitl_type: "execute_tool", id: "call-1", tool_name: "bash_run" }],
      },
      sse_seq_hint: 10,
      notify_seq: 10,
    });

    expect(await pending).toBeNull();
    expect(transcriptStore.entries).toHaveLength(0);
    expect(hitlStore.queue).toHaveLength(0);
  });
});

describe("loadTranscriptFromHydrate", () => {
  beforeEach(() => {
    transcriptStore.entries = [];
    transcriptStore.lastSeq = 0;
  });

  it("loads user and assistant entries with ids", () => {
    loadTranscriptFromHydrate([
      { kind: "user", text: "hi", images: [] },
      { kind: "assistant", text: "hello" },
    ]);
    expect(transcriptStore.entries).toHaveLength(2);
    expect(transcriptStore.entries[0].kind).toBe("user");
    expect(transcriptStore.entries[0].id).toBeGreaterThan(0);
    expect(transcriptStore.entries[1].text).toBe("hello");
  });

  it("preserves tool_call blockId", () => {
    loadTranscriptFromHydrate([
      { kind: "tool_call", blockId: "call-1", data: { tool_name: "bash_run" }, partial: false },
    ]);
    expect(transcriptStore.entries[0].blockId).toBe("call-1");
    expect(transcriptStore.entries[0].partial).toBe(false);
  });
});
