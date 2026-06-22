<script setup>
import { ref } from "vue";
import { fetchLLMConfigs, resolveLLMConfig } from "../api.js";

const emit = defineEmits(["toast"]);
const loading = ref(false);
let agent = null; // 懒创建的 PageAgent 实例（自带 panel 控制台）

async function pickResolved() {
  const configs = await fetchLLMConfigs();
  if (!configs.length) return null;
  const def = configs.find((c) => c.is_default) || configs[0];
  return resolveLLMConfig(def.id);
}

async function openConsole() {
  // 已创建：直接显示它自带的控制台面板
  if (agent) {
    agent.panel?.show?.();
    return;
  }
  loading.value = true;
  try {
    const cfg = await pickResolved();
    if (!cfg) {
      emit("toast", {
        message: "请先在 Node 配置 → LLM 配置 创建一个 LLM 配置",
        type: "error",
      });
      return;
    }
    const mod = await import("page-agent");
    const PageAgent = mod.PageAgent || mod.default?.PageAgent || mod.default;
    agent = new PageAgent({
      model: cfg.model,
      baseURL: cfg.baseURL,
      apiKey: cfg.apiKey,
      language: "zh-CN",
    });
    agent.panel?.show?.();
    emit("toast", { message: "AskAI 已就绪，在面板里输入指令", type: "success" });
  } catch (err) {
    emit("toast", { message: `AskAI 启动失败：${err.message}`, type: "error" });
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <button
    class="askai-fab"
    :disabled="loading"
    title="AskAI · 用自然语言操作控制台（PageAgent）"
    @click="openConsole"
  >
    <svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true" class="askai-icon">
      <path d="M10 1.5l1.6 4.3L16 7.4l-3.6 2.7L13 14.5 10 12l-3 2.5.6-4.4L4 7.4l4.4-1.6L10 1.5z" />
    </svg>
    <span>{{ loading ? "启动中…" : "AskAI" }}</span>
  </button>
</template>

<style scoped>
.askai-fab {
  position: fixed;
  right: 20px;
  bottom: 20px;
  z-index: 60;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 10px 16px;
  border: none;
  border-radius: 999px;
  font-size: 14px;
  font-weight: 600;
  color: #fff;
  cursor: pointer;
  background: linear-gradient(135deg, #6366f1, #312e81);
  box-shadow: 0 6px 18px rgba(49, 46, 129, 0.35);
  transition: transform 0.12s ease, box-shadow 0.12s ease;
}
.askai-fab:hover {
  transform: translateY(-1px);
  box-shadow: 0 8px 22px rgba(49, 46, 129, 0.45);
}
.askai-fab:disabled {
  opacity: 0.7;
  cursor: progress;
}
.askai-icon {
  width: 16px;
  height: 16px;
}
</style>
