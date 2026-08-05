<script setup>
import { onMounted, onUnmounted, ref } from "vue";
import { RouterView } from "vue-router";
import ImageLightbox from "./components/ImageLightbox.vue";
import FirstRunNodeProfile from "./views/FirstRunNodeProfile.vue";
import * as api from "./api/node.js";

defineOptions({ name: "AppRoot" });

const bootReady = ref(false);
const needProfile = ref(false);
const bootError = ref("");

async function refreshOnboarding({ soft = false } = {}) {
  try {
    const boot = await api.getUIBootstrap();
    needProfile.value = boot?.onboarding?.node_profile_completed === false;
    bootError.value = "";
  } catch (e) {
    // 启动竞态或短暂断连：不要把失败当成「已完成」而放行主界面。
    // soft 刷新时保留当前 needProfile，仅在尚未就绪时展示错误。
    if (!soft || !bootReady.value) {
      bootError.value = e.message || "无法连接 Node";
    }
  } finally {
    bootReady.value = true;
  }
}

function onProfileCompleted() {
  needProfile.value = false;
  bootError.value = "";
  bootReady.value = true;
}

function onVisibility() {
  if (document.visibilityState === "visible") {
    void refreshOnboarding({ soft: true });
  }
}

function onPageShow() {
  void refreshOnboarding({ soft: true });
}

onMounted(() => {
  void refreshOnboarding();
  document.addEventListener("visibilitychange", onVisibility);
  window.addEventListener("pageshow", onPageShow);
  window.addEventListener("focus", onPageShow);
});

onUnmounted(() => {
  document.removeEventListener("visibilitychange", onVisibility);
  window.removeEventListener("pageshow", onPageShow);
  window.removeEventListener("focus", onPageShow);
});
</script>

<template>
  <div v-if="!bootReady" class="app-boot">加载中…</div>
  <FirstRunNodeProfile v-else-if="needProfile" @completed="onProfileCompleted" />
  <div v-else-if="bootError" class="app-boot app-boot--error">{{ bootError }}</div>
  <template v-else>
    <RouterView v-slot="{ Component }">
      <KeepAlive include="ChatLayout">
        <component :is="Component" />
      </KeepAlive>
    </RouterView>
    <ImageLightbox />
  </template>
</template>

<style scoped>
.app-boot {
  min-height: 100vh;
  display: grid;
  place-items: center;
  color: var(--color-text-subtle);
  background: var(--color-bg);
}
.app-boot--error {
  color: var(--color-danger);
  padding: var(--space-6);
  text-align: center;
}
</style>
