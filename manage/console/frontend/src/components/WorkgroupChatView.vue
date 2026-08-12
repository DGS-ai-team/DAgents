<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import {
  fetchAuthMe,
  fetchAgents,
  fetchWorkgroup,
  fetchWorkgroupACL,
  fetchWorkgroupMembers,
  fetchWorkgroupTimeline,
  fetchWorkgroupHumanQueue,
  patchWorkgroupHumanQueueItem,
  cancelWorkgroupHumanQueueItem,
  postWorkgroupMessageStream,
  cancelWorkgroupTurn,
  listWorkgroupHITL,
  resolveWorkgroupHITL,
  listWorkgroupRuns,
  getWorkgroupRunHistory,
} from "../api.js";
import { renderMarkdown } from "../utils/markdown.js";
import brandIcon from "@dagents-brand/brand-icon.png";
import BrandActivityIndicator from "../../../../../node/webui/frontend/src/components/BrandActivityIndicator.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
  workgroupId: { type: String, default: "" },
  displayName: { type: String, default: "" },
});

const emit = defineEmits(["toast", "close"]);

const loading = ref(false);
const loadingMembers = ref(false);
const sending = ref(false);
const cancelling = ref(false);
/** @type {import('vue').Ref<Array<Record<string, any>>>} */
const humanQueueItems = ref([]);
const editingQueueId = ref("");
const editQueueDraft = ref("");
let queuePollTimer = null;
const timeline = ref([]);
const members = ref([]);
const nodeDirectory = ref([]);
const acl = ref(null);
const workgroupStatus = ref("");
const fromNodeId = ref("console");
const input = ref("");
const textareaRef = ref(null);
const streamRef = ref(null);
const followTail = ref(true);
const mentionOpen = ref(false);
const mentionQuery = ref("");
const directMember = ref(null);
/** 编排态：成员回报折叠展开态 */
const expandedMemberReports = ref({});
const pendingHitl = ref([]);
const hitlBusy = ref(false);
const hitlDraft = ref("");
/** RunHistory 调试 */
const debugOpen = ref(false);
const debugLoading = ref(false);
const debugRuns = ref([]);
const debugLlm = ref(null);
const debugSelectedRunId = ref("");
const debugHistory = ref(null);
const debugError = ref("");
/** 当前发送的 AbortController，取消时 abort SSE */
let streamAbort = null;

/** 发送中的本地气泡：user 乐观消息 + assistant 流式占位 */
const liveUser = ref(null);
const liveAssistant = ref(null);
const streamPhase = ref(""); // thinking | streaming | tool
const streamToolName = ref("");
const streamMode = ref(""); // leader | direct
/** 发送开始时的 Timeline 水位，仅用之后的里程碑驱动 live status */
const statusWatermarkSeq = ref(0);

const title = computed(() => {
  const name = String(props.displayName || "").trim() || props.workgroupId || "未命名";
  return `工作组 · ${name}`;
});

const canChat = computed(() => String(workgroupStatus.value || "") === "active");
const activeHitl = computed(() => (pendingHitl.value || [])[0] || null);
const hitlMode = computed(() => Boolean(activeHitl.value));
const debugLlmBadge = computed(() => {
  const mode = String(debugLlm.value?.mode || "").trim();
  if (mode === "mock") return "Mock · 回声/脚本";
  if (mode === "live") {
    const model = String(debugLlm.value?.model || "").trim();
    return model ? `Live · ${model}` : "Live";
  }
  return "";
});

const canSubmit = computed(() => {
  if (hitlMode.value) {
    return canChat.value && Boolean(hitlDraft.value.trim()) && !hitlBusy.value;
  }
  // 忙碌时仍可发送：进入 Manage human 队列（Cursor 风）
  return canChat.value && Boolean(input.value.trim()) && Boolean(fromNodeId.value.trim());
});

const composerModel = computed({
  get: () => (hitlMode.value ? hitlDraft.value : input.value),
  set: (v) => {
    if (hitlMode.value) hitlDraft.value = v;
    else input.value = v;
  },
});

const nodeNameById = computed(() => {
  const map = {};
  for (const a of nodeDirectory.value || []) {
    const id = String(a?.agent_id || a?.node_id || "").trim();
    if (!id) continue;
    map[id] = String(a?.name || "").trim() || id;
  }
  return map;
});

const statusLabel = computed(() => {
  if (!sending.value) {
    if (hitlMode.value) return "Supervisor 正在询问…";
    return "";
  }
  const watermark = statusWatermarkSeq.value || 0;
  const list = timeline.value || [];
  const directMode = streamMode.value === "direct";
  for (let i = list.length - 1; i >= 0; i -= 1) {
    const ev = list[i];
    const seq = Number(ev?.seq || 0);
    if (seq && seq <= watermark) break;
    const t = String(ev?.type || "");
    const actor = String(ev?.actor_id || "").trim();
    if (t === "system_notice" || t === "assign_started") {
      return String(ev?.text || "").trim() || "成员工作中…";
    }
    if (t === "assign_finished") {
      const txt = String(ev?.text || "").trim();
      if (txt && txt !== "已完成") return txt;
      return directMode || (actor && actor !== "leader")
        ? "成员已完成"
        : "成员已完成，Supervisor 汇总中…";
    }
    if (t === "actor_final_text" && actor !== "leader") {
      return directMode ? "成员已完成" : "Supervisor 汇总中…";
    }
    if (t === "human_message") break;
  }
  if (streamPhase.value === "tool") {
    const tool = streamToolName.value;
    if (tool === "成员执行任务" || tool === "assign_workgroup_task" || tool.startsWith("直达")) {
      return tool.startsWith("直达") || tool === "直达成员" ? "直连成员工作中…" : "成员执行任务…";
    }
    if (tool === "询问用户" || tool === "ask_workgroup_user") return "等待你的回答…";
    return tool ? `执行工具 ${tool}…` : "执行工具…";
  }
  if (streamPhase.value === "streaming" && liveAssistant.value?.text) return "生成中…";
  return directMode ? "直连成员思考中…" : "Supervisor 思考中…";
});

const visibleEvents = computed(() =>
  (timeline.value || []).filter((event) => {
    const text = String(event?.text || "").trim();
    return Boolean(text) || Boolean(event?.type);
  }),
);

function eventRole(event) {
  const type = String(event?.type || "").toLowerCase();
  const actor = String(event?.actor_id || "").toLowerCase();
  if (type === "human_message") return "user";
  if (
    type === "actor_final_text" ||
    type === "assign_started" ||
    type === "assign_finished" ||
    type === "system_notice" ||
    type.includes("assistant") ||
    type.includes("agent") ||
    type.includes("supervisor") ||
    actor === "leader" ||
    actor === "supervisor"
  ) {
    return "assistant";
  }
  return "user";
}

function eventActorLabel(event) {
  const type = String(event?.type || "");
  const actor = String(event?.actor_id || "").trim();
  if (type === "assign_started" || type === "assign_finished") {
    if (actor && actor !== "leader") {
      const member = (members.value || []).find((m) => String(m?.member_id || "") === actor);
      if (member?.display_name) return String(member.display_name).trim();
      return actor;
    }
    return "Supervisor";
  }
  if (type === "system_notice") {
    if (!actor || actor === "leader") return "工作组";
    const member = (members.value || []).find((m) => String(m?.member_id || "") === actor);
    if (member?.display_name) return String(member.display_name).trim();
    return actor;
  }
  if (!actor || actor === "leader") return "Supervisor";
  const member = (members.value || []).find((m) => String(m?.member_id || "") === actor);
  if (member?.display_name) return String(member.display_name).trim();
  const node = nodeNameById.value[actor];
  if (node) return node;
  return actor;
}

