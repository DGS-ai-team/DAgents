import { describe, expect, it } from "vitest";
import {
  REVEAL_CHARS_PER_SECOND,
  configureStreamReveal,
  flushReveal,
  resetRevealKind,
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

  it("resetRevealKind zeroes the cursor", () => {
    resetStreamReveal();
    const sources = { assistant: "abcdef" };
    configureStreamReveal({
      getSourceText: (kind) => sources[kind] || "",
      onRevealText: () => {},
    });
    flushReveal("assistant");
    expect(revealedLength("assistant")).toBe(6);
    resetRevealKind("assistant");
    expect(revealedLength("assistant")).toBe(0);
  });
});
