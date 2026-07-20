<script setup>
import { useRouter } from "vue-router";
import HelpPanel from "../../components/HelpPanel.vue";
import { COMPOSER_DRAFT_KEY } from "../../utils/helpCommands.js";

const router = useRouter();

function onPick(cmd) {
  try {
    sessionStorage.setItem(COMPOSER_DRAFT_KEY, cmd);
  } catch {
    /* ignore */
  }
  router.push({ name: "agents" });
}
</script>

<template>
  <div class="settings-page settings-embedded">
    <h1 class="settings-page__title">帮助</h1>
    <p class="settings-page__hint">Slash 命令与快捷键；点击命令可回到对话并填入输入框。</p>
    <HelpPanel embedded @close="() => {}" @pick="onPick" />
  </div>
</template>
