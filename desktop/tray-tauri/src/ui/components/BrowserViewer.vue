<script setup>
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { controlAgentBrowser, getAgentBrowserView } from '@desktop-ui/api/node.js'
import UiIcon from '@desktop-ui/components/UiIcon.vue'

const props = defineProps({ agentId: { type: String, default: '' }, workspaceScope: { type: String, default: 'default' }, requestGeneration: { type: Number, default: 0 }, active: { type: Boolean, default: true } })
const emit = defineEmits(['open-connection-settings', 'resource-name'])
const initialized = ref(false)
const data = ref(null), loading = ref(false), navigating = ref(false), error = ref(''), actionError = ref(''), busyNotice = ref(false), configurationNotice = ref(false), selectedPageId = ref('')
const address = ref(''), addressDirty = ref(false), addressInput = ref(null)
let epoch = 0, timer = 0, disposed = false, readRequest = null, queued = false, retryDelay = 0
const pages = computed(() => Array.isArray(data.value?.pages) ? data.value.pages : [])
const screenshotMime = computed(() => ['image/png', 'image/jpeg', 'image/webp'].includes(String(data.value?.mime || '').toLowerCase()) ? String(data.value.mime).toLowerCase() : 'image/png')
const hasPage = computed(() => Boolean(data.value?.page_id && data.value?.url && data.value.url !== 'about:blank'))
const controlsDisabled = computed(() => !props.agentId || navigating.value || configurationNotice.value)
function clearTimer() { if (timer) window.clearTimeout(timer); timer = 0 }
function current(token, agentId) { return !disposed && token === epoch && agentId === props.agentId }
function schedule(token, agentId, delay = 3000) {
  clearTimer()
  if (!props.active || document.visibilityState === 'hidden' || !agentId || disposed || navigating.value) return
  timer = window.setTimeout(() => { timer = 0; void load({ token, agentId }) }, retryDelay || delay)
}
function applyView(result) {
  data.value = result
  selectedPageId.value = result?.page_id || ''
  if (!addressDirty.value) address.value = result?.url === 'about:blank' ? '' : result?.url || ''
  emit('resource-name', hasPage.value ? result?.title || result?.url : '浏览器')
}
async function load({ token = epoch, agentId = props.agentId } = {}) {
  clearTimer()
  if (!agentId || !props.active || document.visibilityState === 'hidden' || disposed || navigating.value) return
  if (readRequest) { queued = true; return }
  loading.value = true
  let delay = 3000
  readRequest = getAgentBrowserView(agentId, selectedPageId.value)
  try {
    const result = await readRequest
    if (!current(token, agentId)) return
    applyView(result)
    busyNotice.value = false; configurationNotice.value = false; error.value = ''; retryDelay = 0
  } catch (e) {
    if (!current(token, agentId)) return
    const code = e?.data?.error?.code || e?.data?.code || ''
    if (code === 'browser_page_not_found' && selectedPageId.value) {
      selectedPageId.value = ''; error.value = ''; busyNotice.value = false; queued = true
      return
    }
    delay = 5000
    busyNotice.value = code === 'browser_busy' || e?.status === 423
    configurationNotice.value = code === 'browser_disabled'
    if (code === 'browser_not_started') { applyView(null); error.value = ''; delay = 30000 }
    else if (busyNotice.value) error.value = ''
    else if (configurationNotice.value) { applyView(null); error.value = ''; delay = 0 }
    else error.value = '暂时无法同步页面，请重试。'
    if (delay) retryDelay = Math.min(retryDelay ? retryDelay * 2 : delay, 30000)
  } finally {
    readRequest = null
    if (current(token, agentId)) { loading.value = false; initialized.value = true }
    if (queued && !navigating.value && props.active && props.agentId && !disposed) {
      queued = false
      void load()
    } else if (current(token, agentId) && delay) schedule(token, agentId, delay)
  }
}
function normalizeAddress(raw) {
  const text = raw.trim()
  if (!text || /\s/.test(text)) throw new Error('请输入有效的网址，例如 example.com')
  const hasScheme = /^[a-z][a-z\d+.-]*:/i.test(text) && !/^[^/:]+:\d+(?:\/|$)/.test(text)
  const url = new URL(hasScheme ? text : `https://${text}`)
  if (!['https:', 'http:'].includes(url.protocol) || !url.hostname || url.username || url.password) throw new Error('仅支持 http 和 https 网址')
  return url.href
}
async function control(command) {
  if (controlsDisabled.value) return
  let url = ''
  if (command === 'navigate') {
    try { url = normalizeAddress(address.value) }
    catch (e) { actionError.value = e?.message?.startsWith('Invalid') ? '请输入有效的网址，例如 example.com' : e.message; return }
  }
  const agentId = props.agentId, token = ++epoch, pageId = command === 'new' ? '' : selectedPageId.value
  navigating.value = true; clearTimer(); queued = false; actionError.value = ''; busyNotice.value = false
  // Finish our screenshot read before submitting a mutation, so polling does
  // not compete with navigation for the Agent browser lock.
  try {
    if (readRequest) await readRequest.catch(() => {})
    if (!current(token, agentId)) return
    const result = await controlAgentBrowser(agentId, command, pageId, url)
    if (!current(token, agentId)) return
    selectedPageId.value = result?.page_id || ''
    addressDirty.value = false
    if (command === 'navigate') address.value = url
    if (command === 'new' || command === 'close') { data.value = null; address.value = '' }
  } catch (e) {
    if (!current(token, agentId)) return
    const code = e?.data?.error?.code || e?.data?.code || ''
    actionError.value = code === 'browser_busy' || e?.status === 423 ? '智能体正在操作浏览器，请稍后再试。' : code === 'browser_page_not_found' ? '这个页面已关闭，请选择其他页面。' : code === 'browser_navigation_timeout' ? '网页加载超时，可以重试或更换网址。' : '未能完成操作，请检查网址和网络后重试。'
  } finally {
    if (current(token, agentId)) {
      navigating.value = false; loading.value = false; void load()
      if (command === 'new') { await nextTick(); addressInput.value?.focus() }
    }
  }
}
function choosePage(id) {
  if (id === selectedPageId.value || navigating.value) return
  selectedPageId.value = id; epoch++; queued = false; addressDirty.value = false; actionError.value = ''
  void load()
}
function restoreAddress() { addressDirty.value = false; address.value = data.value?.url === 'about:blank' ? '' : data.value?.url || '' }
function onVisibility() { if (document.visibilityState === 'hidden') clearTimer(); else if (props.active) void refreshResources() }
function refreshResources() { retryDelay = 0; return load() }
function reset() {
  epoch++; clearTimer(); queued = false; initialized.value = false; data.value = null; selectedPageId.value = ''; error.value = ''; actionError.value = ''; address.value = ''; addressDirty.value = false; busyNotice.value = false; configurationNotice.value = false; navigating.value = false; loading.value = false
  if (props.active && props.agentId) void load()
}
watch(() => [props.agentId, props.workspaceScope], reset)
watch(() => props.requestGeneration, () => { epoch++; clearTimer(); queued = false; navigating.value = false; if (props.active && props.agentId) void refreshResources() })
watch(() => props.active, active => { if (!active) clearTimer(); else if (props.agentId) void refreshResources() }, { immediate: true })
defineExpose({ refreshResources })
document.addEventListener('visibilitychange', onVisibility)
onUnmounted(() => { disposed = true; epoch++; clearTimer(); document.removeEventListener('visibilitychange', onVisibility) })
</script>

