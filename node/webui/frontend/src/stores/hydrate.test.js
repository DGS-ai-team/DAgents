import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import { transcriptStore, loadTranscriptFromHydrate } from "./transcript.js";
import { consumeStartupURL } from "./hydrate.js";
import { agentStore, persistAgentId } from "./agent.js";

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
