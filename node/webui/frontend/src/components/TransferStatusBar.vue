<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import {
  transferStore,
  refreshTransfers,
  startTransferEvents,
  stopTransferEvents,
  cancelTransfer,
} from "../stores/transfers.js";

const expanded = ref(false);
const tick = ref(0);
const dismissedIds = ref(new Set());
let timer = null;
const route = useRoute();

const isChatPage = computed(() => ["agents", "workgroups"].includes(String(route.name || "")));

const activeItems = computed(() => {
  void tick.value;
  return transferStore.items.filter((item) => ["queued", "transferring"].includes(item.status));
});

const recentItems = computed(() => {
  void tick.value;
  const now = Date.now();
  return transferStore.items.filter((item) => {
    if (dismissedIds.value.has(item.transfer_id)) return false;
    if (["queued", "transferring"].includes(item.status)) return true;
    if (["failed", "cancelled"].includes(item.status)) return true;
    const updated = Date.parse(item.updated_at || "");
    return Number.isFinite(updated) && now - updated < 8000;
  });
});

const failedItems = computed(() => recentItems.value.filter((item) => item.status === "failed"));
const cancelledItems = computed(() => recentItems.value.filter((item) => item.status === "cancelled"));

const totalProgress = computed(() => {
  const known = activeItems.value.filter((item) => item.total_bytes > 0);
  if (!known.length) return null;
  const total = known.reduce((sum, item) => sum + item.total_bytes, 0);
  const done = known.reduce((sum, item) => sum + Math.min(item.bytes_done, item.total_bytes), 0);
  return total > 0 ? Math.round((done / total) * 100) : 0;
});

const statusText = computed(() => {
  const active = activeItems.value.length;
  const queued = activeItems.value.filter((item) => item.status === "queued").length;
  const limit = Number(transferStore.maxConcurrentFiles) || 0;
  const activeLabel = limit ? `${active}/${limit} 个文件传输中` : `${active} 个文件传输中`;
  if (failedItems.value.length && active) {
    return `${failedItems.value.length} 个失败 · ${activeLabel}`;
  }
  if (failedItems.value.length) return `${failedItems.value.length} 个传输失败`;
  if (cancelledItems.value.length && !active) return `${cancelledItems.value.length} 个传输已取消`;
  if (!active) return "传输已完成";
  if (queued) return `${activeLabel} · ${queued} 个排队`;
  return activeLabel;
});

function formatBytes(value) {
  const bytes = Number(value) || 0;
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
}

function directionLabel(item) {
  return item.direction === "download" ? "下载" : "上传";
}

function statusLabel(item) {
  return ({ queued: "排队中", transferring: "传输中", completed: "已完成", failed: "失败", cancelled: "已取消" })[
    item.status
  ] || item.status;
}

async function cancel(item) {
  try {
    await cancelTransfer(item.transfer_id);
  } catch {
    /* store keeps the error; the next event/hydration updates the row */
  }
}

function dismiss(item) {
  const id = String(item?.transfer_id || "").trim();
  if (!id) return;
  dismissedIds.value = new Set([...dismissedIds.value, id]);
  if (!recentItems.value.length) expanded.value = false;
}

onMounted(async () => {
  await refreshTransfers();
  startTransferEvents();
  timer = setInterval(() => {
    tick.value += 1;
  }, 1000);
});

onUnmounted(() => {
  stopTransferEvents();
  if (timer) clearInterval(timer);
});
</script>