function isDirectAssignEvent(ev) {
  const actor = String(ev?.actor_id || "").trim();
  const text = String(ev?.text || "").trim();
  return (actor && actor !== "leader") || text.startsWith("直达");
}

/** 用户气泡内 @提及 拆段，便于着色 */
function splitUserMentionParts(text) {
  const raw = String(text || "");
  if (!raw) return [{ type: "text", text: "" }];
  const parts = [];
  const re = /@[^\s@]+/g;
  let last = 0;
  let m = re.exec(raw);
  while (m) {
    if (m.index > last) {
      parts.push({ type: "text", text: raw.slice(last, m.index) });
    }
    parts.push({ type: "mention", text: m[0] });
    last = m.index + m[0].length;
    m = re.exec(raw);
  }
  if (last < raw.length) parts.push({ type: "text", text: raw.slice(last) });
  if (!parts.length) parts.push({ type: "text", text: raw });
  return parts;
}

function previewMemberReport(text) {
  const raw = String(text || "").trim().replace(/\s+/g, " ");
  if (!raw) return "成员结论";
  return raw.length > 72 ? `${raw.slice(0, 72)}…` : raw;
}

/** 解析编排态 assign_started：新格式 `@名\\n任务`；兼容旧 `→ 名 · 摘要` */
function parseAssignStartedText(text) {
  const raw = String(text || "").trim();
  if (!raw) return { mention: "", taskText: "分派任务" };

  const atMatch = raw.match(/^@([^\n\r]+)\r?\n([\s\S]*)$/);
  if (atMatch) {
    return {
      mention: String(atMatch[1] || "").trim(),
      taskText: String(atMatch[2] || "").trim() || "分派任务",
    };
  }

  const arrow = raw.match(/^→\s*(.+?)\s*·\s*([\s\S]*)$/);
  if (arrow) {
    return {
      mention: String(arrow[1] || "").trim(),
      taskText: String(arrow[2] || "").trim() || "分派任务",
    };
  }

  if (raw.startsWith("@")) {
    return { mention: raw.slice(1).trim(), taskText: "分派任务" };
  }

  return { mention: "", taskText: raw };
}

function buildAssignIndex(list) {
  const directAssignIds = new Set();
  const noticeByAssign = {};
  const noticesByAssign = {};
  const finishedByAssign = {};
  const startedByAssign = {};
  const memberFinalByAssign = {};
  for (const ev of list || []) {
    const t = String(ev?.type || "");
    const aid = String(ev?.assign_id || "").trim();
    if (!aid) continue;
    if (t === "assign_started") {
      startedByAssign[aid] = ev;
      if (isDirectAssignEvent(ev)) directAssignIds.add(aid);
    } else if (t === "assign_finished") {
      finishedByAssign[aid] = ev;
      if (isDirectAssignEvent(ev)) directAssignIds.add(aid);
    } else if (t === "system_notice") {
      noticeByAssign[aid] = ev;
      if (!noticesByAssign[aid]) noticesByAssign[aid] = [];
      noticesByAssign[aid].push(ev);
    } else if (t === "actor_final_text") {
      const actor = String(ev?.actor_id || "").trim();
      if (actor && actor !== "leader") memberFinalByAssign[aid] = ev;
    }
  }
  return {
    directAssignIds,
    noticeByAssign,
    noticesByAssign,
    finishedByAssign,
    startedByAssign,
    memberFinalByAssign,
  };
}

function isMemberReportExpanded(key) {
  return Boolean(expandedMemberReports.value[key]);
}

function toggleMemberReport(key) {
  if (!key) return;
  expandedMemberReports.value = {
    ...expandedMemberReports.value,
    [key]: !expandedMemberReports.value[key],
  };
}

function parseNoticeTool(text) {
  const raw = String(text || "").trim();
  if (!raw) return { toolName: "tool", summary: "工具调用" };
  const parts = raw.split(/\s*·\s*/);
  const toolName = String(parts[0] || "tool").trim() || "tool";
  const detail = parts.slice(1).join(" · ").trim();
  return {
    toolName,
    summary: detail ? `${toolName} ${detail}` : toolName,
  };
}

function toolKindLabel(toolName) {
  const n = String(toolName || "");
  if (
    /^(read_file|write_file|glob_files|grep_file|grep_files|search_replace|show_image|read_image)/.test(n) ||
    n.includes("file")
  ) {
    return "fs";
  }
  if (/^(bash|shell)/.test(n)) return "shell";
  if (n.startsWith("browser_")) return "browser";
  return "tool";
}

function makeAssignRow(started, finished, notices, isDirect, memberFinal) {
  const noticeList = Array.isArray(notices) ? notices : notices ? [notices] : [];
  const lastNotice = noticeList.length ? noticeList[noticeList.length - 1] : null;
  const noticeText = lastNotice ? String(lastNotice.text || "").trim() : "";
  const anchor = started || finished;
  const parsed = parseAssignStartedText(started?.text || "");
  const fallbackSummary = String(started?.text || finished?.text || "").trim() || "分派任务";
  const taskText = parsed.taskText || fallbackSummary;
  const statusText = finished
    ? String(finished.text || "").trim() || "已完成"
    : noticeText || "执行中";
  const reportText = memberFinal ? String(memberFinal.text || "").trim() : "";
  const assignId =
    String(started?.assign_id || finished?.assign_id || memberFinal?.assign_id || "").trim();
  const reporter = memberFinal ? String(memberFinal.actor_id || "").trim() : "";
  const reportActorLabel = reporter
    ? members.value.find((m) => m.member_id === reporter)?.display_name || reporter
    : "";
  const mention = parsed.mention || reportActorLabel || "";
  const failed = finished ? /失败|中断/.test(String(finished.text || "")) : false;
  const steps = noticeList.map((ev, idx) => {
    const p = parseNoticeTool(ev?.text);
    const isLast = idx === noticeList.length - 1;
    const done = Boolean(finished) || !isLast;
    return {
      key: ev.event_id || `step-${ev.seq || idx}`,
      toolName: p.toolName,
      toolKind: toolKindLabel(p.toolName),
      summary: p.summary,
      statusText: !done ? "执行中" : failed && isLast ? "已中断" : "已完成",
      done,
      failed: Boolean(failed && done && isLast),
      inProgress: !done,
    };
  });
  return {
    key: anchor?.event_id || `assign-${anchor?.seq}`,
    kind: "assign",
    assignId,
    mention,
    taskText,
    summary: taskText,
    liveProgress: "",
    statusText,
    done: Boolean(finished),
    failed,
    direct: isDirect,
    steps,
    hasReport: Boolean(reportText),
    reportText,
    reportPreview: previewMemberReport(reportText),
    reportActorLabel,
    reportToggleKey: assignId || anchor?.event_id || "",
    text: taskText,
    role: "assistant",
    actorId: "",
    streaming: false,
    phase: "",
    tool: "",
    progress: false,
  };
}

