<script setup>
defineProps({
  status: { type: String, default: "修改后请保存。" },
  error: { type: String, default: "" },
  saving: { type: Boolean, default: false },
  busy: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  label: { type: String, default: "保存" },
});
const emit = defineEmits(["save"]);
</script>

<template>
  <footer class="settings-savebar">
    <p v-if="error" class="settings-savebar__status settings-savebar__status--error" role="alert">{{ error }}</p>
    <p v-else class="settings-savebar__status" role="status">{{ saving ? "正在保存…" : status }}</p>
    <div class="settings-savebar__actions setup-config-panel__actions"><slot /><button type="button" class="btn btn--primary btn--sm" :disabled="disabled || saving || busy" @click="emit('save')">
      {{ saving ? "保存中…" : label }}
    </button></div>
  </footer>
</template>