<template>
  <aside
    v-if="recentItems.length"
    class="transfer-status-bar"
    :class="{ 'transfer-status-bar--chat': isChatPage }"
    aria-live="polite"
  >
    <div class="transfer-status-bar__main">
      <button
        type="button"
        class="transfer-status-bar__summary"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        <span class="transfer-status-bar__icon" aria-hidden="true">⇅</span>
        <span class="transfer-status-bar__text">{{ statusText }}</span>
        <span v-if="transferStore.maxConcurrentFiles" class="transfer-status-bar__limit">
          并发上限 {{ transferStore.maxConcurrentFiles }}
        </span>
        <span class="transfer-status-bar__chevron" :class="{ 'is-open': expanded }">⌃</span>
      </button>
      <div class="transfer-status-bar__progress" role="progressbar" :aria-valuenow="totalProgress ?? undefined" aria-valuemin="0" aria-valuemax="100">
        <span v-if="totalProgress !== null" :style="{ width: `${totalProgress}%` }" />
      </div>
    </div>

    <div v-if="expanded" class="transfer-status-bar__details">
      <div v-for="item in recentItems" :key="item.transfer_id" class="transfer-status-row">
        <div class="transfer-status-row__topline">
          <span class="transfer-status-row__direction">{{ directionLabel(item) }}</span>
          <span class="transfer-status-row__path" :title="item.remote_path">{{ item.remote_path }}</span>
          <span class="transfer-status-row__status">{{ statusLabel(item) }}</span>
          <button
            v-if="['queued', 'transferring'].includes(item.status)"
            type="button"
            class="transfer-status-row__cancel"
            @click="cancel(item)"
          >取消</button>
          <button
            v-else-if="['failed', 'cancelled'].includes(item.status)"
            type="button"
            class="transfer-status-row__dismiss"
            @click="dismiss(item)"
          >清除</button>
        </div>
        <div class="transfer-status-row__meta">
          <span>{{ item.progress }}%</span>
          <span>{{ formatBytes(item.bytes_done) }}<template v-if="item.total_bytes"> / {{ formatBytes(item.total_bytes) }}</template></span>
          <span v-if="item.error" class="transfer-status-row__error">{{ item.error }}</span>
        </div>
        <div class="transfer-status-row__progress"><span :style="{ width: `${item.progress}%` }" /></div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.transfer-status-bar {
  position: fixed;
  z-index: 40;
  right: 16px;
  bottom: 16px;
  left: 16px;
  max-width: 760px;
  margin: 0 auto;
  color: var(--color-text, #233044);
  background: color-mix(in srgb, var(--color-surface, #fff) 94%, transparent);
  border: 1px solid var(--color-border, #dbe3ec);
  border-radius: 10px;
  box-shadow: 0 8px 28px rgba(25, 52, 82, 0.14);
  backdrop-filter: blur(12px);
}

.transfer-status-bar--chat {
  bottom: 124px;
  left: calc(var(--chat-sidebar-width) + 16px);
}

.transfer-status-bar__main { padding: 8px 12px 7px; }
.transfer-status-bar__summary {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  cursor: pointer;
  text-align: left;
}
.transfer-status-bar__icon { color: var(--color-primary, #3388d6); font-weight: 700; }
.transfer-status-bar__text { flex: 1; font-size: 12px; font-weight: 600; }
.transfer-status-bar__limit { color: var(--color-text-subtle, #7c8999); font-size: 11px; }
.transfer-status-bar__chevron { color: var(--color-text-subtle, #7c8999); transition: transform .15s ease; }
.transfer-status-bar__chevron.is-open { transform: rotate(180deg); }
.transfer-status-bar__progress,
.transfer-status-row__progress { height: 3px; overflow: hidden; border-radius: 99px; background: var(--color-surface-muted, #edf2f7); }
.transfer-status-bar__progress { margin-top: 7px; }
.transfer-status-bar__progress span,
.transfer-status-row__progress span { display: block; height: 100%; border-radius: inherit; background: var(--color-primary, #3388d6); transition: width .2s ease; }
.transfer-status-bar__progress span { min-width: 2px; }
.transfer-status-bar__details { max-height: 260px; overflow: auto; padding: 0 12px 8px; border-top: 1px solid var(--color-border, #dbe3ec); }
.transfer-status-row { padding: 8px 0 5px; }
.transfer-status-row + .transfer-status-row { border-top: 1px solid var(--color-border, #edf0f3); }
.transfer-status-row__topline, .transfer-status-row__meta { display: flex; align-items: center; gap: 8px; font-size: 11px; }
.transfer-status-row__direction { color: var(--color-primary-strong, #2467a8); font-weight: 600; }
.transfer-status-row__path { flex: 1; overflow: hidden; color: var(--color-text-muted, #66758a); text-overflow: ellipsis; white-space: nowrap; }
.transfer-status-row__status { color: var(--color-text-subtle, #7c8999); }
.transfer-status-row__cancel,
.transfer-status-row__dismiss { padding: 1px 6px; border: 1px solid var(--color-border, #dbe3ec); border-radius: 5px; background: transparent; color: var(--color-danger, #c44); font: inherit; cursor: pointer; }
.transfer-status-row__dismiss { color: var(--color-text-subtle, #7c8999); }
.transfer-status-row__cancel:hover,
.transfer-status-row__dismiss:hover { background: var(--color-surface-hover, #f0f0f0); }
.transfer-status-row__meta { margin-top: 4px; color: var(--color-text-subtle, #7c8999); }
.transfer-status-row__error { flex: 1; overflow: hidden; color: var(--color-danger, #c44); text-overflow: ellipsis; white-space: nowrap; }
.transfer-status-row__progress { flex: 1; margin-top: 5px; }

@media (max-width: 760px) {
  .transfer-status-bar,
  .transfer-status-bar--chat { right: 10px; bottom: 92px; left: 10px; max-width: none; }
}
</style>