function makeDirectToolRow(ev, { assignFinished, isLast, failed }) {
  const parsed = parseNoticeTool(ev?.text);
  const done = Boolean(assignFinished) || !isLast;
  return {
    key: ev.event_id || `tool-${ev.seq}`,
    kind: "tool",
    toolName: parsed.toolName,
    toolKind: toolKindLabel(parsed.toolName),
    summary: parsed.summary,
    statusText: !done ? "执行中" : failed ? "已中断" : "已完成",
    done,
    failed: Boolean(failed && done && isLast),
    inProgress: !done,
    role: "assistant",
    actorId: String(ev?.actor_id || "").trim(),
    streaming: false,
    progress: false,
  };
}

const displayGroups = computed(() => {
  const groups = [];
  const rawGroups = [];
  const list = visibleEvents.value || [];
  const {
    directAssignIds,
    noticesByAssign,
    finishedByAssign,
    startedByAssign,
    memberFinalByAssign,
  } = buildAssignIndex(list);
  const consumedFinished = new Set();

  const pushRow = (role, actorId, label, item) => {
    const bucket = `${role}:${actorId || "_"}`;
    const last = rawGroups[rawGroups.length - 1];
    if (last && last.bucket === bucket) {
      last.items.push(item);
      return;
    }
    rawGroups.push({
      key: `${bucket}-${item.key}`,
      bucket,
      role,
      label,
      items: [item],
    });
  };

  for (const ev of list) {
    const t = String(ev?.type || "");
    const aid = String(ev?.assign_id || "").trim();

    if (t === "assign_started") {
      const finished = aid ? finishedByAssign[aid] || null : null;
      if (aid && finished) consumedFinished.add(aid);
      const isDirect = Boolean(aid && directAssignIds.has(aid));
      if (isDirect) continue;
      const notices = aid ? noticesByAssign[aid] || [] : [];
      const memberFinal = aid ? memberFinalByAssign[aid] || null : null;
      const actorId = String(ev?.actor_id || "leader").trim() || "leader";
      const row = makeAssignRow(ev, finished, notices, false, memberFinal);
      row.actorId = actorId;
      row.actor = eventActorLabel({ ...ev, actor_id: actorId, type: "assign_started" });
      pushRow("assistant", actorId, row.actor || "Supervisor", row);
      continue;
    }

    if (t === "assign_finished") {
      if (aid && startedByAssign[aid]) continue;
      if (aid && consumedFinished.has(aid)) continue;
      const isDirect = Boolean(aid && directAssignIds.has(aid));
      if (isDirect) continue;
      const actorId = String(ev?.actor_id || "leader").trim() || "leader";
      const notices = aid ? noticesByAssign[aid] || [] : [];
      const memberFinal = aid ? memberFinalByAssign[aid] || null : null;
      const row = makeAssignRow(null, ev, notices, false, memberFinal);
      row.actorId = actorId;
      row.actor = eventActorLabel({ ...ev, actor_id: actorId, type: "assign_finished" });
      pushRow("assistant", actorId, row.actor || "Supervisor", row);
      continue;
    }

    if (t === "actor_final_text") {
      const actor = String(ev?.actor_id || "").trim();
      if (actor && actor !== "leader" && aid && !directAssignIds.has(aid)) {
        continue;
      }
    }

    if (t === "system_notice") {
      if (aid && !directAssignIds.has(aid)) continue;
      if (String(ev?.text || "").startsWith("已直达")) continue;
      const actorId = String(ev?.actor_id || "").trim();
      if (aid && directAssignIds.has(aid)) {
        const chain = noticesByAssign[aid] || [];
        const idx = chain.findIndex((n) => n === ev || n.event_id === ev.event_id);
        const isLast = idx < 0 || idx === chain.length - 1;
        const finished = finishedByAssign[aid] || null;
        const failed = finished ? /失败|中断/.test(String(finished.text || "")) : false;
        pushRow(
          "assistant",
          actorId,
          eventActorLabel(ev),
          makeDirectToolRow(ev, {
            assignFinished: Boolean(finished),
            isLast,
            failed,
          }),
        );
        continue;
      }
      pushRow("assistant", actorId, eventActorLabel(ev), {
        key: ev.event_id || `seq-${ev.seq}`,
        kind: "progress",
        role: "assistant",
        text: ev.text || "",
        actor: eventActorLabel(ev),
        actorId,
        streaming: false,
        phase: "",
        tool: "",
        progress: true,
      });
      continue;
    }

    const role = eventRole(ev);
    const actorId = String(ev?.actor_id || "").trim();
    pushRow(role, actorId, eventActorLabel(ev) || (role === "user" ? "" : "Supervisor"), {
      key: ev.event_id || `seq-${ev.seq}`,
      kind: "message",
      role,
      text: ev.text || "",
      actor: eventActorLabel(ev),
      actorId,
      streaming: false,
      phase: "",
      tool: "",
      progress: false,
    });
  }

  if (liveUser.value) {
    const already = visibleEvents.value.some(
      (ev) =>
        String(ev?.type || "") === "human_message" &&
        String(ev?.text || "") === liveUser.value.text,
    );
    if (!already) {
      const actorId = fromNodeId.value || "console";
      pushRow("user", actorId, "", {
        key: liveUser.value.id,
        kind: "message",
        role: "user",
        text: liveUser.value.text,
        actor: "",
        actorId,
        streaming: false,
        phase: "",
        tool: "",
        progress: false,
      });
    }
  }

  if (liveAssistant.value && streamMode.value !== "direct") {
    pushRow("assistant", "leader", "Supervisor", {
      key: liveAssistant.value.id,
      kind: "message",
      role: "assistant",
      text: liveAssistant.value.text || "",
      actor: "Supervisor",
      actorId: "leader",
      streaming: true,
      phase: streamPhase.value,
      tool: streamToolName.value,
      progress: false,
    });
  }

  for (const g of rawGroups) {
    groups.push({
      key: g.key,
      bucket: g.bucket,
      role: g.role,
      label: g.label,
      hasStreaming: g.items.some((it) => it.streaming),
      items: g.items,
    });
  }
  return groups;
});

const mentionCandidates = computed(() => {
  const q = mentionQuery.value.trim().toLowerCase();
  const list = (members.value || []).filter((m) => String(m?.status || "") === "ready");
  if (!q) return list.slice(0, 8);
  return list
    .filter((m) => {
      const name = String(m?.display_name || "").toLowerCase();
      const id = String(m?.member_id || "").toLowerCase();
      return name.includes(q) || id.includes(q);
    })
    .slice(0, 8);
});

function onDraftInput(e) {
  if (hitlMode.value) {
    resizeTextarea();
    return;
  }
  const val = String(e?.target?.value ?? input.value);
  input.value = val;
  const cursor = e?.target?.selectionStart ?? val.length;
  const before = val.slice(0, cursor);
  const m = before.match(/(^|[\s])@([^\s@]*)$/);
  if (m) {
    mentionOpen.value = true;
    mentionQuery.value = m[2] || "";
  } else {
    mentionOpen.value = false;
    mentionQuery.value = "";
  }
  resizeTextarea();
}

function pickMention(member) {
  const name = String(member?.display_name || "").trim();
  const mid = String(member?.member_id || "").trim();
  if (!name || !mid) return;
  const val = input.value;
  const replaced = val.replace(/(^|[\s])@([^\s@]*)$/, "$1");
  input.value = replaced.replace(/[ \t]+$/g, " ").trimStart();
  directMember.value = { member_id: mid, display_name: name };
  mentionOpen.value = false;
  mentionQuery.value = "";
  nextTick(() => {
    resizeTextarea();
    textareaRef.value?.focus?.();
  });
}