<template>
  <main class="browser-viewer" aria-label="浏览器" :aria-busy="navigating">
    <header class="browser-viewer__toolbar">
      <div class="browser-viewer__navigation">
        <button type="button" title="后退" aria-label="后退" :disabled="controlsDisabled || !data?.can_go_back" @click="control('back')"><UiIcon name="arrow-left" :size="16" /></button>
        <button type="button" title="前进" aria-label="前进" :disabled="controlsDisabled || !data?.can_go_forward" @click="control('forward')"><UiIcon name="arrow-left" class="browser-viewer__forward" :size="16" /></button>
        <button type="button" title="刷新网页" aria-label="刷新网页" :disabled="controlsDisabled || !hasPage" @click="control('reload')"><UiIcon name="refresh-cw" :size="16" /></button>
      </div>
      <form class="browser-viewer__address" @submit.prevent="control('navigate')">
        <UiIcon name="globe" :size="15" />
        <input ref="addressInput" v-model="address" aria-label="网址" placeholder="输入网址，按 Enter 打开" autocomplete="off" spellcheck="false" :disabled="controlsDisabled" @input="addressDirty = true" @keydown.esc.stop="restoreAddress">
        <button type="submit" title="打开网页" aria-label="打开网页" :disabled="controlsDisabled || !address.trim()"><UiIcon name="arrow-left" class="browser-viewer__forward" :size="15" /></button>
      </form>
      <button type="button" title="新建页面" aria-label="新建页面" :disabled="controlsDisabled" @click="control('new')"><UiIcon name="plus" :size="17" /></button>
    </header>
    <nav v-if="pages.length" class="browser-viewer__tabs" aria-label="已打开页面">
      <div v-for="page in pages" :key="page.page_id" class="browser-viewer__tab" :class="{ selected: page.page_id === selectedPageId }">
        <button type="button" :aria-current="page.page_id === selectedPageId ? 'page' : undefined" :title="page.url" :disabled="navigating" @click="choosePage(page.page_id)"><UiIcon name="globe" :size="13" /><span>{{ page.title || (page.url === 'about:blank' ? '新页面' : page.url) || '新页面' }}</span></button>
        <button v-if="page.page_id === selectedPageId" type="button" title="关闭页面" aria-label="关闭页面" :disabled="navigating" @click="control('close')"><UiIcon name="close" :size="12" /></button>
      </div>
    </nav>
    <p v-if="actionError || error" class="browser-viewer__notice browser-viewer__notice--error" role="alert">{{ actionError || error }} <button v-if="error" type="button" @click="refreshResources">重试</button></p>
    <p v-else-if="busyNotice" class="browser-viewer__notice" role="status">智能体正在浏览，页面稍后会自动更新。</p>
    <p v-if="navigating" class="browser-viewer__progress" role="status">正在处理…</p>
    <section v-if="hasPage && data?.image_base64" class="browser-viewer__canvas">
      <p>页面预览 · 网页内操作可通过聊天完成</p>
      <img :src="`data:${screenshotMime};base64,${data.image_base64}`" :alt="data.title || '当前网页预览'">
    </section>
    <section v-else class="browser-viewer__empty" :role="configurationNotice || loading ? 'status' : undefined">
      <div class="browser-viewer__empty-icon"><UiIcon name="browser" :size="28" /></div>
      <template v-if="configurationNotice"><h1>启用浏览器，开始浏览</h1><p>在能力设置中启用浏览器后，就可以在这里打开网页。</p><button type="button" aria-label="前往浏览器能力设置" @click="emit('open-connection-settings')">打开设置</button></template>
      <template v-else-if="loading && !initialized"><h1>正在连接浏览器…</h1></template>
      <template v-else><h1>打开一个网页</h1><p>在上方输入网址，或让智能体为你浏览。</p><small>你打开的页面会与当前智能体共享。</small><button type="button" :disabled="controlsDisabled" @click="addressInput?.focus()">输入网址</button></template>
    </section>
  </main>
