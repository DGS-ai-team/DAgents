import { computed } from "vue";
import { approvalItemDisplayName } from "../utils/format.js";
import { inferToolKind } from "../utils/toolSource.js";
import { workgroupApprovalItems } from "../utils/workgroupApproval.js";

export function useWorkgroupTimeline({
  events,
  memberNameById,
  memberApprovalByAssign,
  selfNodeId,
  selfNodeName,
  liveUser,
  showLiveAssistant,
  liveAssistant,
  sending,
  streamMode,
  streamActorId,
  streamPhase,
  streamToolName,
  statusWatermarkSeq,
  expandedMemberReports,
  expandedAssignTasks,
}) {
function eventLabel(ev) {
  const actor = String(ev?.actor_id || "").trim();
  const type = String(ev?.type || "");
  if (type === "human_message") {
    if (actor && selfNodeId.value && actor === selfNodeId.value) {
      return selfNodeName.value || actor;
    }
    return actor || "human";
  }
  if (type === "actor_final_text" || type === "assistant_content") {
    if (actor === "leader") return "Supervisor";
    if (actor && memberNameById.value[actor]) return memberNameById.value[actor];
    return actor || "member";
  }
  if (type === "assign_started" || type === "assign_finished" || type === "system_notice") {
    if (actor === "leader") return "Supervisor";
    if (actor && memberNameById.value[actor]) return memberNameById.value[actor];
    return actor || "工作组";
  }
  return type || "event";
}

function isHumanEvent(ev) {
  return String(ev?.type || "") === "human_message";
}

/** 只有结构化直达事件才高亮 @成员；普通文本中的 @ 保持原样。 */
function splitUserMentionParts(text, directMemberId = "") {
  const raw = String(text || "");
  if (!raw) return [{ type: "text", text: "" }];
  const memberId = String(directMemberId || "").trim();
  const displayName = memberNameById.value[memberId] || "";
  const token = displayName ? `@${displayName}` : "";
  if (!memberId || !token) return [{ type: "text", text: raw }];
  const parts = [];
  let last = 0;
  let index = raw.indexOf(token);
  while (index >= 0) {
    const before = index === 0 ? " " : raw[index - 1];
    const afterIndex = index + token.length;
    const after = afterIndex >= raw.length ? " " : raw[afterIndex];
    if (/\s/.test(before) && /\s/.test(after)) {
      if (index > last) parts.push({ type: "text", text: raw.slice(last, index) });
      parts.push({ type: "mention", text: token });
      last = afterIndex;
    }
    index = raw.indexOf(token, Math.max(afterIndex, index + 1));
  }
  if (last < raw.length) parts.push({ type: "text", text: raw.slice(last) });
  if (!parts.length) parts.push({ type: "text", text: raw });
  return parts;
}

function isDirectAssignEvent(ev) {
  const actor = String(ev?.actor_id || "").trim();
  const text = String(ev?.text || "").trim();
  // 新路径：挂在成员下；旧路径：leader +「直达」前缀
  return (actor && actor !== "leader") || text.startsWith("直达");
}

function previewMemberReport(text) {
  const raw = String(text || "").trim().replace(/\s+/g, " ");
  if (!raw) return "成员结论";
  return raw.length > 72 ? `${raw.slice(0, 72)}…` : raw;
}

function previewAssignTask(text) {
  const raw = String(text || "").trim().replace(/\s+/g, " ");
  if (!raw) return "分派任务";
  return raw.length > 96 ? `${raw.slice(0, 96)}…` : raw;
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
    const name = raw.slice(1).trim();
    return { mention: name, taskText: "分派任务" };
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
  const assistantContentByAssign = {};
  const noticeIndexByEventId = {};
  const toolEventsByAssign = {};
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
      if (ev.event_id) noticeIndexByEventId[ev.event_id] = noticesByAssign[aid].length;
      noticesByAssign[aid].push(ev);
    } else if (t === "actor_final_text") {
      const actor = String(ev?.actor_id || "").trim();
      if (actor && actor !== "leader") memberFinalByAssign[aid] = ev;
    } else if (t === "assistant_content") {
      if (!assistantContentByAssign[aid]) assistantContentByAssign[aid] = [];
      assistantContentByAssign[aid].push(ev);
    } else if (t === "tool_started" || t === "tool_finished") {
      if (!toolEventsByAssign[aid]) toolEventsByAssign[aid] = [];
      toolEventsByAssign[aid].push(ev);
    }
  }
  return {
    directAssignIds,
    noticeByAssign,
    noticesByAssign,
    finishedByAssign,
    startedByAssign,
    memberFinalByAssign,
    assistantContentByAssign,
    noticeIndexByEventId,
    toolEventsByAssign,
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

function isAssignTaskExpanded(key) {
  return Boolean(expandedAssignTasks.value[key]);
}

function toggleAssignTask(key) {
  if (!key) return;
  expandedAssignTasks.value = {
    ...expandedAssignTasks.value,
    [key]: !expandedAssignTasks.value[key],
  };
}

function taskDetailsId(item) {
  const key = String(item?.taskToggleKey || item?.key || "task").replace(/[^a-zA-Z0-9_-]/g, "-");
  return `wg-task-details-${key}`;
}

function parseNoticeTool(text, fallbackToolName = "") {
  const raw = String(text || "").trim() || String(fallbackToolName || "").trim();
  if (!raw) return { toolName: "tool", summary: "执行成员工具" };
  const parts = raw.split(/\s*·\s*/);
  const toolName = String(parts[0] || "tool").trim() || "tool";
  const purposeByTool = {
    read_file: "读取文件",
    show_image: "展示图片",
    read_image: "分析图片",
    write_file: "写入文件",
    glob_files: "查找文件",
    grep_file: "搜索内容",
    grep_files: "搜索内容",
    search_replace: "替换内容",
    bash_run: "执行命令",
    background_job_status: "查看后台任务",
    background_job_cancel: "取消后台任务",
  };
  const knownToolNames = new Set(Object.keys(purposeByTool));
  const purpose =
    purposeByTool[toolName] ||
    (Object.values(purposeByTool).includes(toolName)
      ? toolName
      : knownToolNames.has(toolName)
        ? "执行成员工具"
        : parts.length === 1
          ? toolName
          : "执行成员工具");
  return {
    toolName,
    summary: purpose,
  };
}

function toolEventMatches(left, right) {
  const leftCall = String(left?.tool_call_id || "").trim();
  const rightCall = String(right?.tool_call_id || "").trim();
  if (leftCall || rightCall) return Boolean(leftCall && rightCall && leftCall === rightCall);
  const leftCommand = String(left?.command_id || "").trim();
  const rightCommand = String(right?.command_id || "").trim();
  return Boolean(leftCommand && rightCommand && leftCommand === rightCommand);
}

function toolKindLabel(toolName) {
  const kind = inferToolKind(toolName);
  if (kind === "fs") return "fs";
  if (kind === "shell") return "shell";
  if (kind === "terminal") return "terminal";
  if (kind === "browser") return "browser";
  if (kind === "mcp") return "mcp";
  return "tool";
}

function approvalForTool(assignId, toolCallId, toolName = "", allowNameFallback = false) {
  const hitl = memberApprovalByAssign.value[String(assignId || "").trim()] || null;
  if (!hitl) return null;
  const callId = String(toolCallId || "").trim();
  const name = String(toolName || "").trim();
  const items = workgroupApprovalItems(hitl);
  const item =
    items.find((entry) => callId && entry.callId === callId) ||
    (allowNameFallback ? items.find((entry) => name && entry.name === name) : null);
  if (!item) return null;
  return {
    hitlId: String(hitl.hitl_id || hitl.id || ""),
    items: [item],
    allItems: items,
  };
}

function makeAssignItem(
  started,
  finished,
  notices,
  isDirect,
  memberFinal,
  assistantContents = [],
  toolEvents = [],
) {
  const noticeList = Array.isArray(notices) ? notices : notices ? [notices] : [];
  const structuredEvents = Array.isArray(toolEvents) ? toolEvents : [];
  const structuredStarts = structuredEvents.filter((ev) => String(ev?.type || "") === "tool_started");
  const structuredFinishes = structuredEvents.filter((ev) => String(ev?.type || "") === "tool_finished");
  const lastNotice = noticeList.length ? noticeList[noticeList.length - 1] : null;
  const noticeText = lastNotice ? String(lastNotice.text || "").trim() : "";
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
    ? memberNameById.value[reporter] || reporter
    : "";
  const mention =
    parsed.mention ||
    reportActorLabel ||
    "";
  const failed = finished ? /失败|中断/.test(String(finished.text || "")) : false;
  const structuredSteps = structuredStarts.map((ev, idx) => {
    const toolCallId = String(ev?.tool_call_id || "").trim();
    const commandId = String(ev?.command_id || "").trim();
    const toolFinished = structuredFinishes.find((item) => {
      const sameCall = toolCallId && String(item?.tool_call_id || "").trim() === toolCallId;
      const sameCommand = commandId && String(item?.command_id || "").trim() === commandId;
      return sameCall || sameCommand;
    });
    const toolName = String(ev?.tool_name || "").trim() || "tool";
    const status = String(toolFinished?.status || "").toLowerCase();
    const failedStep = ["failed", "rejected", "indeterminate", "canceled", "cancelled", "timed_out"].includes(status);
    const done = Boolean(toolFinished) || Boolean(finished);
    return {
      key: ev.event_id || `tool-${ev.seq || idx}`,
      kind: "tool",
      assignId,
      toolCallId,
      commandId,
      toolName,
      toolKind: toolKindLabel(toolName),
      summary: parseNoticeTool("", toolName).summary,
      statusText: !done ? "执行中" : failedStep ? "已中断" : "已完成",
      done,
      failed: failedStep,
      inProgress: !done,
    };
  });
  const contentList = Array.isArray(assistantContents) ? assistantContents : [];
  const activity = [
    ...contentList.map((ev) => ({ kind: "content", ev })),
    ...(structuredStarts.length
      ? structuredStarts.map((ev) => ({ kind: "tool", ev }))
      : noticeList.map((ev) => ({ kind: "tool", ev }))),
  ].sort((a, b) => Number(a.ev?.seq || 0) - Number(b.ev?.seq || 0));
  const stepByKey = new Map(structuredSteps.map((step) => [step.key, step]));
  const lastToolIndex = activity.reduce(
    (last, entry, index) => (entry.kind === "tool" ? index : last),
    -1,
  );
  const baseSteps = activity.flatMap((entry, idx) => {
    const ev = entry.ev;
    if (entry.kind === "content") {
      return [{
        key: ev.event_id || `content-${ev.seq || idx}`,
        kind: "content",
        text: String(ev.text || ""),
      }];
    }
    if (structuredStarts.length) {
      return [stepByKey.get(ev.event_id) || {
        key: ev.event_id || `tool-${ev.seq || idx}`,
        kind: "tool",
        toolName: String(ev?.tool_name || "tool"),
        toolKind: toolKindLabel(ev?.tool_name),
        summary: parseNoticeTool("", ev?.tool_name).summary,
        statusText: finished ? "已完成" : "执行中",
        done: Boolean(finished),
        failed: false,
        inProgress: !finished,
      }];
    }
    const p = parseNoticeTool(ev?.text);
    const isLast = idx === lastToolIndex;
    const done = Boolean(finished) || !isLast;
    return {
      key: ev.event_id || `step-${ev.seq || idx}`,
      kind: "tool",
      toolName: p.toolName,
      toolKind: toolKindLabel(p.toolName),
      summary: p.summary,
      statusText: !done ? "执行中" : failed && isLast ? "已中断" : "已完成",
      done,
      failed: Boolean(failed && done && isLast),
      inProgress: !done,
    };
  });
  const approvalHitl = memberApprovalByAssign.value[assignId] || null;
  const approvalItems = workgroupApprovalItems(approvalHitl);
  const approvalByCall = new Map(approvalItems.map((item) => [item.callId, item]));
  const assignedApprovalByStep = new Map();
  const matchedApprovalIds = new Set();
  // Prefer the protocol call id.  This is the only unambiguous association.
  for (const step of baseSteps) {
    if (step.kind !== "tool" || !step.toolCallId) continue;
    const item = approvalByCall.get(step.toolCallId);
    if (item) {
      assignedApprovalByStep.set(step.key, item);
      matchedApprovalIds.add(item.callId);
    }
  }
  // Older timelines may not have tool_call_id on tool_started.  In that case
  // associate a name match with the newest unmatched step only.  The previous
  // implementation matched the same approval to every same-named step, which
  // produced 1, then 2, then 3 approval cards as the turn progressed.
  for (let i = baseSteps.length - 1; i >= 0; i -= 1) {
    const step = baseSteps[i];
    if (
      step.kind !== "tool" ||
      step.toolCallId ||
      assignedApprovalByStep.has(step.key)
    ) continue;
    const item = approvalItems.find(
      (entry) => !matchedApprovalIds.has(entry.callId) && entry.name === step.toolName,
    );
    if (!item) continue;
    assignedApprovalByStep.set(step.key, item);
    matchedApprovalIds.add(item.callId);
  }
  let steps;
  if (approvalItems.length > 1 && approvalHitl) {
    // A batch is one approval interaction.  Keep it as one card with all
    // calls, anchored to the first matching tool step, instead of rendering
    // one card per step and making the batch appear cumulative.
    const anchorKey = baseSteps.find((step) => assignedApprovalByStep.has(step.key))?.key || "";
    steps = baseSteps.map((step) => {
      if (step.key !== anchorKey) return step;
      return {
        ...step,
        summary: `批量审批 · ${approvalItems.length} 个工具调用`,
        statusText: "等待确认",
        done: false,
        inProgress: false,
        approval: {
          hitlId: String(approvalHitl.hitl_id || approvalHitl.id || ""),
          items: approvalItems,
          allItems: approvalItems,
        },
      };
    });
    if (!anchorKey) {
      steps.push({
        key: `approval-${approvalHitl.hitl_id}`,
        kind: "tool",
        toolCallId: "",
        toolName: "tool",
        toolKind: "tool",
        summary: `批量审批 · ${approvalItems.length} 个工具调用`,
        statusText: "等待确认",
        done: false,
        failed: false,
        inProgress: false,
        approval: {
          hitlId: String(approvalHitl.hitl_id || ""),
          items: approvalItems,
          allItems: approvalItems,
        },
      });
    }
  } else {
    steps = baseSteps.map((step) => {
      const item = assignedApprovalByStep.get(step.key);
      if (!item || !approvalHitl) return step;
      return {
        ...step,
        summary: approvalItemDisplayName(item),
        approval: {
          hitlId: String(approvalHitl.hitl_id || approvalHitl.id || ""),
          items: [item],
          allItems: approvalItems,
        },
      };
    });
    const remainingApprovalItems = approvalItems.filter((item) => !matchedApprovalIds.has(item.callId));
    if (approvalHitl && remainingApprovalItems.length) {
      const item = remainingApprovalItems[0];
      steps.push({
      key: `approval-${approvalHitl.hitl_id}`,
      kind: "tool",
      toolCallId: item?.callId || "",
      toolName: item?.name || "tool",
      toolKind: toolKindLabel(item?.name || "tool"),
      summary: approvalItemDisplayName(item),
      statusText: "等待确认",
      done: false,
      failed: false,
      inProgress: false,
      approval: {
        hitlId: String(approvalHitl.hitl_id || ""),
        items: [item],
        allItems: approvalItems,
      },
      });
    }
  }
  return {
    key: (started || finished)?.event_id || `assign-${(started || finished)?.seq}`,
    kind: "assign",
    assignId,
    started: started || null,
    finished: finished || null,
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
    reportToggleKey: assignId || (started || finished)?.event_id || "",
    taskPreview: previewAssignTask(taskText),
    taskToggleKey: assignId || (started || finished)?.event_id || "",
  };
}

function makeDirectToolItem(ev, { assignFinished, isLast, failed, toolFinished = null }) {
  const parsed = parseNoticeTool(ev?.text, ev?.tool_name);
  const finishStatus = String(toolFinished?.status || "").toLowerCase();
  const terminalToolResult = [
    "succeeded",
    "failed",
    "rejected",
    "indeterminate",
    "canceled",
    "cancelled",
    "timed_out",
  ].includes(finishStatus);
  const done = Boolean(assignFinished) || terminalToolResult || (!toolFinished && !isLast);
  const failedResult =
    Boolean(failed) || ["failed", "rejected", "indeterminate", "canceled", "cancelled", "timed_out"].includes(finishStatus);
  const assignId = String(ev?.assign_id || "").trim();
  const toolCallId = String(ev?.tool_call_id || "").trim();
  const toolName = String(ev?.tool_name || "").trim();
  return {
    key: ev.event_id || `tool-${ev.seq}`,
    kind: "tool",
    toolName: parsed.toolName,
    toolKind: toolKindLabel(parsed.toolName),
    summary: parsed.summary,
    statusText: !done ? "执行中" : failedResult ? "已中断" : "已完成",
    done,
    failed: Boolean(failedResult && done),
    inProgress: !done,
    assignId,
    toolCallId,
    approval: approvalForTool(assignId, toolCallId, toolName, isLast),
  };
}

function makeLeaderToolItem(ev) {
  const parsed = parseNoticeTool(ev?.text);
  const inProgress = Boolean(
    sending.value &&
      streamMode.value === "leader" &&
      streamActorId.value === "leader" &&
      streamPhase.value === "tool" &&
      Number(ev?.seq || 0) > Number(statusWatermarkSeq.value || 0),
  );
  return {
    key: ev.event_id || `leader-tool-${ev.seq}`,
    kind: "tool",
    toolName: parsed.toolName,
    toolKind: toolKindLabel(parsed.toolName),
    summary: parsed.summary,
    statusText: inProgress ? "生成中" : "已完成",
    done: !inProgress,
    failed: false,
    inProgress,
  };
}

function mixRenderHash(hash, value) {
  let next = hash >>> 0;
  const raw = String(value ?? "");
  for (let i = 0; i < raw.length; i += 1) {
    next = Math.imul(next ^ raw.charCodeAt(i), 16777619) >>> 0;
  }
  return next;
}

function renderItemToken(item) {
  const lastStep = item?.steps?.[item.steps.length - 1];
  const expanded = item?.reportToggleKey
    ? expandedMemberReports.value[item.reportToggleKey] ? 1 : 0
    : 0;
  const taskExpanded = item?.taskToggleKey
    ? expandedAssignTasks.value[item.taskToggleKey] ? 1 : 0
    : 0;
  return [
    item?.key,
    item?.kind,
    item?.text?.length || item?.ev?.text?.length || 0,
    item?.statusText,
    item?.done ? 1 : 0,
    item?.failed ? 1 : 0,
    item?.streaming ? 1 : 0,
    item?.phase,
    item?.tool,
    item?.reportText?.length || 0,
    expanded,
    taskExpanded,
    lastStep?.key,
    lastStep?.statusText,
    lastStep?.inProgress ? 1 : 0,
    lastStep?.failed ? 1 : 0,
    lastStep?.approval?.hitlId,
    lastStep?.approval?.items?.length || 0,
    item?.approval?.hitlId,
    item?.approval?.items?.length || 0,
  ].join("|");
}

function appendGroupItem(group, item) {
  group.items.push(item);
  group.renderHash = mixRenderHash(group.renderHash, renderItemToken(item));
  group.renderKey = `${group.bucket}|${group.label}|${group.items.length}|${group.renderHash}`;
}

/** 先按 assign_id 全局收口，再按 actor 分组；直连不展示分派壳，改为工具行 */
const eventGroups = computed(() => {
  const list = events.value || [];
  const {
    directAssignIds,
    noticesByAssign,
    finishedByAssign,
    startedByAssign,
    memberFinalByAssign,
    assistantContentByAssign,
    noticeIndexByEventId,
    toolEventsByAssign,
  } = buildAssignIndex(list);
  const consumedFinished = new Set();

  const flat = [];
  for (const ev of list) {
    const t = String(ev?.type || "");
    const aid = String(ev?.assign_id || "").trim();

    if (t === "assign_started") {
      const finished = aid ? finishedByAssign[aid] || null : null;
      if (aid && finished) consumedFinished.add(aid);
      const isDirect = Boolean(aid && directAssignIds.has(aid));
      // 直连：跳过分派气泡，工具过程由 notice 转成工具行
      if (isDirect) continue;
      const notices = aid ? noticesByAssign[aid] || [] : [];
      const memberFinal = aid ? memberFinalByAssign[aid] || null : null;
      const actorId = String(ev?.actor_id || "leader").trim() || "leader";
      flat.push({
        role: "assistant",
        actorId,
        label: eventLabel({ ...ev, actor_id: actorId, type: "assign_started" }),
        item: makeAssignItem(
          ev,
          finished,
          notices,
          false,
          memberFinal,
          aid ? assistantContentByAssign[aid] || [] : [],
          aid ? toolEventsByAssign[aid] || [] : [],
        ),
      });
      continue;
    }

    if (t === "assign_finished") {
      if (aid && startedByAssign[aid]) continue;
      if (aid && consumedFinished.has(aid)) continue;
      const isDirect = Boolean(aid && directAssignIds.has(aid));
      if (isDirect) {
        // Direct turns normally render the persisted member final text. If a
        // turn was canceled/failed before that text existed, keep a durable
        // status row visible instead of dropping the whole task.
        if (memberFinalByAssign[aid] || (toolEventsByAssign[aid] || []).length) continue;
        flat.push({
          role: "assistant",
          actorId: String(ev?.actor_id || "").trim(),
          label: eventLabel({ ...ev, type: "assign_finished" }),
          item: makeDirectToolItem(
            { ...ev, text: "tool", tool_name: "tool" },
            {
              assignFinished: true,
              isLast: true,
              failed: /失败|中断/.test(String(ev?.text || "")),
            },
          ),
        });
        continue;
      }
      const actorId = String(ev?.actor_id || "leader").trim() || "leader";
      const notices = aid ? noticesByAssign[aid] || [] : [];
      const memberFinal = aid ? memberFinalByAssign[aid] || null : null;
      flat.push({
        role: "assistant",
        actorId,
        label: eventLabel({ ...ev, actor_id: actorId, type: "assign_finished" }),
        item: makeAssignItem(
          null,
          ev,
          notices,
          false,
          memberFinal,
          aid ? assistantContentByAssign[aid] || [] : [],
          aid ? toolEventsByAssign[aid] || [] : [],
        ),
      });
      continue;
    }

    if (t === "tool_started" || t === "tool_finished") {
      const isDirect = Boolean(aid && directAssignIds.has(aid));
      if (isDirect) {
        const chain = toolEventsByAssign[aid] || [];
        const starts = chain.filter((item) => item?.type === "tool_started");
        const matchingStart = starts.find((item) => toolEventMatches(item, ev));
        // A structured tool_finished closes the preceding tool_started; one
        // row is enough for the user and avoids doubled start/finish entries.
        if (t === "tool_finished" && matchingStart) continue;
        const renderEvent = matchingStart || ev;
        const index = starts.findIndex((item) => item?.event_id === renderEvent?.event_id);
        const matchingFinish = chain.find(
          (item) => item?.type === "tool_finished" && toolEventMatches(item, renderEvent),
        );
        const finished = finishedByAssign[aid] || null;
        const failed = Boolean(
          (finished && /失败|中断/.test(String(finished.text || ""))) ||
            ["failed", "rejected", "indeterminate", "canceled", "cancelled", "timed_out"].includes(
              String(matchingFinish?.status || "").toLowerCase(),
            ),
        );
        flat.push({
          role: "assistant",
          actorId: String(renderEvent?.actor_id || ev?.actor_id || "").trim(),
          label: eventLabel({ ...renderEvent, type: "assign_started" }),
          item: makeDirectToolItem(renderEvent, {
            assignFinished: Boolean(finished),
            isLast: index < 0 || index === starts.length - 1,
            failed,
            toolFinished: matchingFinish,
          }),
        });
      }
      // Structured tool events are represented by the direct tool row or the
      // surrounding assign card; never leak raw tool_started/tool_finished.
      continue;
    }

    // 编排态成员回报并入分派气泡折叠展示；直连仍走普通消息
    if (t === "assistant_content" && aid && !directAssignIds.has(aid)) {
      continue;
    }

    if (t === "actor_final_text") {
      const actor = String(ev?.actor_id || "").trim();
      if (actor && actor !== "leader" && aid && !directAssignIds.has(aid)) {
        continue;
      }
    }

    if (t === "system_notice") {
      // 编排态：notice 只滚进分派气泡
      if (aid && !directAssignIds.has(aid)) continue;
      // 旧「已直达」提示不展示
      if (String(ev?.text || "").startsWith("已直达")) continue;
      const actorId = String(ev?.actor_id || "").trim();
      if (aid && directAssignIds.has(aid)) {
        if ((toolEventsByAssign[aid] || []).length) continue;
        const chain = noticesByAssign[aid] || [];
        const idx = ev.event_id ? noticeIndexByEventId[ev.event_id] ?? -1 : -1;
        const isLast = idx < 0 || idx === chain.length - 1;
        const finished = finishedByAssign[aid] || null;
        const failed = finished ? /失败|中断/.test(String(finished.text || "")) : false;
        flat.push({
          role: "assistant",
          actorId,
          label: eventLabel(ev),
          item: makeDirectToolItem(ev, {
            assignFinished: Boolean(finished),
            isLast,
            failed,
          }),
        });
        continue;
      }
      if (actorId === "leader") {
        flat.push({
          role: "assistant",
          actorId,
          label: eventLabel(ev),
          item: makeLeaderToolItem(ev),
        });
        continue;
      }
      // Transient member progress is rendered by the composer runtime rail;
      // keeping it in the message stream duplicates the same status.
      continue;
    }

    const role = isHumanEvent(ev) ? "user" : "assistant";
    const actorId = String(ev?.actor_id || "").trim();
    flat.push({
      role,
      actorId,
      label: eventLabel(ev),
      item: {
        key: ev.event_id || `msg-${ev.seq}`,
        kind: "message",
        ev,
      },
    });
  }

  if (liveUser.value) {
    const already = (list || []).some(
      (ev) =>
        String(ev?.type || "") === "human_message" &&
        String(ev?.text || "") === liveUser.value.text,
    );
    if (!already) {
      const actorId = selfNodeId.value || "node";
      flat.push({
        role: "user",
        actorId,
        label: selfNodeName.value || actorId,
      item: {
        key: liveUser.value.id,
        kind: "live_user",
        text: liveUser.value.text,
        directMemberId: liveUser.value.directMemberId,
      },
      });
    }
  }

  if (showLiveAssistant.value) {
    const actorId = streamActorId.value || (streamMode.value === "leader" ? "leader" : "member");
    flat.push({
      role: "assistant",
      actorId,
      label: actorId === "leader" ? "Supervisor" : memberNameById.value[actorId] || actorId,
      item: {
        key: liveAssistant.value.id,
        kind: "live_assistant",
        text: liveAssistant.value.text || "",
        streaming: true,
        phase: streamPhase.value,
        tool: streamToolName.value,
      },
    });
  }

  const groups = [];
  for (const row of flat) {
    const bucket = `${row.role}:${row.actorId || "_"}`;
    const last = groups[groups.length - 1];
    if (last && last.bucket === bucket) {
      appendGroupItem(last, row.item);
      continue;
    }
    const group = {
      key: `${bucket}-${row.item.key}`,
      bucket,
      role: row.role,
      label: row.label,
      items: [row.item],
      renderHash: mixRenderHash(2166136261, renderItemToken(row.item)),
    };
    group.renderKey = `${group.bucket}|${group.label}|1|${group.renderHash}`;
    groups.push(group);
  }
  return groups;
});
  return {
    eventGroups,
    isHumanEvent,
    splitUserMentionParts,
    isMemberReportExpanded,
    toggleMemberReport,
    isAssignTaskExpanded,
    toggleAssignTask,
    taskDetailsId,
    parseNoticeTool,
    isDirectAssignEvent,
  };
}
