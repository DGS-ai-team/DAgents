import { beforeEach, describe, expect, it } from "vitest";
import {
  applyRemoteWorkerEvent,
  childProgressForTool,
  currentRemoteWorkerEventVersion,
  onChildCreated,
  onChildFinished,
  onChildProgress,
  resetRemoteWorkers,
  replaceChildrenFromHydrate,
  setRemoteWorkerParent,
} from "./remoteWorkers.js";

describe("remote child agent progress", () => {
  beforeEach(() => {
    resetRemoteWorkers();
    setRemoteWorkerParent("parent-1");
  });

  it("keeps progress associated with the parent tool call", () => {
    onChildCreated({
      child_agent_id: "child-1",
      tool_call_id: "call-create-1",
      purpose: "inspect files",
    });
    onChildProgress({
      child_agent_id: "child-1",
      tool_call_id: "call-create-1",
      status: "active",
      phase: "tool_executing",
      turn_count: 2,
      current_tool: "bash_run",
      current_tool_status: "running",
      revision: 2,
    });

    const rows = childProgressForTool("call-create-1");
    expect(rows).toHaveLength(1);
    expect(rows[0].progress.currentTool).toBe("bash_run");
    expect(rows[0].progress.stepCount).toBe(2);
  });

  it("projects Node child lifecycle events through the same state as hydrate", () => {
    expect(applyRemoteWorkerEvent("temporary_agent_created", {
      child_agent_id: "child-1", tool_call_id: "call-1", status: "creating", revision: 1,
    })).toBe(true);
    applyRemoteWorkerEvent("temporary_agent_progress", {
      child_agent_id: "child-1", tool_call_id: "call-1", status: "active", phase: "tool_executing", revision: 2,
    });
    expect(childProgressForTool("call-1")[0].progress.phase).toBe("tool_executing");

    applyRemoteWorkerEvent("temporary_agent_cancelled", {
      child_agent_id: "child-1", tool_call_id: "call-1", revision: 3,
    });
    expect(childProgressForTool("call-1")[0].progress.status).toBe("cancelled");
    expect(applyRemoteWorkerEvent("unrelated", {})).toBe(false);
  });

  it("ignores stale progress after a refresh/reconnect ordering race", () => {
    onChildCreated({ child_agent_id: "child-1", tool_call_id: "call-1" });
    onChildProgress({ child_agent_id: "child-1", phase: "model_generating", revision: 4 });
    onChildProgress({ child_agent_id: "child-1", phase: "queued", revision: 3 });

    expect(childProgressForTool("call-1")[0].progress.phase).toBe("model_generating");
  });

  it("keeps terminal children for the result card and refresh projection", () => {
    onChildCreated({ child_agent_id: "child-1", tool_call_id: "call-1" });
    onChildFinished({ child_agent_id: "child-1", status: "completed", summary: "done" });

    const rows = childProgressForTool("call-1");
    expect(rows).toHaveLength(1);
    expect(rows[0].progress.status).toBe("completed");
    expect(rows[0].progress.summary).toBe("done");
  });

  it("hydrates recent child tool activities and keeps them through terminal state", () => {
    onChildProgress({
      child_agent_id: "child-1",
      tool_call_id: "call-1",
      status: "active",
      phase: "tool_executing",
      current_tool: "bash_run",
      current_tool_call_id: "child-call-2",
      current_tool_status: "running",
      recent_tools: [
        {
          tool_call_id: "child-call-1",
          tool_name: "read_file",
          status: "succeeded",
          input_summary: "README.md",
          output_preview: "读取成功",
        },
        {
          tool_call_id: "child-call-2",
          tool_name: "bash_run",
          status: "running",
          input_summary: "Write-Output child-ok",
        },
      ],
      revision: 4,
    });

    const rows = childProgressForTool("call-1");
    expect(rows).toHaveLength(1);
    expect(rows[0].progress.recentTools).toEqual([
      expect.objectContaining({ toolName: "read_file", inputSummary: "README.md" }),
      expect.objectContaining({ toolName: "bash_run", status: "running" }),
    ]);

    onChildFinished({ child_agent_id: "child-1", status: "completed", summary: "done" });
    expect(childProgressForTool("call-1")[0].progress.recentTools).toHaveLength(2);
  });

  it("uses hydrate snapshots as the owner-scoped authority, including empty terminal snapshots", () => {
    replaceChildrenFromHydrate([
      { child_agent_id: "child-1", tool_call_id: "call-1", status: "active", revision: 2 },
    ], "parent-1");
    expect(childProgressForTool("call-1")).toHaveLength(1);

    replaceChildrenFromHydrate([
      { child_agent_id: "child-1", tool_call_id: "call-1", status: "completed", revision: 3 },
    ], "parent-1");
    expect(childProgressForTool("call-1")[0].progress.status).toBe("completed");

    replaceChildrenFromHydrate([], "parent-1");
    expect(childProgressForTool("call-1")).toEqual([]);
  });

  it("does not let an old Agent snapshot repopulate the current Agent or roll back newer SSE", () => {
    onChildProgress({ child_agent_id: "child-1", tool_call_id: "call-1", status: "active", phase: "tool_executing", revision: 5 });
    replaceChildrenFromHydrate([
      { child_agent_id: "child-1", tool_call_id: "call-1", status: "creating", revision: 4 },
    ], "parent-1");
    expect(childProgressForTool("call-1")[0].progress.revision).toBe(5);

    setRemoteWorkerParent("parent-2");
    replaceChildrenFromHydrate([
      { child_agent_id: "child-1", tool_call_id: "call-old", status: "active", revision: 7 },
    ], "parent-1");
    expect(childProgressForTool("call-1")).toEqual([]);
    expect(childProgressForTool("call-old")).toEqual([]);
  });

  it("clears omitted active children unless their lifecycle event arrived after hydrate began", () => {
    onChildProgress({ child_agent_id: "child-1", tool_call_id: "call-1", status: "active", revision: 2 });
    const snapshotStart = currentRemoteWorkerEventVersion();
    replaceChildrenFromHydrate([], "parent-1", snapshotStart);
    expect(childProgressForTool("call-1")).toEqual([]);

    const nextSnapshotStart = currentRemoteWorkerEventVersion();
    onChildProgress({ child_agent_id: "child-2", tool_call_id: "call-2", status: "active", revision: 1 });
    replaceChildrenFromHydrate([], "parent-1", nextSnapshotStart);
    expect(childProgressForTool("call-2")).toHaveLength(1);

    const reconciledStart = currentRemoteWorkerEventVersion();
    replaceChildrenFromHydrate([], "parent-1", reconciledStart);
    expect(childProgressForTool("call-2")).toEqual([]);
  });
});
