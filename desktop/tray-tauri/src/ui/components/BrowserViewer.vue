<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { getAgentBrowserView } from '@desktop-ui/api/node.js'
import UiIcon from '@desktop-ui/components/UiIcon.vue'

const props = defineProps({ agentId: { type: String, default: '' }, workspaceScope: { type: String, default: 'default' }, requestGeneration: { type: Number, default: 0 }, active: { type: Boolean, default: true } })
const emit = defineEmits(['open-connection-settings', 'resource-name'])
const data = ref(null), loading = ref(false), error = ref(''), busyNotice = ref(false), notStartedNotice = ref(false), configurationNotice = ref(false), showConnectionSettings = ref(false), selectedPageId = ref('')
let epoch = 0, timer = 0, disposed = false, inFlight = false, queued = false, retryDelay = 0
const screenshotMime = computed(() => ['image/png', 'image/jpeg', 'image/webp'].includes(String(data.value?.mime || '').toLowerCase()) ? String(data.value.mime).toLowerCase() : 'image/png')
function clearTimer() { if (timer) window.clearTimeout(timer); timer = 0 }
function current(token, agentId) { return !disposed && token === epoch && agentId === props.agentId }
function schedule(token, agentId, delay = 3000) { clearTimer(); if (!props.active || document.visibilityState === 'hidden' || !agentId || disposed) return; timer = window.setTimeout(() => { timer = 0; void load({ poll: true, token, agentId }) }, retryDelay || delay) }
async function load({ poll = false, token = epoch, agentId = props.agentId } = {}) {
  clearTimer()
  if (!agentId || !props.active || document.visibilityState === 'hidden' || disposed) return
  if (inFlight) { queued = true; return }
  if (!poll) { token = epoch; agentId = props.agentId; if (!data.value) loading.value = true; busyNotice.value = false; notStartedNotice.value = false; configurationNotice.value = false; showConnectionSettings.value = false }
  inFlight = true
  let delay = 3000
  try {
    const result = await getAgentBrowserView(agentId, selectedPageId.value)
    if (!current(token, agentId)) return
    data.value = result
    busyNotice.value = false
    notStartedNotice.value = false
    configurationNotice.value = false
    emit('resource-name', result?.title || result?.url || '浏览器')
    const pages = Array.isArray(result?.pages) ? result.pages : []
    if (!pages.some(page => page.page_id === selectedPageId.value)) selectedPageId.value = result?.page_id || pages.at(-1)?.page_id || ''
    error.value = ''
    retryDelay = 0
  } catch (e) {
    if (!current(token, agentId)) return
    const code = e?.data?.error?.code || e?.data?.code || ''
    if (code === 'browser_page_not_found' && selectedPageId.value) {
      selectedPageId.value = ''
      error.value = ''
      busyNotice.value = false
      queued = true
      delay = 0
      return
    }
    delay = 5000
    if (code === 'browser_busy' || e?.status === 423) {
      busyNotice.value = true
      error.value = ''
    } else {
      if (code === 'browser_not_started' || e?.status === 409) delay = 30000
      else if (code === 'browser_disabled') delay = 0
      busyNotice.value = false
      notStartedNotice.value = code === 'browser_not_started' || e?.status === 409
      configurationNotice.value = code === 'browser_disabled'
      error.value = notStartedNotice.value ? '此 Agent 的浏览器尚未启动；请先在聊天工具中调用浏览器。' : configurationNotice.value ? '此 Agent 尚未启用浏览器。请前往能力设置启用后重试。' : e?.message || '无法读取浏览器页面'
    }
    showConnectionSettings.value = e?.status === 503 || code === 'browser_disabled'
  } finally {
    inFlight = false
    if (queued && props.active && document.visibilityState !== 'hidden' && props.agentId) {
      queued = false
      void load({ poll: true, token: epoch, agentId: props.agentId })
    } else if (current(token, agentId)) {
      loading.value = false
      if (delay) {
        if (error.value || busyNotice.value || notStartedNotice.value) retryDelay = Math.min(retryDelay ? retryDelay * 2 : delay, 30000)
        schedule(token, agentId, delay)
      }
    }
  }
}
function choosePage(id) { if (id === selectedPageId.value) return; selectedPageId.value = id; epoch++; queued = false; void load() }
function onVisibility() { if (document.visibilityState === 'hidden') clearTimer(); else if (props.active) void refreshResources('return') }
function refreshResources(reason = 'manual') { if (reason === 'tool' || reason === 'return' || reason === 'manual') retryDelay = 0; return load() }
watch(() => props.agentId, () => { epoch++; clearTimer(); data.value = null; error.value = ''; busyNotice.value = false; notStartedNotice.value = false; configurationNotice.value = false; showConnectionSettings.value = false; selectedPageId.value = ''; queued = false; if (props.active && props.agentId) void load() })
watch(() => props.workspaceScope, () => { epoch++; clearTimer(); queued = false; data.value = null; selectedPageId.value = ''; error.value = ''; if (props.active && props.agentId) void refreshResources('return') })
watch(() => props.requestGeneration, () => { epoch++; clearTimer(); queued = false; if (props.active && props.agentId) void refreshResources('return') })
watch(() => props.active, active => { if (!active) clearTimer(); else if (props.agentId) void refreshResources('return') }, { immediate: true })
defineExpose({ refreshResources })
if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisibility)
onUnmounted(() => { disposed = true; epoch++; clearTimer(); document.removeEventListener('visibilitychange', onVisibility) })
</script>

