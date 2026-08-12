<script setup>
import { computed } from "vue";
import {
  buildToolCallMap,
  filterLinkedToolMessages,
  resolveToolName,
} from "../caseToolResolve.js";

const props = defineProps({
  messages: { type: Array, default: () => [] },
  editing: { type: Boolean, default: false },
});

const emit = defineEmits(["insert", "edit", "delete"]);

const displayMessages = computed(() =>
  props.editing ? props.messages : filterLinkedToolMessages(props.messages),
);

const toolCallMap = computed(() => buildToolCallMap(props.messages));

function rolePillClass(role) {
  const map = {
    user: "pill-role-user",
    assistant: "pill-role-assistant",
    system: "pill-role-system",
    tool: "pill-role-tool",
  };
  return map[role] || "pill-muted";
}

function messageBody(msg) {
  const content = String(msg.content ?? "").trim();
  if (content) return content;
  const rawContent = msg.raw?.content;
  if (typeof rawContent === "string" && rawContent.trim()) return rawContent.trim();
  return "";
}

function resolveArgs(raw, priorRaws) {
  const callId = String(raw.tool_call_id || "").trim();
  const content = messageBody({ content: raw.content, raw });
  if (callId.startsWith("async-job-")) {
    const srcId = content.split("\n").map((l) => l.trim()).find((l) => l.startsWith("source_tool_call_id="));
    if (srcId) {
      const id = srcId.slice("source_tool_call_id=".length).trim();
      if (toolCallMap.value.has(id)) return toolCallMap.value.get(id).arguments ?? "";
    }
    const jobId = callId.slice("async-job-".length).trim();
    if (jobId) {
      for (let i = priorRaws.length - 1; i >= 0; i -= 1) {
        const prev = priorRaws[i];
        if (prev.role !== "tool") continue;
        const prevContent = String(prev.content ?? "");
        if (!prevContent.includes(jobId)) continue;
        const prevCall = String(prev.tool_call_id || "").trim();
        if (prevCall && toolCallMap.value.has(prevCall)) {
          return toolCallMap.value.get(prevCall).arguments ?? "";
        }
      }
    }
  }
  if (callId && toolCallMap.value.has(callId)) {
    return toolCallMap.value.get(callId).arguments ?? "";
  }
  return "";
}

function toolDetails(msg, msgIndex) {
  const raws = props.messages.map((m) => m.raw || {});
  const prior = raws.slice(0, msgIndex >= 0 ? msgIndex : raws.indexOf(msg.raw || {}));
  const raw = msg.raw || {};
  const toolName = resolveToolName(raw, prior, toolCallMap.value) || "—";
  const args = resolveArgs(raw, prior);
  const result = messageBody(msg) || "—";
  return { toolName, args: formatToolArgs(args), result };
}

function formatToolArgs(args) {
  const s = String(args ?? "").trim();
  if (!s) return "—";
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

function preview(text, max = 120) {
  const s = String(text || "").replace(/\s+/g, " ").trim();
  if (s.length <= max) return s || "—";
  return `${s.slice(0, max)}…`;
}

function sourceIndex(msg) {
  return props.messages.findIndex((m) => m.id === msg.id);
}

function onInsert(index) {
  emit("insert", index);
}

function onEdit(msg, index) {
  emit("edit", msg, index);
}

function onDelete(msg) {
  emit("delete", msg);
}
</script>

<template>
  <div v-if="!editing" class="case-msg-list-wrap">
    <ul v-if="displayMessages.length" class="case-msg-list">
      <li
        v-for="(msg, idx) in displayMessages"
        :key="msg.id"
        class="case-msg-item"
        :class="`case-msg-item--${msg.role || 'user'}`"
      >
        <div class="case-msg-item__head">
          <span class="pill" :class="rolePillClass(msg.role)">{{ msg.role }}</span>
          <span class="case-msg-item__index muted">#{{ idx + 1 }}</span>
        </div>

        <div v-if="msg.role === 'tool'" class="case-msg-tool">
          <dl class="case-msg-tool__grid">
            <div>
              <dt>工具名</dt>
              <dd class="mono">{{ toolDetails(msg, sourceIndex(msg)).toolName }}</dd>
            </div>
            <div>
              <dt>参数</dt>
              <dd class="mono case-msg-tool__pre">{{ toolDetails(msg, sourceIndex(msg)).args }}</dd>
            </div>
            <div>
              <dt>结果</dt>
              <dd class="case-msg-tool__pre">{{ toolDetails(msg, sourceIndex(msg)).result }}</dd>
            </div>
          </dl>
        </div>

        <div v-else class="case-msg-body">{{ messageBody(msg) || "—" }}</div>
      </li>
    </ul>
    <p v-else class="panel-meta case-msg-empty">暂无消息</p>
  </div>

  <div v-else class="table-scroll">
    <table class="data-table">
      <thead>
        <tr>
          <th>#</th>
          <th>role</th>
          <th>内容预览</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(msg, idx) in messages" :key="msg.id">
          <td class="muted">{{ idx + 1 }}</td>
          <td>
            <span class="pill" :class="rolePillClass(msg.role)">{{ msg.role }}</span>
          </td>
          <td class="cell-wrap">
            <template v-if="msg.role === 'tool'">
              {{ toolDetails(msg, idx).toolName }} · {{ preview(toolDetails(msg, idx).result) }}
            </template>
            <template v-else>{{ preview(messageBody(msg)) }}</template>
          </td>
          <td class="actions-cell">
            <button type="button" class="btn btn-ghost btn-sm" @click="onInsert(idx)">前插</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="onEdit(msg, idx)">编辑</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="onDelete(msg)">删</button>
          </td>
        </tr>
        <tr v-if="!messages.length">
          <td colspan="4" class="empty">暂无消息</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
