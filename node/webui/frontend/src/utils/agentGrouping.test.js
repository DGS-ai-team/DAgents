/** @vitest-environment jsdom */
import { describe, expect, it } from "vitest";
import { agentWorkspace, filterAgents, groupAgents } from "./agentGrouping.js";
import { nodePreferenceKey, readNodePreference, writeNodePreference } from "./nodePreference.js";

const agents = [
  { agent_id: "auto-1", agent_type: "auto", display_name: "巡检", workspace: { path: "D:/work" } },
  { agent_id: "normal-1", agent_type: "normal", display_name: "聊天", workspace_path: "D:/work" },
  { agent_id: "normal-2", display_name: "默认", workspace: { mode: "private" } },
];

describe("agent grouping", () => {
  it("filters by explicit type while preserving Agent identities", () => {
    expect(filterAgents(agents, "auto").map((a) => a.agent_id)).toEqual(["auto-1"]);
    expect(filterAgents(agents).map((a) => a.agent_id)).toEqual(["auto-1", "normal-1", "normal-2"]);
  });

  it("groups Auto first by type and keeps same-directory agents separate", () => {
    expect(groupAgents(agents, "type").map((g) => g.key)).toEqual(["type:auto", "type:normal"]);
    const workspaceGroups = groupAgents(agents, "workspace");
    expect(workspaceGroups.find((g) => g.key === "workspace:path:D:/work").agents.map((a) => a.agent_id)).toEqual(["auto-1", "normal-1"]);
    expect(agentWorkspace(agents[2])).toBe("");
    expect(workspaceGroups.find((g) => g.key === "workspace:private:normal-2").label).toBe("独立工作目录");
  });

  it("isolates persisted view preferences by trusted Node ID", () => {
    localStorage.clear();
    expect(nodePreferenceKey("node-a", "agent-view")).not.toBe(nodePreferenceKey("node-b", "agent-view"));
    expect(writeNodePreference("node-a", "agent-view", { filter: "auto" })).toBe(true);
    expect(readNodePreference("node-a", "agent-view", null)).toEqual({ filter: "auto" });
    expect(readNodePreference("node-b", "agent-view", null)).toBeNull();
    expect(readNodePreference("", "agent-view", "default")).toBe("default");
  });
});
