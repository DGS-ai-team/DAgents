<script setup>
import { onMounted, ref } from "vue";
import { RouterView } from "vue-router";
import ImageLightbox from "./components/ImageLightbox.vue";
import FirstRunNodeProfile from "./views/FirstRunNodeProfile.vue";
import * as api from "./api/node.js";

defineOptions({ name: "AppRoot" });

const bootReady = ref(false);
const needProfile = ref(false);
const bootError = ref("");

async function refreshOnboarding() {
  bootError.value = "";
  try {
    const boot = await api.getUIBootstrap();
    needProfile.value = boot?.onboarding?.node_profile_completed === false;
  } catch (e) {
    bootError.value = e.message || "无法连接 Node";
    needProfile.value = false;
  } finally {
    bootReady.value = true;
  }
}

function onProfileCompleted() {
  needProfile.value = false;
  // 刷新一次，确保后续业务请求在服务端门闩放开后进入主界面
  bootReady.value = true;
}

onMounted(refreshOnboarding);
</script>

<template>
  <div v-if="!bootReady" class="app-boot">加载中…</div>
  <div v-else-if="bootError" class="app-boot app-boot--error">{{ bootError }}</div>
  <FirstRunNodeProfile v-else-if="needProfile" @completed="onProfileCompleted" />
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
