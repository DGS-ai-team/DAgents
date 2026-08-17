<script setup>
import { useRouter } from "vue-router";
import UpdatePanel from "../../components/UpdatePanel.vue";
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
    <header class="settings-page__header">
      <div class="settings-page__header-main">
        <h1 class="settings-page__title">关于</h1>
      </div>
    </header>
    <UpdatePanel embedded @close="() => {}" />

    <section class="settings-section settings-section--standalone">
      <h2 class="settings-section__title">帮助</h2>
      <HelpPanel embedded @close="() => {}" @pick="onPick" />
    </section>
  </div>
</template>
