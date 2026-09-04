<script setup>
import { computed, nextTick, reactive, ref, watch } from "vue";
import * as api from "../api/node.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: "create" }, // create | edit
  workgroupId: { type: String, default: "" },
  memberId: { type: String, default: "" },
  member: { type: Object, default: null },
  defaultHomeNodeId: { type: String, default: "" },
});

const emit = defineEmits(["close", "saved"]);

const agentOptions = ref([]);
const draft = reactive(emptyDraft());
const busy = ref(false);
const error = ref("");
const nameInput = ref(null);

const title = computed(() => (props.mode === "create" ? "添加成员" : "配置成员"));
const lead = computed(() =>
  props.mode === "create"
    ? "成员在独立工作区协作，由 Supervisor 编排调用。"
    : "修改后会重新同步到 Home Node。",
);
const primaryLabel = computed(() => {
  if (busy.value) return props.mode === "create" ? "添加中…" : "保存中…";
  return props.mode === "create" ? "添加" : "保存";
});
const canSubmit = computed(() => {
  if (busy.value) return false;
  return String(draft.displayName || "").trim().length > 0 &&
    (props.mode === "edit" || String(draft.agentId || "").trim().length > 0);
});

function emptyDraft() {
  return {
    displayName: "",
    homeNodeId: "",
    agentId: "",
    description: "",
  };
}

function onBackdropClick(event) {
  if (busy.value) return;
  if (event.target === event.currentTarget) emit("close");
}

async function loadAgentCatalog() {
  if (!props.workgroupId) return;
  try {
    const res = await api.listWorkgroupAgents();
    const rows = Array.isArray(res?.agents) ? res.agents : [];
    agentOptions.value = rows
      .map((item) => {
        const id = String(item?.agent_id || "").trim();
        const nodeId = String(item?.node_id || "").trim();
        const name = String(item?.name || "").trim() || id;
        return {
          id,
          nodeId,
          status: String(item?.status || "unknown"),
          label: nodeId && nodeId !== id ? `${name} · ${nodeId}` : name,
        };
      })
      .filter((item) => item.id);
  } catch (e) {
    agentOptions.value = [];
    error.value = e?.message || "加载可用 Agent 失败";
  }
}

async function resetFromProps() {
  error.value = "";
  busy.value = false;
  Object.assign(draft, emptyDraft());
  draft.homeNodeId = String(props.defaultHomeNodeId || "").trim();

  if (props.mode !== "edit" || !props.workgroupId || !props.memberId) {
    await nextTick();
    nameInput.value?.focus?.();
    return;
  }

  const member = props.member || {};
  draft.displayName = String(member.display_name || "").trim();
  draft.agentId = String(member.agent_id || "").trim();
  draft.homeNodeId = String(member.home_node_id || props.defaultHomeNodeId || "").trim();
  draft.description = String(member.description || "").trim();
  await nextTick();
  nameInput.value?.focus?.();
}

async function submit() {
  error.value = "";
  const name = String(draft.displayName || "").trim();
  if (!name || !props.workgroupId || busy.value) return;
  const body = {
    display_name: name,
    description: String(draft.description || "").trim(),
  };
  if (props.mode === "create") {
    const agentId = String(draft.agentId || "").trim();
    if (!agentId) {
      error.value = "请选择一个已注册的 Agent";
      return;
    }
    body.agent_id = agentId;
    const target = agentOptions.value.find((item) => item.id === agentId);
    if (target?.nodeId) body.home_node_id = target.nodeId;
  }
  busy.value = true;
  try {
    if (props.mode === "create") {
      await api.createWorkgroupMember(props.workgroupId, body);
    } else {
      await api.patchWorkgroupMember(props.workgroupId, props.memberId, body);
    }
    emit("saved", {
      mode: props.mode,
      workgroupId: props.workgroupId,
      memberId: props.memberId,
      displayName: name,
    });
    emit("close");
  } catch (e) {
    error.value = e?.message || (props.mode === "create" ? "添加失败" : "保存失败");
  } finally {
    busy.value = false;
  }
}

