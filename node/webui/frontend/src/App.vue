<script setup>
import { onMounted, onUnmounted, ref } from "vue";
import { RouterView } from "vue-router";
import ImageLightbox from "./components/ImageLightbox.vue";
import FirstRunNodeProfile from "./views/FirstRunNodeProfile.vue";
import * as api from "./api/node.js";
import TransferStatusBar from "./components/TransferStatusBar.vue";

defineOptions({ name: "AppRoot" });

const bootReady = ref(false);
const needProfile = ref(false);
const bootError = ref("");
const bootRetrying = ref(false);
const bootDetailsOpen = ref(false);
const bootCopyStatus = ref("");
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
      }
    } else {
      needProfile.value = incomplete;
    }
    bootError.value = "";
    bootDetailsOpen.value = false;
    bootCopyStatus.value = "";
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

async function retryBootstrap() {
  if (bootRetrying.value) return;
  bootRetrying.value = true;
  bootError.value = "";
  bootDetailsOpen.value = false;
  bootReady.value = false;
  try {
    await refreshOnboarding();
  } finally {
    bootRetrying.value = false;
  }
}

async function copyBootError() {
  const text = String(bootError.value || "无法完成启动").slice(0, 1000);
  try {
    await navigator.clipboard.writeText(text);
    bootCopyStatus.value = "已复制";
  } catch {
    bootCopyStatus.value = "复制失败，请手动选择错误详情";
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
    <section v-else-if="bootError" key="error" class="app-boot app-boot--error" aria-labelledby="boot-error-title">
      <div class="app-boot__error-card">
        <p class="app-boot__eyebrow">DAgents Node</p>
        <h1 id="boot-error-title">无法完成启动</h1>
        <p class="app-boot__lead">暂时无法连接到当前 Node。请检查 Node 是否正在运行，然后重试。</p>
        <div class="app-boot__actions">
          <button type="button" class="btn btn--primary" :disabled="bootRetrying" @click="retryBootstrap">
            {{ bootRetrying ? "正在重试…" : "重试" }}
          </button>
          <button type="button" class="btn btn--ghost" :aria-expanded="bootDetailsOpen" @click="bootDetailsOpen = !bootDetailsOpen">
            {{ bootDetailsOpen ? "收起错误详情" : "查看错误详情" }}
          </button>
        </div>
        <div v-if="bootDetailsOpen" class="app-boot__details">
          <code>{{ bootError }}</code>
          <button type="button" class="btn btn--ghost btn--sm" @click="copyBootError">复制</button>
          <span v-if="bootCopyStatus" class="app-boot__copy-status" role="status">{{ bootCopyStatus }}</span>
        </div>
        <div class="app-boot__help" aria-label="连接自查">
          <strong>可以先自查</strong>
          <ol>
            <li>确认 dagents-node 进程仍在运行。</li>
            <li>确认本机地址和端口没有被防火墙拦截。</li>
            <li>修复后点击“重试”；首配未完成时仍会回到首配页面。</li>
          </ol>
        </div>
      </div>
    </section>
    <div v-else key="app">
      <RouterView v-slot="{ Component }">
        <KeepAlive include="ChatLayout">
          <component :is="Component" />
        </KeepAlive>
      </RouterView>
      <TransferStatusBar />
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
  background: var(--app-background);
}
.app-boot--error {
  color: var(--color-text);
  padding: var(--space-6);
  text-align: center;
}
.app-boot__error-card { width: min(520px, 100%); padding: 32px; border: 1px solid var(--color-border); border-radius: 10px; background: var(--color-surface); box-shadow: var(--shadow-elevated, 0 12px 36px rgb(0 0 0 / 8%)); }
.app-boot__eyebrow { margin: 0 0 8px; color: var(--color-text-subtle); font-size: 12px; }
.app-boot__error-card h1 { margin: 0; font-size: 22px; }
.app-boot__lead, .app-boot__help { color: var(--color-text-subtle); line-height: 1.55; }
.app-boot__actions { display: flex; justify-content: center; gap: 10px; margin-top: 24px; }
.app-boot__details { display: flex; align-items: flex-start; gap: 8px; margin-top: 20px; padding: 12px; text-align: left; border-radius: 6px; background: var(--color-surface-alt); }
.app-boot__details code { flex: 1; overflow-wrap: anywhere; color: var(--color-text-subtle); font-size: 12px; }
.app-boot__copy-status { color: var(--color-success); font-size: 12px; }
.app-boot__help { margin: 20px 0 0; font-size: 12px; text-align: left; }
.app-boot__help ol { margin: 8px 0 0; padding-left: 20px; }

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
