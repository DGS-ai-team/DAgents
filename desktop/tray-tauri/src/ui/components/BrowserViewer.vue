<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { getAgentBrowserView, postAgentBrowserAction } from '@desktop-ui/api/node.js'
import UiIcon from '@desktop-ui/components/UiIcon.vue'

const props = defineProps({ agentId: { type: String, default: '' }, workspaceScope: { type: String, default: 'default' }, requestGeneration: { type: Number, default: 0 }, active: { type: Boolean, default: true } })
const emit = defineEmits(['open-connection-settings', 'resource-name'])
const data = ref(null), loading = ref(false), actionBusy = ref(false), error = ref(''), busyNotice = ref(false), notStartedNotice = ref(false), configurationNotice = ref(false), showConnectionSettings = ref(false), selectedPageId = ref(''), addressDraft = ref(''), addressInput = ref(null)
const screenshotMime = computed(() => ['image/png', 'image/jpeg', 'image/webp'].includes(String(data.value?.mime || '').toLowerCase()) ? String(data.value.mime).toLowerCase() : 'image/png')
const pages = computed(() => Array.isArray(data.value?.pages) ? data.value.pages : [])
const canClosePage = computed(() => pages.value.length > 1)
let epoch = 0, timer = 0, disposed = false, inFlight = false, queued = false, retryDelay = 0

