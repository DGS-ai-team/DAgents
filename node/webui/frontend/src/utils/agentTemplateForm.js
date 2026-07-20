function clone(value) {
  return JSON.parse(JSON.stringify(value ?? null));
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

export function emptyAgentDraft() {
  return {
    templateId: "",
    displayName: "",
    description: "",
    sandboxEnabled: false,
    sandboxBackend: "process",
    llmProfileId: "",
  };
}

export function readTemplateDefaults(template) {
  return asObject(template?.defaults);
}

export function draftFromTemplate(template, llmProfileIds = []) {
  const defaults = readTemplateDefaults(template);
  const draft = emptyAgentDraft();
  draft.templateId = String(template?.id || "").trim();
  draft.displayName = String(template?.display_name || template?.id || "").trim();
  draft.description = String(template?.description || "").trim();
  draft.sandboxEnabled = !!template?.sandbox?.enabled;
  draft.sandboxBackend = String(template?.sandbox?.backend || "process").trim() || "process";
  const fromTpl = String(defaults?.llm?.active || "").trim();
  const ids = Array.isArray(llmProfileIds) ? llmProfileIds.map((x) => String(x || "").trim()).filter(Boolean) : [];
  if (fromTpl && ids.includes(fromTpl)) {
    draft.llmProfileId = fromTpl;
  } else {
    draft.llmProfileId = ids[0] || fromTpl || "";
  }
  return draft;
}

/** 仅覆盖 Agent 专属字段：绑定的 LLM 配置 id；工具/Hook 等沿用模板与 Node 全局。 */
export function buildCreateAgentPayload(draft) {
  const llmActive = String(draft.llmProfileId || "").trim();
  const defaults = {};
  if (llmActive) {
    defaults.llm = { active: llmActive };
  }
  return {
    templateId: draft.templateId,
    displayName: String(draft.displayName || "").trim() || undefined,
    sandbox: {
      enabled: !!draft.sandboxEnabled,
      backend: draft.sandboxBackend || "process",
    },
    defaults,
  };
}

export function llmActiveFromAgentView(agent) {
  const snap = agent?.config_snapshot;
  let parsed = snap;
  if (typeof snap === "string") {
    try {
      parsed = JSON.parse(snap);
    } catch {
      return "";
    }
  }
  if (!parsed || typeof parsed !== "object") return "";
  const active = parsed?.defaults?.llm?.active;
  return String(active || "").trim();
}

export { clone };
