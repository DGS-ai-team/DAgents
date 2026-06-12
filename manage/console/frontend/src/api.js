const REGISTRY_API = "/v1/registry/agents";
const INBOX_API = "/v1/admin/a2a/tasks";

export async function apiFetch(path, params = {}, options = {}) {
  const url = new URL(path, window.location.origin);
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "") {
      url.searchParams.set(key, String(value));
    }
  });
  const method = options.method || "GET";
  const headers = { Accept: "application/json" };
  const init = { method, headers };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }
  const resp = await fetch(url, init);
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const detail = body?.detail;
    const message = typeof detail === "string" ? detail : `HTTP ${resp.status}`;
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  return body;
}

export function parseGroupInput(raw) {
  return String(raw || "")
    .split(/[,，\s]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export async function saveAgentGroups(agentId, raw) {
  const discovery_group = parseGroupInput(raw);
  if (!discovery_group.length) {
    throw new Error("至少填写一个 discovery_group");
  }
  return apiFetch(
    `/v1/registry/agents/${encodeURIComponent(agentId)}/groups`,
    {},
    { method: "PATCH", body: { discovery_group } },
  );
}

export async function bulkAssignGroups(agents, rawGroups) {
  const groups = parseGroupInput(rawGroups);
  if (!groups.length) {
    throw new Error("至少填写一个 discovery_group");
  }
  for (const agent of agents) {
    await saveAgentGroups(agent.agent_id, groups.join(", "));
    agent.discovery_group = groups;
  }
}

export async function fetchHealth() {
  return apiFetch("/health");
}

export async function fetchAgents(params) {
  return apiFetch(REGISTRY_API, params);
}

export async function fetchInboxTasks(params) {
  return apiFetch(INBOX_API, params);
}

export async function fetchAudit(limit = 100) {
  return apiFetch("/v1/admin/audit", { limit });
}

export async function fetchNodeSessions(agentId) {
  return apiFetch(`/v1/admin/nodes/${encodeURIComponent(agentId)}/sessions`);
}

export async function fetchNodeSessionContext(agentId, sessionId) {
  return apiFetch(
    `/v1/admin/nodes/${encodeURIComponent(agentId)}/sessions/${encodeURIComponent(sessionId)}/context`,
  );
}

export { REGISTRY_API };