watch(
  () => props.open,
  (visible) => {
    if (visible) {
      void (async () => {
        await loadAgentCatalog();
        await resetFromProps();
      })();
    }
  },
);
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="wg-member-overlay" @click="onBackdropClick">
      <section
        class="wg-member-modal"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="'wg-member-title'"
      >
        <header class="wg-member-modal__header">
          <div class="wg-member-modal__heading">
            <h2 id="wg-member-title" class="wg-member-modal__title">{{ title }}</h2>
            <p class="wg-member-modal__lead">{{ lead }}</p>
          </div>
          <button
            type="button"
            class="wg-member-modal__close"
            aria-label="关闭"
            :disabled="busy"
            @click="emit('close')"
          >
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path
                d="M4 4l8 8M12 4l-8 8"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
              />
            </svg>
          </button>
        </header>

        <div class="wg-member-modal__body">
            <label class="wg-member-modal__field">
              <span class="wg-member-modal__label">显示名</span>
              <input
                ref="nameInput"
                v-model="draft.displayName"
                class="wg-member-modal__input"
                type="text"
                maxlength="64"
                placeholder="例如：资料员、审稿助手"
                autocomplete="off"
                :disabled="busy"
                @keydown.enter.prevent="canSubmit && submit()"
              />
            </label>

            <label v-if="props.mode === 'create'" class="wg-member-modal__field">
              <span class="wg-member-modal__label">选择 Agent</span>
              <select
                v-model="draft.agentId"
                class="wg-member-modal__input"
                :disabled="busy || !agentOptions.length"
                required
              >
                <option value="" disabled>
                  {{ agentOptions.length ? "选择已注册的 Agent" : "暂无在线 Agent" }}
                </option>
                <option v-for="item in agentOptions" :key="item.id" :value="item.id">
                  {{ item.label }}
                </option>
              </select>
              <span class="wg-member-modal__hint-text">
                直接复用 Node 上已有 Agent；会话与个人对话隔离。
              </span>
            </label>

            <p v-if="!agentOptions.length && props.mode === 'create'" class="wg-member-modal__hint-text wg-member-modal__hint-text--warn">
              未发现可用 Agent。请确认 Node 已连接 Manage，并完成 Agent 注册。
            </p>

            <p v-if="props.mode === 'create'" class="wg-member-modal__hint-text">
              工具、提示词和 LLM 配置由所选 Agent 自己管理；工作组只建立独立会话。
            </p>

            <label class="wg-member-modal__field">
              <span class="wg-member-modal__label">成员说明</span>
              <textarea
                v-model="draft.description"
                class="wg-member-modal__input wg-member-modal__textarea"
                rows="3"
                placeholder="描述这个成员适合处理的任务（可选）"
                :disabled="busy"
              />
            </label>

            <label v-if="props.mode !== 'create'" class="wg-member-modal__field">
              <span class="wg-member-modal__label">绑定 Agent</span>
              <input class="wg-member-modal__input" type="text" :value="draft.agentId" disabled />
              <span class="wg-member-modal__hint-text">创建后不可更换绑定的 Agent 或 Home Node。</span>
            </label>
        </div>

        <footer class="wg-member-modal__footer">
          <p v-if="error" class="wg-member-modal__error">{{ error }}</p>
          <div class="wg-member-modal__actions">
            <button type="button" class="btn btn--ghost" :disabled="busy" @click="emit('close')">
              取消
            </button>
            <button
              type="button"
              class="btn btn--primary"
              :disabled="!canSubmit"
              @click="submit"
            >
              {{ primaryLabel }}
            </button>
          </div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.wg-member-overlay {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: grid;
  place-items: center;
  padding: 24px;
  background: var(--color-overlay);
  backdrop-filter: blur(2px);
}

.wg-member-modal {
  width: min(460px, 96vw);
  max-height: min(88vh, 720px);
  display: flex;
  flex-direction: column;
  border-radius: 14px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  box-shadow: var(--shadow-lg, 0 16px 48px rgba(0, 0, 0, 0.28));
  overflow: hidden;
}

.wg-member-modal__header {
  flex: 0 0 auto;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--color-border);
}

.wg-member-modal__footer {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
  padding: 12px 20px;
  border-top: 1px solid var(--color-border);
}

.wg-member-modal__heading {
  min-width: 0;
}

.wg-member-modal__title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  line-height: 1.3;
  color: var(--color-text);
}

