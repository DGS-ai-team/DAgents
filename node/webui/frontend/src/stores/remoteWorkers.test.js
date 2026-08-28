import { beforeEach, describe, expect, it } from "vitest";
import {
  childProgressForTool,
  onChildCreated,
  onChildFinished,
  onChildProgress,
  resetRemoteWorkers,
} from "./remoteWorkers.js";

describe("remote child agent progress", () => {
  beforeEach(() => {
    resetRemoteWorkers();
  });

  it("keeps progress associated with the parent tool call", () => {
    onChildCreated({
      child_agent_id: "child-1",
      tool_call_id: "call-create-1",
      purpose: "inspect files",
      max_turns: 8,
    });
    onChildProgress({
      child_agent_id: "child-1",
      tool_call_id: "call-create-1",
      status: "active",
      phase: "tool_executing",
      turn_count: 2,
      max_turns: 8,
      current_tool: "bash_run",
      current_tool_status: "running",
      revision: 2,
    });

    const rows = childProgressForTool("call-create-1");
    expect(rows).toHaveLength(1);
    expect(rows[0].progress.currentTool).toBe("bash_run");
    expect(rows[0].progress.turnCount).toBe(2);
  });

  it("ignores stale progress after a refresh/reconnect ordering race", () => {
    onChildCreated({ child_agent_id: "child-1", tool_call_id: "call-1" });
    onChildProgress({ child_agent_id: "child-1", phase: "model_generating", revision: 4 });
    onChildProgress({ child_agent_id: "child-1", phase: "queued", revision: 3 });

    expect(childProgressForTool("call-1")[0].progress.phase).toBe("model_generating");
  });

  it("removes terminal children from the active progress projection", () => {
    onChildCreated({ child_agent_id: "child-1", tool_call_id: "call-1" });
    onChildFinished({ child_agent_id: "child-1" });

    expect(childProgressForTool("call-1")).toEqual([]);
  });
});
