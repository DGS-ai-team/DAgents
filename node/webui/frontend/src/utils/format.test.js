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
});
