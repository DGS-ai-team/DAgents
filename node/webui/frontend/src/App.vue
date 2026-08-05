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
let refreshSeq = 0;
let mountedAt = 0;

/**
 * soft=true：窗口聚焦/pageshow 时的重检。
 * 只允许「进入」首配，禁止仅凭一次 bootstrap 读数就退出首配
 * （避免与首配页自身请求竞态，或短暂错误响应把用户闪进对话页）。
 * 退出首配只走 @completed。
 */
async function refreshOnboarding({ soft = false } = {}) {
  const seq = ++refreshSeq;
  try {
    const boot = await api.getUIBootstrap();
    if (seq !== refreshSeq) return;
    const incomplete = boot?.onboarding?.node_profile_completed === false;
    if (soft) {
      if (incomplete) {
        needProfile.value = true;
      } else if (needProfile.value) {
        // 已在首配页时允许退出（例如在其它窗口/标签页完成首配）。
        needProfile.value = false;
      }
    } else {
      needProfile.value = incomplete;
    }
    bootError.value = "";
  } catch (e) {
    if (seq !== refreshSeq) return;
    // 启动竞态或短暂断连：不要把失败当成「已完成」而放行主界面。
    // soft 刷新时保留当前 needProfile，仅在尚未就绪时展示错误。
    if (!soft || !bootReady.value) {
      bootError.value = e.message || "无法连接 Node";
    }
  } finally {
    if (seq === refreshSeq) {
      bootReady.value = true;
    }
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
  // 首屏加载时 pageshow/focus 常与 onMounted 并发，跳过避免双次 bootstrap 竞态。
  if (mountedAt && Date.now() - mountedAt < 500) return;
  void refreshOnboarding({ soft: true });
}

onMounted(() => {
  mountedAt = Date.now();
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
  <Transition name="app-shell" mode="out-in">
    <div v-if="!bootReady" key="boot" class="app-boot">加载中…</div>
    <FirstRunNodeProfile
      v-else-if="needProfile"
      key="onboarding"
      @completed="onProfileCompleted"
    />
    <div v-else-if="bootError" key="error" class="app-boot app-boot--error">{{ bootError }}</div>
    <div v-else key="app">
      <RouterView v-slot="{ Component }">
        <KeepAlive include="ChatLayout">
          <component :is="Component" />
        </KeepAlive>
      </RouterView>
      <ImageLightbox />
    </div>
  </Transition>
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

.app-shell-enter-active,
.app-shell-leave-active {
  transition: opacity 0.35s ease;
}
.app-shell-enter-from,
.app-shell-leave-to {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {
  .app-shell-enter-active,
  .app-shell-leave-active {
    transition: none;
  }
}
</style>
