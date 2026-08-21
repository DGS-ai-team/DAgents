import { describe, expect, it } from "vitest";
import { deriveActivityFromTranscript } from "./workspaceActivity.js";

describe("deriveActivityFromTranscript", () => {
  it("collects write/replace files and bash commands", () => {
    const snap = deriveActivityFromTranscript([
      {
        kind: "tool_call",
        blockId: "c1",
        data: { tool_name: "write_file", arguments: { path: "a.txt" } },
      },
      {
        kind: "tool_result",
        blockId: "c1",
        data: { tool_call_id: "c1", tool_name: "write_file", content: "wrote 2 bytes to a.txt", arguments: { path: "a.txt" } },
      },
      {
        kind: "tool_call",
        blockId: "c2",
        data: { tool_name: "bash_run", arguments: { command: "ls" } },
      },
      {
        kind: "tool_result",
        blockId: "c2",
        data: { tool_call_id: "c2", tool_name: "bash_run", content: "ok", arguments: { command: "ls" } },
      },
    ]);
    expect(snap.file_count).toBe(1);
    expect(snap.files[0].path).toBe("a.txt");
    expect(snap.command_count).toBe(1);
    expect(snap.commands[0].command).toBe("ls");
    expect(snap.commands[0].status).toBe("ok");
  });

  it("uses result status instead of inferring failure from content", () => {
    const snap = deriveActivityFromTranscript([
      {
        kind: "tool_call",
        blockId: "c-failed",
        data: { tool_name: "bash_run", arguments: { command: "ls" } },
      },
      {
        kind: "tool_result",
        blockId: "c-failed",
        data: {
          tool_call_id: "c-failed",
          tool_name: "bash_run",
          status: "failed",
          rejected: false,
          content: "connection refused",
          arguments: { command: "ls" },
        },
      },
    ]);
    expect(snap.commands[0].status).toBe("error");
    expect(snap.commands[0].rejected).toBe(false);
  });

  it("does not label a failed file operation as policy denied", () => {
    const snap = deriveActivityFromTranscript([
      {
        kind: "tool_result",
        blockId: "c-write-failed",
        data: {
          tool_call_id: "c-write-failed",
          tool_name: "write_file",
          status: "failed",
          rejected: false,
          content: "ERROR: disk full",
          arguments: { path: "a.txt" },
        },
      },
    ]);
    expect(snap.files[0].rejected).toBe(false);
    expect(snap.files[0].failed).toBe(true);
  });
});
