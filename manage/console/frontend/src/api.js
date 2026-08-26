const REGISTRY_API = "/v1/registry/agents";

export async function apiFetch(path, params = {}, options = {}) {
  const url = new URL(path, window.location.origin);
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "") {
      url.searchParams.set(key, String(value));
    }
  });
  const method = options.method || "GET";
  const headers = { Accept: "application/json" };
  const init = { method, headers, credentials: "include" };
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

export async function fetchAuthMe() {
  return apiFetch("/v1/auth/me");
}

export async function loginAdmin({ username, password }) {
  return apiFetch("/v1/auth/login", {}, { method: "POST", body: { username, password } });
}

export async function loginNode(nodeId) {
  return apiFetch("/v1/auth/login/node", {}, { method: "POST", body: { node_id: nodeId } });
}

export async function logoutAuth() {
  return apiFetch("/v1/auth/logout", {}, { method: "POST", body: {} });
}

export function parseGroupInput(raw) {
  return String(raw || "")
    .split(/[,?\s]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export async function saveAgentGroups(agentId, raw) {
  const discovery_group = typeof raw === "string" ? parseGroupInput(raw) : Array.isArray(raw) ? raw : [];
  return apiFetch(
    `/v1/registry/agents/${encodeURIComponent(agentId)}/groups`,
    {},
    { method: "PATCH", body: { discovery_group } },
  );
}

export async function fetchDiscoveryGroups() {
  return apiFetch("/v1/registry/discovery-groups");
}

export async function createDiscoveryGroup(name) {
  return apiFetch("/v1/registry/discovery-groups", {}, { method: "POST", body: { name } });
}

export async function deleteDiscoveryGroup(name, { detachNodes = true } = {}) {
  return apiFetch(
    `/v1/registry/discovery-groups/${encodeURIComponent(name)}`,
    { detach_nodes: detachNodes },
    { method: "DELETE" },
  );
}

export async function fetchHealth() {
  return apiFetch("/health");
}

export async function fetchAgents(params) {
  return apiFetch(REGISTRY_API, params);
}

// --- LLM ?? ---
export async function fetchLLMConfigs() {
  return apiFetch("/v1/llm/configs");
}

export async function createLLMConfig(body) {
  return apiFetch("/v1/llm/configs", {}, { method: "POST", body });
}

export async function updateLLMConfig(id, body) {
  return apiFetch(`/v1/llm/configs/${encodeURIComponent(id)}`, {}, { method: "PUT", body });
}

export async function deleteLLMConfig(id) {
  return apiFetch(`/v1/llm/configs/${encodeURIComponent(id)}`, {}, { method: "DELETE" });
}

export async function resolveLLMConfig(id) {
  return apiFetch(`/v1/llm/configs/${encodeURIComponent(id)}/resolve`);
}

export async function probeLLMModels(body) {
  return apiFetch("/v1/llm/probe-models", {}, { method: "POST", body });
}

// --- Skills ?? ---
export async function fetchSkillCatalog() {
  return apiFetch("/v1/skills/catalog");
}

export async function publishSkill(skillId, version) {
  return apiFetch(
    `/v1/skills/packages/${encodeURIComponent(skillId)}/versions/${encodeURIComponent(version)}/publish`,
    {},
    { method: "POST" },
  );
}

export async function uploadSkillPackage({ skillId, version, name, riskLevel, file }) {
  const form = new FormData();
  form.set("skill_id", skillId);
  form.set("version", version);
  form.set("name", name);
  form.set("risk_level", riskLevel || "low");
  form.set("file", file);
  const resp = await fetch(new URL("/v1/skills/packages", window.location.origin), { method: "POST", body: form, credentials: "include" });
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  return body;
}

// --- External Tools ?? ---
export async function fetchExternalToolCatalog() {
  return apiFetch("/v1/externaltools/catalog");
}

export async function publishExternalTool(toolId, version) {
  return apiFetch(
    `/v1/externaltools/packages/${encodeURIComponent(toolId)}/versions/${encodeURIComponent(version)}/publish`,
    {},
    { method: "POST" },
  );
}

export async function uploadExternalToolPackage({ toolId, version, name, platform, riskLevel, file }) {
  const form = new FormData();
  form.set("tool_id", toolId);
  form.set("version", version);
  form.set("name", name);
  form.set("platform", platform || "any");
  form.set("risk_level", riskLevel || "low");
  form.set("file", file);
  const resp = await fetch(new URL("/v1/externaltools/packages", window.location.origin), { method: "POST", body: form, credentials: "include" });
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  return body;
}

// --- Plugins ?? ---
export async function fetchPluginCatalog() {
  return apiFetch("/v1/plugins/catalog");
}

export async function publishPlugin(pluginId, version) {
  return apiFetch(
    `/v1/plugins/packages/${encodeURIComponent(pluginId)}/versions/${encodeURIComponent(version)}/publish`,
    {},
    { method: "POST" },
  );
}

export async function uploadPluginPackage({ pluginId, version, name, platform, riskLevel, file }) {
  const form = new FormData();
  form.set("plugin_id", pluginId);
  form.set("version", version);
  form.set("name", name);
  form.set("platform", platform || "any");
  form.set("risk_level", riskLevel || "low");
  form.set("file", file);
  const resp = await fetch(new URL("/v1/plugins/packages", window.location.origin), { method: "POST", body: form, credentials: "include" });
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  return body;
}

// --- Release Hub ---
export async function fetchReleasePackages(params = {}) {
  return apiFetch("/v1/releases/packages", params);
}

export async function publishReleasePackage(pkg, { setLatest = false } = {}) {
  const { artifact, channel, platform, version } = pkg;
  return apiFetch(
    `/v1/releases/packages/${encodeURIComponent(artifact)}/${encodeURIComponent(channel)}/${encodeURIComponent(platform)}/${encodeURIComponent(version)}/publish`,
    {},
    { method: "POST", body: { set_latest: setLatest } },
  );
}

export async function promoteReleasePackage(pkg) {
  const { artifact, channel, platform, version } = pkg;
  return apiFetch(
    `/v1/releases/packages/${encodeURIComponent(artifact)}/${encodeURIComponent(channel)}/${encodeURIComponent(platform)}/${encodeURIComponent(version)}/promote`,
    {},
    { method: "POST" },
  );
}

export async function deleteReleasePackage(pkg) {
  const { artifact, channel, platform, version } = pkg;
  return apiFetch(
    `/v1/releases/packages/${encodeURIComponent(artifact)}/${encodeURIComponent(channel)}/${encodeURIComponent(platform)}/${encodeURIComponent(version)}`,
    {},
    { method: "DELETE" },
  );
}

export async function uploadReleasePackage({
  version,
  platform,
  channel = "stable",
  releaseNotes = "",
  publish = false,
  setLatest = false,
  file,
}) {
  const form = new FormData();
  form.set("artifact", "dagents-local-assistant");
  form.set("version", version);
  form.set("platform", platform);
  form.set("channel", channel);
  form.set("release_notes", releaseNotes);
  form.set("publish", publish ? "true" : "false");
  form.set("set_latest", setLatest ? "true" : "false");
  form.set("file", file);
  const resp = await fetch(new URL("/v1/releases/packages", window.location.origin), { method: "POST", body: form, credentials: "include" });
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  return body;
}

// --- Cases ????---
export async function fetchCases() {
  return apiFetch("/v1/cases");
}

// --- Workgroup ---
export async function fetchWorkgroups(params = {}) {
  return apiFetch("/v1/workgroups", params);
}

export async function fetchMemberToolCatalog() {
  return apiFetch("/v1/workgroups/meta/member-tools");
}

export async function fetchWorkgroup(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}`);
}

export async function createWorkgroup(body) {
  return apiFetch("/v1/workgroups", {}, { method: "POST", body });
}

export async function patchWorkgroup(workgroupId, body) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}`, {}, { method: "PATCH", body });
}