function clearDirectMention() {
  directMember.value = null;
}

function stripLeadingMention(text, displayName) {
  const name = String(displayName || "").trim();
  let out = String(text || "").trim();
  if (!name) return out;
  const token = `@${name}`;
  if (out === token) return "";
  if (out.startsWith(`${token} `)) return out.slice(token.length + 1).trimStart();
  return out;
}

function onComposerBackspace(e) {
  if (hitlMode.value || !directMember.value) return;
  const el = e?.target;
  const start = el?.selectionStart ?? 0;
  const end = el?.selectionEnd ?? 0;
  if (start === 0 && end === 0 && !String(input.value || "")) {
    e.preventDefault();
    clearDirectMention();
  }
}

const sortedMembers = computed(() => {
  const list = [...(members.value || [])];
  list.sort((a, b) => String(a.display_name || "").localeCompare(String(b.display_name || ""), "zh"));
  return list;
});

const railMembers = computed(() => {
  const rows = [
    {
      member_id: "leader",
      display_name: "Supervisor",
      home_node_id: "manage",
      status: sending.value ? "busy" : "ready",
      kind: "supervisor",
    },
  ];
  for (const m of sortedMembers.value) {
    rows.push({ ...m, kind: "member" });
  }
  return rows;
});

const aclPeople = computed(() => {
  const owners = Array.isArray(acl.value?.owners) ? acl.value.owners : [];
  const collaborators = Array.isArray(acl.value?.collaborators) ? acl.value.collaborators : [];
  const seen = new Set();
  const rows = [];
  for (const id of owners) {
    const node = String(id || "").trim();
    if (!node || seen.has(node)) continue;
    seen.add(node);
    rows.push({ id: node, role: "owner" });
  }
  for (const id of collaborators) {
    const node = String(id || "").trim();
    if (!node || seen.has(node)) continue;
    seen.add(node);
    rows.push({ id: node, role: "collaborator" });
  }
  return rows;
});

function memberStatusLabel(status) {
  const map = {
    requested: "已请求",
    provisioning: "配置中",
    ready: "就绪",
    busy: "忙碌",
    archived: "已归档",
    error: "错误",
  };
  return map[status] || status || "—";
}

function initialOf(name) {
  const s = String(name || "").trim();
  return s ? s.slice(0, 1).toUpperCase() : "?";
}

