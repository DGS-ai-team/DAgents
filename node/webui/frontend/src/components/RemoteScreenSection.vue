<script setup>
import { computed, onBeforeUnmount, ref, watch } from "vue";

const props = defineProps({
  agentId: { type: String, default: "" },
  enabled: { type: Boolean, default: false },
  hostLabel: { type: String, default: "" },
});

const watching = ref(false);
const error = ref("");
const frameSrc = ref("");
const statusLabel = ref("");
/** @type {import('vue').Ref<EventSource|null>} */
const esRef = ref(null);

const titleHint = computed(() => {
  const os = String(props.hostLabel || "").trim();
  return os ? `远程桌面（旁观）· ${os}` : "远程桌面（旁观）";
});

function stop() {
  if (esRef.value) {
    esRef.value.close();
    esRef.value = null;
  }
  watching.value = false;
  frameSrc.value = "";
  statusLabel.value = "";
}

function start() {
  const id = String(props.agentId || "").trim();
  if (!id || !props.enabled) return;
  stop();
  error.value = "";
  watching.value = true;
  const url = new URL(`/v1/agents/${encodeURIComponent(id)}/screen/stream`, window.location.origin);
  const es = new EventSource(url.toString());
  esRef.value = es;

  es.addEventListener("status", (ev) => {
    try {
      const data = JSON.parse(ev.data || "{}");
      const backend = String(data.backend || "").trim();
      const label = String(data.display_label || "").trim();
      statusLabel.value = [label, backend && backend !== "none" ? backend : ""].filter(Boolean).join(" · ");
    } catch {
      /* ignore */
    }
  });

  es.addEventListener("frame", (ev) => {
    try {
      const data = JSON.parse(ev.data || "{}");
      const mime = String(data.mime || "image/jpeg").trim() || "image/jpeg";
      const b64 = String(data.b64 || "").trim();
      if (!b64) return;
      frameSrc.value = `data:${mime};base64,${b64}`;
      error.value = "";
    } catch (e) {
      error.value = e.message || "帧解析失败";
    }
  });

  es.addEventListener("error", (ev) => {
    // EventSource 原生 error 与自定义 event:error 都会进这里；自定义带 data
    if (ev?.data) {
      try {
        const data = JSON.parse(ev.data || "{}");
        error.value = data.error === "screen_unavailable" ? "屏幕不可用" : String(data.error || "屏幕流错误");
      } catch {
        error.value = "屏幕流错误";
      }
    } else if (es.readyState === EventSource.CLOSED) {
      error.value = error.value || "屏幕流已断开";
      watching.value = false;
    }
  });

  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) {
      watching.value = false;
      if (!error.value) error.value = "无法连接屏幕流（home 可能无 GUI 或 Edge 不可用）";
    }
  };
}

function toggle() {
  if (watching.value) stop();
  else start();
}

watch(
  () => [props.agentId, props.enabled],
  () => {
    stop();
    error.value = "";
  },
);

onBeforeUnmount(stop);

defineExpose({ start, stop });
</script>

<template>
  <div class="remote-screen">
    <div class="remote-screen__toolbar">
      <p class="remote-screen__hint">{{ titleHint }} · 只读，无键鼠</p>
      <button type="button" class="remote-screen__btn" :disabled="!enabled || !agentId" @click="toggle">
        {{ watching ? "停止旁观" : "开始旁观" }}
      </button>
    </div>
    <p v-if="statusLabel && watching" class="remote-screen__status">{{ statusLabel }}</p>
    <p v-if="error" class="remote-screen__error">{{ error }}</p>
    <div class="remote-screen__viewport" aria-live="polite">
      <img v-if="frameSrc" :src="frameSrc" alt="远程桌面旁观画面" class="remote-screen__frame" />
      <p v-else class="remote-screen__placeholder">{{ watching ? "等待首帧…" : "未连接" }}</p>
    </div>
  </div>
</template>

<style scoped>
.remote-screen {
  padding: 0 10px 12px 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.remote-screen__toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}

.remote-screen__hint {
  margin: 0;
  font-size: 11px;
  line-height: 1.4;
  color: var(--color-text-subtle);
}

.remote-screen__btn {
  flex: 0 0 auto;
  border: 1px solid var(--color-border);
  border-radius: 6px;
  background: var(--color-surface-muted);
  color: inherit;
  font-size: 11px;
  padding: 4px 8px;
  cursor: pointer;
}

.remote-screen__btn:hover:not(:disabled) {
  border-color: var(--color-border-strong);
}

.remote-screen__btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.remote-screen__status {
  margin: 0;
  font-size: 11px;
  color: var(--color-text-muted);
}

.remote-screen__error {
  margin: 0;
  font-size: 11px;
  color: var(--color-danger);
}

.remote-screen__viewport {
  border: 1px solid var(--color-border);
  border-radius: 8px;
  background: #12151a;
  min-height: 120px;
  overflow: hidden;
  display: grid;
  place-items: center;
}

.remote-screen__frame {
  display: block;
  width: 100%;
  height: auto;
  max-height: 220px;
  object-fit: contain;
  pointer-events: none;
  user-select: none;
}

.remote-screen__placeholder {
  margin: 0;
  padding: 24px 12px;
  font-size: 12px;
  color: var(--color-text-subtle);
}
</style>