</template>

<style scoped>
.browser-viewer{height:100%;min-height:0;min-width:0;width:100%;flex:1;display:flex;flex-direction:column;overflow:hidden;background:var(--surface-app,#fff);color:var(--text-primary,#263740)}
.browser-viewer button{display:inline-flex;align-items:center;justify-content:center;flex:none;border:0;border-radius:6px;background:transparent;color:inherit;cursor:pointer;font:inherit}
.browser-viewer button:hover:not(:disabled){background:var(--color-surface-hover,#eef1f3)}.browser-viewer button:disabled{opacity:.35;cursor:default}.browser-viewer button:focus-visible{outline:2px solid #8295a2;outline-offset:2px}
.browser-viewer__toolbar{display:flex;align-items:center;gap:8px;padding:10px 14px;border-bottom:1px solid var(--border-subtle,#e5e9ec);background:var(--color-bg-soft,#f7f8fa)}
.browser-viewer__toolbar button{width:30px;height:30px}.browser-viewer__navigation{display:flex;gap:2px}.browser-viewer__forward{transform:rotate(180deg)}
.browser-viewer__address{display:flex;align-items:center;gap:8px;flex:1;min-width:0;height:34px;border:1px solid var(--border-subtle,#e0e5e8);border-radius:8px;background:var(--surface-app,#fff);padding:0 4px 0 10px;color:var(--text-secondary,#6d7b86)}
.browser-viewer__address:focus-within{border-color:#a8b5bf;box-shadow:0 0 0 2px #7c8c9b12}.browser-viewer__address input{min-width:0;flex:1;width:100%;border:0;outline:none;background:transparent;color:var(--text-primary,#263740);font:inherit;font-size:12px}.browser-viewer__address input::placeholder{color:var(--text-secondary,#85919b)}
.browser-viewer__tabs{display:flex;gap:3px;flex:none;overflow-x:auto;padding:5px 14px;border-bottom:1px solid var(--border-subtle,#e5e9ec)}.browser-viewer__tab{display:flex;align-items:center;min-width:0;max-width:220px;flex-shrink:0;border-radius:6px;color:var(--text-secondary,#6d7b86)}.browser-viewer__tab.selected{background:var(--color-surface-hover,#eef1f3);color:var(--text-primary,#263740)}.browser-viewer__tab>button:first-child{min-width:0;padding:7px 9px;gap:6px;font-size:12px}.browser-viewer__tab span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.browser-viewer__tab>button+button{width:24px;height:24px;margin-right:3px}
.browser-viewer__notice{display:flex;align-items:center;gap:8px;margin:0;padding:8px 18px;background:var(--color-bg-soft,#f7f8fa);color:var(--text-secondary,#6d7b86);font-size:12px}.browser-viewer__notice--error{color:#a14343}.browser-viewer__notice button{text-decoration:underline;padding:2px 5px}.browser-viewer__progress{margin:0;padding:6px 18px;font-size:11px;color:var(--text-secondary,#6d7b86)}
.browser-viewer__canvas{min-height:0;flex:1;overflow:auto;display:flex;flex-direction:column;align-items:center;padding:12px 16px 76px;background:var(--color-bg-soft,#f7f8fa)}.browser-viewer__canvas img{display:block;width:100%;height:auto;flex-shrink:0;object-fit:contain;background:#fff;border:1px solid var(--border-subtle,#e5e9ec);border-radius:3px}.browser-viewer__canvas p{align-self:flex-start;margin:0 0 8px;font-size:11px;color:var(--text-secondary,#7e8a94);text-align:center}
.browser-viewer__empty{flex:1;min-height:180px;overflow:auto;display:flex;flex-direction:column;align-items:center;justify-content:center;text-align:center;padding:28px 20px 90px;color:var(--text-secondary,#7e8a94)}.browser-viewer__empty-icon{display:grid;place-items:center;width:56px;height:56px;margin-bottom:16px;border-radius:15px;background:var(--color-bg-soft,#f3f5f7);color:#8798a5}.browser-viewer__empty h1{margin:0;font-size:18px;font-weight:600;color:var(--text-primary,#344651)}.browser-viewer__empty p{margin:10px 0 5px;font-size:13px}.browser-viewer__empty small{font-size:12px}.browser-viewer__empty>button{margin-top:20px;gap:8px;padding:8px 13px;background:var(--color-bg-soft,#f3f5f7);font-size:12px;color:var(--text-primary,#344651)}
@media(max-width:640px){.browser-viewer__toolbar{gap:4px;padding:8px}.browser-viewer__navigation{gap:0}.browser-viewer__toolbar button{width:27px}.browser-viewer__canvas{padding:8px 8px 76px}}
</style>
