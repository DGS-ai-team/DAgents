function clone(value) {
  return JSON.parse(JSON.stringify(value ?? null));
}

function asObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

/** 有正文才开启注入；开关由内容推导，UI 不再单独暴露。 */
function promptFieldEnabled(text) {
  return String(text || "").trim().length > 0;
}

export const BLANK_TEMPLATE_ID = "__blank__";

const LEGACY_TOOL_GROUP_ALIASES = Object.freeze({ linux: "terminal" });

export function canonicalToolGroupName(value) {
  const name = String(value || "").trim();
  return LEGACY_TOOL_GROUP_ALIASES[name] || name;
}

export function normalizeToolGroupNames(groups) {
  const seen = new Set();
  const out = [];
  for (const value of Array.isArray(groups) ? groups : []) {
    const name = canonicalToolGroupName(value);
    if (!name || seen.has(name)) continue;
    seen.add(name);
    out.push(name);
  }
  return out;
}

export const TOOL_GROUPS = [
  { name: "bash", label: "命令行" },
  { name: "browser", label: "浏览器", beta: true, hint: "任务级派发至伴生（需真实 LLM）" },
  { name: "child_agents", label: "子智能体" },
  { name: "fs", label: "文件" },
  { name: "hitl", label: "用户询问" },
  { name: "memory", label: "记忆" },
  { name: "skills", label: "技能" },
  { name: "terminal", label: "终端与 Linux 通道", hint: "本机终端、SSH 命令与文件传输" },
  { name: "triggers", label: "定时任务" },
  { name: "wecom", label: "企业微信" },
];

/**
 * 按 Node 能力过滤可展示的工具组。
 * 优先使用 setup.available_tool_groups；否则按 features.browser_enabled / wecom_enabled 回退。
 */
export function toolGroupsFromSetup(setup, all = TOOL_GROUPS) {
  const names = Array.isArray(setup?.available_tool_groups)
    ? normalizeToolGroupNames(setup.available_tool_groups)
    : null;
  if (names && names.length) {
    const allow = new Set(names);
    return all.filter((g) => allow.has(g.name));
  }
  const features = setup?.features && typeof setup.features === "object" ? setup.features : {};
  return all.filter((g) => {
    if (g.name === "browser") return !!features.browser_enabled;
    if (g.name === "wecom") return !!features.wecom_enabled;
    return true;
  });
}

/** 将 draft.toolGroups 限制在 available 清单内（就地修改）。 */
export function pruneDraftToolGroups(draft, availableGroups) {
  if (!draft || typeof draft !== "object") return;
  const allow = new Set(
    (Array.isArray(availableGroups) ? availableGroups : [])
      .map((g) => (typeof g === "string" ? g : g?.name))
      .map((x) => String(x || "").trim())
      .filter(Boolean),
  );
  const cur = Array.isArray(draft.toolGroups) ? draft.toolGroups : [];
  draft.toolGroups = normalizeToolGroupNames(cur).filter((n) => allow.has(n));
}

export const LONG_TERM_SCOPES = [
  { value: "agent", label: "仅本智能体" },
  { value: "global", label: "本机所有智能体共享" },
];

export function emptyAgentDraft() {
  return {
    // 溯源：创建请求可不传；仅用于 UI 展示来源模板
    templateId: "",
    displayName: "",
    description: "",
    role: "assistant",
    llmProfileId: "",
    maxToolLoops: 32,
    toolGroups: [],
    // null = 不限制（全部可见）；string[] = 显式白名单（可为空）
    visibleSkills: null,
    promptSoulEnabled: true,
    promptCustomEnabled: true,
    promptLongTermEnabled: true,
    promptLongTermScope: "agent",
    promptSoulMd: "",
    promptCustomMd: "",
  };
}

/** 从 agent.host JSON 取 OS 展示标签。 */
export function agentHostLabel(agent) {
  const host = asObject(typeof agent?.host === "string" ? (() => {
    try { return JSON.parse(agent.host); } catch { return {}; }
  })() : agent?.host);
  const label = String(host.display_label || "").trim();
  if (label) return label;
  const kind = String(host.os_kind || host.sys_platform || "").trim().toLowerCase();
  if (kind === "windows") return "Windows";
  if (kind === "darwin") return "macOS";
  if (kind === "linux") return "Linux";
  if (kind) return kind.charAt(0).toUpperCase() + kind.slice(1);
  return "";
}

export function readTemplateDefaults(template) {
  return asObject(template?.defaults);
}

function boolOr(value, fallback) {
  return typeof value === "boolean" ? value : fallback;
}

