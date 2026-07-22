import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import {
  transcriptStore,
  appendAssistant,
  clearTranscript,
  finalizeAssistant,
  upsertToolCallFromSSE,
} from "./transcript.js";
import { revealedLength, resetStreamReveal } from "./streamReveal.js";
import { resetToolStream } from "./toolStream.js";

describe("upsertToolCallFromSSE does not split streaming assistant text", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "requestAnimationFrame",
      vi.fn(() => 1),
    );
    vi.stubGlobal(
      "cancelAnimationFrame",
      vi.fn(),
    );
    clearTranscript();
    resetToolStream();
    resetStreamReveal();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("keeps one buffer across partial tool_call then more content (Not|epad case)", () => {
    appendAssistant("看起来 Not");
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 0,
      tool_calls: [
        {
          id: "",
          type: "function",
          function: { name: "bash_run", arguments: '{"command":"' },
        },
      ],
    });

    expect(transcriptStore.assistantBuffer).toBe("看起来 Not");
    expect(transcriptStore.entries.filter((e) => e.kind === "assistant" && !e.streaming)).toHaveLength(0);
    expect(transcriptStore.entries.some((e) => e.kind === "tool_call" && e.partial)).toBe(true);

    appendAssistant("epad++ 已安装");
    expect(transcriptStore.assistantBuffer).toBe("看起来 Notepad++ 已安装");
    expect(transcriptStore.entries.filter((e) => e.kind === "assistant" && !e.streaming)).toHaveLength(0);
  });

  it("finalizes assistant only on non-partial tool_call", () => {
    appendAssistant("看起来 Notepad++ 已安装");
    upsertToolCallFromSSE({
      partial: false,
      tool_index: 0,
      tool_calls: [
        {
          id: "call-1",
          type: "function",
          function: { name: "bash_run", arguments: '{"command":"echo hi"}' },
        },
      ],
    });

    expect(transcriptStore.assistantBuffer).toBe("");
    const sealed = transcriptStore.entries.filter((e) => e.kind === "assistant" && !e.streaming);
    expect(sealed).toHaveLength(1);
    expect(sealed[0].text).toBe("看起来 Notepad++ 已安装");
  });

  it("keeps assistant text before tool_call after partial then final", () => {
    appendAssistant("看起来 Not");
    upsertToolCallFromSSE({
      partial: true,
      tool_index: 0,
      tool_calls: [
        {
          id: "",
          type: "function",
          function: { name: "bash_run", arguments: "{" },
        },
      ],
    });
    appendAssistant("epad++ 已安装");
    upsertToolCallFromSSE({
      partial: false,
      tool_index: 0,
      tool_calls: [
        {
          id: "call-1",
          type: "function",
          function: { name: "bash_run", arguments: '{"command":"echo hi"}' },
        },
      ],
    });

    const kinds = transcriptStore.entries.map((e) => e.kind);
    expect(kinds[0]).toBe("assistant");
    expect(kinds[1]).toBe("tool_call");
    expect(transcriptStore.entries[0].text).toBe("看起来 Notepad++ 已安装");
    expect(transcriptStore.entries.filter((e) => e.kind === "assistant")).toHaveLength(1);
  });

  it("empty partial early-return does not seal assistant text", () => {
    appendAssistant("看起来 Not");
    upsertToolCallFromSSE({
      partial: true,
      // no tool_index, no call id → early return after old bug had already finalized
    });

    expect(transcriptStore.assistantBuffer).toBe("看起来 Not");
    expect(transcriptStore.entries.filter((e) => e.kind === "assistant" && !e.streaming)).toHaveLength(0);
  });

  it("resets reveal cursor after finalize so next segment starts at 0", () => {
    appendAssistant("第一段文字内容稍长一些");
    finalizeAssistant();
    expect(revealedLength("assistant")).toBe(0);

    appendAssistant("第二段");
    expect(transcriptStore.assistantBuffer).toBe("第二段");
  });
});
