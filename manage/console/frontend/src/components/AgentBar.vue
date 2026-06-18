<script setup>
import { onMounted, ref } from "vue";
import { fetchLLMConfigs, resolveLLMConfig } from "../api.js";

const emit = defineEmits(["toast"]);

const configs = ref([]);
const selected = ref("");
const task = ref("");
const running = ref(false);
const status = ref("");

async function loadConfigs() {
  try {
    configs.value = await fetchLLMConfigs();
    const def = configs.value.find((c) => c.is_default) || configs.value[0];
    if (def) selected.value = def.id;
  } catch (err) {
    emit("toast", { message: `加载 LLM 配置失败：${err.message}`, type: "error" });
  }
}

async function run() {
  if (!selected.value) {
    emit("toast", { message: "请先在『LLM 配置』里创建并选择一个配置", type: "error" });
    return;
  }
  if (!task.value.trim()) return;
  running.value = true;
  status.value = "解析配置…";
  try {
    const cfg = await resolveLLMConfig(selected.value);
    status.value = "加载 PageAgent…";
    const mod = await import("page-agent");
    const PageAgent = mod.PageAgent || mod.default?.PageAgent || mod.default;
    status.value = "执行中…";
    const agent = new PageAgent({
      model: cfg.model,
      baseURL: cfg.baseURL,
      apiKey: cfg.apiKey,
      language: "zh-CN",
    });
    const res = await agent.execute(task.value.trim());
    const summary =
      (res && (res.summary || res.message || res.result)) ||
      (typeof res === "string" ? res : JSON.stringify(res ?? "done"));
    status.value = "完成：" + String(summary).slice(0, 240);
    emit("toast", { message: "PageAgent 执行完成", type: "success" });
  } catch (err) {
    status.value = "失败：" + err.message;
    emit("toast", { message: `PageAgent 失败：${err.message}`, type: "error" });
  } finally {
    running.value = false;
  }
}

onMounted(loadConfigs);
defineExpose({ loadConfigs });
</script>

<template>
  <section class="agent-bar">
    <div class="agent-bar-row">
      <span class="agent-bar-label">PageAgent</span>
      <select v-model="selected" class="agent-bar-select" title="使用的 LLM 配置">
        <option v-if="!configs.length" value="">无配置 → 去 LLM 配置 创建</option>
        <option v-for="c in configs" :key="c.id" :value="c.id">
          {{ c.name }} · {{ c.model }}{{ c.is_default ? "（默认）" : "" }}
        </option>
      </select>
      <input
        v-model="task"
        class="agent-bar-input"
        placeholder="用自然语言操作控制台，如：给 node-a 分配 a2a-lab 组"
        @keyup.enter="run"
      />
      <button class="btn btn-primary" :disabled="running" @click="run">
        {{ running ? "执行中…" : "执行" }}
      </button>
    </div>
    <p v-if="status" class="agent-bar-status">{{ status }}</p>
  </section>
</template>

<style scoped>
.agent-bar {
  background: var(--surface, #fff);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: 12px;
  padding: 10px 14px;
  margin-bottom: 16px;
}
.agent-bar-row {
  display: flex;
  gap: 10px;
  align-items: center;
}
.agent-bar-label {
  font-weight: 600;
  color: var(--accent, #6366f1);
  white-space: nowrap;
}
.agent-bar-select {
  flex: 0 0 auto;
  max-width: 240px;
}
.agent-bar-input {
  flex: 1 1 auto;
}
.agent-bar-status {
  margin: 8px 0 0;
  font-size: 13px;
  color: var(--muted, #6b7280);
  word-break: break-all;
}
</style>