export async function publishWorkgroup(workgroupId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/publish`,
    {},
    { method: "POST", body: {} },
  );
}

export async function archiveWorkgroup(workgroupId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/archive`,
    {},
    { method: "POST", body: {} },
  );
}

export async function fetchWorkgroupTimeline(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/timeline`);
}

export async function listWorkgroupHITL(workgroupId, pendingOnly = true) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/hitl`, {
    pending_only: pendingOnly ? "true" : "false",
  });
}

export async function resolveWorkgroupHITL(workgroupId, hitlId, answer, resolution = null) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/hitl/${encodeURIComponent(hitlId)}/resolve`,
    {},
    { method: "POST", body: { answer, resolution: resolution || { answer } } },
  );
}

export async function listWorkgroupRuns(workgroupId, { actorId, limit = 20 } = {}) {
  const params = { limit };
  if (actorId) params.actor_id = actorId;
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/runs`, params);
}

export async function getWorkgroupRunHistory(workgroupId, runId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/runs/${encodeURIComponent(runId)}/history`,
  );
}

export async function fetchWorkgroupLLMConfigs(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/llm-configs`);
}

export async function fetchWorkgroupMembers(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/members`);
}

export async function fetchWorkgroupMemberSpec(workgroupId, memberId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/members/${encodeURIComponent(memberId)}/spec`,
  );
}