function clearTimer() { if (timer) window.clearTimeout(timer); timer = 0 }
function current(token, agentId) { return !disposed && token === epoch && agentId === props.agentId }
function schedule(token, agentId, delay = 3000) { clearTimer(); if (!props.active || document.visibilityState === 'hidden' || !agentId || disposed) return; timer = window.setTimeout(() => { timer = 0; void load({ poll: true, token, agentId }) }, retryDelay || delay) }
function tabName(page) {
  if (page?.title?.trim()) return page.title.trim()
  try { return page?.url ? new URL(page.url).host : '新标签页' } catch { return page?.url || '新标签页' }
}
function setPageResult(result) {
  data.value = result
  selectedPageId.value = result?.page_id || ''
  if (document.activeElement !== addressInput.value) addressDraft.value = result?.url || ''
  emit('resource-name', result?.title || result?.url || '浏览器')
}
async function load({ poll = false, token = epoch, agentId = props.agentId } = {}) {
  clearTimer()
  if (!agentId || !props.active || document.visibilityState === 'hidden' || disposed) return
  if (inFlight) { queued = true; return }
  if (!poll) {
    token = epoch; agentId = props.agentId
    if (!data.value) loading.value = true
    busyNotice.value = false; notStartedNotice.value = false; configurationNotice.value = false; showConnectionSettings.value = false
  }
  inFlight = true
  let delay = 3000
  try {
    const result = await getAgentBrowserView(agentId, selectedPageId.value)
    if (!current(token, agentId)) return
    setPageResult(result)
    busyNotice.value = false; notStartedNotice.value = false; configurationNotice.value = false
    error.value = ''; retryDelay = 0
  } catch (e) {
    if (!current(token, agentId)) return
    const code = e?.data?.error?.code || e?.data?.code || ''
    if (code === 'browser_page_not_found' && selectedPageId.value) {
      selectedPageId.value = ''
      error.value = ''; busyNotice.value = false; queued = true; delay = 0
      return
    }
    delay = 5000
    if (code === 'browser_busy' || e?.status === 423) {
      busyNotice.value = true; error.value = ''
    } else {
      if (code === 'browser_not_started' || e?.status === 409) delay = 30000
      else if (code === 'browser_disabled') delay = 0
      busyNotice.value = false
      notStartedNotice.value = code === 'browser_not_started' || e?.status === 409
      configurationNotice.value = code === 'browser_disabled'
      error.value = notStartedNotice.value ? '浏览器会话尚未启动。' : configurationNotice.value ? '此 Agent 尚未启用浏览器。' : e?.message || '无法读取浏览器页面'
      if (notStartedNotice.value) showConnectionSettings.value = false
    }
    showConnectionSettings.value = (e?.status === 503 || code === 'browser_disabled')
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
function choosePage(page) {
  if (!page?.page_id || page.page_id === selectedPageId.value) return
  selectedPageId.value = page.page_id
  addressDraft.value = page.url || ''
  epoch++; queued = false
  void load()
}
function normalizeAddress(value) {
  const raw = String(value || '').trim()
  if (!raw) return ''
  return /^[a-z][a-z\d+.-]*:\/\//i.test(raw) ? raw : `https://${raw}`
}
function updateAddress(event) { addressDraft.value = event?.target?.value || '' }
async function runAction(action, extra = {}) {
  if (!props.agentId || actionBusy.value) return
  const agentId = props.agentId, token = epoch
  actionBusy.value = true; error.value = ''; busyNotice.value = false
  clearTimer()
  try {
    const result = await postAgentBrowserAction(agentId, { action, page_id: selectedPageId.value || undefined, ...extra })
    if (!current(token, agentId)) return
    setPageResult(result)
    if (action === 'navigate') addressDraft.value = result?.url || extra.url || ''
    notStartedNotice.value = false; configurationNotice.value = false; showConnectionSettings.value = false
    retryDelay = 0
  } catch (e) {
    if (!current(token, agentId)) return
    const code = e?.data?.error?.code || e?.data?.code || ''
    if (code === 'browser_busy' || e?.status === 423) {
      busyNotice.value = true; error.value = ''
    } else {
      error.value = e?.data?.error?.message || e?.message || '浏览器操作失败'
      notStartedNotice.value = code === 'browser_not_started' || e?.status === 409
      configurationNotice.value = code === 'browser_disabled'
      showConnectionSettings.value = configurationNotice.value
    }
  } finally {
    actionBusy.value = false
    if (current(token, agentId) && props.active) schedule(epoch, agentId, 1200)
  }
}
function submitAddress(event) {
  // A scheduled screenshot refresh can race with typing and leave the Vue
  // draft behind the focused input's visible value. Read the submitted field
  // itself so Enter always navigates to what the user can see.
  const form = event?.currentTarget?.closest?.('form') ?? event?.currentTarget
  const targetInput = event?.target?.tagName === 'INPUT' ? event.target : null
  const submitted = targetInput?.value ?? form?.querySelector?.('input')?.value ?? addressDraft.value
  const url = normalizeAddress(submitted)
  if (!url) return
  addressDraft.value = url
  void runAction('navigate', { url })
}
function onAddressFocus(event) { event.target.select() }
function onVisibility() { if (document.visibilityState === 'hidden') clearTimer(); else if (props.active) void refreshResources('return') }
function refreshResources(reason = 'manual') { if (reason === 'tool' || reason === 'return' || reason === 'manual') retryDelay = 0; return load() }
watch(() => props.agentId, () => { epoch++; clearTimer(); data.value = null; addressDraft.value = ''; error.value = ''; busyNotice.value = false; notStartedNotice.value = false; configurationNotice.value = false; showConnectionSettings.value = false; selectedPageId.value = ''; queued = false; if (props.active && props.agentId) void load() })
watch(() => props.workspaceScope, () => { epoch++; clearTimer(); queued = false; data.value = null; selectedPageId.value = ''; error.value = ''; if (props.active && props.agentId) void refreshResources('return') })
watch(() => props.requestGeneration, () => { epoch++; clearTimer(); queued = false; if (props.active && props.agentId) void refreshResources('return') })
watch(() => props.active, active => { if (!active) clearTimer(); else if (props.agentId) void refreshResources('return') }, { immediate: true })
defineExpose({ refreshResources })
if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisibility)
onUnmounted(() => { disposed = true; epoch++; clearTimer(); document.removeEventListener('visibilitychange', onVisibility) })
</script>

<template>
  <main class="browser-viewer" aria-label="浏览器">
    <header class="browser-toolbar" aria-label="浏览器工具栏">
      <div class="browser-toolbar__navigation">
        <button class="browser-icon-button" type="button" title="后退" aria-label="后退" :disabled="!data || loading || actionBusy" @click="runAction('back')"><UiIcon name="arrow-left" :size="16" /></button>
        <button class="browser-icon-button browser-icon-button--forward" type="button" title="前进" aria-label="前进" :disabled="!data || loading || actionBusy" @click="runAction('forward')"><UiIcon name="chevron-right" :size="16" /></button>
        <button class="browser-icon-button" type="button" title="重新加载网页" aria-label="重新加载网页" :disabled="loading || actionBusy || !agentId" @click="data ? runAction('reload') : load()"><UiIcon name="refresh-cw" :size="15" /></button>
      </div>
      <form class="browser-address" role="search" @submit.prevent="submitAddress">
        <UiIcon name="globe" :size="15" aria-hidden="true" />
        <input ref="addressInput" :value="addressDraft" type="text" inputmode="url" autocomplete="url" spellcheck="false" aria-label="网址" placeholder="输入网址并按 Enter" :disabled="actionBusy || !data" @input="updateAddress" @focus="onAddressFocus" @keydown.enter.prevent="submitAddress">
        <button v-if="addressDraft && addressDraft !== data?.url" class="browser-address__go" type="button" title="访问网址" aria-label="访问网址" :disabled="actionBusy" @click="submitAddress"><UiIcon name="arrow-right" :size="14" /></button>
        <button v-if="addressDraft" class="browser-address__clear" type="button" title="清除网址" aria-label="清除网址" @click="addressDraft = ''"><UiIcon name="close" :size="13" /></button>
      </form>
    </header>

    <nav v-if="pages.length || data" class="browser-tabs" role="tablist" aria-label="浏览器标签页">
      <div v-for="page in pages" :key="page.page_id" class="browser-tab" :class="{ 'is-active': page.page_id === selectedPageId }" role="tab" :aria-selected="page.page_id === selectedPageId" :tabindex="page.page_id === selectedPageId ? 0 : -1" :title="page.url || tabName(page)" @click="choosePage(page)" @keydown.enter.prevent="choosePage(page)" @keydown.space.prevent="choosePage(page)">
        <span class="browser-tab__favicon"><UiIcon name="globe" :size="12" /></span>
        <span class="browser-tab__title">{{ tabName(page) }}</span>
        <span v-if="page.page_id === selectedPageId && busyNotice" class="browser-tab__loading" aria-label="页面更新中"></span>
        <span v-else-if="page.page_id === selectedPageId && actionBusy" class="browser-tab__loading" aria-label="页面加载中"></span>
        <button v-else class="browser-tab__close" type="button" :aria-label="`关闭 ${tabName(page)}`" :title="`关闭 ${tabName(page)}`" :disabled="!canClosePage || actionBusy" @click.stop="runAction('close_tab', { page_id: page.page_id })"><UiIcon name="close" :size="12" /></button>
      </div>
      <button class="browser-icon-button browser-tabs__new" type="button" title="新建标签页" aria-label="新建标签页" :disabled="actionBusy || !agentId || configurationNotice" @click="data ? runAction('new_tab') : runAction('start')"><UiIcon name="plus" :size="16" /></button>
    </nav>

    <p v-if="busyNotice" class="browser-notice" role="status">Agent 正在操作浏览器，完成后会自动更新。</p>
    <p v-if="error && !notStartedNotice && !configurationNotice" class="browser-error" role="alert" :title="error">{{ error }}</p>

    <section v-if="data?.image_base64" class="browser-canvas" aria-live="polite">
      <img :src="`data:${screenshotMime};base64,${data.image_base64}`" :alt="data.title || '当前浏览器页面'">
    </section>
    <section v-else-if="loading" class="browser-empty" role="status"><span class="browser-empty__spinner"></span><span>正在连接浏览器</span></section>
    <section v-else-if="notStartedNotice" class="browser-empty" role="status">
      <span class="browser-empty__icon"><UiIcon name="globe" :size="21" /></span>
      <strong>打开浏览器</strong>
      <p>为此 Agent 启动一个独立的浏览器会话。</p>
      <button class="browser-primary-action" type="button" :disabled="actionBusy" @click="runAction('start')">{{ actionBusy ? '正在启动…' : '启动浏览器' }}</button>
    </section>
    <section v-else-if="configurationNotice" class="browser-empty" role="status">
      <span class="browser-empty__icon"><UiIcon name="globe" :size="21" /></span>
      <strong>浏览器尚未启用</strong>
      <p>请在 Node 功能设置中启用浏览器后重试。</p>
      <button v-if="showConnectionSettings" class="browser-primary-action" type="button" @click="emit('open-connection-settings')">打开设置</button>
    </section>
    <section v-else class="browser-empty">
      <span class="browser-empty__icon"><UiIcon name="globe" :size="21" /></span>
      <strong>尚无打开的网页</strong>
      <p>在上方输入网址，或让 Agent 使用浏览器工具打开页面。</p>
    </section>
  </main>
</template>

<style scoped>
.browser-viewer{height:100%;min-height:0;min-width:0;width:100%;flex:1;display:flex;flex-direction:column;overflow:hidden;background:#f4f6f6;color:#263438}
.browser-toolbar{display:flex;align-items:center;gap:8px;flex:none;height:43px;padding:0 12px;border-bottom:1px solid #dce2e3;background:#fff}
.browser-toolbar__navigation{display:flex;align-items:center;gap:3px;flex:none}
.browser-icon-button{display:grid;flex:none;width:30px;height:30px;place-items:center;border:0;border-radius:6px;background:transparent;color:#485458;cursor:pointer;transition:background .14s ease,color .14s ease}
.browser-icon-button:hover:not(:disabled){background:#eef1f1;color:#1d272a}.browser-icon-button:disabled{opacity:.38;cursor:default}
.browser-icon-button--forward :deep(svg){transform:scaleX(-1)}
.browser-address{display:flex;align-items:center;gap:8px;min-width:0;max-width:1180px;height:30px;flex:1;margin:0 auto;padding:0 9px;border:1px solid #dfe4e4;border-radius:7px;background:#f7f8f8;color:#718083;transition:border-color .15s,background .15s}
.browser-address:focus-within{border-color:#8b9a9e;background:#fff}.browser-address :deep(svg){flex:none}
.browser-address input{width:100%;min-width:0;height:100%;border:0;outline:0;background:transparent;color:#283639;font:inherit;font-size:12px}
.browser-address input:disabled{color:#879194}.browser-address input::placeholder{color:#98a2a4}
.browser-address__clear{display:grid;flex:none;width:20px;height:20px;place-items:center;border:0;border-radius:4px;background:transparent;color:#758184;cursor:pointer}.browser-address__clear:hover{background:#e8ecec}
.browser-address__go{display:grid;flex:none;width:22px;height:22px;place-items:center;border:0;border-radius:5px;background:#e8eeee;color:#45565a;cursor:pointer}.browser-address__go:hover:not(:disabled){background:#dce5e5;color:#243235}.browser-address__go:disabled{opacity:.45;cursor:default}
.browser-tabs{display:flex;align-items:flex-end;gap:4px;flex:none;min-width:0;height:39px;padding:5px 10px 0;border-bottom:1px solid #dce2e3;background:#f7f8f8;overflow:auto;scrollbar-width:thin}
.browser-tab{display:flex;align-items:center;gap:7px;min-width:116px;max-width:250px;height:33px;flex:none;padding:0 8px;border:1px solid transparent;border-bottom:0;border-radius:7px 7px 0 0;background:transparent;color:#687477;text-align:left;cursor:pointer}
.browser-tab:hover{background:#eef1f1;color:#354144}.browser-tab.is-active{position:relative;border-color:#dce2e3;background:#fff;color:#202b2e}.browser-tab.is-active::after{position:absolute;right:0;bottom:-1px;left:0;height:1px;background:#fff;content:""}
.browser-tab__favicon{display:grid;flex:none;width:16px;height:16px;place-items:center;color:#7c888a}.browser-tab__title{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px}
.browser-tab__close{display:grid;flex:none;width:20px;height:20px;place-items:center;padding:0;border:0;border-radius:4px;background:transparent;color:#768184;cursor:pointer}.browser-tab__close:hover:not(:disabled){background:#e8ecec;color:#253033}.browser-tab__close:disabled{opacity:.35;cursor:default}
.browser-tab__loading{width:11px;height:11px;flex:none;border:1.5px solid #ced7d8;border-top-color:#55686d;border-radius:50%;animation:browser-spin .7s linear infinite}
.browser-tabs__new{width:28px;height:28px;margin:0 0 2px 1px}
.browser-canvas{position:relative;display:flex;flex:1;min-height:0;min-width:0;align-items:flex-start;justify-content:center;overflow:auto;background:#e7ebeb}
.browser-canvas img{display:block;width:100%;height:auto;min-height:100%;object-fit:contain;object-position:top center;background:#fff;box-shadow:0 1px 5px #26343814}
.browser-empty{display:flex;flex:1;min-height:220px;flex-direction:column;align-items:center;justify-content:center;gap:8px;padding:24px;text-align:center;color:#748184}.browser-empty strong{color:#2e3a3d;font-size:14px;font-weight:550}.browser-empty p{max-width:330px;margin:0;color:#7c888b;font-size:12px;line-height:1.6}
.browser-empty__icon{display:grid;width:42px;height:42px;place-items:center;margin-bottom:2px;border-radius:12px;background:#e9eeee;color:#59696c}.browser-empty__spinner{width:19px;height:19px;border:2px solid #d4dddd;border-top-color:#52666a;border-radius:50%;animation:browser-spin .7s linear infinite}
.browser-primary-action{height:31px;margin-top:5px;padding:0 13px;border:1px solid #cbd4d5;border-radius:6px;background:#fff;color:#344144;font-size:12px;cursor:pointer}.browser-primary-action:hover:not(:disabled){background:#f1f4f4}.browser-primary-action:disabled{opacity:.55;cursor:default}
.browser-notice,.browser-error{flex:none;margin:0;padding:6px 13px;font-size:11px}.browser-notice{border-bottom:1px solid #e5e8e8;background:#fafbfb;color:#657276}.browser-error{border-bottom:1px solid #ead6d6;background:#fff9f9;color:#a44343;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
@keyframes browser-spin{to{transform:rotate(360deg)}}
</style>