function newClientMessageId() {
  return `cmsg_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
}

async function resolveSender() {
  try {
    const me = await fetchAuthMe();
    if (me?.authenticated) {
      if (me.kind === "node") {
        fromNodeId.value = String(me.agent_id || me.subject || "").trim() || "console";
        return;
      }
      if (me.kind === "admin") {
        fromNodeId.value = String(me.subject || "admin").trim() || "admin";
        return;
      }
    }
  } catch {
    /* ignore */
  }
  fromNodeId.value = "console";
}

async function loadMembers() {
  if (!props.workgroupId) {
    members.value = [];
    acl.value = null;
    return;
  }
  loadingMembers.value = true;
  try {
    const [memberList, aclData] = await Promise.all([
      fetchWorkgroupMembers(props.workgroupId),
      fetchWorkgroupACL(props.workgroupId).catch(() => null),
    ]);
    members.value = Array.isArray(memberList) ? memberList : [];
    acl.value = aclData;
  } catch (err) {
    members.value = [];
    emit("toast", { message: err.message || "加载成员失败", type: "error" });
  } finally {
    loadingMembers.value = false;
  }
}

async function loadNodeDirectory() {
  try {
    const page = await fetchAgents({ status: "all", page: 1, page_size: 200 });
    nodeDirectory.value = Array.isArray(page?.agents) ? page.agents : [];
  } catch {
    nodeDirectory.value = [];
  }
}

let workPollTimer = null;

function stopWorkPoll() {
  if (workPollTimer) {
    clearInterval(workPollTimer);
    workPollTimer = null;
  }
}

function startWorkPoll() {
  stopWorkPoll();
  workPollTimer = window.setInterval(() => {
    refreshTimelineQuiet();
  }, 900);
}

async function refreshTimelineQuiet() {
  if (!props.workgroupId) return;
  try {
    timeline.value = await fetchWorkgroupTimeline(props.workgroupId);
    await loadPendingHitl();
    await nextTick();
    scrollToBottom();
  } catch {
    /* ignore during live poll */
  }
}

async function loadPendingHitl() {
  if (!props.workgroupId) {
    pendingHitl.value = [];
    return;
  }
  try {
    const list = await listWorkgroupHITL(props.workgroupId, true);
    pendingHitl.value = Array.isArray(list) ? list : [];
  } catch {
    /* ignore */
  }
}

async function submitHitlAnswer() {
  const hitl = activeHitl.value;
  const answer = hitlDraft.value.trim();
  if (!hitl || !props.workgroupId || !answer || hitlBusy.value) return;
  hitlBusy.value = true;
  try {
    await resolveWorkgroupHITL(props.workgroupId, hitl.hitl_id, answer);
    hitlDraft.value = "";
    await loadPendingHitl();
    await loadTimeline();
  } catch (err) {
    if (err?.status === 409 || /already_resolved/i.test(String(err?.message || ""))) {
      await loadPendingHitl();
    } else {
      emit("toast", { message: err.message || "提交回答失败", type: "error" });
    }
  } finally {
    hitlBusy.value = false;
  }
}

async function loadDebugRuns() {
  if (!props.workgroupId) {
    debugRuns.value = [];
    debugLlm.value = null;
    return;
  }
  debugLoading.value = true;
  debugError.value = "";
  try {
    const res = await listWorkgroupRuns(props.workgroupId, { limit: 12 });
    debugRuns.value = Array.isArray(res?.runs) ? res.runs : [];
    debugLlm.value = res?.llm || null;
    if (!debugSelectedRunId.value && debugRuns.value.length) {
      await selectDebugRun(debugRuns.value[0].run_id);
    } else if (debugSelectedRunId.value) {
      const still = debugRuns.value.some((r) => r.run_id === debugSelectedRunId.value);
      if (still) await selectDebugRun(debugSelectedRunId.value);
      else if (debugRuns.value.length) await selectDebugRun(debugRuns.value[0].run_id);
      else {
        debugHistory.value = null;
        debugSelectedRunId.value = "";
      }
    }
  } catch (err) {
    debugError.value = err?.message || "加载 Run 失败";
    debugRuns.value = [];
  } finally {
    debugLoading.value = false;
  }
}

async function selectDebugRun(runId) {
  const id = String(runId || "").trim();
  if (!id || !props.workgroupId) return;
  debugSelectedRunId.value = id;
  debugLoading.value = true;
  debugError.value = "";
  try {
    const res = await getWorkgroupRunHistory(props.workgroupId, id);
    debugHistory.value = res?.history || null;
    if (res?.llm) debugLlm.value = res.llm;
  } catch (err) {
    debugError.value = err?.message || "加载 History 失败";
    debugHistory.value = null;
  } finally {
    debugLoading.value = false;
  }
}

async function toggleDebugPanel() {
  debugOpen.value = !debugOpen.value;
  if (debugOpen.value) await loadDebugRuns();
}

function formatDebugMsg(m) {
  const role = String(m?.role || "");
  if (role === "assistant" && Array.isArray(m?.tool_calls) && m.tool_calls.length) {
    const names = m.tool_calls.map((tc) => tc?.function?.name || tc?.name || "?").join(", ");
    return `tool_calls: ${names}`;
  }
  if (role === "tool") {
    const body = String(m?.content || "").trim();
    return body.length > 180 ? `${body.slice(0, 180)}…` : body || "(empty)";
  }
  const body = String(m?.content || "").trim();
  return body.length > 220 ? `${body.slice(0, 220)}…` : body || "(empty)";
}

async function loadTimeline() {
  if (!props.workgroupId) {
    timeline.value = [];
    return;
  }
  loading.value = true;
  try {
    timeline.value = await fetchWorkgroupTimeline(props.workgroupId);
    await nextTick();
    scrollToBottom(true);
  } catch (err) {
    emit("toast", { message: err.message || "加载对话失败", type: "error" });
  } finally {
    loading.value = false;
  }
}

async function loadWorkgroupMeta() {
  if (!props.workgroupId) {
    workgroupStatus.value = "";
    return;
  }
  try {
    const wg = await fetchWorkgroup(props.workgroupId);
    workgroupStatus.value = String(wg?.status || "");
  } catch {
    workgroupStatus.value = "";
  }
}

async function loadAll() {
  await Promise.all([
    loadWorkgroupMeta(),
    loadTimeline(),
    loadMembers(),
    loadNodeDirectory(),
    loadPendingHitl(),
    refreshHumanQueue(),
  ]);
  startQueuePoll();
}

function resizeTextarea() {
  const el = textareaRef.value;
  if (!el) return;
  el.style.height = "auto";
  el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
}

function onStreamScroll() {
  const el = streamRef.value;
  if (!el) return;
  followTail.value = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
}

function scrollToBottom(force = false) {
  const el = streamRef.value;
  if (!el) return;
  if (!force && !followTail.value) return;
  el.scrollTop = el.scrollHeight;
}

function clearLive() {
  liveUser.value = null;
  liveAssistant.value = null;
  streamPhase.value = "";
  streamToolName.value = "";
  streamMode.value = "";
  statusWatermarkSeq.value = 0;
}

async function cancelTurn() {
  if (!props.workgroupId || !sending.value || cancelling.value) return;
  cancelling.value = true;
  try {
    await cancelWorkgroupTurn(props.workgroupId);
    if (streamAbort) {
      try {
        streamAbort.abort();
      } catch {
        /* ignore */
      }
    }
    await loadTimeline().catch(() => {});
  } catch (err) {
    emit("toast", { message: err.message || "取消失败", type: "error" });
  } finally {
    cancelling.value = false;
  }
}

async function refreshHumanQueue() {
  if (!props.workgroupId) {
    humanQueueItems.value = [];
    return;
  }
  try {
    const out = await fetchWorkgroupHumanQueue(props.workgroupId);
    humanQueueItems.value = Array.isArray(out?.items) ? out.items : [];
  } catch {
    /* ignore poll errors */
  }
}

function applyQueuePayload(data) {
  const q = data?.queue;
  if (q && Array.isArray(q.items)) {
    humanQueueItems.value = q.items;
    return;
  }
  if (data?.queue_id) {
    const rest = (humanQueueItems.value || []).filter((x) => x.queue_id !== data.queue_id);
    humanQueueItems.value = [...rest, data].sort(
      (a, b) => Number(a.position || 0) - Number(b.position || 0),
    );
  }
}

function startQueuePoll() {
  stopQueuePoll();
  queuePollTimer = setInterval(() => {
    if (!props.active || !props.workgroupId) return;
    if (!sending.value && !(humanQueueItems.value || []).length) return;
    void refreshHumanQueue();
    if (sending.value || (humanQueueItems.value || []).length) {
      void loadTimeline().catch(() => {});
    }
  }, 1500);
}

function stopQueuePoll() {
  if (queuePollTimer) {
    clearInterval(queuePollTimer);
    queuePollTimer = null;
  }
}

function beginEditQueued(item) {
  editingQueueId.value = String(item?.queue_id || "");
  editQueueDraft.value = String(item?.text || "");
}

function cancelEditQueued() {
  editingQueueId.value = "";
  editQueueDraft.value = "";
}

async function saveQueuedEdit(item) {
  const qid = String(item?.queue_id || "").trim();
  const text = editQueueDraft.value.trim();
  if (!props.workgroupId || !qid || !text) return;
  try {
    const out = await patchWorkgroupHumanQueueItem(props.workgroupId, qid, text);
    applyQueuePayload(out);
    await refreshHumanQueue();
    cancelEditQueued();
  } catch (err) {
    emit("toast", { message: err.message || "修改排队消息失败", type: "error" });
  }
}

async function removeQueued(item) {
  const qid = String(item?.queue_id || "").trim();
  if (!props.workgroupId || !qid) return;
  try {
    await cancelWorkgroupHumanQueueItem(props.workgroupId, qid);
    await refreshHumanQueue();
    if (editingQueueId.value === qid) cancelEditQueued();
  } catch (err) {
    emit("toast", { message: err.message || "取消排队失败", type: "error" });
  }
}

async function sendMessage() {
  if (hitlMode.value) {
    await submitHitlAnswer();
    return;
  }
  let text = input.value.trim();
  const sender = fromNodeId.value.trim();
  if (!props.workgroupId || !text || !sender || !canChat.value) return;

  let directId = "";
  if (directMember.value) {
    directId = directMember.value.member_id;
    const token = `@${directMember.value.display_name}`;
    if (!text.includes(token)) {
      text = `${token} ${text}`;
    }
  }

  const clientMessageId = newClientMessageId();
  const enqueueOnly = sending.value;
  followTail.value = true;
  input.value = "";
  const sentDirect = directMember.value;
  directMember.value = null;
  mentionOpen.value = false;
  await nextTick();
  resizeTextarea();

  if (enqueueOnly) {
    try {
      await postWorkgroupMessageStream(
        props.workgroupId,
        {
          text,
          from_node_id: sender,
          client_message_id: clientMessageId,
          direct_member_id: directId || undefined,
        },
        {
          onEvent: (eventName, data) => {
            if (eventName === "queued") applyQueuePayload(data);
          },
        },
      );
      await refreshHumanQueue();
      startQueuePoll();
    } catch (err) {
      emit("toast", { message: err.message || "入队失败", type: "error" });
      input.value = stripLeadingMention(text, sentDirect?.display_name);
      directMember.value = sentDirect;
    }
    return;
  }

  sending.value = true;
  cancelling.value = false;

  const maxSeq = (timeline.value || []).reduce(
    (m, ev) => Math.max(m, Number(ev?.seq || 0)),
    0,
  );
  statusWatermarkSeq.value = maxSeq;

  liveUser.value = { id: `live-user-${clientMessageId}`, text };
  liveAssistant.value = { id: `live-asst-${clientMessageId}`, text: "" };
  streamPhase.value = "thinking";
  streamToolName.value = "";
  streamMode.value = directId ? "direct" : "leader";
  streamAbort = new AbortController();
  startWorkPoll();
  startQueuePoll();
  await nextTick();
  scrollToBottom(true);

  try {
    let becameQueued = false;
    await postWorkgroupMessageStream(
      props.workgroupId,
      {
        text,
        from_node_id: sender,
        client_message_id: clientMessageId,
        direct_member_id: directId || undefined,
      },
      {
        signal: streamAbort.signal,
        onEvent: async (eventName, data) => {
          if (eventName === "queued") {
            becameQueued = true;
            applyQueuePayload(data);
            clearLive();
            return;
          }
          if (eventName === "status") {
            const phase = String(data?.phase || "thinking");
            streamPhase.value = phase === "tool" ? "tool" : phase === "streaming" ? "streaming" : "thinking";
            streamToolName.value = String(data?.tool || "");
            if (data?.mode) streamMode.value = String(data.mode);
            if (phase === "tool" && liveAssistant.value) {
              liveAssistant.value = { ...liveAssistant.value, text: "" };
            }
            if (phase === "tool") {
              await loadTimeline().catch(() => {});
            }
          } else if (eventName === "delta") {
            const piece = String(data?.text || "");
            if (!piece || !liveAssistant.value) return;
            streamPhase.value = "streaming";
            liveAssistant.value = {
              ...liveAssistant.value,
              text: `${liveAssistant.value.text || ""}${piece}`,
            };
          } else if (eventName === "assistant_final") {
            const finalText = String(data?.text || "").trim();
            if (data?.mode) streamMode.value = String(data.mode);
            if (liveAssistant.value && finalText) {
              liveAssistant.value = { ...liveAssistant.value, text: finalText };
            }
            streamPhase.value = "streaming";
          } else if (eventName === "final" || eventName === "done") {
            /* sealed after await */
          }
          await nextTick();
          scrollToBottom();
        },
      },
    );
    directMember.value = null;
    clearLive();
    await loadTimeline();
    await loadPendingHitl();
    await refreshHumanQueue();
    if (debugOpen.value) await loadDebugRuns();
    if (becameQueued) startQueuePoll();
  } catch (err) {
    const aborted = err?.name === "AbortError" || /abort/i.test(String(err?.message || ""));
    clearLive();
    if (!aborted) {
      emit("toast", { message: err.message || "发送失败", type: "error" });
      input.value = stripLeadingMention(text, sentDirect?.display_name);
      directMember.value = sentDirect;
      await nextTick();
      resizeTextarea();
    }
    await loadTimeline().catch(() => {});
    await loadPendingHitl().catch(() => {});
    if (debugOpen.value) await loadDebugRuns().catch(() => {});
  } finally {
    streamAbort = null;
    stopWorkPoll();
    sending.value = false;
    cancelling.value = false;
    streamPhase.value = "";
    streamToolName.value = "";
    streamMode.value = "";
    void refreshHumanQueue();
  }
}

function onKeydown(event) {
  if (event.key === "Escape" && mentionOpen.value) {
    event.preventDefault();
    mentionOpen.value = false;
    return;
  }
  if (event.key === "Enter" && !event.shiftKey) {
    if (hitlMode.value) {
      event.preventDefault();
      submitHitlAnswer();
      return;
    }
    if (mentionOpen.value && mentionCandidates.value.length) {
      event.preventDefault();
      pickMention(mentionCandidates.value[0]);
      return;
    }
    event.preventDefault();
    sendMessage();
  }
}

watch(
  () => [props.active, props.workgroupId],
  ([active, id]) => {
    if (active && id) {
      clearLive();
      expandedMemberReports.value = {};
      pendingHitl.value = [];
      hitlDraft.value = "";
      debugOpen.value = false;
      debugRuns.value = [];
      debugLlm.value = null;
      debugSelectedRunId.value = "";
      debugHistory.value = null;
      debugError.value = "";
      loadAll();
    }
  },
);

watch(input, () => {
  nextTick(resizeTextarea);
});

onMounted(async () => {
  await resolveSender();
  if (props.active && props.workgroupId) await loadAll();
  nextTick(resizeTextarea);
});

onUnmounted(() => {
  stopQueuePoll();
});
</script>

<template>
  <div class="wg-chat-shell">
    <aside class="wg-chat-rail" aria-label="工作组成员">
      <div class="wg-chat-rail__head">
        <strong>成员</strong>
        <span class="muted">{{ railMembers.length }}</span>
      </div>

      <div v-if="loadingMembers" class="wg-chat-rail__state muted">加载中…</div>
      <ul v-else class="wg-chat-rail__list">
        <li
          v-for="m in railMembers"
          :key="m.member_id"
          class="wg-chat-rail__item"
          :class="{ 'wg-chat-rail__item--supervisor': m.kind === 'supervisor' }"
        >
          <span class="wg-chat-rail__avatar" :data-status="m.status" aria-hidden="true">{{ initialOf(m.display_name) }}</span>
          <div class="wg-chat-rail__meta">
            <strong class="wg-chat-rail__name" :title="m.display_name">
              {{ m.display_name }}
              <span v-if="m.kind === 'supervisor'" class="wg-chat-rail__badge">编排</span>
            </strong>
            <span class="wg-chat-rail__sub muted" :title="m.home_node_id">{{ m.home_node_id }}</span>
          </div>
          <span class="wg-chat-rail__status" :data-status="m.status">{{ memberStatusLabel(m.status) }}</span>
        </li>
      </ul>

      <div class="wg-chat-rail__section">
        <div class="wg-chat-rail__head">
          <strong>访问身份</strong>
          <span class="muted">{{ aclPeople.length }}</span>
        </div>
        <ul v-if="aclPeople.length" class="wg-chat-rail__list">
          <li v-for="p in aclPeople" :key="p.id" class="wg-chat-rail__item">
            <span class="wg-chat-rail__avatar wg-chat-rail__avatar--node" aria-hidden="true">{{ initialOf(p.id) }}</span>
            <div class="wg-chat-rail__meta">
              <strong class="wg-chat-rail__name" :title="p.id">{{ p.id }}</strong>
              <span class="wg-chat-rail__sub muted">
                {{ p.role === "owner" ? "归属人" : "协作者" }}
              </span>
            </div>
          </li>
        </ul>
        <p v-else class="wg-chat-rail__state muted">暂无 ACL 记录</p>
      </div>
    </aside>

    <section class="panel panel--flex chat wg-chat-page" aria-label="工作组对话">
      <header class="chat__header">
        <div class="chat__title">
          <span class="chat__title-main">{{ title }}</span>
        </div>
        <div class="chat__header-meta">
          <button
            type="button"
            class="wg-debug-toggle"
            :class="{ 'wg-debug-toggle--active': debugOpen }"
            title="RunHistory / LLM 调试"
            @click="toggleDebugPanel"
          >
            调试
          </button>
          <span class="chat__header-id" :title="workgroupId">{{ workgroupId }}</span>
        </div>
      </header>

      <div class="wg-chat-body" :class="{ 'wg-chat-body--debug': debugOpen }">
      <div ref="streamRef" class="chat__stream" @scroll="onStreamScroll">
        <div v-if="loading && !displayGroups.length && !activeHitl" class="chat__empty">
          <div class="chat__empty-inner">
            <div class="chat__empty-hint">加载对话中…</div>
          </div>
        </div>
        <div v-else-if="!displayGroups.length && !activeHitl && !sending" class="chat__empty">
          <div class="chat__empty-inner">
            <img class="wg-chat__empty-mark" :src="brandIcon" alt="" aria-hidden="true" />
            <div class="chat__empty-title">开始对话</div>
            <div class="chat__empty-hint">输入消息与工作组协作</div>
          </div>
        </div>
        <template v-if="displayGroups.length">
          <article
            v-for="group in displayGroups"
            :key="group.key"
            class="msg"
            :class="[
              group.role === 'user' ? 'msg--user' : 'msg--assistant',
              group.hasStreaming ? 'msg--generating' : '',
              group.items.every((it) => it.progress || it.kind === 'progress') ? 'msg--progress' : '',
            ]"
          >
            <div
              class="msg__body msg__body--grouped"
              :class="{
                'msg__body--hint-only': group.items.every(
                  (it) => it.progress || it.kind === 'progress' || (it.streaming && !it.text),
                ),
              }"
            >
              <div v-if="group.role === 'assistant' || group.label" class="msg__hint">
                <span v-if="group.role === 'assistant'" class="wg-chat__message-mark" aria-hidden="true">
                  <img :src="brandIcon" alt="" />
                </span>
                {{ group.label || (group.role === 'assistant' ? 'Supervisor' : '') }}
                <BrandActivityIndicator
                  v-if="group.hasStreaming"
                  :label="group.label || '生成中'"
                  mode="generating"
                  :show-label="false"
                  compact
                />
              </div>
              <template v-for="row in group.items" :key="row.key">
                <div
                  v-if="row.kind === 'assign'"
                  class="wg-task"
                  :class="{
                    'wg-task--done': row.done && !row.failed,
                    'wg-task--failed': row.failed,
                    'wg-task--running': !row.done,
                  }"
                >
                  <div v-if="row.mention" class="wg-task__mention">
                    <span class="wg-task__at">@{{ row.mention }}</span>
                  </div>
                  <div class="wg-task__card">
                    <div class="wg-task__head">
                      <span class="wg-task__label">任务</span>
                      <span class="wg-task__status">
                        <BrandActivityIndicator
                          v-if="!row.done"
                          class="wg-task__dots"
                          mode="tool"
                          :show-label="false"
                          compact
                        />
                        <span v-if="row.done && !row.failed" class="wg-task__check" aria-hidden="true">✓</span>
                        <span v-else-if="row.failed" class="wg-task__mark" aria-hidden="true">−</span>
                        {{ row.statusText }}
                      </span>
                    </div>
                    <div class="wg-task__body">{{ row.taskText }}</div>
                    <div v-if="row.steps?.length" class="wg-task__steps">
                      <div
                        v-for="step in row.steps"
                        :key="step.key"
                        class="wg-tool-row"
                        :class="{
                          'wg-tool-row--progress': step.inProgress,
                          [`wg-tool-row--${step.toolKind || 'tool'}`]: true,
                        }"
                      >
                        <div class="wg-tool-row__bar">
                          <span class="wg-tool-row__glyph" aria-hidden="true">
                            <span v-if="step.inProgress" class="tool-exec-spinner" />
                            <span v-else-if="step.failed" class="wg-tool-row__mark">−</span>
                            <span v-else class="wg-tool-row__check">✓</span>
                          </span>
                          <span class="wg-tool-row__kind">{{ step.toolKind || "tool" }}</span>
                          <span class="wg-tool-row__text">{{ step.summary }}</span>
                          <span class="wg-tool-row__status">
                            <BrandActivityIndicator
                              v-if="step.inProgress"
                              class="wg-tool-row__dots"
                              mode="tool"
                              :show-label="false"
                              compact
                            />
                            {{ step.statusText }}
                          </span>
                        </div>
                      </div>
                    </div>
                    <div v-if="row.hasReport" class="wg-task__report">
                      <button
                        type="button"
                        class="wg-task__report-bar"
                        :aria-expanded="isMemberReportExpanded(row.reportToggleKey)"
                        :aria-label="
                          isMemberReportExpanded(row.reportToggleKey)
                            ? '收起成员结论'
                            : '展开成员结论'
                        "
                        @click="toggleMemberReport(row.reportToggleKey)"
                      >
                        <span class="wg-task__report-kind">成员结论</span>
                        <span
                          v-if="!isMemberReportExpanded(row.reportToggleKey)"
                          class="wg-task__report-preview"
                        >
                          {{ row.reportPreview }}
                        </span>
                        <span class="wg-task__report-chevron" aria-hidden="true">
                          {{ isMemberReportExpanded(row.reportToggleKey) ? "▾" : "▸" }}
                        </span>
                      </button>
                      <div
                        v-if="isMemberReportExpanded(row.reportToggleKey)"
                        class="wg-task__report-body tool-exec-bubble__markdown assistant-msg__md"
                        v-html="renderMarkdown(row.reportText)"
                      />
                    </div>
                  </div>
                </div>
                <div
                  v-else-if="row.kind === 'tool'"
                  class="wg-tool-row"
                  :class="{
                    'wg-tool-row--progress': row.inProgress,
                    [`wg-tool-row--${row.toolKind || 'tool'}`]: true,
                  }"
                >
                  <div class="wg-tool-row__bar">
                    <span class="wg-tool-row__glyph" aria-hidden="true">
                      <span v-if="row.inProgress" class="tool-exec-spinner" />
                      <span v-else-if="row.failed" class="wg-tool-row__mark">−</span>
                      <span v-else class="wg-tool-row__check">✓</span>
                    </span>
                    <span class="wg-tool-row__kind">{{ row.toolKind || "tool" }}</span>
                    <span class="wg-tool-row__text">{{ row.summary }}</span>
                    <span class="wg-tool-row__status">
                      <BrandActivityIndicator
                        v-if="row.inProgress"
                        class="wg-tool-row__dots"
                        mode="tool"
                        :show-label="false"
                        compact
                      />
                      {{ row.statusText }}
                    </span>
                  </div>
                </div>
                <div
                  v-else-if="row.progress || row.kind === 'progress'"
                  class="msg__hint msg__hint--stream-meta"
                >
                  {{ row.text }}
                </div>
                <div
                  v-else-if="row.streaming && !row.text"
                  class="msg__body--hint-only"
                >
                  <div
                    class="msg__hint msg__hint--stream-meta"
                    role="status"
                    :aria-label="statusLabel || '协作进行中'"
                  >
                    <span class="msg__meta-label">{{ statusLabel || "协作进行中" }}</span>
                    <BrandActivityIndicator
                      :label="statusLabel || '协作进行中'"
                      mode="generating"
                      :show-label="false"
                      compact
                    />
                  </div>
                </div>
                <div
                  v-else
                  class="msg__bubble"
                  :class="
                    row.role === 'user' ? 'msg__bubble--user' : 'msg__bubble--assistant-md'
                  "
                >
                  <pre
                    v-if="row.streaming"
                    class="assistant-msg__stream-plain"
                  >{{ row.text }}</pre>
                  <div
                    v-else-if="row.role === 'assistant'"
                    class="tool-exec-bubble__markdown assistant-msg__md"
                    v-html="renderMarkdown(row.text || '')"
                  />
                  <template v-else>
                    <template
                      v-for="(part, pi) in splitUserMentionParts(row.text)"
                      :key="pi"
                    >
                      <span v-if="part.type === 'mention'" class="wg-msg-at">{{ part.text }}</span>
                      <template v-else>{{ part.text }}</template>
                    </template>
                  </template>
                </div>
              </template>
            </div>
          </article>
        </template>
        <article v-if="activeHitl" class="msg msg--assistant">
          <div class="msg__body msg__body--grouped">
            <div class="msg__hint">
              <span class="wg-chat__message-mark" aria-hidden="true">
                <img :src="brandIcon" alt="" />
              </span>
              Supervisor
            </div>
            <div class="wg-hitl-bubble">
              <div class="wg-hitl-bubble__badge">询问</div>
              <p class="wg-hitl-bubble__prompt">{{ activeHitl.prompt }}</p>
              <p class="wg-hitl-bubble__hint">在下方输入框回答后 Enter 提交</p>
            </div>
          </div>
        </article>
      </div>
      <aside v-if="debugOpen" class="wg-debug" aria-label="RunHistory 调试">
        <header class="wg-debug__head">
          <strong>RunHistory</strong>
          <span v-if="debugLlmBadge" class="wg-debug__badge" :data-mode="debugLlm?.mode">
            {{ debugLlmBadge }}
          </span>
          <button type="button" class="wg-debug__refresh" :disabled="debugLoading" @click="loadDebugRuns">
            刷新
          </button>
        </header>
        <p v-if="debugError" class="wg-debug__error">{{ debugError }}</p>
        <p v-else-if="debugLoading && !debugRuns.length" class="wg-debug__muted">加载中…</p>
        <p v-else-if="!debugRuns.length" class="wg-debug__muted">暂无 ActorRun（发一条消息后会出现）</p>
        <ul v-else class="wg-debug__runs">
          <li v-for="r in debugRuns" :key="r.run_id">
            <button
              type="button"
              class="wg-debug__run"
              :class="{ 'wg-debug__run--active': r.run_id === debugSelectedRunId }"
              @click="selectDebugRun(r.run_id)"
            >
              <span class="wg-debug__run-actor">{{ r.actor_id === "leader" ? "Supervisor" : r.actor_id }}</span>
              <span class="wg-debug__run-status">{{ r.status }}</span>
              <span class="wg-debug__run-id" :title="r.run_id">{{ String(r.run_id).slice(-8) }}</span>
            </button>
          </li>
        </ul>
        <div v-if="debugHistory" class="wg-debug__msgs">
          <div
            v-for="(m, i) in debugHistory.messages || []"
            :key="i"
            class="wg-debug__msg"
            :data-role="m.role"
          >
            <div class="wg-debug__msg-role">{{ m.role }}</div>
            <pre class="wg-debug__msg-body">{{ formatDebugMsg(m) }}</pre>
            <details
              v-if="m.role === 'assistant' && m.tool_calls?.length"
              class="wg-debug__details"
            >
              <summary>工具参数</summary>
              <pre
                v-for="(tc, ti) in m.tool_calls"
                :key="ti"
                class="wg-debug__msg-body"
              >{{ tc.function?.name || "?" }}
{{ tc.function?.arguments || "{}" }}</pre>
            </details>
          </div>
        </div>
      </aside>
      </div>

      <footer class="chat__composer">
        <p v-if="!canChat && workgroupStatus === 'configuring'" class="muted chat__composer-gate">
          工作组仍在配置中，请先在配置页点击「发布」后再对话。
        </p>
        <div v-if="humanQueueItems.length" class="chat__queue" aria-label="排队中的消息">
          <div
            v-for="item in humanQueueItems"
            :key="item.queue_id"
            class="chat__queue-item"
          >
            <span class="chat__queue-pos">#{{ item.position }}</span>
            <template v-if="editingQueueId === item.queue_id">
              <input
                v-model="editQueueDraft"
                class="chat__queue-edit"
                type="text"
                @keydown.enter.prevent="saveQueuedEdit(item)"
                @keydown.escape.prevent="cancelEditQueued"
              />
              <button type="button" class="chat__queue-btn" @click="saveQueuedEdit(item)">保存</button>
              <button type="button" class="chat__queue-btn chat__queue-btn--ghost" @click="cancelEditQueued">
                取消
              </button>
            </template>
            <template v-else>
              <span class="chat__queue-text" :title="item.text">{{ item.text }}</span>
              <button type="button" class="chat__queue-btn" @click="beginEditQueued(item)">修改</button>
              <button
                type="button"
                class="chat__queue-btn chat__queue-btn--ghost"
                title="取消排队"
                @click="removeQueued(item)"
              >
                ×
              </button>
            </template>
          </div>
        </div>
        <div class="chat__composer-pill" style="position: relative">
          <div
            v-if="mentionOpen && mentionCandidates.length && canChat && !hitlMode"
            class="wg-mention-menu"
            role="listbox"
          >
            <button
              v-for="m in mentionCandidates"
              :key="m.member_id"
              type="button"
              class="wg-mention-menu__item"
              @mousedown.prevent="pickMention(m)"
            >
              <strong>{{ m.display_name }}</strong>
              <span class="muted">{{ m.member_id }}</span>
            </button>
          </div>
          <div class="chat__composer-pill-center wg-composer-field">
            <button
              v-if="directMember && !hitlMode"
              type="button"
              class="wg-task__at wg-composer-at"
              :title="`取消 @${directMember.display_name}`"
              :aria-label="`取消 @${directMember.display_name}`"
              @click="clearDirectMention"
            >
              @{{ directMember.display_name }}
            </button>
            <textarea
              ref="textareaRef"
              v-model="composerModel"
              class="chat__textarea"
              rows="1"
              :placeholder="
                hitlMode
                  ? '回答 Supervisor 的问题…'
                  : !canChat
                    ? '发布后可输入消息…'
                    : directMember
                      ? '输入直达成员的任务…'
                      : '输入消息，@ 直达成员…'
              "
              :disabled="hitlMode ? hitlBusy || !canChat : !canChat"
              @keydown="onKeydown"
              @keydown.backspace="onComposerBackspace"
              @input="onDraftInput"
            />
          </div>
          <div class="chat__composer-pill-right">
            <button
              v-if="sending && !hitlMode"
              type="button"
              class="chat__composer-send chat__composer-send--cancel"
              title="取消"
              aria-label="取消"
              :disabled="cancelling"
              @click="cancelTurn"
            >
              {{ cancelling ? "…" : "□" }}
            </button>
            <button
              v-else-if="hitlMode"
              type="button"
              class="chat__composer-send"
              title="提交回答"
              aria-label="提交回答"
              :disabled="!canSubmit"
              @click="submitHitlAnswer"
            >
              <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path
                  d="M8 12.25V3.75M8 3.75L4.5 7.25M8 3.75l3.5 3.5"
                  stroke="currentColor"
                  stroke-width="1.7"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button
              v-else
              type="button"
              class="chat__composer-send"
              title="发送"
              aria-label="发送"
              :disabled="!canSubmit"
              @click="sendMessage"
            >
              <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path
                  d="M8 12.25V3.75M8 3.75L4.5 7.25M8 3.75l3.5 3.5"
                  stroke="currentColor"
                  stroke-width="1.7"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
          </div>
        </div>
        <div class="chat__composer-statusline">
          <div class="chat__composer-statusline-left">
            <span class="chat__input-strip-left">{{
              hitlMode
                ? hitlBusy
                  ? "提交回答中…"
                  : "回答询问 · Enter 提交"
                : sending
                  ? cancelling
                    ? "正在取消…"
                    : statusLabel || "协作进行中…"
                  : directMember
                    ? "直达成员 · Enter 发送 · 点击 @ 取消"
                    : "Enter 发送 · @ 直达成员 · Shift+Enter 换行"
            }}</span>
          </div>
          <div class="chat__composer-statusline-right">
            <span v-if="sending && !hitlMode" class="chat__input-strip-right chat__input-strip-right--live">
              {{ streamMode === "direct" ? "直连成员" : "编排中" }}
            </span>
          </div>
        </div>
      </footer>
    </section>
  </div>
</template>