export async function fetchWorkgroupACL(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/acl`);
}

export async function patchWorkgroupACL(workgroupId, body) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/acl`,
    {},
    { method: "PATCH", body },
  );
}

export async function createWorkgroupMember(workgroupId, body) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/members`,
    {},
    { method: "POST", body },
  );
}

export async function patchWorkgroupMember(workgroupId, memberId, body) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/members/${encodeURIComponent(memberId)}`,
    {},
    { method: "PATCH", body },
  );
}

export async function archiveWorkgroupMember(workgroupId, memberId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/members/${encodeURIComponent(memberId)}/archive`,
    {},
    { method: "POST", body: {} },
  );
}

export async function postWorkgroupMessage(workgroupId, body) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/messages`, {}, { method: "POST", body });
}

export async function cancelWorkgroupTurn(workgroupId, body = {}) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/turn/cancel`,
    {},
    { method: "POST", body },
  );
}

export async function cancelWorkgroupAssign(workgroupId, assignId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/assigns/${encodeURIComponent(assignId)}/cancel`,
    {},
    { method: "POST", body: {} },
  );
}

export async function cancelWorkgroupTool(workgroupId, assignId, toolCallId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/assigns/${encodeURIComponent(assignId)}/tools/${encodeURIComponent(toolCallId)}/cancel`,
    {},
    { method: "POST", body: {} },
  );
}

export async function fetchWorkgroupHumanQueue(workgroupId) {
  return apiFetch(`/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue`);
}

export async function patchWorkgroupHumanQueueItem(workgroupId, queueId, text) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue/${encodeURIComponent(queueId)}`,
    {},
    { method: "PATCH", body: { text } },
  );
}

export async function cancelWorkgroupHumanQueueItem(workgroupId, queueId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue/${encodeURIComponent(queueId)}`,
    {},
    { method: "DELETE" },
  );
}

export async function sendWorkgroupHumanQueueItemNow(workgroupId, queueId) {
  return apiFetch(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/human-queue/${encodeURIComponent(queueId)}/send-now`,
    {},
    { method: "POST", body: {} },
  );
}

/**
 * 工作组消息 SSE；onEvent(eventName, data)。可传 signal 以中断读取。
 * @returns {Promise<{ finalText?: string }>}
 */
