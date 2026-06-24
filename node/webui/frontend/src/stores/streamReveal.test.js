import { describe, expect, it, vi } from "vitest";
import {
  REVEAL_CHARS_PER_SECOND,
  configureStreamReveal,
  flushReveal,
  resetStreamReveal,
  revealedLength,
} from "./streamReveal.js";

describe("streamReveal", () => {
  it("exports a positive reveal rate", () => {
    expect(REVEAL_CHARS_PER_SECOND).toBeGreaterThan(0);
  });

  it("flushReveal exposes full source immediately", () => {
    resetStreamReveal();
    const sources = { assistant: "hello" };
    const seen = [];
    configureStreamReveal({
      getSourceText: (kind) => sources[kind] || "",
      onRevealText: (kind, text, flushed) => {
        seen.push({ kind, text, flushed });
      },
    });
    expect(flushReveal("assistant")).toBe("hello");
    expect(revealedLength("assistant")).toBe(5);
    expect(seen.at(-1)).toEqual({ kind: "assistant", text: "hello", flushed: true });
  });
});
