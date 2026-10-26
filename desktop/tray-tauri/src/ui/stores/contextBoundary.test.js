import { beforeEach, describe, expect, it } from "vitest";
import {
  addContextBoundaryNotice,
  addSystem,
  clearTranscript,
  loadTranscriptFromHydrate,
  transcriptStore,
} from "./transcript.js";

describe("context boundary transcript projection", () => {
  beforeEach(() => clearTranscript());

  it("renders hydrate and realtime projections with the same safe entry shape", () => {
    loadTranscriptFromHydrate([
      { kind: "user", text: "previous user turn" },
      { kind: "assistant", text: "previous answer" },
      { kind: "context_boundary", boundary_id: "boundary-1" },
      { kind: "user", text: "next question" },
    ]);
    const hydratedKinds = transcriptStore.entries.map((entry) => entry.kind);
    expect(hydratedKinds).toEqual(["user", "assistant", "context_boundary", "user"]);
    expect(transcriptStore.entries[0].text).toBe("previous user turn");
    expect(transcriptStore.entries[1].text).toBe("previous answer");
    expect(transcriptStore.entries[2]).not.toHaveProperty("text");

    clearTranscript();
    transcriptStore.entries.push(
      { kind: "user", text: "old question" },
      { kind: "assistant", text: "old answer" },
      { kind: "user", text: "new question" },
      { kind: "user", text: "later queued question" },
    );
    expect(addContextBoundaryNotice({
      message_kind: "context_boundary",
      boundary_id: "boundary-1",
      message: "private bridge content",
    })).toBe(true);
    expect(addContextBoundaryNotice({
      message_kind: "context_boundary",
      boundary_id: "boundary-1",
    })).toBe(false);
    expect(transcriptStore.entries.map((entry) => entry.text || entry.kind)).toEqual([
      "old question", "old answer", "context_boundary", "new question", "later queued question",
    ]);
    expect(transcriptStore.entries[2]).not.toHaveProperty("text");
    expect(transcriptStore.entries[2]).not.toHaveProperty("message");
  });

  it("keeps unrelated Auto Agent system prompts visible", () => {
    addSystem("Auto Agent 即将开始");
    expect(transcriptStore.entries).toEqual([
      expect.objectContaining({ kind: "system", text: "Auto Agent 即将开始" }),
    ]);
  });
});
