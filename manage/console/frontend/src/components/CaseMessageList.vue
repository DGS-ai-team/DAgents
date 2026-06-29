<script setup>
import { computed } from "vue";

const props = defineProps({
  messages: { type: Array, default: () => [] },
  editing: { type: Boolean, default: false },
});

const emit = defineEmits(["insert", "edit", "delete"]);

const toolCallMap = computed(() => {
  const map = new Map();
  for (const msg of props.messages) {
    const raw = msg.raw;
    if (!raw?.tool_calls || !Array.isArray(raw.tool_calls)) continue;
    for (const tc of raw.tool_calls) {
      const id = tc?.id;
      if (!id) continue;
      const fn = tc.function || {};
      map.set(id, {
        name: fn.name || tc.name || "",
        arguments: fn.arguments ?? "",
      });
    }
  }
  return map;
});

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

function toolDetails(msg) {
  const raw = msg.raw || {};
  const callId = raw.tool_call_id || "";
  const matched = callId ? toolCallMap.value.get(callId) : null;
  const toolName = raw.name || matched?.name || "—";
  const args = matched?.arguments ?? "";
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
    <ul v-if="messages.length" class="case-msg-list">
      <li
        v-for="(msg, idx) in messages"
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
              <dd class="mono">{{ toolDetails(msg).toolName }}</dd>
            </div>
            <div>
              <dt>参数</dt>
              <dd class="mono case-msg-tool__pre">{{ toolDetails(msg).args }}</dd>
            </div>
            <div>
              <dt>结果</dt>
              <dd class="case-msg-tool__pre">{{ toolDetails(msg).result }}</dd>
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
              {{ toolDetails(msg).toolName }} · {{ preview(toolDetails(msg).result) }}
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
