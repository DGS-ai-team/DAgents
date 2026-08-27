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
import { applyTurnState, resetTurnState, turnStateStore } from "./turnState.js";

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
    resetTurnState();
  });

  afterEach(() => {
    resetStatusLines();
    resetTurnState();
  });

  it("clears leftover prefilling/thinking when turn is idle", () => {
    startStatus("prefilling");
    startStatus("thinking");
    applyTurnState({ phase: "idle", terminal: false }, { source: "hydrate" });
    syncStatusAfterHydrate();
    expect(hasStatus("prefilling")).toBe(false);
    expect(hasStatus("thinking")).toBe(false);
    expect(Object.keys(statusStore.phases)).toHaveLength(0);
  });

  it("restores the explicit model-generating phase while no output channel is known", () => {
    startStatus("prefilling");
    applyTurnState({ phase: "model_generating", terminal: false }, { source: "hydrate" });
    syncStatusAfterHydrate();
    expect(hasStatus("prefilling")).toBe(false);
    expect(hasStatus("model_generating")).toBe(true);
  });

  it("does not show thinking during tool execution", () => {
    startStatus("thinking");
    applyTurnState({ phase: "tool_executing", terminal: false }, { source: "hydrate" });
    syncStatusAfterHydrate();
    expect(hasStatus("thinking")).toBe(false);
  });
});

describe("hydrateAgent lifecycle", () => {
  beforeEach(() => {
    persistAgentId("agt-1");
    clearHitl();
    transcriptStore.entries = [];
    transcriptStore.lastSeq = 0;
    transcriptStore.lastAgentSeq = 0;
    transcriptStore.streamEpoch = "";
    transcriptStore.historyRevision = 0;
    transcriptStore.historyDirty = false;
    resetTurnState();
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
      stream_epoch: "test-epoch",
      stream_seq_hint: 10,
      agent_seq_hint: 8,
      notify_seq: 10,
    });

    expect(await pending).toBeNull();
    expect(transcriptStore.entries).toHaveLength(0);
    expect(hitlStore.queue).toHaveLength(0);
  });

  it("clears stale frontend approvals when hydrate reports no pending HITL", async () => {
    enqueueStaleApprovalForHydrateTest();
    expect(hitlStore.queue).toHaveLength(1);
    vi.mocked(api.getAgentHydrate).mockResolvedValueOnce({
      transcript: [{ kind: "assistant", text: "done" }],
      pending_hitl: null,
      turn_state: { phase: "idle", terminal: false },
      stream_epoch: "test-epoch",
      stream_seq_hint: 20,
      agent_seq_hint: 10,
      notify_seq: 20,
    });

    const data = await hydrateAgent();
    expect(data?.pending_hitl).toBeNull();
    expect(hitlStore.queue).toHaveLength(0);
    expect(turnStateStore.phase).toBe("idle");
  });

  it("keeps locally streamed history when hydrate has the same revision", async () => {
    transcriptStore.historyRevision = 12;
    transcriptStore.historyDirty = true;
    transcriptStore.entries = [{ id: 1, kind: "assistant", text: "local answer" }];
    vi.mocked(api.getAgentHydrate).mockResolvedValueOnce({
      transcript: [{ kind: "user", text: "old snapshot" }],
      history_revision: 12,
      turn_state: { phase: "completed", terminal: true, history_revision: 12 },
      pending_hitl: null,
    });

    await hydrateAgent();

    expect(transcriptStore.entries).toHaveLength(1);
    expect(transcriptStore.entries[0].text).toBe("local answer");
    expect(transcriptStore.historyDirty).toBe(true);
  });
});

function enqueueStaleApprovalForHydrateTest() {
  hitlStore.queue.push({
    kind: "approval",
    data: {
      approval_id: "stale-hitl",
      approval_args: {
        tool_calls: [{ id: "stale-call", name: "bash_run", arguments: {} }],
      },
    },
  });
}

describe("loadTranscriptFromHydrate", () => {
  beforeEach(() => {
    transcriptStore.entries = [];
    transcriptStore.lastSeq = 0;
    transcriptStore.historyRevision = 0;
    transcriptStore.historyDirty = false;
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
