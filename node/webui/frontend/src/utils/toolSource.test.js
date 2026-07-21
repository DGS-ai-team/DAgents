import { describe, expect, it } from "vitest";
import { inferToolKind, resolveToolVisual } from "./toolSource.js";

describe("inferToolKind", () => {
  it("maps fs tools", () => {
    expect(inferToolKind("glob_files")).toBe("fs");
    expect(inferToolKind("read_file")).toBe("fs");
    expect(inferToolKind("grep_files")).toBe("fs");
  });

  it("maps shell tools", () => {
    expect(inferToolKind("bash_run")).toBe("shell");
    expect(inferToolKind("background_job_status")).toBe("shell");
  });

  it("maps triggers and browser by prefix", () => {
    expect(inferToolKind("trigger_list")).toBe("triggers");
    expect(inferToolKind("browser_navigate")).toBe("browser");
  });

  it("falls back to tool instead of agent", () => {
    expect(inferToolKind("unknown_custom_tool")).toBe("tool");
    expect(inferToolKind("")).toBe("tool");
  });
});

describe("resolveToolVisual", () => {
  it("uses short group label for summary row badge", () => {
    expect(resolveToolVisual({ data: { tool_name: "glob_files" } })).toMatchObject({
      kind: "fs",
      label: "fs",
    });
    expect(resolveToolVisual({ data: { tool_name: "bash_run" } })).toMatchObject({
      kind: "shell",
      label: "shell",
    });
    expect(resolveToolVisual({ data: { tool_name: "load_skills" } })).toMatchObject({
      kind: "skills",
      label: "skills",
    });
  });
});
