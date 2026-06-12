<script setup>
import { ref } from "vue";
import { bulkAssignGroups } from "../api.js";

const props = defineProps({
  agents: { type: Array, default: () => [] },
});

const emit = defineEmits(["saved", "error"]);

const bulkInput = ref("a2a-lab");
const bulkMsg = ref("");
const saving = ref(false);

async function onBulkSave() {
  bulkMsg.value = "保存中…";
  saving.value = true;
  try {
    if (!props.agents.length) {
      throw new Error("本页无 Node");
    }
    await bulkAssignGroups(props.agents, bulkInput.value);
    bulkMsg.value = `已更新 ${props.agents.length} 个 Node`;
    emit("saved", props.agents.length);
  } catch (err) {
    bulkMsg.value = err.message;
    emit("error", err.message);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <section class="panel callout-panel groups-bulk-panel">
    <div class="callout-icon" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
      </svg>
    </div>
    <div class="callout-body">
      <div class="section-title">discovery_group 分配</div>
      <p class="muted groups-bulk-hint">
        Node 注册时不会自动填分组。caller 与 target 须<strong>至少共享一个</strong>
        discovery_group，否则 <code>agent_discover</code> 与
        <code>agent_invoke</code> 将被 Manage 拒绝。
      </p>
      <div class="groups-editor">
        <label class="field field-grow">
          <span>分组名（逗号分隔，应用到本页全部 Node）</span>
          <input
            v-model="bulkInput"
            type="text"
            placeholder="a2a-lab, ops"
            autocomplete="off"
          />
        </label>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="saving"
          @click="onBulkSave"
        >
          批量保存
        </button>
        <span class="muted inline-msg">{{ bulkMsg }}</span>
      </div>
    </div>
  </section>
</template>
