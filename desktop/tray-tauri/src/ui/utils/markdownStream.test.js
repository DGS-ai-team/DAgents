import { describe, expect, it } from "vitest";
import { advanceMarkdownBoundaryScanner, createMarkdownBoundaryScanner } from "./markdownStream.js";

describe("streaming Markdown safe boundaries", () => {
  it("commits complete paragraphs and scans only appended lines", () => {
    const scanner = createMarkdownBoundaryScanner();
    const first = "First paragraph.\n\nSecond";
    expect(advanceMarkdownBoundaryScanner(scanner, first).stableBoundary).toBe("First paragraph.\n\n".length);
    const priorOffset = scanner.scanOffset;
    const next = `${first} paragraph\n\nThird`;
    expect(advanceMarkdownBoundaryScanner(scanner, next).stableBoundary).toBe("First paragraph.\n\nSecond paragraph\n\n".length);
    expect(scanner.scanOffset).toBeGreaterThan(priorOffset);
  });

  it("keeps fenced code and dagents render wrappers intact until they close", () => {
    const scanner = createMarkdownBoundaryScanner();
    const prefix = "Intro\n\n```ts\nconst x = 1;\n\nconst y = 2;\n```\n\n<dagents-render type=\"html\">\n<p>safe</p>\n";
    const result = advanceMarkdownBoundaryScanner(scanner, prefix);
    expect(prefix.slice(0, result.stableBoundary)).toContain("const y = 2;");
    expect(prefix.slice(0, result.stableBoundary)).not.toContain("<dagents-render");

    const closed = `${prefix}</dagents-render>\n\nTail\n\nNext`;
    const next = advanceMarkdownBoundaryScanner(scanner, closed);
    expect(closed.slice(0, next.stableBoundary)).toContain("</dagents-render>");
    expect(closed.slice(next.stableBoundary)).toBe("Next");
  });

  it("does not split lists or quotes at blank lines that continue the same block", () => {
    const scanner = createMarkdownBoundaryScanner();
    const list = "- first\n\n- second";
    expect(advanceMarkdownBoundaryScanner(scanner, list).stableBoundary).toBe(0);
    const listClosed = `${list}\n\nSummary\n\nTail`;
    const listResult = advanceMarkdownBoundaryScanner(scanner, listClosed);
    expect(listClosed.slice(0, listResult.stableBoundary)).toContain("- second");
    expect(listClosed.slice(listResult.stableBoundary)).toBe("Tail");

    const quoteScanner = createMarkdownBoundaryScanner();
    const quote = "> first\n\n> second";
    expect(advanceMarkdownBoundaryScanner(quoteScanner, quote).stableBoundary).toBe(0);
    expect(advanceMarkdownBoundaryScanner(quoteScanner, `${quote}\n\nEnd`).stableBoundary).toBe(quote.length + 2);
  });

  it("keeps inline code spanning blank lines and resets safely after a correction", () => {
    const scanner = createMarkdownBoundaryScanner();
    const inline = "Before\n\n`code\n\ncontinues`\n\nAfter";
    const boundary = advanceMarkdownBoundaryScanner(scanner, inline).stableBoundary;
    expect(inline.slice(0, boundary)).toContain("continues`");

    const corrected = "Replacement\n\nTail";
    expect(advanceMarkdownBoundaryScanner(scanner, corrected)).toEqual({ stableBoundary: "Replacement\n\n".length, reset: true });
  });
});