<template>
  <main class="browser-viewer" aria-label="浏览器查看器">
    <header class="browser-viewer__header"><div><h1>{{ data?.title || '浏览器' }}</h1><p class="browser-viewer__url" :title="data?.url">{{ data?.url || '只读查看 Agent 已打开的页面' }}</p></div><button class="browser-viewer__refresh" type="button" title="刷新浏览器画面" aria-label="刷新浏览器画面" :disabled="loading || !agentId" @click="load"><UiIcon name="refresh-cw" :size="16" /></button></header>
    <nav v-if="data?.pages?.length > 1" class="browser-viewer__tabs" aria-label="已打开页面"><button v-for="page in data.pages" :key="page.page_id" type="button" :class="{ selected: page.page_id === selectedPageId }" :title="page.url" @click="choosePage(page.page_id)">{{ page.title || page.url || page.page_id }}</button></nav>
    <p v-if="busyNotice" class="browser-viewer__busy" role="status">Agent 正在操作浏览器，画面暂时未刷新；稍后将自动重试。</p>
    <p v-if="error" class="browser-viewer__error" :class="{ 'browser-viewer__error--neutral': notStartedNotice || configurationNotice }" :role="notStartedNotice || configurationNotice ? 'status' : 'alert'">{{ error }} <button v-if="showConnectionSettings" class="browser-viewer__settings" type="button" title="前往浏览器能力设置" aria-label="前往浏览器能力设置" @click="emit('open-connection-settings')"><UiIcon name="settings" :size="16" /></button></p>
    <section v-if="data?.image_base64" class="browser-viewer__canvas" aria-live="polite"><img :src="`data:${screenshotMime};base64,${data.image_base64}`" :alt="data.title || '当前浏览器页面截图'"><p>只读页面画面 · {{ screenshotMime }}</p></section>
    <section v-else-if="loading" class="browser-viewer__empty" role="status">正在连接 Agent 浏览器…</section>
    <section v-else class="browser-viewer__empty"><strong>没有可查看的浏览器页面</strong><p>请先在此 Agent 的聊天工具中启动浏览器并打开页面。</p></section>
  </main>
</template>

<style scoped>
.browser-viewer{height:100%;min-height:0;min-width:0;width:100%;flex:1;display:flex;flex-direction:column;gap:12px;padding:22px 26px;overflow:auto;background:#f7f9fa;color:#263740}.browser-viewer__header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;border-bottom:1px solid #dce5eb;padding-bottom:14px}.browser-viewer__header small{color:#627581;letter-spacing:.12em}.browser-viewer h1{font-size:19px;margin:4px 0}.browser-viewer__url{margin:4px 0;color:#627581;overflow-wrap:anywhere;font-size:12px}.browser-viewer button{color:#263740;background:#fff;border:1px solid #d1d9de;border-radius:6px;padding:8px 11px;cursor:pointer}.browser-viewer button:disabled{opacity:.5}.browser-viewer__tabs{display:flex;gap:6px;overflow:auto}.browser-viewer__tabs button{max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.browser-viewer__tabs button.selected{border-color:#667985;background:#eef3f5}.browser-viewer__canvas{min-height:0;flex:1;display:flex;flex-direction:column;align-items:center;gap:5px}.browser-viewer__canvas img{max-width:100%;max-height:calc(100% - 25px);object-fit:contain;border:1px solid #d1d9de;background:#fff;box-shadow:0 2px 12px #2637401a}.browser-viewer__canvas p{color:#627581;font-size:11px;margin:2px}.browser-viewer__empty{flex:1;min-height:180px;display:flex;flex-direction:column;justify-content:center;align-items:center;text-align:center;color:#627581}.browser-viewer__empty strong{color:#263740}.browser-viewer__error{padding:18px;border:1px solid #e2c7c8;border-radius:8px;background:#fff7f7;color:#a4262c}.browser-viewer__error button{margin-left:8px}.browser-viewer__error--neutral{border-color:#dce5eb;background:#eef3f5;color:#627581}.browser-viewer__busy{margin:0;padding:8px 11px;border:1px solid #dbe3e8;border-radius:6px;background:#eef3f5;color:#52616a;font-size:12px}
.browser-viewer__refresh,.browser-viewer__settings{display:grid;place-items:center;flex:none;width:34px;height:34px;padding:0!important}.browser-viewer__settings{display:inline-grid;vertical-align:middle;margin-left:8px}
</style>