.wg-member-modal__lead {
  margin: 6px 0 0;
  font-size: 13px;
  line-height: 1.45;
  color: var(--color-text-muted);
}

.wg-member-modal__close {
  flex: 0 0 auto;
  width: 30px;
  height: 30px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin: -4px -6px 0 0;
  border: none;
  border-radius: var(--radius-md, 6px);
  background: transparent;
  color: var(--color-text-muted);
  cursor: pointer;
  transition: color 0.12s ease, background 0.12s ease;
}

.wg-member-modal__close:hover:not(:disabled) {
  color: var(--color-text);
  background: var(--color-surface-hover, rgba(0, 0, 0, 0.04));
}

.wg-member-modal__close:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.wg-member-modal__body {
  flex: 1 1 auto;
  overflow: auto;
  padding: 18px 20px 12px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.wg-member-modal__hint {
  margin: 0;
  color: var(--color-text-muted);
  font-size: 13px;
}

.wg-member-modal__field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.wg-member-modal__label {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-muted);
  letter-spacing: 0.01em;
}

.wg-member-modal__input {
  width: 100%;
  box-sizing: border-box;
  padding: 9px 11px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  border-radius: 8px;
  background: var(--color-surface);
  color: var(--color-text);
  font: inherit;
  font-size: 13px;
  line-height: 1.4;
  transition: border-color 0.12s ease, box-shadow 0.12s ease;
}

.wg-member-modal__input:focus {
  outline: none;
  border-color: var(--color-primary, #0078d4);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-primary, #0078d4) 18%, transparent);
}

.wg-member-modal__input:disabled {
  opacity: 0.65;
  cursor: not-allowed;
}

.wg-member-modal__hint-text {
  margin: 0;
  font-size: 12px;
  line-height: 1.4;
  color: var(--color-text-subtle, var(--color-text-muted));
}

.wg-member-modal__hint-text--warn {
  color: var(--color-warning, #b45309);
}

.wg-member-modal__tools {
  margin: 0;
  padding: 0;
  border: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.wg-member-modal__tool-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.wg-member-modal__tool {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  border-radius: 10px;
  background: var(--color-surface);
  color: var(--color-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.12s ease, background 0.12s ease;
}

.wg-member-modal__tool:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--color-primary, #0078d4) 45%, var(--color-border));
  background: var(--color-surface-hover, rgba(0, 0, 0, 0.02));
}

.wg-member-modal__tool--on {
  border-color: color-mix(in srgb, var(--color-primary, #0078d4) 55%, var(--color-border));
  background: color-mix(in srgb, var(--color-primary, #0078d4) 8%, var(--color-surface));
}

.wg-member-modal__tool:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.wg-member-modal__tool-check {
  flex: 0 0 auto;
  width: 18px;
  height: 18px;
  margin-top: 1px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 5px;
  border: 1px solid var(--color-border-strong, var(--color-border));
  background: var(--color-surface);
  color: var(--color-primary-strong, var(--color-primary, #0078d4));
}

.wg-member-modal__tool--on .wg-member-modal__tool-check {
  border-color: var(--color-primary, #0078d4);
  background: color-mix(in srgb, var(--color-primary, #0078d4) 14%, transparent);
}

.wg-member-modal__tool-text {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.wg-member-modal__tool-name {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.3;
}

.wg-member-modal__tool-hint {
  font-size: 12px;
  line-height: 1.4;
  color: var(--color-text-muted);
}

.wg-member-modal__advanced-toggle {
  align-self: flex-start;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: none;
  background: none;
  padding: 2px 0;
  color: var(--color-text-muted);
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

.wg-member-modal__advanced-toggle:hover:not(:disabled) {
  color: var(--color-text);
}

.wg-member-modal__advanced-toggle:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.wg-member-modal__advanced-chevron {
  opacity: 0.7;
  transition: transform 0.15s ease;
}

.wg-member-modal__advanced-chevron--open {
  transform: rotate(180deg);
}

.wg-member-modal__advanced {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding-top: 2px;
}

.wg-member-modal__textarea {
  resize: vertical;
  min-height: 72px;
  font-family: inherit;
  line-height: 1.45;
}

.wg-member-modal__error {
  margin: 0;
  color: var(--color-danger, #c42b1c);
  font-size: 12px;
}

.wg-member-modal__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
