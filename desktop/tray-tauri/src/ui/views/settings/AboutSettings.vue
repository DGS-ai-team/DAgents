<script setup>
import { useRouter } from "vue-router";
import UpdatePanel from "../../components/UpdatePanel.vue";
import HelpPanel from "../../components/HelpPanel.vue";
import SettingsPageHeader from "../../components/SettingsPageHeader.vue";
import { COMPOSER_DRAFT_KEY } from "../../utils/helpCommands.js";

const isDesktop = typeof window !== "undefined" && !!window.__TAURI_INTERNALS__;

const router = useRouter();

function onPick(cmd) {
  if (window.__TAURI_INTERNALS__) {
    window.dispatchEvent(new CustomEvent("desktop-use-chat-command", { detail: { command: cmd } }));
    return;
  }
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
    <SettingsPageHeader
      title="关于与帮助"
      description="查看当前版本、更新渠道与常用命令；需要更新时按页面提示操作。"
    />

    <section class="settings-section">
      <div class="settings-section__head">
        <div>
          <h2 class="settings-section__title">版本与更新</h2>
          <p class="settings-section__desc">检查当前 Node 的版本状态和可用升级方式。</p>
        </div>
      </div>
      <UpdatePanel embedded @close="() => {}" />
    </section>

    <section class="settings-section settings-section--standalone">
      <div class="settings-section__head">
        <div>
          <h2 class="settings-section__title">命令帮助</h2>
          <p class="settings-section__desc">{{ isDesktop ? "点击命令可填入对话输入区，补全参数后发送。" : "点击命令即可填入对话输入框，补全参数后发送。" }}</p>
        </div>
      </div>
      <HelpPanel embedded :desktop="isDesktop" @close="() => {}" @pick="onPick" />
    </section>
  </div>
</template>
