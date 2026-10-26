export function agentType(agent) {
  return String(agent?.agent_type || agent?.AgentType || "").toLowerCase() === "auto" ? "auto" : "normal";
}

export function agentWorkspace(agent) {
  return String(agent?.workspace?.path || agent?.workspace_path || agent?.workspace?.root || "").trim();
}

export function workspaceGroup(agent) {
  const path = agentWorkspace(agent);
  if (path) return { key: `workspace:path:${path}`, label: path };
  const mode = String(agent?.workspace?.mode || agent?.workspace_mode || "").toLowerCase();
  if (mode === "private") return { key: `workspace:private:${String(agent?.agent_id || agent?.id || "")}`, label: "独立工作目录" };
  return { key: "workspace:unknown", label: "未指定工作目录" };
}

export function filterAgents(agents, filter = "all") {
  return (Array.isArray(agents) ? agents : []).filter((agent) => filter === "all" || agentType(agent) === filter);
}

export function searchAgents(agents, query = "") {
  const q = String(query || "").trim().toLowerCase();
  if (!q) return Array.isArray(agents) ? agents : [];
  return (Array.isArray(agents) ? agents : []).filter((agent) =>
    [agent?.display_name, agent?.name, agent?.agent_id, agentWorkspace(agent), agentType(agent)]
      .some((value) => String(value || "").toLowerCase().includes(q)),
  );
}

export function groupAgents(agents, mode = "type") {
  const groups = new Map();
  for (const agent of Array.isArray(agents) ? agents : []) {
    const key = mode === "workspace" ? workspaceGroup(agent).key : `type:${agentType(agent)}`;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(agent);
  }
  if (mode === "type") {
    return ["type:auto", "type:normal"].filter((key) => groups.has(key)).map((key) => ({ key, label: key === "type:auto" ? "自主智能体" : "普通智能体", agents: groups.get(key) }));
  }
  return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([key, rows]) => ({
    key,
    label: key === "workspace:unknown" ? "未指定工作目录" : key.startsWith("workspace:private:") ? "独立工作目录" : key.slice("workspace:path:".length),
    agents: rows,
  }));
}