export async function postWorkgroupMessageStream(workgroupId, body, { onEvent, signal } = {}) {
  const url = new URL(
    `/v1/workgroups/${encodeURIComponent(workgroupId)}/messages/stream`,
    window.location.origin,
  );
  const resp = await fetch(url, {
    method: "POST",
    credentials: "include",
    headers: {
      Accept: "text/event-stream",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
    signal,
  });
  if (!resp.ok) {
    let message = `HTTP ${resp.status}`;
    try {
      const errBody = await resp.json();
      const detail = errBody?.detail;
      if (typeof detail === "string") message = detail;
      else if (detail?.message) message = detail.message;
    } catch {
      /* ignore */
    }
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  if (!resp.body) {
    throw new Error("??????????");
  }

  const reader = resp.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";
  let finalText = "";
  let sawError = null;

  const flushBlock = (block) => {
    const lines = block.split(/\r?\n/);
    let eventName = "message";
    const dataLines = [];
    for (const line of lines) {
      if (line.startsWith("event:")) eventName = line.slice(6).trim() || "message";
      else if (line.startsWith("data:")) dataLines.push(line.slice(5).trimStart());
    }
    if (!dataLines.length && eventName === "message") return;
    let data = {};
    const raw = dataLines.join("\n");
    if (raw) {
      try {
        data = JSON.parse(raw);
      } catch {
        data = { raw };
      }
    }
    if (eventName === "error") {
      sawError = data;
    }
    if (eventName === "final" || eventName === "assistant_final") {
      finalText = data?.loop?.final_text || data?.text || finalText;
    }
    if (typeof onEvent === "function") onEvent(eventName, data);
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    buffer = buffer.replace(/\r\n/g, "\n");
    let idx;
    while ((idx = buffer.indexOf("\n\n")) >= 0) {
      const block = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      if (block.trim()) flushBlock(block);
    }
  }
  if (buffer.trim()) flushBlock(buffer);

  if (sawError) {
    const err = new Error(sawError.message || sawError.code || "??????");
    err.detail = sawError;
    throw err;
  }
  return { finalText };
}

export async function parseCaseJsonl(file) {
  const form = new FormData();
  form.set("file", file);
  const resp = await fetch(new URL("/v1/cases/parse-jsonl", window.location.origin), { method: "POST", body: form, credentials: "include" });
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    throw new Error(message);
  }
  return body;
}

export async function replaceCaseMessages(caseId, messages) {
  return apiFetch(
    `/v1/cases/${encodeURIComponent(caseId)}/messages`,
    {},
    { method: "PUT", body: { messages } },
  );
}

export async function createCase({
  name,
  description = "",
  skillIds = [],
  pluginIds = [],
  externaltoolIds = [],
  file = null,
}) {
  const form = new FormData();
  form.set("name", name);
  form.set("description", description);
  form.set("skill_ids", (skillIds || []).join(", "));
  form.set("plugin_ids", (pluginIds || []).join(", "));
  form.set("externaltool_ids", (externaltoolIds || []).join(", "));
  if (file) form.set("file", file);
  const resp = await fetch(new URL("/v1/cases", window.location.origin), { method: "POST", body: form, credentials: "include" });
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    throw new Error(message);
  }
  return body;
}

export async function patchCase(caseId, payload) {
  return apiFetch(`/v1/cases/${encodeURIComponent(caseId)}`, {}, { method: "PATCH", body: payload });
}

export async function deleteCase(caseId) {
  return apiFetch(`/v1/cases/${encodeURIComponent(caseId)}`, {}, { method: "DELETE" });
}

export async function importCaseJsonl(caseId, file, { replace = true } = {}) {
  const form = new FormData();
  form.set("file", file);
  form.set("replace", replace ? "true" : "false");
  const resp = await fetch(
    new URL(`/v1/cases/${encodeURIComponent(caseId)}/import-jsonl`, window.location.origin),
    { method: "POST", body: form, credentials: "include" },
  );
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    throw new Error(message);
  }
  return body;
}

export async function insertCaseMessage(caseId, payload) {
  return apiFetch(`/v1/cases/${encodeURIComponent(caseId)}/messages`, {}, { method: "POST", body: payload });
}

export async function updateCaseMessage(caseId, messageId, payload) {
  return apiFetch(
    `/v1/cases/${encodeURIComponent(caseId)}/messages/${encodeURIComponent(messageId)}`,
    {},
    { method: "PATCH", body: payload },
  );
}

export async function deleteCaseMessage(caseId, messageId) {
  return apiFetch(
    `/v1/cases/${encodeURIComponent(caseId)}/messages/${encodeURIComponent(messageId)}`,
    {},
    { method: "DELETE" },
  );
}

export async function exportCaseJsonl(caseId) {
  const resp = await fetch(
    new URL(`/v1/cases/${encodeURIComponent(caseId)}/export/jsonl`, window.location.origin),
    { credentials: "include" },
  );
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}`);
  }
  const blob = await resp.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `${caseId}.jsonl`;
  a.click();
  URL.revokeObjectURL(url);
}

export async function uploadCaseAttachment(caseId, file) {
  const form = new FormData();
  form.set("file", file);
  const resp = await fetch(
    new URL(`/v1/cases/${encodeURIComponent(caseId)}/attachments`, window.location.origin),
    { method: "POST", body: form, credentials: "include" },
  );
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const message = typeof body?.detail === "string" ? body.detail : `HTTP ${resp.status}`;
    throw new Error(message);
  }
  return body;
}

export async function deleteCaseAttachment(caseId, blobId) {
  return apiFetch(
    `/v1/cases/${encodeURIComponent(caseId)}/attachments/${encodeURIComponent(blobId)}`,
    {},
    { method: "DELETE" },
  );
}

export function blobDownloadUrl(blobId) {
  return `/v1/blobs/${encodeURIComponent(blobId)}`;
}

export { REGISTRY_API };
