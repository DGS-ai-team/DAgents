import { describe, expect, it } from "vitest";
import { formatToolElapsed, formatToolResultDisplay } from "./format.js";

describe("formatToolElapsed", () => {
  it("formats milliseconds under 1s", () => {
    expect(formatToolElapsed(0.5)).toBe(" · 500ms");
  });

  it("formats seconds", () => {
    expect(formatToolElapsed(2.3)).toBe(" · 2.3s");
  });

  it("formats minutes", () => {
    expect(formatToolElapsed(90)).toBe(" · 1m30s");
  });
});

describe("formatToolResultDisplay", () => {
  it("uses temporary agent parser for create result", () => {
    const display = formatToolResultDisplay({
      data: {
        name: "create_temporary_agent",
        content: JSON.stringify({ child_agent_id: "x", purpose: "test" }),
        duration_seconds: 1.2,
      },
    });
    expect(display.headline).toContain("已创建临时 Agent");
    expect(display.headline).toContain("1.2s");
  });

  it("marks rejected tools", () => {
    const display = formatToolResultDisplay({
      data: {
        name: "bash_run",
        content: "ok",
        rejected: true,
        arguments: { command: "ls" },
      },
    });
    expect(display.headline).toContain("已拒绝");
    expect(display.detail).toBe("");
  });

  it("includes raw content for completed generic tools", () => {
    const display = formatToolResultDisplay({
      data: {
        name: "read_file",
        content: "file body",
        arguments: { path: "/tmp/a" },
      },
    });
    expect(display.headline).toContain("已完成");
    expect(display.detail).toBe("file body");
  });

  it("removes WSL protocol and ANSI sequences from terminal results", () => {
    const output =
      "top -bn1 | head -20\r\n" +
      "\u001b]3008;start=abc;type=shell\u001b\\" +
      "\u001b[?2004h\u001b[?2004l" +
      "\u001b]0;aphrodite@host\u0007" +
      "\u001b[32m\u001b[1maphrodite\u001b[m$ \r\n" +
      "Tasks: 76 total\r\n" +
      "\u001b[11;1Hdone\u001b[?25h\n";
    const display = formatToolResultDisplay({
      data: {
        name: "terminal_read",
        content: JSON.stringify({ terminal_id: "term-1", output, next_seq: 15, exited: false }),
      },
    });

    expect(display.detail).not.toContain("\u001b");
    expect(display.detail).not.toContain("3008;");
    expect(display.detail).not.toContain("[?2004");
    expect(display.detail).not.toContain("\r");
    expect(display.detail).toContain("Tasks: 76 total");
  });
});
