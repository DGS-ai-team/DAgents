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

export async function fetchHealth() {
  return apiFetch("/health");
}

export async function fetchAgents(params) {
  return apiFetch(REGISTRY_API, params);
}

export async function fetchAudit(limit = 100) {
  return apiFetch("/v1/admin/audit", { limit });
}

// --- LLM 配置注册中心 ---
export async function fetchLLMConfigs() {
  return apiFetch("/v1/llm/configs");
}

export async function createLLMConfig(body) {
  return apiFetch("/v1/llm/configs", {}, { method: "POST", body });
}

export async function deleteLLMConfig(id) {
  return apiFetch(`/v1/llm/configs/${encodeURIComponent(id)}`, {}, { method: "DELETE" });
}

export async function resolveLLMConfig(id) {
  return apiFetch(`/v1/llm/configs/${encodeURIComponent(id)}/resolve`);
}

// --- Skills 分发 ---
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
  const resp = await fetch(new URL("/v1/skills/packages", window.location.origin), {
    method: "POST",
    body: form,
  });
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

// --- External Tools 分发 ---
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
  const resp = await fetch(new URL("/v1/externaltools/packages", window.location.origin), {
    method: "POST",
    body: form,
  });
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

// --- Plugins 分发 ---
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
  const resp = await fetch(new URL("/v1/plugins/packages", window.location.origin), {
    method: "POST",
    body: form,
  });
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
  const resp = await fetch(new URL("/v1/releases/packages", window.location.origin), {
    method: "POST",
    body: form,
  });
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

// --- Cases 案例库 ---
export async function fetchCases() {
  return apiFetch("/v1/cases");
}

export async function parseCaseJsonl(file) {
  const form = new FormData();
  form.set("file", file);
  const resp = await fetch(new URL("/v1/cases/parse-jsonl", window.location.origin), {
    method: "POST",
    body: form,
  });
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
  const resp = await fetch(new URL("/v1/cases", window.location.origin), {
    method: "POST",
    body: form,
  });
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
    { method: "POST", body: form },
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
    { method: "POST", body: form },
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