function numberOr(value, fallback) {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

/** 技能能力与工具组 skills 收敛：未收窄（空列表）视为开启；否则看是否勾选 skills。 */
export function skillsEnabledFromToolGroups(toolGroups) {
  const groups = Array.isArray(toolGroups) ? toolGroups : [];
  if (groups.length === 0) return true;
  return groups.some((g) => String(g || "").trim() === "skills");
}

/** 启用「记忆」工具组即视为开启长期记忆注入。 */
export function memoryEnabledFromToolGroups(toolGroups) {
  const groups = Array.isArray(toolGroups) ? toolGroups : [];
  return groups.some((g) => String(g || "").trim() === "memory");
}

/** null = 未限制；数组 = 显式白名单。 */
function normalizeVisibleSkills(skills) {
  if (!skills || typeof skills !== "object" || !Object.prototype.hasOwnProperty.call(skills, "visible")) {
    return null;
  }
  if (!Array.isArray(skills.visible)) return [];
  const out = [];
  const seen = new Set();
  for (const item of skills.visible) {
    const name = String(item || "").trim();
    if (!name || seen.has(name)) continue;
    seen.add(name);
    out.push(name);
  }
  return out;
}

/** 写入 defaults.skills：能力由工具组控制；此处仅保留 visible 白名单。 */
function skillsPayload(draft) {
  if (!skillsEnabledFromToolGroups(draft?.toolGroups)) {
    return {};
  }
  if (draft?.visibleSkills === null || draft?.visibleSkills === undefined) {
    return {};
  }
  const visible = Array.isArray(draft.visibleSkills)
    ? draft.visibleSkills.map((x) => String(x || "").trim()).filter(Boolean)
    : [];
  return { visible };
}

/** 从模板展开为可编辑草稿（创建时由前端持有完整设置）。 */
/** 空白 Agent 草稿（不依赖模板）。 */
export function draftFromBlank(llmProfileIds = []) {
  const draft = emptyAgentDraft();
  const ids = Array.isArray(llmProfileIds) ? llmProfileIds.map((x) => String(x || "").trim()).filter(Boolean) : [];
  draft.llmProfileId = ids[0] || "";
  return draft;
}

export function draftFromTemplate(template, llmProfileIds = []) {
  const defaults = readTemplateDefaults(template);
  const agent = asObject(defaults.agent);
  const llm = asObject(defaults.llm);
  const tools = asObject(defaults.tools);
  const skills = asObject(defaults.skills);
  const prompt = asObject(defaults.prompt_context);

  const draft = emptyAgentDraft();
  draft.templateId = String(template?.id || "").trim();
  draft.displayName = String(template?.display_name || template?.id || "").trim();
  draft.description = String(template?.description || agent.description || "").trim();
  draft.role = String(agent.role || "assistant").trim() || "assistant";
  draft.maxToolLoops = numberOr(llm.max_tool_loops, 32);
  draft.toolGroups = normalizeToolGroupNames(tools.enabled_groups);
  draft.visibleSkills = normalizeVisibleSkills(skills);
  draft.promptSoulEnabled = boolOr(prompt.soul_enabled, true);
  draft.promptCustomEnabled = boolOr(prompt.custom_enabled, true);
  draft.promptLongTermEnabled = boolOr(prompt.long_term_enabled, true);
  draft.promptLongTermScope = String(prompt.long_term_scope || "agent").trim() === "global" ? "global" : "agent";
  draft.promptSoulMd = String(prompt.soul_md || "").trim();
  draft.promptCustomMd = String(prompt.custom_md || "").trim();

  const fromTpl = String(llm.active || "").trim();
  const ids = Array.isArray(llmProfileIds) ? llmProfileIds.map((x) => String(x || "").trim()).filter(Boolean) : [];
  if (fromTpl && ids.includes(fromTpl)) {
    draft.llmProfileId = fromTpl;
  } else {
    draft.llmProfileId = ids[0] || fromTpl || "";
  }
  return draft;
}

/** 从已有 Agent 视图还原草稿（设置页编辑）。 */
export function draftFromAgentView(agent, llmProfileIds = []) {
  const snap = parseSnapshot(agent?.config_snapshot);
  const defaults = asObject(snap.defaults);
  const agentMeta = asObject(defaults.agent);
  const llm = asObject(defaults.llm);
  const tools = asObject(defaults.tools);
  const skills = asObject(defaults.skills);
  const prompt = asObject(defaults.prompt_context);

  const draft = emptyAgentDraft();
  draft.templateId = String(agent?.template_id || snap.template_id || "").trim();
  draft.displayName = String(agent?.display_name || "").trim();
  draft.description = String(agentMeta.description || "").trim();
  draft.role = String(agentMeta.role || "assistant").trim() || "assistant";
  draft.maxToolLoops = numberOr(llm.max_tool_loops, 32);
  draft.toolGroups = normalizeToolGroupNames(tools.enabled_groups);
  draft.visibleSkills = normalizeVisibleSkills(skills);
  draft.promptSoulEnabled = boolOr(prompt.soul_enabled, true);
  draft.promptCustomEnabled = boolOr(prompt.custom_enabled, true);
  draft.promptLongTermEnabled = boolOr(prompt.long_term_enabled, true);
  draft.promptLongTermScope = String(prompt.long_term_scope || "agent").trim() === "global" ? "global" : "agent";

  const fromSnap = String(llm.active || "").trim();
  const ids = Array.isArray(llmProfileIds) ? llmProfileIds.map((x) => String(x || "").trim()).filter(Boolean) : [];
  if (fromSnap && ids.includes(fromSnap)) {
    draft.llmProfileId = fromSnap;
  } else {
    draft.llmProfileId = ids[0] || fromSnap || "";
  }
  return draft;
}

function parseSnapshot(snap) {
  if (!snap) return {};
  if (typeof snap === "string") {
    try {
      return asObject(JSON.parse(snap));
    } catch {
      return {};
    }
  }
  return asObject(snap);
}

/** 创建入参：完整 Agent 设置；template_id 仅溯源可选。 */
export function buildCreateAgentPayload(draft) {
  const llmActive = String(draft.llmProfileId || "").trim();
  const payload = {
    display_name: String(draft.displayName || "").trim() || undefined,
    defaults: {
      agent: {
        role: String(draft.role || "assistant").trim() || "assistant",
        description: String(draft.description || "").trim(),
      },
      llm: {
        ...(llmActive ? { active: llmActive } : {}),
        max_tool_loops: numberOr(draft.maxToolLoops, 32),
      },
      tools: {
        enabled_groups: normalizeToolGroupNames(draft.toolGroups),
      },
      skills: skillsPayload(draft),
      prompt_context: {
        soul_enabled: promptFieldEnabled(draft.promptSoulMd),
        custom_enabled: promptFieldEnabled(draft.promptCustomMd),
        long_term_enabled: memoryEnabledFromToolGroups(draft.toolGroups),
        long_term_scope: draft.promptLongTermScope === "global" ? "global" : "agent",
        ...(String(draft.promptSoulMd || "").trim()
          ? { soul_md: String(draft.promptSoulMd).trim() }
          : {}),
        ...(String(draft.promptCustomMd || "").trim()
          ? { custom_md: String(draft.promptCustomMd).trim() }
          : {}),
      },
    },
  };
  const tpl = String(draft.templateId || "").trim();
  if (tpl && tpl !== BLANK_TEMPLATE_ID) payload.template_id = tpl;
  return payload;
}

/** PATCH 入参：完整可编辑字段。 */
export function buildPatchAgentPayload(draft) {
  const created = buildCreateAgentPayload(draft);
  return {
    display_name: created.display_name,
    defaults: created.defaults,
  };
}

/** 从 Agent 草稿构建创建模板 API 入参。 */
export function buildCreateTemplatePayload(meta, draft) {
  const id = String(meta?.id || "").trim();
  const displayName = String(meta?.displayName || meta?.display_name || "").trim();
  const description = String(meta?.description || "").trim();
  const llmActive = String(draft?.llmProfileId || "").trim();
  return {
    id,
    display_name: displayName || id,
    description,
    version: 1,
    defaults: {
      agent: {
        role: String(draft?.role || "assistant").trim() || "assistant",
        description: String(draft?.description || "").trim(),
      },
      llm: {
        ...(llmActive ? { active: llmActive } : {}),
        max_tool_loops: numberOr(draft?.maxToolLoops, 32),
      },
      tools: {
        enabled_groups: normalizeToolGroupNames(draft?.toolGroups),
      },
      skills: skillsPayload(draft),
      prompt_context: {
        soul_enabled: promptFieldEnabled(draft?.promptSoulMd),
        custom_enabled: promptFieldEnabled(draft?.promptCustomMd),
        long_term_enabled: memoryEnabledFromToolGroups(draft?.toolGroups),
        ...(String(draft?.promptSoulMd || "").trim()
          ? { soul_md: String(draft.promptSoulMd).trim() }
          : {}),
        ...(String(draft?.promptCustomMd || "").trim()
          ? { custom_md: String(draft.promptCustomMd).trim() }
          : {}),
      },
    },
  };
}

export function llmActiveFromAgentView(agent) {
  const snap = parseSnapshot(agent?.config_snapshot);
  const active = snap?.defaults?.llm?.active;
  return String(active || "").trim();
}

export { clone };
