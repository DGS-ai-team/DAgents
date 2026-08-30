/**
 * 工作组成员工具组 ↔ allow_tool_names 互转（对齐 Node WorkgroupMemberModal）。
 * API 仍提交扁平 allow_tool_names；UI 按 catalog.groups 编辑。
 */

/** @typedef {{ id: string, label: string, hint?: string, defaultOn?: boolean, toolIds: string[] }} MemberToolGroup */

/** catalog 失败时的离线兜底：fs 默认开、bash 默认关。 */
export const FALLBACK_MEMBER_TOOL_GROUPS = /** @type {MemberToolGroup[]} */ ([
  {
    id: "fs",
    label: "文件系统",
    hint: "读/写/搜索等工作区文件工具",
    defaultOn: true,
    toolIds: [
      "read_file",
      "show_image",
      "read_image",
      "write_file",
      "glob_files",
      "grep_file",
      "grep_files",
      "search_replace",
    ],
  },
  {
    id: "bash",
    label: "Shell",
    hint: "bash_run 等（无额外沙箱，默认不勾选）",
    defaultOn: false,
    toolIds: ["bash_run"],
  },
]);

/**
 * @param {MemberToolGroup[]} [groups]
 * @returns {string[]}
 */
export function defaultMemberGroupIds(groups = FALLBACK_MEMBER_TOOL_GROUPS) {
  return (groups || []).filter((g) => g.defaultOn).map((g) => g.id);
}

/**
 * @param {string[]} groupIds
 * @param {MemberToolGroup[]} [groups]
 * @returns {string[]}
 */
export function expandMemberGroupsToTools(groupIds, groups = FALLBACK_MEMBER_TOOL_GROUPS) {
  const want = new Set((groupIds || []).map(String));
  const out = [];
  const seen = new Set();
  for (const g of groups || []) {
    if (!want.has(g.id)) continue;
    for (const id of g.toolIds || []) {
      if (seen.has(id)) continue;
      seen.add(id);
      out.push(id);
    }
  }
  return out;
}

/**
 * @param {string[]} allowNames
 * @param {MemberToolGroup[]} [groups]
 * @returns {string[]}
 */
export function memberGroupsFromAllowNames(allowNames, groups = FALLBACK_MEMBER_TOOL_GROUPS) {
  const allow = new Set((allowNames || []).map(String));
  return (groups || [])
    .filter((g) => (g.toolIds || []).some((id) => allow.has(id)))
    .map((g) => g.id);
}

/**
 * @param {string[]} groupIds
 * @param {MemberToolGroup[]} [groups]
 * @returns {string}
 */
export function formatMemberGroupLabels(groupIds, groups = FALLBACK_MEMBER_TOOL_GROUPS) {
  const byId = new Map((groups || []).map((g) => [g.id, g.label || g.id]));
  const labels = (groupIds || []).map((id) => byId.get(id) || id).filter(Boolean);
  return labels.length ? labels.join("、") : "—";
}

/**
 * @param {{ tools?: any[], groups?: any[] } | null | undefined} catalog
 * @returns {MemberToolGroup[]}
 */
export function memberToolGroupsFromCatalog(catalog) {
  const tools = Array.isArray(catalog?.tools) ? catalog.tools : [];
  const groups = Array.isArray(catalog?.groups) ? catalog.groups : [];
  /** @type {Map<string, MemberToolGroup>} */
  const byGroup = new Map();
  for (const t of tools) {
    const gid = String(t.group || "").trim() || "other";
    if (!byGroup.has(gid)) {
      byGroup.set(gid, {
        id: gid,
        label: String(t.group_label || gid).trim() || gid,
        hint: "",
        defaultOn: false,
        toolIds: [],
      });
    }
    const entry = byGroup.get(gid);
    const tid = String(t.id || "").trim();
    if (tid) entry.toolIds.push(tid);
    if (t.default) entry.defaultOn = true;
    if (!entry.hint && t.hint) entry.hint = String(t.hint);
  }
  /** @type {MemberToolGroup[]} */
  const ordered = [];
  const seen = new Set();
  for (const g of groups) {
    const id = String(g.id || "").trim();
    if (!id || !byGroup.has(id)) continue;
    const entry = byGroup.get(id);
    if (g.label) entry.label = String(g.label);
    ordered.push(entry);
    seen.add(id);
  }
  for (const [id, entry] of byGroup) {
    if (!seen.has(id)) ordered.push(entry);
  }
  return ordered.length ? ordered : FALLBACK_MEMBER_TOOL_GROUPS.map((g) => ({
    ...g,
    toolIds: [...g.toolIds],
  }));
}
